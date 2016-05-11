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
	"encoding/binary"
	"fmt"
	"time"
)

const (
	_flagsIndex       = 0
	_ttlIndex         = 1
	_ttlLen           = 4
	_serviceLenIndex  = 1 /* flags */ + _ttlLen + 25 /* tracing */
	_serviceNameIndex = _serviceLenIndex + 1
)

type lazyCallReq struct {
	*Frame
}

func newLazyCallReq(f *Frame) lazyCallReq {
	if msgType := f.Header.messageType; msgType != messageTypeCallReq {
		panic(fmt.Errorf("newLazyCallReq called for wrong messageType: %v", msgType))
	}
	return lazyCallReq{f}
}

// Service returns the name of the destination service for this callReq.
func (f lazyCallReq) Service() string {
	l := f.Payload[_serviceLenIndex]
	return string(f.Payload[_serviceNameIndex : _serviceNameIndex+l])
}

// TTL returns the time to live for this callReq.
func (f lazyCallReq) TTL() time.Duration {
	ttl := binary.BigEndian.Uint32(f.Payload[_ttlIndex : _ttlIndex+_ttlLen])
	return time.Duration(ttl) * time.Millisecond
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
