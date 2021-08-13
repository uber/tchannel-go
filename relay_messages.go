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
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/uber/tchannel-go/relay"
	"github.com/uber/tchannel-go/thrift/arg2"
	"github.com/uber/tchannel-go/typed"
)

var _ relay.RespFrame = (*lazyCallRes)(nil)

var (
	_callerNameKeyBytes      = []byte(CallerName)
	_routingDelegateKeyBytes = []byte(RoutingDelegate)
	_routingKeyKeyBytes      = []byte(RoutingKey)
	_argSchemeKeyBytes       = []byte(ArgScheme)
	_tchanThriftValueBytes   = []byte(Thrift)
)

const (
	// Common to many frame types.
	_flagsIndex = 0

	// For call req, indexes into the frame.
	// Use int for indexes to avoid overflow caused by accidental byte arithmentic.
	_ttlIndex         int = 1
	_ttlLen           int = 4
	_spanIndex        int = _ttlIndex + _ttlLen
	_spanLength       int = 25
	_serviceLenIndex  int = _spanIndex + _spanLength
	_serviceNameIndex int = _serviceLenIndex + 1

	// For call res and call res continue.
	_resCodeOK        = 0x00
	_resCodeIndex int = 1

	// For error.
	_errCodeIndex int = 0
)

type lazyError struct {
	*Frame
}

func newLazyError(f *Frame) lazyError {
	if msgType := f.Header.messageType; msgType != messageTypeError {
		panic(fmt.Errorf("newLazyError called for wrong messageType: %v", msgType))
	}
	return lazyError{f}
}

func (e lazyError) Code() SystemErrCode {
	return SystemErrCode(e.Payload[_errCodeIndex])
}

type lazyCallRes struct {
	*Frame

	as               []byte
	arg2IsFragmented bool
	arg2Payload      []byte
}

func newLazyCallRes(f *Frame) (lazyCallRes, error) {
	if msgType := f.Header.messageType; msgType != messageTypeCallRes {
		panic(fmt.Errorf("newLazyCallRes called for wrong messageType: %v", msgType))
	}

	rbuf := typed.NewReadBuffer(f.SizedPayload())
	rbuf.SkipBytes(1)           // flags
	rbuf.SkipBytes(1)           // code
	rbuf.SkipBytes(_spanLength) // tracing

	var as []byte
	nh := int(rbuf.ReadSingleByte())
	for i := 0; i < nh; i++ {
		keyLen := int(rbuf.ReadSingleByte())
		key := rbuf.ReadBytes(keyLen)
		valLen := int(rbuf.ReadSingleByte())
		val := rbuf.ReadBytes(valLen)

		if bytes.Equal(key, _argSchemeKeyBytes) {
			as = val
			continue
		}
	}

	csumtype := ChecksumType(rbuf.ReadSingleByte()) // csumtype
	rbuf.SkipBytes(csumtype.ChecksumSize())         // csum

	// arg1: ignored
	narg1 := int(rbuf.ReadUint16())
	rbuf.SkipBytes(narg1)

	// arg2: keep track of payload
	narg2 := int(rbuf.ReadUint16())
	arg2Payload := rbuf.ReadBytes(narg2)
	arg2IsFragmented := rbuf.BytesRemaining() == 0 && hasMoreFragments(f)

	// arg3: ignored

	// Make sure we didn't hit any issues reading the buffer
	if err := rbuf.Err(); err != nil {
		return lazyCallRes{}, fmt.Errorf("read response frame: %v", err)
	}

	return lazyCallRes{
		Frame:            f,
		as:               as,
		arg2IsFragmented: arg2IsFragmented,
		arg2Payload:      arg2Payload,
	}, nil
}

// OK implements relay.RespFrame
func (cr lazyCallRes) OK() bool {
	return isCallResOK(cr.Frame)
}

// ArgScheme implements relay.RespFrame
func (cr lazyCallRes) ArgScheme() []byte {
	return cr.as
}

// Arg2IsFragmented implements relay.RespFrame
func (cr lazyCallRes) Arg2IsFragmented() bool {
	return cr.arg2IsFragmented
}

// Arg2 implements relay.RespFrame
func (cr lazyCallRes) Arg2() []byte {
	return cr.arg2Payload
}

type lazyCallReq struct {
	*Frame

	checksumTypeOffset             uint16
	arg2StartOffset, arg2EndOffset uint16
	arg3StartOffset                uint16

	caller, method, delegate, key, as []byte
	arg2Appends                       []relay.KeyVal
	checksumType                      ChecksumType
	isArg2Fragmented                  bool

	// Intentionally an array to combine allocations with that of lazyCallReq
	arg2InitialBuf [1]relay.KeyVal
}

// TODO: Consider pooling lazyCallReq and using pointers to the struct.

