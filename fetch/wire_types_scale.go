// Code generated by github.com/spacemeshos/go-scale/scalegen. DO NOT EDIT.

// nolint
package fetch

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
)

func (t *RequestMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeString(enc, string(t.Hint))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.Hash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *RequestMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeString(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Hint = datastore.Hint(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.Hash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ResponseMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.Hash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSlice(enc, t.Data)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ResponseMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.Hash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Data = field
	}
	return total, nil
}

func (t *RequestBatch) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSlice(enc, t.Requests)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *RequestBatch) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeStructSlice[RequestMessage](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Requests = field
	}
	return total, nil
}

func (t *ResponseBatch) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteArray(enc, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSlice(enc, t.Responses)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *ResponseBatch) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := scale.DecodeByteArray(dec, t.ID[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeStructSlice[ResponseMessage](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Responses = field
	}
	return total, nil
}

func (t *MeshHashRequest) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := t.From.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.To.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Delta))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeCompact32(enc, uint32(t.Steps))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *MeshHashRequest) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		n, err := t.From.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.To.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Delta = uint32(field)
	}
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Steps = uint32(field)
	}
	return total, nil
}

func (t *MeshHashes) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSlice(enc, t.Layers)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSlice(enc, t.Hashes)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *MeshHashes) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSlice[types.LayerID](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Layers = field
	}
	{
		field, n, err := scale.DecodeStructSlice[types.Hash32](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Hashes = field
	}
	return total, nil
}

func (t *LayerData) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeStructSlice(enc, t.Ballots)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSlice(enc, t.Blocks)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *LayerData) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeStructSlice[types.BallotID](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Ballots = field
	}
	{
		field, n, err := scale.DecodeStructSlice[types.BlockID](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Blocks = field
	}
	return total, nil
}

func (t *LayerOpinion) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact64(enc, uint64(t.EpochWeight))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteArray(enc, t.PrevAggHash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.Verified.EncodeScale(enc)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSlice(enc, t.Valid)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeStructSlice(enc, t.Invalid)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeOption(enc, t.Cert)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (t *LayerOpinion) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		t.EpochWeight = uint64(field)
	}
	{
		n, err := scale.DecodeByteArray(dec, t.PrevAggHash[:])
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := t.Verified.DecodeScale(dec)
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		field, n, err := scale.DecodeStructSlice[types.BlockID](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Valid = field
	}
	{
		field, n, err := scale.DecodeStructSlice[types.BlockID](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Invalid = field
	}
	{
		field, n, err := scale.DecodeOption[types.Certificate](dec)
		if err != nil {
			return total, err
		}
		total += n
		t.Cert = field
	}
	return total, nil
}
