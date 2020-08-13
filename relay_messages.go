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
	"errors"
	"fmt"
	"time"

	"github.com/uber/tchannel-go/thrift/arg2"
	"github.com/uber/tchannel-go/typed"
)

var (
	_callerNameKeyBytes      = []byte(CallerName)
	_routingDelegateKeyBytes = []byte(RoutingDelegate)
	_routingKeyKeyBytes      = []byte(RoutingKey)
	_argSchemeKeyBytes       = []byte(ArgScheme)
	_tchanThriftValueBytes   = []byte(Thrift)

	errBadArg2Len = errors.New("bad Arg2 length")
)

const (
	// Common to many frame types.
	_flagsIndex = 0

	// For call req.
	_ttlIndex         = 1
	_ttlLen           = 4
	_spanIndex        = _ttlIndex + _ttlLen
	_spanLength       = 25
	_serviceLenIndex  = _spanIndex + _spanLength
	_serviceNameIndex = _serviceLenIndex + 1

	// For call res and call res continue.
	_resCodeOK    = 0x00
	_resCodeIndex = 1

	// For error.
	_errCodeIndex = 0
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
}

func newLazyCallRes(f *Frame) lazyCallRes {
	if msgType := f.Header.messageType; msgType != messageTypeCallRes {
		panic(fmt.Errorf("newLazyCallRes called for wrong messageType: %v", msgType))
	}
	return lazyCallRes{f}
}

func (cr lazyCallRes) OK() bool {
	return cr.Payload[_resCodeIndex] == _resCodeOK
}

type lazyCallReq struct {
	*Frame

	caller, method, delegate, key, as []byte

	checksumTypeOffset             uint16
	arg2StartOffset, arg2EndOffset uint16
	arg3StartOffset                uint16

	checksumType     ChecksumType
	isArg2Fragmented bool
}

// TODO: Consider pooling lazyCallReq and using pointers to the struct.

func newLazyCallReq(f *Frame) (*lazyCallReq, error) {
	if msgType := f.Header.messageType; msgType != messageTypeCallReq {
		panic(fmt.Errorf("newLazyCallReq called for wrong messageType: %v", msgType))
	}

	cr := &lazyCallReq{Frame: f}

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
	return f.Payload[_serviceNameIndex : _serviceNameIndex+l]
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
	return f.Payload[f.arg3StartOffset:f.Header.PayloadSize()]
}

// Arg2Iterator returns the iterator for reading Arg2 key value pair
// of TChannel-Thrift Arg Scheme.
func (f *lazyCallReq) Arg2Iterator() (arg2.KeyValIterator, error) {
	if !bytes.Equal(f.as, _tchanThriftValueBytes) {
		return arg2.KeyValIterator{}, fmt.Errorf("non thrift scheme %s", f.as)
	}

	// TODO(echung): drop this since we already do error checks in the constructor
	if f.arg2EndOffset > f.Header.PayloadSize() {
		return arg2.KeyValIterator{}, errBadArg2Len
	}

	return arg2.NewKeyValIterator(f.Payload[f.arg2StartOffset:f.arg2EndOffset])
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
