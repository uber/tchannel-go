package mocks

import "github.com/uber/tchannel-go/thrift"
import "github.com/stretchr/testify/mock"

import athrift "github.com/apache/thrift/lib/go/thrift"

type Interceptor struct {
	mock.Mock
}

// Pre provides a mock function with given fields: ctx, method, args
func (_m *Interceptor) Pre(ctx thrift.Context, method string, args athrift.TStruct) error {
	ret := _m.Called(ctx, method, args)

	var r0 error
	if rf, ok := ret.Get(0).(func(thrift.Context, string, athrift.TStruct) error); ok {
		r0 = rf(ctx, method, args)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Post provides a mock function with given fields: ctx, method, args, response, err
func (_m *Interceptor) Post(ctx thrift.Context, method string, args athrift.TStruct, response athrift.TStruct, err error) error {
	ret := _m.Called(ctx, method, args, response, err)

	var r0 error
	if rf, ok := ret.Get(0).(func(thrift.Context, string, athrift.TStruct, athrift.TStruct, error) error); ok {
		r0 = rf(ctx, method, args, response, err)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
