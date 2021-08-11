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
	"math"
	"strconv"
	"strings"
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

var (
	exampleArg2Map = map[string]string{
		"foo": "bar",
		"baz": "qux",
	}
)

const (
	exampleService  = "fooservice"
	exampleArg3Data = "some arg3 data"
)

type testCallReqParams struct {
	flags           byte
	hasTChanThrift  bool
	argScheme       Format
	arg2Buf         []byte
	overrideArg2Len int
	skipArg3        bool
	arg3Buf         []byte

	serviceOverride string
}

func (cr testCallReq) req(tb testing.TB) lazyCallReq {
	return cr.reqWithParams(tb, testCallReqParams{})
}

func (cr testCallReq) reqWithParams(tb testing.TB, p testCallReqParams) lazyCallReq {
	lcr, err := newLazyCallReq(cr.frameWithParams(tb, p))
	require.NoError(tb, err)
	return *lcr
}

func (cr testCallReq) frameWithParams(t testing.TB, p testCallReqParams) *Frame {
	// TODO: Constructing a frame is ugly because the initial flags byte is
	// written in reqResWriter instead of callReq. We should instead handle that
	// in callReq, which will allow our tests to be sane.
	f := NewFrame(MaxFramePayloadSize)
	fh := fakeHeader(messageTypeCallReq)

	// Set the size in the header and write out the header after we know the payload contents.
	defer func() {
		fh.size = FrameHeaderSize + uint16(len(f.Payload))
		f.Header = fh
		require.NoError(t, fh.write(typed.NewWriteBuffer(f.headerBuffer)), "failed to write header")
	}()

	payload := typed.NewWriteBuffer(f.Payload)
	payload.WriteSingleByte(p.flags)     // flags
	payload.WriteUint32(42)              // TTL
	payload.WriteBytes(make([]byte, 25)) // tracing

	svc := p.serviceOverride
	if svc == "" {
		svc = exampleService
	}
	payload.WriteLen8String(svc) // service

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

	if !p.skipArg3 {
		arg3Len := len(p.arg3Buf)
		payload.WriteUint16(uint16(arg3Len))
		payload.WriteBytes(p.arg3Buf)
	}
	f.Payload = f.Payload[:payload.BytesWritten()]

	require.NoError(t, payload.Err(), "failed to write payload")
	return f
}

func withLazyCallReqCombinations(f func(cr testCallReq)) {
	for cr := testCallReq(0); cr < reqTotalCombinations; cr++ {
		f(cr)
	}
}

type testCallRes int

type testCallResParams struct {
	hasFragmentedArg2 bool

	flags       byte
	code        byte
	span        [25]byte
	headers     map[string]string
	csumType    byte
	arg1        []byte
	arg2Prefix  []byte // used for corrupting arg2
	arg2KeyVals map[string]string
	arg3        []byte
}

const (
	resIsContinued testCallRes = (1 << iota)
	resIsOK
	resHasHeaders
	resHasChecksum
	resHasArg2
	resHasFragmentedArg2
	resTotalCombinations
)

func (cr testCallRes) res(tb testing.TB) lazyCallRes {
	var params testCallResParams

	if cr&resHasFragmentedArg2 != 0 {
		params.hasFragmentedArg2 = true
	}
	if cr&(resIsContinued|resHasFragmentedArg2) != 0 {
		params.flags |= hasMoreFragmentsFlag
	}
	if cr&resIsOK == 0 {
		params.code = 1
	}
	params.headers = map[string]string{}
	if cr&resHasHeaders != 0 {
		params.headers["k1"] = "v1"
		params.headers["k222222"] = ""
		params.headers["k3"] = "thisisalonglongkey"
	}
	if cr&(resHasArg2|resHasFragmentedArg2) != 0 {
		params.headers[string(_argSchemeKeyBytes)] = string(_tchanThriftValueBytes)
	}
	if cr&resHasChecksum != 0 {
		params.csumType = byte(ChecksumTypeCrc32C)
	}
	if cr&resHasArg2 != 0 {
		params.arg2KeyVals = exampleArg2Map
	}

	lcr, err := newLazyCallRes(newCallResFrame(tb, params))
	require.NoError(tb, err, "Unexpected error creating lazyCallRes")

	return lcr
}

