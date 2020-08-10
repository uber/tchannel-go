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
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/uber/tchannel-go/testutils/thriftarg2test"
	"github.com/uber/tchannel-go/typed"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCallReq int

const (
	reqHasHeaders testCallReq = (1 << iota)
	reqHasCaller
	reqHasDelegate
	reqHasRoutingKey
	reqHasChecksum
	reqTotalCombinations
	reqHasAll testCallReq = reqTotalCombinations - 1
)

type testCallReqParams struct {
	flags           byte
	hasTChanThrift  bool
	argScheme       Format
	arg2Buf         []byte
	overrideArg2Len int
	arg3Buf         []byte
}

func (cr testCallReq) req(tb testing.TB) lazyCallReq {
	return cr.reqWithParams(tb, testCallReqParams{})
}

func (cr testCallReq) reqWithParams(tb testing.TB, p testCallReqParams) lazyCallReq {
	lcr, err := newLazyCallReq(cr.frameWithParams(p))
	require.NoError(tb, err)
	return *lcr
}

func (cr testCallReq) frameWithParams(p testCallReqParams) *Frame {
	// TODO: Constructing a frame is ugly because the initial flags byte is
	// written in reqResWriter instead of callReq. We should instead handle that
	// in callReq, which will allow our tests to be sane.
	f := NewFrame(200)
	fh := fakeHeader()
	fh.size = 0xD8 // 200 + 16 bytes of header = 216 (0xD8)
	f.Header = fh
	fh.write(typed.NewWriteBuffer(f.headerBuffer))

	payload := typed.NewWriteBuffer(f.Payload)
	payload.WriteSingleByte(p.flags)     // flags
	payload.WriteUint32(42)              // TTL
	payload.WriteBytes(make([]byte, 25)) // tracing
	payload.WriteLen8String("bankmoji")  // service

	headers := make(map[string]string)
	switch p.argScheme {
	case HTTP, JSON, Raw, Thrift:
		headers["as"] = p.argScheme.String()
	}
	if cr&reqHasHeaders != 0 {
		addRandomHeaders(headers)
	}
	if cr&reqHasCaller != 0 {
		headers["cn"] = "fake-caller"
	}
	if cr&reqHasDelegate != 0 {
		headers["rd"] = "fake-delegate"
	}
	if cr&reqHasRoutingKey != 0 {
		headers["rk"] = "fake-routingkey"
	}
	writeHeaders(payload, headers)

	if cr&reqHasChecksum == 0 {
		payload.WriteSingleByte(byte(ChecksumTypeNone)) // checksum type
		// no checksum contents for None
	} else {
		payload.WriteSingleByte(byte(ChecksumTypeCrc32C)) // checksum type
		payload.WriteUint32(0)                            // checksum contents
	}
	payload.WriteLen16String("moneys") // method

	arg2Len := len(p.arg2Buf)
	if p.overrideArg2Len > 0 {
		arg2Len = p.overrideArg2Len
	}
	payload.WriteUint16(uint16(arg2Len))
	payload.WriteBytes(p.arg2Buf)
	payload.WriteUint16(uint16(len(p.arg3Buf)))
	payload.WriteBytes(p.arg3Buf)

	return f
}

func withLazyCallReqCombinations(f func(cr testCallReq)) {
	for cr := testCallReq(0); cr < reqTotalCombinations; cr++ {
		f(cr)
	}
}

type testCallRes int

const (
	resIsContinued testCallRes = (1 << iota)
	resIsOK
	resHasHeaders
	resHasChecksum
	resTotalCombinations
)

