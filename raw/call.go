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

package raw

import (
	"errors"
	"io/ioutil"

	"golang.org/x/net/context"

	"github.com/uber/tchannel-go"
)

// ErrAppError is returned if the application sets an error response.
var ErrAppError = errors.New("application error")

// ReadJustArg2 reads all of arg2 into a byte buffer, and returns the arg3
// reader (as a convenience so that caller can decide how to read it).
func ReadJustArg2(r tchannel.ArgReadable) ([]byte, tchannel.ArgReader, error) {
	arg2Reader, err := r.Arg2Reader()
	if err != nil {
		return nil, nil, err
	}

	arg2, err := ioutil.ReadAll(arg2Reader)
	if err != nil {
		return arg2, nil, err
	}

	if err := arg2Reader.Close(); err != nil {
		return arg2, nil, err
	}

	arg3Reader, err := r.Arg3Reader()
	if err != nil {
		return arg2, nil, err
	}

	return arg2, arg3Reader, nil
}

// ReadArgsV2 reads arg2 and arg3 from an ArgReadable.
func ReadArgsV2(r tchannel.ArgReadable) ([]byte, []byte, error) {
	arg2, arg3Reader, err := ReadJustArg2(r)
	if err != nil {
		return arg2, nil, err
	}

	arg3, err := ioutil.ReadAll(arg3Reader)
	if err != nil {
		return arg2, arg3, err
	}

	if err := arg3Reader.Close(); err != nil {
		return arg2, arg3, err
	}

	return arg2, arg3, nil
}

// WriteArgs writes the given arguments to the call, and returns the response args.
func WriteArgs(call *tchannel.OutboundCall, arg2, arg3 []byte) ([]byte, []byte, *tchannel.OutboundCallResponse, error) {
	if err := tchannel.NewArgWriter(call.Arg2Writer()).Write(arg2); err != nil {
		return nil, nil, nil, err
	}

	if err := tchannel.NewArgWriter(call.Arg3Writer()).Write(arg3); err != nil {
		return nil, nil, nil, err
	}

	resp := call.Response()
	respArg2, respArg3, err := ReadArgsV2(resp)
	return respArg2, respArg3, resp, err
}

// Call makes a call to the given hostPort with the given arguments and returns the response args.
func Call(ctx context.Context, ch *tchannel.Channel, hostPort string, serviceName, method string,
	arg2, arg3 []byte) ([]byte, []byte, *tchannel.OutboundCallResponse, error) {

	call, err := ch.BeginCall(ctx, hostPort, serviceName, method, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	return WriteArgs(call, arg2, arg3)
}

// CallSC makes a call using the given subcahnnel
func CallSC(ctx context.Context, sc *tchannel.SubChannel, method string, arg2, arg3 []byte) (
	[]byte, []byte, *tchannel.OutboundCallResponse, error) {

	call, err := sc.BeginCall(ctx, method, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	return WriteArgs(call, arg2, arg3)
}

// CArgs are the call arguments passed to CallV2.
type CArgs struct {
	Method      string
	Arg2        []byte
	Arg3        []byte
	CallOptions *tchannel.CallOptions
}

// CRes is the result of making a call.
type CRes struct {
	Arg2     []byte
	Arg3     []byte
	AppError bool
}

// CallV2 makes a call and does not attempt any retries.
func CallV2(ctx context.Context, sc *tchannel.SubChannel, cArgs CArgs) (*CRes, error) {
	call, err := sc.BeginCall(ctx, cArgs.Method, cArgs.CallOptions)
	if err != nil {
		return nil, err
	}

	arg2, arg3, res, err := WriteArgs(call, cArgs.Arg2, cArgs.Arg3)
	if err != nil {
		return nil, err
	}

	return &CRes{
		Arg2:     arg2,
		Arg3:     arg3,
		AppError: res.ApplicationError(),
	}, nil
}
