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
package tchannel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go/typed"
)

func TestCallReqService(t *testing.T) {
	// TODO: This test doesn't work, since the initial flags byte is written in
	// reqResWriter instead of callReq. We should instead handle that in
	// callReq, which will allow tests to be sane.
	if 1 == 2 { // go vet doesn't like unreachable code...
		frame := NewFrame(MaxFramePayloadSize)
		err := frame.write(&callReq{Service: "udr"})
		require.NoError(t, err, "Error writing message to frame.")

		callReq := newLazyCallReq(frame)
		assert.Equal(t, "udr", callReq.Service(), "Failed to read service name from frame.")
	}
}

func TestCallReqServiceTerrible(t *testing.T) {
	// TODO: Delete in favor of TestCallReqService.
	f := NewFrame(100)
	fh := fakeHeader()
	f.Header = fh
	fh.write(typed.NewWriteBuffer(f.headerBuffer))

	payload := typed.NewWriteBuffer(f.Payload)
	payload.WriteSingleByte(0)           // flags
	payload.WriteUint32(42)              // TTL
	payload.WriteBytes(make([]byte, 25)) // tracing
	payload.WriteLen8String("bankmoji")  // service

	callReq := newLazyCallReq(f)
	assert.Equal(t, "bankmoji", callReq.Service(), "Failed to read service name from frame.")
}

func TestServiceOtherMessages(t *testing.T) {
	msg := &initReq{initMessage{id: 1, Version: 0x1, initParams: initParams{
		InitParamHostPort:    "0.0.0.0:0",
		InitParamProcessName: "test",
	}}}
	frame := NewFrame(MaxFramePayloadSize)
	err := frame.write(msg)
	require.NoError(t, err, "Error writing message to frame.")
	assert.Panics(t, func() {
		newLazyCallReq(frame)
	}, "Should panic when creating callReq from non-callReq frame.")
}