func withLazyCallResCombinations(t *testing.T, f func(t *testing.T, cr testCallRes)) {
	for cr := testCallRes(0); cr < resTotalCombinations; cr++ {
		t.Run(fmt.Sprintf("cr=%v", strconv.FormatInt(int64(cr), 2)), func(t *testing.T) {
			f(t, cr)
		})
	}
}

func newCallResFrame(tb testing.TB, p testCallResParams) *Frame {
	f := NewFrame(MaxFramePayloadSize)
	fh := fakeHeader(messageTypeCallRes)
	payload := typed.NewWriteBuffer(f.Payload)

	defer func() {
		fh.SetPayloadSize(uint16(payload.BytesWritten()))
		f.Header = fh
		require.NoError(tb, fh.write(typed.NewWriteBuffer(f.headerBuffer)), "Failed to write header")
	}()

	payload.WriteSingleByte(p.flags)              // flags
	payload.WriteSingleByte(p.code)               // code
	payload.WriteBytes(p.span[:])                 // span
	payload.WriteSingleByte(byte(len(p.headers))) // headers
	for k, v := range p.headers {
		payload.WriteSingleByte(byte(len(k)))
		payload.WriteBytes([]byte(k))
		payload.WriteSingleByte(byte(len(v)))
		payload.WriteBytes([]byte(v))
	}
	payload.WriteSingleByte(p.csumType)                                       // checksum type
	payload.WriteBytes(make([]byte, ChecksumType(p.csumType).ChecksumSize())) // dummy checksum (not used in tests)

	// arg1
	payload.WriteUint16(uint16(len(p.arg1)))
	payload.WriteBytes(p.arg1)
	require.NoError(tb, payload.Err(), "Got unexpected error constructing callRes frame")

	// arg2
	payload.WriteBytes(p.arg2Prefix) // prefix is used only for corrupting arg2
	arg2SizeRef := payload.DeferUint16()
	arg2StartBytes := payload.BytesWritten()
	arg2NHRef := payload.DeferUint16()
	var arg2NH uint16
	for k, v := range p.arg2KeyVals {
		arg2NH++
		payload.WriteLen16String(k)
		payload.WriteLen16String(v)
	}
	if p.hasFragmentedArg2 {
		// fill remainder of frame with the next key/val
		arg2NH++
		payload.WriteLen16String("ube")
		payload.WriteLen16String(strings.Repeat("r", payload.BytesRemaining()-2))
	}
	arg2NHRef.Update(arg2NH)
	require.NoError(tb, payload.Err(), "Got unexpected error constructing callRes frame")
	arg2SizeRef.Update(uint16(payload.BytesWritten() - arg2StartBytes))

	if !p.hasFragmentedArg2 {
		// arg3
		payload.WriteUint16(uint16(len(p.arg3)))
		payload.WriteBytes(p.arg3)
	}

	require.NoError(tb, payload.Err(), "Got unexpected error constructing callRes frame")

	return f
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
		resIsContinued.res(t).Frame,
		func(f *Frame) { newLazyCallReq(f) },
	)
}

