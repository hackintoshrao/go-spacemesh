package sql

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/sqlite"
	"github.com/spacemeshos/sqlite/sqlitex"

	"github.com/spacemeshos/go-spacemesh/database"
)

var (
	// ErrNoConnection is returned if pooled connection is not available.
	ErrNoConnection = errors.New("database: no free connection")
	// ErrNotFound is returned if requested record is not found.
	// TODO(dshulyak) is an alias to datatabase.ErrNotFound until full transition is implemented.
	ErrNotFound = database.ErrNotFound
	// ErrObjectExists is returned if database constraints didn't allow to insert an object.
	ErrObjectExists = errors.New("database: object exists")
)

// Executor is an interface for executing raw statement.
type Executor interface {
	Exec(string, Encoder, Decoder) (int, error)
}

// Statement is an sqlite statement.
type Statement = sqlite.Stmt

// Encoder for parameters.
// Both positional parameters:
// select block from blocks where id = ?1;
//
// and named parameters are supported:
// select blocks from blocks where id = @id;
//
// For complete information see https://www.sqlite.org/c3ref/bind_blob.html.
type Encoder func(*Statement)

// Decoder for sqlite rows.
type Decoder func(*Statement) bool

func defaultConf() *conf {
	return &conf{
		connections: 16,
		migrations:  embeddedMigrations,
	}
}

type conf struct {
	flags       sqlite.OpenFlags
	connections int
	migrations  Migrations
}

// WithConnections overwrites number of pooled connections.
func WithConnections(n int) Opt {
	return func(c *conf) {
		c.connections = n
	}
}

// WithMigrations overwrites embedded migrations.
func WithMigrations(migrations Migrations) Opt {
	return func(c *conf) {
		c.migrations = migrations
	}
}

// Opt for configuring database.
type Opt func(c *conf)

// InMemory database for testing.
func InMemory(opts ...Opt) *Database {
	opts = append(opts, WithConnections(1))
	db, err := Open("file::memory:?mode=memory", opts...)
	if err != nil {
		panic(err)
	}
	return db
}

// Open database with options.
//
// Database is opened in WAL mode and pragma synchronous=normal.
// https://sqlite.org/wal.html
// https://www.sqlite.org/pragma.html#pragma_synchronous
func Open(uri string, opts ...Opt) (*Database, error) {
	config := defaultConf()
	for _, opt := range opts {
		opt(config)
	}
	pool, err := sqlitex.Open(uri, config.flags, config.connections)
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", uri, err)
	}
	db := &Database{pool: pool}
	if config.migrations != nil {
		tx, err := db.Tx(context.Background())
		if err != nil {
			return nil, err
		}
		err = config.migrations(tx)
		if err == nil {
			tx.Commit()
		}
		tx.Release()
		if err != nil {
			return nil, err
		}
	}
	for i := 0; i < config.connections; i++ {
		conn := pool.Get(context.Background())
		if err := registerFunctions(conn); err != nil {
			return nil, err
		}
		defer pool.Put(conn)
	}
	return db, nil
}

// Database is an instance of sqlite database.
type Database struct {
	pool *sqlitex.Pool
}

// Exec statement using one of the connection from the pool.
//
// If you care about atomicity of the operation (for example writing rewards to multiple accounts)
// Tx should be used. Otherwise sqlite will not guarantee that all side-effects of operations are
// applied to the database if machine crashes.
//
// Note that Exec will block until database is closed or statement has finished.
// If application needs to control statement execution lifetime use one of the transaction.
func (db *Database) Exec(query string, encoder Encoder, decoder Decoder) (int, error) {
	conn := db.pool.Get(context.Background())
	if conn == nil {
		return 0, ErrNoConnection
	}
	defer db.pool.Put(conn)
	return exec(conn, query, encoder, decoder)
}

// Close closes all pooled connections.
func (db *Database) Close() error {
	if err := db.pool.Close(); err != nil {
		return fmt.Errorf("close pool %w", err)
	}
	return nil
}

func exec(conn *sqlite.Conn, query string, encoder Encoder, decoder Decoder) (int, error) {
	stmt, err := conn.Prepare(query)
	if err != nil {
		return 0, fmt.Errorf("prepare %s: %w", query, err)
	}
	if encoder != nil {
		encoder(stmt)
	}
	defer stmt.ClearBindings()

	rows := 0
	for {
		row, err := stmt.Step()
		if err != nil {
			code := sqlite.ErrCode(err)
			if code == sqlite.SQLITE_CONSTRAINT_PRIMARYKEY {
				return 0, ErrObjectExists
			}
			return 0, fmt.Errorf("step %d: %w", rows, err)
		}
		if !row {
			return rows, nil
		}
		rows++
		// exhaust iterator
		if decoder == nil {
			continue
		}
		if !decoder(stmt) {
			if err := stmt.Reset(); err != nil {
				return rows, fmt.Errorf("statement reset %w", err)
			}
			return rows, nil
		}
	}
}

// GetSQLiteError returns sqlite error for the given error.
func (db *Database) GetSQLiteError(err error) *sqlite.Error {
	if err == nil {
		return nil
	}
	tmpErr := err
	if sqliteErr, ok := tmpErr.(sqlite.Error); ok {
		return &sqliteErr
	}
	for {
		tmpErr = errors.Unwrap(tmpErr)
		if tmpErr == nil {
			return nil
		}
		if sqliteErr, ok := tmpErr.(sqlite.Error); ok {
			return &sqliteErr
		}
	}
}
