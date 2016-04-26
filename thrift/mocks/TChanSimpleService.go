// Copyright (c) 2015 Uber Technologies, Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package mocks

import "github.com/uber/tchannel-go/thrift/gen-go/test"
import "github.com/stretchr/testify/mock"

import "github.com/uber/tchannel-go/thrift"

type TChanSimpleService struct {
	mock.Mock
}

// Call provides a mock function with given fields: ctx, arg
func (_m *TChanSimpleService) Call(ctx thrift.Context, arg *test.Data) (*test.Data, error) {
	ret := _m.Called(ctx, arg)

	var r0 *test.Data
	if rf, ok := ret.Get(0).(func(thrift.Context, *test.Data) *test.Data); ok {
		r0 = rf(ctx, arg)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*test.Data)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(thrift.Context, *test.Data) error); ok {
		r1 = rf(ctx, arg)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Simple provides a mock function with given fields: ctx
func (_m *TChanSimpleService) Simple(ctx thrift.Context) error {
	ret := _m.Called(ctx)

	var r0 error
	if rf, ok := ret.Get(0).(func(thrift.Context) error); ok {
		r0 = rf(ctx)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Throws provides a mock function with given fields: ctx, arg
func (_m *TChanSimpleService) Throws(ctx thrift.Context, arg string) (string, error) {
	ret := _m.Called(ctx, arg)

	var r0 string
	if rf, ok := ret.Get(0).(func(thrift.Context, string) string); ok {
		r0 = rf(ctx, arg)
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(thrift.Context, string) error); ok {
		r1 = rf(ctx, arg)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