func TestLazyCallReqService(t *testing.T) {
	withLazyCallReqCombinations(func(crt testCallReq) {
		cr := crt.req(t)
		assert.Equal(t, exampleService, string(cr.Service()), "Service name mismatch")
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
					arg2Size := MaxFramePayloadSize - crNoArg2.Arg2StartOffset()
					var flags byte
					if tt.hasMore {
						flags |= hasMoreFragmentsFlag
					}
					f := crt.frameWithParams(t, testCallReqParams{
						flags:    flags,
						arg2Buf:  make([]byte, arg2Size),
						skipArg3: true,
					})
					cr, err := newLazyCallReq(f)
					if tt.wantError != "" {
						require.EqualError(t, err, tt.wantError)
						return
					}
					require.NoError(t, err)
					endOffset, hasMore := cr.Arg2EndOffset()
					assert.Equal(t, hasMore, tt.hasMore)
					assert.EqualValues(t, MaxFramePayloadSize, endOffset)
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
			wantBadErr: "non-Thrift",
		},
		{
			msg:        "not arg scheme",
			bufKV:      map[string]string{"key": "val"},
			wantBadErr: "non-Thrift",
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
			frm := crt.frameWithParams(t, testCallReqParams{
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

func TestLazyCallRes(t *testing.T) {
	withLazyCallResCombinations(t, func(t *testing.T, crt testCallRes) {
		cr := crt.res(t)

		// isOK
		if crt&resIsOK == 0 {
			assert.False(t, cr.OK(), "Expected call res to have a non-ok code.")
		} else {
			assert.True(t, cr.OK(), "Expected call res to have code ok.")
		}

		// isThrift
		if crt&(resHasArg2|resHasFragmentedArg2) != 0 {
			assert.True(t, cr.isThrift, "Expected call res to have isThrift=true")
			assert.Equal(t, cr.as, _tchanThriftValueBytes, "Expected arg scheme to be thrift")
		} else {
			assert.False(t, cr.isThrift, "Expected call res to have isThrift=false")
			assert.NotEqual(t, cr.as, _tchanThriftValueBytes, "Expected arg scheme to not be thrift")
		}

		// arg2IsFragmented
		if crt&resHasFragmentedArg2 != 0 {
			assert.True(t, cr.arg2IsFragmented, "Expected arg2 to be fragmented")
		}

		iter, err := cr.Arg2Iterator()
		if crt&(resHasArg2|resHasFragmentedArg2) != 0 {
			require.NoError(t, err, "Got unexpected error for .Arg2()")
			kvMap := make(map[string]string)
			for ; err == nil; iter, err = iter.Next() {
				kvMap[string(iter.Key())] = string(iter.Value())
			}
			if crt&resHasArg2 != 0 {
				for k, v := range exampleArg2Map {
					assert.Equal(t, kvMap[k], v)
				}
			}
		} else {
			require.Error(t, err, "Got unexpected error for .Arg2()")
		}
	})
}

func TestLazyCallResCorruptedFrame(t *testing.T) {
	_, err := newLazyCallRes(newCallResFrame(t, testCallResParams{
		arg2Prefix:  []byte{0, 100},
		arg2KeyVals: exampleArg2Map,
	}))

	require.EqualError(t, err, "read response frame: buffer is too small", "Got unexpected error for corrupted frame")
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

// TODO(cinchurge): replace with e.g. decodeThriftHeader once we've resolved the import cycle
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
		arg2Buf: thriftarg2test.BuildKVBuffer(exampleArg2Map),
		arg3Buf: []byte(exampleArg3Data),
	})

	t.Run("checksum", func(t *testing.T) {
		assert.Equal(t, ChecksumTypeCrc32C, cr.checksumType, "Got unexpected checksum type")
		assert.Equal(t, byte(ChecksumTypeCrc32C), cr.Frame.Payload[cr.checksumTypeOffset], "Unexpected value read from checksum offset")
	})

	t.Run(".arg2()", func(t *testing.T) {
		assert.Equal(t, exampleArg2Map, uint16KeyValToMap(t, cr.arg2()), "Got unexpected headers")
	})

	t.Run(".arg3()", func(t *testing.T) {
		// TODO(echung): switch to assert.Equal once we have more robust test frame generation
		assert.Contains(t, string(cr.arg3()), exampleArg3Data, "Got unexpected headers")
	})
}

func TestLazyCallReqLargeService(t *testing.T) {
	for _, svcSize := range []int{10, 100, 200, 240, math.MaxInt8} {
		t.Run(fmt.Sprintf("size=%v", svcSize), func(t *testing.T) {
			largeService := strings.Repeat("a", svcSize)
			withLazyCallReqCombinations(func(cr testCallReq) {
				f := cr.frameWithParams(t, testCallReqParams{
					serviceOverride: largeService,
				})

				callReq, err := newLazyCallReq(f)
				require.NoError(t, err, "newLazyCallReq failed")

				assert.Equal(t, largeService, string(callReq.Service()), "service name mismatch")
			})
		})
	}
}