func (cr testCallRes) res() lazyCallRes {
	f := NewFrame(100)
	fh := FrameHeader{
		size:        uint16(0xFF34),
		messageType: messageTypeCallRes,
		ID:          0xDEADBEEF,
	}
	f.Header = fh
	fh.write(typed.NewWriteBuffer(f.headerBuffer))

	payload := typed.NewWriteBuffer(f.Payload)

	if cr&resIsContinued == 0 {
		payload.WriteSingleByte(0) // flags
	} else {
		payload.WriteSingleByte(hasMoreFragmentsFlag) // flags
	}

	if cr&resIsOK == 0 {
		payload.WriteSingleByte(1) // code not ok
	} else {
		payload.WriteSingleByte(0) // code ok
	}

	headers := make(map[string]string)
	if cr&resHasHeaders != 0 {
		addRandomHeaders(headers)
	}
	writeHeaders(payload, headers)

	if cr&resHasChecksum == 0 {
		payload.WriteSingleByte(byte(ChecksumTypeNone)) // checksum type
		// No contents for ChecksumTypeNone.
	} else {
		payload.WriteSingleByte(byte(ChecksumTypeCrc32C)) // checksum type
		payload.WriteUint32(0)                            // checksum contents
	}
	payload.WriteUint16(0) // no arg1 for call res
	return newLazyCallRes(f)
}

func withLazyCallResCombinations(f func(cr testCallRes)) {
	for cr := testCallRes(0); cr < resTotalCombinations; cr++ {
		f(cr)
	}
}

func (ec SystemErrCode) fakeErrFrame() lazyError {
	f := NewFrame(100)
	fh := FrameHeader{
		size:        uint16(0xFF34),
		messageType: messageTypeError,
		ID:          invalidMessageID,
	}
	f.Header = fh
	fh.write(typed.NewWriteBuffer(f.headerBuffer))

	payload := typed.NewWriteBuffer(f.Payload)
	payload.WriteSingleByte(byte(ec))
	payload.WriteBytes(make([]byte, 25)) // tracing

	msg := ec.String()
	payload.WriteUint16(uint16(len(msg)))
	payload.WriteBytes([]byte(msg))
	return newLazyError(f)
}

func withLazyErrorCombinations(f func(ec SystemErrCode)) {
	codes := []SystemErrCode{
		ErrCodeInvalid,
		ErrCodeTimeout,
		ErrCodeCancelled,
		ErrCodeBusy,
		ErrCodeDeclined,
		ErrCodeUnexpected,
		ErrCodeBadRequest,
		ErrCodeNetwork,
		ErrCodeProtocol,
	}
	for _, ec := range codes {
		f(ec)
	}
}

func addRandomHeaders(headers map[string]string) {
	headers["k1"] = "v1"
	headers["k222222"] = ""
	headers["k3"] = "thisisalonglongkey"
}

func writeHeaders(w *typed.WriteBuffer, headers map[string]string) {
	w.WriteSingleByte(byte(len(headers))) // number of headers
	for k, v := range headers {
		w.WriteLen8String(k)
		w.WriteLen8String(v)
	}
}

func assertWrappingPanics(t testing.TB, f *Frame, wrap func(f *Frame)) {
	assert.Panics(t, func() {
		wrap(f)
	}, "Should panic when wrapping an unexpected frame type.")
}

func TestLazyCallReqRejectsOtherFrames(t *testing.T) {
	assertWrappingPanics(
		t,
		resIsContinued.res().Frame,
		func(f *Frame) { newLazyCallReq(f) },
	)
}

func TestLazyCallReqService(t *testing.T) {
	withLazyCallReqCombinations(func(crt testCallReq) {
		cr := crt.req(t)
		assert.Equal(t, "bankmoji", string(cr.Service()), "Service name mismatch")
	})
}

func TestLazyCallReqCaller(t *testing.T) {
	withLazyCallReqCombinations(func(crt testCallReq) {
		cr := crt.req(t)
		if crt&reqHasCaller == 0 {
			assert.Equal(t, []byte(nil), cr.Caller(), "Unexpected caller name.")
		} else {
			assert.Equal(t, "fake-caller", string(cr.Caller()), "Caller name mismatch")
		}
	})
}

