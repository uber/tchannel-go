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
	"sync"
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

var bufPool = sync.Pool{
	New: func() interface{} {
		// The Pool's New function should generally only return pointer
		// types, since a pointer can be put into the return interface
		// value without an allocation:
		return new(bytes.Buffer)
	},
}

type lazyCallReqOpType int

const (
	invalid lazyCallReqOpType = iota
	opTypeArg2Append
	opTypeArg2Set
	opTypeArg2Delete
)

type arg2Append struct {
	key, val []byte
}

type arg2Set struct {
	key, val []byte
}

type arg2Delete struct {
	key []byte
}

type lazyCallReqOp struct {
	opType lazyCallReqOpType
	arg2Append
	arg2Set
	arg2Delete
}

type keyVal struct {
	key []byte
	val []byte
}

type lazyCallReq struct {
	*Frame

	caller, method, delegate, key, as []byte

	checksumOffset   int
	checksumType     ChecksumType
	checksumSize     int
	arg1Offset       int
	arg1Len          int
	arg2Offset       int
	arg2Len          int
	arg2DataOffset   int
	isArg2Fragmented bool
	arg3Offset       int
	arg3Len          int

	arg2appends []keyVal
}

// TODO: Consider pooling lazyCallReq and using pointers to the struct.

func newLazyCallReq(f *Frame) lazyCallReq {
	if msgType := f.Header.messageType; msgType != messageTypeCallReq {
		panic(fmt.Errorf("newLazyCallReq called for wrong messageType: %v", msgType))
	}

	cr := lazyCallReq{Frame: f}

	serviceLen := f.Payload[_serviceLenIndex]
	// nh:1 (hk~1 hv~1){nh}
	headerStart := _serviceLenIndex + 1 /* length byte */ + serviceLen
	numHeaders := int(f.Payload[headerStart])
	cur := int(headerStart) + 1
	for i := 0; i < numHeaders; i++ {
		keyLen := int(f.Payload[cur])
		cur++
		key := f.Payload[cur : cur+keyLen]
		cur += keyLen

		valLen := int(f.Payload[cur])
		cur++
		val := f.Payload[cur : cur+valLen]
		cur += valLen

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

	cr.checksumOffset = cur

	// csumtype:1 (csum:4){0,1} arg1~2 arg2~2 arg3~2
	cr.checksumType = ChecksumType(f.Payload[cur])
	cr.checksumSize = cr.checksumType.ChecksumSize()
	cur += 1 /* checksum */ + cr.checksumSize

	// arg1~2
	cr.arg1Offset = cur
	cr.arg1Len = int(binary.BigEndian.Uint16(f.Payload[cur : cur+2]))
	cur += 2
	cr.method = f.Payload[cur : cur+cr.arg1Len]
	cur += cr.arg1Len

	// arg2~2
	cr.arg2Offset = cur
	cr.arg2Len = int(binary.BigEndian.Uint16(f.Payload[cur : cur+2]))
	cur += 2
	cr.arg2DataOffset = cur
	cur += cr.arg2Len

	// arg3~2
	cr.arg3Offset = cur
	cr.arg3Len = int(binary.BigEndian.Uint16(f.Payload[cur : cur+2]))

	// arg2 is fragmented if we don't see arg3 in this frame.
	cr.isArg2Fragmented = int(cr.Header.PayloadSize()) <= cr.arg3Offset && cr.HasMoreFragments()

	return cr
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
func (f lazyCallReq) RoutingKey() []byte {
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
	return f.arg3Offset, f.isArg2Fragmented
}

// Arg2StartOffset returns the offset from start of payload to the beginning
// of Arg2 in bytes.
func (f *lazyCallReq) Arg2StartOffset() int {
	return f.arg2DataOffset
}

// Arg2Iterator returns the iterator for reading Arg2 key value pair
// of TChannel-Thrift Arg Scheme.
func (f *lazyCallReq) Arg2Iterator() (arg2.KeyValIterator, error) {
	if !bytes.Equal(f.as, _tchanThriftValueBytes) {
		return arg2.KeyValIterator{}, fmt.Errorf("non thrift scheme %s", f.as)
	}

	if f.arg3Offset > int(f.Header.PayloadSize()) {
		return arg2.KeyValIterator{}, errBadArg2Len
	}

	return arg2.NewKeyValIterator(f.Payload[f.arg2DataOffset:f.arg3Offset])
}

func (f *lazyCallReq) Arg2Append(key, val []byte) {
	f.arg2appends = append(f.arg2appends, keyVal{key, val})
}

func (f *lazyCallReq) header() []byte {
	return f.Payload[:f.checksumOffset]
}

func (f *lazyCallReq) arg1() []byte {
	return f.Payload[f.arg1Offset+2 : f.arg1Offset+2+f.arg1Len]
}

func (f *lazyCallReq) arg2() []byte {
	return f.Payload[f.arg2Offset+2 : f.arg2Offset+2+f.arg2Len]
}

func (f *lazyCallReq) arg3() []byte {
	return f.Payload[f.arg3Offset+2 : f.arg3Offset+2+f.arg3Len]
}

func (f *lazyCallReq) getFrames() ([]*Frame, error) {
	var frames []*Frame

	fragment, err := f.newFragment()
	if err != nil {
		return nil, fmt.Errorf("create fragment: %v", err)
	}

	frames = append(frames, fragment.frame)

	// arg1
	fragment.payloadWBuf.WriteUint16(uint16(len(f.arg1())))
	fragment.payloadWBuf.WriteBytes(f.arg1())
	if fragment.csum != nil {
		fragment.csum.Add(f.arg1())
	}

	// arg2
	// Calculate new length
	arg2Len := len(f.arg2())
	arg2NH := binary.BigEndian.Uint16(f.arg2()[:2])
	for _, kv := range f.arg2appends {
		arg2Len += 2 + len(kv.key) + 2 + len(kv.val)
		arg2NH++
	}

	// Write arg2 with optional append
	fragment.payloadWBuf.WriteUint16(uint16(arg2Len))
	if arg2Len > fragment.payloadWBuf.BytesRemaining() {
		return nil, fmt.Errorf("arg2 must be within the call req frame")
	}

	arg2Ref := fragment.payloadWBuf.DeferBytes(arg2Len)
	var arg2wbuf typed.WriteBuffer
	arg2wbuf.Wrap(arg2Ref)
	arg2wbuf.WriteUint16(arg2NH)
	arg2wbuf.WriteBytes(f.arg2()[2:])
	for _, kv := range f.arg2appends {
		arg2wbuf.WriteUint16(uint16(len(kv.key)))
		arg2wbuf.WriteBytes(kv.key)
		arg2wbuf.WriteUint16(uint16(len(kv.val)))
		arg2wbuf.WriteBytes(kv.val)
	}
	if fragment.csum != nil {
		fragment.csum.Add(arg2Ref)
	}

	// if by now we don't have enough space for arg3, move it into the next frame.
	if f.arg3Len > fragment.payloadWBuf.BytesRemaining() {
		// Write arg3 size and finalize previous frame
		fragment.payloadWBuf.WriteUint16(0)
		fragment.finalize()

		// Start new frame
		fragment, err = f.newFragment()
		if err != nil {
			return nil, fmt.Errorf("new fragment: %v", err)
		}
		// arg1~2
		fragment.payloadWBuf.WriteUint16(0)
		// arg2~2
		fragment.payloadWBuf.WriteUint16(0)
	}

	// arg3
	fragment.payloadWBuf.WriteUint16(uint16(len(f.arg3())))
	fragment.payloadWBuf.WriteBytes(f.arg3())
	if fragment.csum != nil {
		fragment.csum.Add(f.arg3())
	}
	fragment.finalize()

	return frames, nil
}

type fragment struct {
	frame        *Frame
	payloadWBuf  typed.WriteBuffer
	checksumType ChecksumType
	csum         Checksum
	checksumRef  typed.BytesRef
}

func (f *lazyCallReq) newFragment() (*fragment, error) {
	frame := DefaultFramePool.Get()
	copy(frame.headerBuffer, f.headerBuffer)

	if err := frame.Header.read(typed.NewReadBuffer(f.headerBuffer)); err != nil {
		return nil, fmt.Errorf("copy frame header: %v", err)
	}

	var payloadWBuf typed.WriteBuffer
	payloadWBuf.Wrap(frame.Payload)

	// headers are preserved across fragments
	payloadWBuf.WriteBytes(f.header())

	// checksumtype~1
	payloadWBuf.WriteSingleByte(byte(f.checksumType))

	var checksumRef typed.BytesRef
	var csum Checksum
	if f.checksumType != ChecksumTypeNone {
		checksumRef = payloadWBuf.DeferBytes(f.checksumSize)
		csum = f.checksumType.New()
	}

	return &fragment{
		frame:        frame,
		payloadWBuf:  payloadWBuf,
		checksumType: f.checksumType,
		checksumRef:  checksumRef,
		csum:         csum,
	}, nil
}

func (f *fragment) finalize() {
	f.frame.Header.SetPayloadSize(uint16(f.payloadWBuf.BytesWritten()))
	if f.csum != nil && f.checksumRef != nil {
		f.checksumRef.Update(f.csum.Sum())
		f.csum.Release()
	}
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
