// Code generated by MockGen. DO NOT EDIT.
// Source: ./tortoise.go

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	types "github.com/spacemeshos/go-spacemesh/common/types"
)

// MockTortoise is a mock of Tortoise interface.
type MockTortoise struct {
	ctrl     *gomock.Controller
	recorder *MockTortoiseMockRecorder
}

// MockTortoiseMockRecorder is the mock recorder for MockTortoise.
type MockTortoiseMockRecorder struct {
	mock *MockTortoise
}

// NewMockTortoise creates a new mock instance.
func NewMockTortoise(ctrl *gomock.Controller) *MockTortoise {
	mock := &MockTortoise{ctrl: ctrl}
	mock.recorder = &MockTortoiseMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTortoise) EXPECT() *MockTortoiseMockRecorder {
	return m.recorder
}

// HandleIncomingLayer mocks base method.
func (m *MockTortoise) HandleIncomingLayer(arg0 context.Context, arg1 types.LayerID) types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HandleIncomingLayer", arg0, arg1)
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// HandleIncomingLayer indicates an expected call of HandleIncomingLayer.
func (mr *MockTortoiseMockRecorder) HandleIncomingLayer(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandleIncomingLayer", reflect.TypeOf((*MockTortoise)(nil).HandleIncomingLayer), arg0, arg1)
}

// LatestComplete mocks base method.
func (m *MockTortoise) LatestComplete() types.LayerID {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LatestComplete")
	ret0, _ := ret[0].(types.LayerID)
	return ret0
}

// LatestComplete indicates an expected call of LatestComplete.
func (mr *MockTortoiseMockRecorder) LatestComplete() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LatestComplete", reflect.TypeOf((*MockTortoise)(nil).LatestComplete))
}

// OnBallot mocks base method.
func (m *MockTortoise) OnBallot(arg0 *types.Ballot) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnBallot", arg0)
}

// OnBallot indicates an expected call of OnBallot.
func (mr *MockTortoiseMockRecorder) OnBallot(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnBallot", reflect.TypeOf((*MockTortoise)(nil).OnBallot), arg0)
}

// OnBlock mocks base method.
func (m *MockTortoise) OnBlock(arg0 *types.Block) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnBlock", arg0)
}

// OnBlock indicates an expected call of OnBlock.
func (mr *MockTortoiseMockRecorder) OnBlock(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnBlock", reflect.TypeOf((*MockTortoise)(nil).OnBlock), arg0)
}

// OnHareOutput mocks base method.
func (m *MockTortoise) OnHareOutput(arg0 types.LayerID, arg1 types.BlockID) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "OnHareOutput", arg0, arg1)
}

// OnHareOutput indicates an expected call of OnHareOutput.
func (mr *MockTortoiseMockRecorder) OnHareOutput(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnHareOutput", reflect.TypeOf((*MockTortoise)(nil).OnHareOutput), arg0, arg1)
}