func TestLazyCallReqRoutingDelegate(t *testing.T) {
	withLazyCallReqCombinations(func(crt testCallReq) {
		cr := crt.req(t)
		if crt&reqHasDelegate == 0 {
			assert.Equal(t, []byte(nil), cr.RoutingDelegate(), "Unexpected routing delegate.")
		} else {
			assert.Equal(t, "fake-delegate", string(cr.RoutingDelegate()), "Routing delegate mismatch.")
		}
	})
}

func TestLazyCallReqRoutingKey(t *testing.T) {
	withLazyCallReqCombinations(func(crt testCallReq) {
		cr := crt.req(t)
		if crt&reqHasRoutingKey == 0 {
			assert.Equal(t, []byte(nil), cr.RoutingKey(), "Unexpected routing key.")
		} else {
			assert.Equal(t, "fake-routingkey", string(cr.RoutingKey()), "Routing key mismatch.")
		}
	})
}

func TestLazyCallReqMethod(t *testing.T) {
	withLazyCallReqCombinations(func(crt testCallReq) {
		cr := crt.req(t)
		assert.Equal(t, "moneys", string(cr.Method()), "Method name mismatch")
	})
}

func TestLazyCallReqTTL(t *testing.T) {
	withLazyCallReqCombinations(func(crt testCallReq) {
		cr := crt.req(t)
		assert.Equal(t, 42*time.Millisecond, cr.TTL(), "Failed to parse TTL from frame.")
	})
}

func TestLazyCallReqSetTTL(t *testing.T) {
	withLazyCallReqCombinations(func(crt testCallReq) {
		cr := crt.req(t)
		cr.SetTTL(time.Second)
		assert.Equal(t, time.Second, cr.TTL(), "Failed to write TTL to frame.")
	})
}

func byteKeyValToMap(tb testing.TB, buffer []byte) map[string]string {
	rbuf := typed.NewReadBuffer(buffer)
	nh := int(rbuf.ReadSingleByte())
	retMap := make(map[string]string, nh)
	for i := 0; i < nh; i++ {
		keyLen := int(rbuf.ReadSingleByte())
		key := rbuf.ReadBytes(keyLen)
		valLen := int(rbuf.ReadSingleByte())
		val := rbuf.ReadBytes(valLen)
		retMap[string(key)] = string(val)
	}
	require.NoError(tb, rbuf.Err())
	return retMap
}

func uint16KeyValToMap(tb testing.TB, buffer []byte) map[string]string {
	rbuf := typed.NewReadBuffer(buffer)
	nh := int(rbuf.ReadUint16())
	retMap := make(map[string]string, nh)
	for i := 0; i < nh; i++ {
		keyLen := int(rbuf.ReadUint16())
		key := rbuf.ReadBytes(keyLen)
		valLen := int(rbuf.ReadUint16())
		val := rbuf.ReadBytes(valLen)
		retMap[string(key)] = string(val)
	}
	require.NoError(tb, rbuf.Err())
	return retMap
}

func TestLazyCallReqContents(t *testing.T) {
	cr := reqHasAll.reqWithParams(t, testCallReqParams{
		arg2Buf: thriftarg2test.BuildKVBuffer(map[string]string{
			"foo": "bar",
			"baz": "qux",
		}),
		arg3Buf: []byte("some arg3 data"),
	})

	t.Run(".header()", func(t *testing.T) {
		assert.Equal(t, map[string]string{
			"cn":      "fake-caller",
			"k1":      "v1",
			"k222222": "",
			"k3":      "thisisalonglongkey",
			"rd":      "fake-delegate",
			"rk":      "fake-routingkey",
		}, byteKeyValToMap(t, cr.header()))
	})

	t.Run(".arg2()", func(t *testing.T) {
		assert.Equal(t, map[string]string{
			"baz": "qux",
			"foo": "bar",
		}, uint16KeyValToMap(t, cr.arg2()))
	})

	t.Run(".arg3()", func(t *testing.T) {
		assert.Equal(t, "some arg3 data", string(cr.arg3()))
	})
}