func newLazyCallReq(f *Frame) (*lazyCallReq, error) {
	if msgType := f.Header.messageType; msgType != messageTypeCallReq {
		panic(fmt.Errorf("newLazyCallReq called for wrong messageType: %v", msgType))
	}

	cr := &lazyCallReq{Frame: f}
	cr.arg2Appends = cr.arg2InitialBuf[:0]

	rbuf := typed.NewReadBuffer(f.SizedPayload())
	rbuf.SkipBytes(_serviceLenIndex)

	// service~1
	serviceLen := rbuf.ReadSingleByte()
	rbuf.SkipBytes(int(serviceLen))

	// nh:1 (hk~1 hv~1){nh}
	numHeaders := int(rbuf.ReadSingleByte())
	for i := 0; i < numHeaders; i++ {
		keyLen := int(rbuf.ReadSingleByte())
		key := rbuf.ReadBytes(keyLen)

		valLen := int(rbuf.ReadSingleByte())
		val := rbuf.ReadBytes(valLen)

		if bytes.Equal(key, _argSchemeKeyBytes) {
			cr.as = val
		} else if bytes.Equal(key, _callerNameKeyBytes) {
			cr.caller = val
		} else if bytes.Equal(key, _routingDelegateKeyBytes) {
			cr.delegate = val
		} else if bytes.Equal(key, _routingKeyKeyBytes) {
			cr.key = val
		}
	}

	// csumtype:1 (csum:4){0,1} arg1~2 arg2~2 arg3~2
	cr.checksumTypeOffset = uint16(rbuf.BytesRead())
	cr.checksumType = ChecksumType(rbuf.ReadSingleByte())
	rbuf.SkipBytes(cr.checksumType.ChecksumSize())

	// arg1~2
	arg1Len := int(rbuf.ReadUint16())
	cr.method = rbuf.ReadBytes(arg1Len)

	// arg2~2
	arg2Len := rbuf.ReadUint16()
	cr.arg2StartOffset = uint16(rbuf.BytesRead())
	cr.arg2EndOffset = cr.arg2StartOffset + arg2Len

	// arg2 is fragmented if we don't see arg3 in this frame.
	rbuf.SkipBytes(int(arg2Len))
	cr.isArg2Fragmented = rbuf.BytesRemaining() == 0 && cr.HasMoreFragments()

	if !cr.isArg2Fragmented {
		// arg3~2
		rbuf.SkipBytes(2)
		cr.arg3StartOffset = uint16(rbuf.BytesRead())
	}

	if rbuf.Err() != nil {
		return nil, rbuf.Err()
	}

	return cr, nil
}

// Caller returns the name of the originator of this callReq.
func (f *lazyCallReq) Caller() []byte {
	return f.caller
}

// Service returns the name of the destination service for this callReq.
func (f *lazyCallReq) Service() []byte {
	l := f.Payload[_serviceLenIndex]
	return f.Payload[_serviceNameIndex : _serviceNameIndex+int(l)]
}

// Method returns the name of the method being called.
func (f *lazyCallReq) Method() []byte {
	return f.method
}

// RoutingDelegate returns the routing delegate for this call req, if any.
func (f *lazyCallReq) RoutingDelegate() []byte {
	return f.delegate
}

// RoutingKey returns the routing delegate for this call req, if any.
func (f *lazyCallReq) RoutingKey() []byte {
	return f.key
}

// TTL returns the time to live for this callReq.
func (f *lazyCallReq) TTL() time.Duration {
	ttl := binary.BigEndian.Uint32(f.Payload[_ttlIndex : _ttlIndex+_ttlLen])
	return time.Duration(ttl) * time.Millisecond
}

// SetTTL overwrites the frame's TTL.
func (f *lazyCallReq) SetTTL(d time.Duration) {
	ttl := uint32(d / time.Millisecond)
	binary.BigEndian.PutUint32(f.Payload[_ttlIndex:_ttlIndex+_ttlLen], ttl)
}

// Span returns the Span
func (f *lazyCallReq) Span() Span {
	return callReqSpan(f.Frame)
}

// HasMoreFragments returns whether the callReq has more fragments.
func (f *lazyCallReq) HasMoreFragments() bool {
	return f.Payload[_flagsIndex]&hasMoreFragmentsFlag != 0
}

// Arg2EndOffset returns the offset from start of payload to the end of Arg2
// in bytes, and hasMore to be true if there are more frames and arg3 has
// not started.
func (f *lazyCallReq) Arg2EndOffset() (_ int, hasMore bool) {
	return int(f.arg2EndOffset), f.isArg2Fragmented
}

// Arg2StartOffset returns the offset from start of payload to the beginning
// of Arg2 in bytes.
func (f *lazyCallReq) Arg2StartOffset() int {
	return int(f.arg2StartOffset)
}

func (f *lazyCallReq) arg2() []byte {
	return f.Payload[f.arg2StartOffset:f.arg2EndOffset]
}

func (f *lazyCallReq) arg3() []byte {
	return f.SizedPayload()[f.arg3StartOffset:]
}

// Arg2Iterator returns the iterator for reading Arg2 key value pair
// of TChannel-Thrift Arg Scheme.
func (f *lazyCallReq) Arg2Iterator() (arg2.KeyValIterator, error) {
	if !bytes.Equal(f.as, _tchanThriftValueBytes) {
		return arg2.KeyValIterator{}, fmt.Errorf("%v: got %s", errArg2ThriftOnly, f.as)
	}
	return arg2.NewKeyValIterator(f.Payload[f.arg2StartOffset:f.arg2EndOffset])
}

func (f *lazyCallReq) Arg2Append(key, val []byte) {
	f.arg2Appends = append(f.arg2Appends, relay.KeyVal{Key: key, Val: val})
}

// finishesCall checks whether this frame is the last one we should expect for
// this RPC req-res.
func finishesCall(f *Frame) bool {
	switch f.messageType() {
	case messageTypeError:
		return true
	case messageTypeCallRes, messageTypeCallResContinue:
		flags := f.Payload[_flagsIndex]
		return flags&hasMoreFragmentsFlag == 0
	default:
		return false
	}
}

// isCallResOK indicates whether the call was successful
func isCallResOK(f *Frame) bool {
	return f.Payload[_resCodeIndex] == _resCodeOK
}

// hasMoreFragments indicates whether there are more fragments following this frame
func hasMoreFragments(f *Frame) bool {
	return f.Payload[_flagsIndex]&hasMoreFragmentsFlag != 0
}
