package mocks

import "github.com/uber/tchannel-go/hyperbahn/gen-go/hyperbahn"
import "github.com/stretchr/testify/mock"

import "github.com/uber/tchannel-go/thrift"

type TChanHyperbahn struct {
	mock.Mock
}

func (_m *TChanHyperbahn) Discover(ctx thrift.Context, query *hyperbahn.DiscoveryQuery) (*hyperbahn.DiscoveryResult_, error) {
	ret := _m.Called(ctx, query)

	var r0 *hyperbahn.DiscoveryResult_
	if rf, ok := ret.Get(0).(func(thrift.Context, *hyperbahn.DiscoveryQuery) *hyperbahn.DiscoveryResult_); ok {
		r0 = rf(ctx, query)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*hyperbahn.DiscoveryResult_)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(thrift.Context, *hyperbahn.DiscoveryQuery) error); ok {
		r1 = rf(ctx, query)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