func TestLazyCallArg2Offset(t *testing.T) {
	wantArg2Buf := []byte("test arg2 buf")
	tests := []struct {
		msg     string
		flags   byte
		arg2Buf []byte
	}{
		{
			msg:     "arg2 is fully contained in frame",
			arg2Buf: wantArg2Buf,
		},
		{
			msg: "has no arg2",
		},
		{
			msg:     "frame fragmented but arg2 is fully contained",
			flags:   hasMoreFragmentsFlag,
			arg2Buf: wantArg2Buf,
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			withLazyCallReqCombinations(func(crt testCallReq) {
				cr := crt.reqWithParams(t, testCallReqParams{
					flags:   tt.flags,
					arg2Buf: tt.arg2Buf,
				})
				arg2EndOffset, hasMore := cr.Arg2EndOffset()
				assert.False(t, hasMore)
				if len(tt.arg2Buf) == 0 {
					assert.Zero(t, arg2EndOffset-cr.Arg2StartOffset())
					return
				}

				arg2Payload := cr.Payload[cr.Arg2StartOffset():arg2EndOffset]
				assert.Equal(t, tt.arg2Buf, arg2Payload)
			})
		})
	}

	t.Run("no arg3 set", func(t *testing.T) {
		tests := []struct {
			msg       string
			hasMore   bool
			wantError string
		}{
			{
				msg:     "hasMore flag=true",
				hasMore: true,
			},
			{
				msg:       "hasMore flag=false",
				wantError: "buffer is too small",
			},
		}

		for _, tt := range tests {
			t.Run(fmt.Sprintf(tt.msg), func(t *testing.T) {
				withLazyCallReqCombinations(func(crt testCallReq) {
					// For each CallReq, we first get the remaining space left, and
					// fill up the remaining space with arg2.
					crNoArg2 := crt.req(t)
					arg2Size := int(crNoArg2.Header.PayloadSize()) - crNoArg2.Arg2StartOffset()
					var flags byte
					if tt.hasMore {
						flags |= hasMoreFragmentsFlag
					}
					f := crt.frameWithParams(testCallReqParams{
						flags:   flags,
						arg2Buf: make([]byte, arg2Size),
					})
					cr, err := newLazyCallReq(f)
					if tt.wantError != "" {
						require.EqualError(t, err, tt.wantError)
						return
					}
					require.NoError(t, err)
					endOffset, hasMore := cr.Arg2EndOffset()
					assert.Equal(t, hasMore, tt.hasMore)
					assert.EqualValues(t, crNoArg2.Header.PayloadSize(), endOffset)
				})
			})
		}
	})
}

func TestLazyCallReqSetTChanThriftArg2(t *testing.T) {
	tests := []struct {
		msg            string
		bufKV          map[string]string
		wantKV         map[string]string // if not set, use bufKV
		argScheme      Format
		overrideBufLen int
		wantBadErr     string
	}{
		{
			msg:       "two key value pairs",
			argScheme: Thrift,
			bufKV: map[string]string{
				"key":  "val",
				"key2": "val2",
			},
		},
		{
			msg:       "length not enough to cover key len",
			argScheme: Thrift,
			bufKV: map[string]string{
				"key": "val",
			},
			overrideBufLen: 3, // 2 (nh) + 2 - 1
			wantBadErr:     "buffer is too small",
		},
		{
			msg:       "length not enough to cover key",
			argScheme: Thrift,
			bufKV: map[string]string{
				"key": "val",
			},
			overrideBufLen: 6, // 2 (nh) + 2 + len(key) - 1
			wantBadErr:     "buffer is too small",
		},
		{
			msg:       "length not enough to cover value len",
			argScheme: Thrift,
			bufKV: map[string]string{
				"key": "val",
			},
			overrideBufLen: 8, // 2 (nh) + 2 + len(key) + 2 - 1
			wantBadErr:     "buffer is too small",
		},
		{
			msg:       "length not enough to cover value",
			argScheme: Thrift,
			bufKV: map[string]string{
				"key": "val",
			},
			overrideBufLen: 10, // 2 (nh) + 2 + len(key) + 2 + len(val) - 2
			wantBadErr:     "buffer is too small",
		},
		{
			msg:       "no key value pairs",
			argScheme: Thrift,
			bufKV:     map[string]string{},
		},
		{
			msg:        "not tchannel thrift",
			argScheme:  HTTP,
			bufKV:      map[string]string{"key": "val"},
			wantBadErr: "non thrift scheme",
		},
		{
			msg:        "not arg scheme",
			bufKV:      map[string]string{"key": "val"},
			wantBadErr: "non thrift scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			withLazyCallReqCombinations(func(crt testCallReq) {
				arg2Buf := thriftarg2test.BuildKVBuffer(tt.bufKV)
				if tt.overrideBufLen > 0 {
					arg2Buf = arg2Buf[:tt.overrideBufLen]
				}
				cr := crt.reqWithParams(t, testCallReqParams{
					arg2Buf:   arg2Buf,
					argScheme: tt.argScheme,
				})
				gotIter := make(map[string]string)
				iter, err := cr.Arg2Iterator()
				for err == nil {
					gotIter[string(iter.Key())] = string(iter.Value())
					iter, err = iter.Next()
				}
				if tt.wantBadErr != "" {
					require.NotEqual(t, io.EOF, err, "should not return EOF for iterator exit")
					assert.Contains(t, err.Error(), tt.wantBadErr)
				} else {
					assert.Equal(t, io.EOF, err, "should return EOF for iterator exit")
					wantKV := tt.wantKV
					if wantKV == nil {
						wantKV = tt.bufKV
					}
					assert.Equal(t, wantKV, gotIter, "unexpected arg2 keys, call req %+v", crt)
				}
			})
		})
	}

	t.Run("bad Arg2 length", func(t *testing.T) {
		withLazyCallReqCombinations(func(crt testCallReq) {
			crNoArg2 := crt.req(t)
			leftSpace := int(crNoArg2.Header.PayloadSize()) - crNoArg2.Arg2StartOffset()
			frm := crt.frameWithParams(testCallReqParams{
				arg2Buf:         make([]byte, leftSpace),
				argScheme:       Thrift,
				overrideArg2Len: leftSpace + 5, // Arg2 length extends beyond payload
			})
			_, err := newLazyCallReq(frm)
			assert.EqualError(t, err, "buffer is too small")
		})
	})
}

func TestLazyCallResRejectsOtherFrames(t *testing.T) {
	assertWrappingPanics(
		t,
		reqHasHeaders.req(t).Frame,
		func(f *Frame) { newLazyCallRes(f) },
	)
}

func TestLazyCallResOK(t *testing.T) {
	withLazyCallResCombinations(func(crt testCallRes) {
		cr := crt.res()
		if crt&resIsOK == 0 {
			assert.False(t, cr.OK(), "Expected call res to have a non-ok code.")
		} else {
			assert.True(t, cr.OK(), "Expected call res to have code ok.")
		}
	})
}

func TestLazyErrorRejectsOtherFrames(t *testing.T) {
	assertWrappingPanics(
		t,
		reqHasHeaders.req(t).Frame,
		func(f *Frame) { newLazyError(f) },
	)
}

func TestLazyErrorCodes(t *testing.T) {
	withLazyErrorCombinations(func(ec SystemErrCode) {
		f := ec.fakeErrFrame()
		assert.Equal(t, ec, f.Code(), "Mismatch between error code and lazy frame's Code() method.")
	})
}
