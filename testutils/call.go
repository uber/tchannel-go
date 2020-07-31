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

package testutils

import (
	"time"

	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/relay"
	"github.com/uber/tchannel-go/testutils/thriftarg2test"
	"github.com/uber/tchannel-go/thrift/arg2"
)

// FakeIncomingCall implements IncomingCall interface.
// Note: the F suffix for the fields is to clash with the method name.
type FakeIncomingCall struct {
	// CallerNameF is the calling service's name.
	CallerNameF string

	// ShardKeyF is the intended destination for this call.
	ShardKeyF string

	// RemotePeerF is the calling service's peer info.
	RemotePeerF tchannel.PeerInfo

	// LocalPeerF is the local service's peer info.
	LocalPeerF tchannel.LocalPeerInfo

	// RoutingKeyF is the routing key.
	RoutingKeyF string

	// RoutingDelegateF is the routing delegate.
	RoutingDelegateF string
}

// CallerName returns the caller name as specified in the fake call.
func (f *FakeIncomingCall) CallerName() string {
	return f.CallerNameF
}

// ShardKey returns the shard key as specified in the fake call.
func (f *FakeIncomingCall) ShardKey() string {
	return f.ShardKeyF
}

// RoutingKey returns the routing delegate as specified in the fake call.
func (f *FakeIncomingCall) RoutingKey() string {
	return f.RoutingKeyF
}

// RoutingDelegate returns the routing delegate as specified in the fake call.
func (f *FakeIncomingCall) RoutingDelegate() string {
	return f.RoutingDelegateF
}

// LocalPeer returns the local peer information for this call.
func (f *FakeIncomingCall) LocalPeer() tchannel.LocalPeerInfo {
	return f.LocalPeerF
}

// RemotePeer returns the remote peer information for this call.
func (f *FakeIncomingCall) RemotePeer() tchannel.PeerInfo {
	return f.RemotePeerF
}

// CallOptions returns the incoming call options suitable for proxying a request.
func (f *FakeIncomingCall) CallOptions() *tchannel.CallOptions {
	return &tchannel.CallOptions{
		ShardKey:        f.ShardKey(),
		RoutingKey:      f.RoutingKey(),
		RoutingDelegate: f.RoutingDelegate(),
	}
}

// NewIncomingCall creates an incoming call for tests.
func NewIncomingCall(callerName string) tchannel.IncomingCall {
	return &FakeIncomingCall{CallerNameF: callerName}
}

// FakeCallFrame is a stub implementation of the CallFrame interface.
type FakeCallFrame struct {
	TTLF time.Duration

	ServiceF, MethodF, CallerF, RoutingKeyF, RoutingDelegateF string

	Arg2StartOffsetVal, Arg2EndOffsetVal int
	IsArg2Fragmented                     bool

	Arg2KVIterator    arg2.KeyValIterator
	hasArg2KVIterator error
}

var _ relay.CallFrame = FakeCallFrame{}

// TTL returns the TTL field.
func (f FakeCallFrame) TTL() time.Duration {
	return f.TTLF
}

// Service returns the service name field.
func (f FakeCallFrame) Service() []byte {
	return []byte(f.ServiceF)
}

// Method returns the method field.
func (f FakeCallFrame) Method() []byte {
	return []byte(f.MethodF)
}

// Caller returns the caller field.
func (f FakeCallFrame) Caller() []byte {
	return []byte(f.CallerF)
}

// RoutingKey returns the routing delegate field.
func (f FakeCallFrame) RoutingKey() []byte {
	return []byte(f.RoutingKeyF)
}

// RoutingDelegate returns the routing delegate field.
func (f FakeCallFrame) RoutingDelegate() []byte {
	return []byte(f.RoutingDelegateF)
}

// Arg2StartOffset returns the offset from start of payload to
// the beginning of Arg2.
func (f FakeCallFrame) Arg2StartOffset() int {
	return f.Arg2StartOffsetVal
}

// Arg2EndOffset returns the offset from start of payload to the end
// of Arg2 and whether Arg2 is fragmented.
func (f FakeCallFrame) Arg2EndOffset() (int, bool) {
	return f.Arg2EndOffsetVal, f.IsArg2Fragmented
}

// Arg2Iterator returns the iterator for reading Arg2 key value pair
// of TChannel-Thrift Arg Scheme.
func (f FakeCallFrame) Arg2Iterator() (arg2.KeyValIterator, error) {
	return f.Arg2KVIterator, f.hasArg2KVIterator
}

// CopyCallFrame copies the relay.CallFrame and returns a FakeCallFrame with
// corresponding values
func CopyCallFrame(f relay.CallFrame) FakeCallFrame {
	endOffset, hasMore := f.Arg2EndOffset()
	copyIterator, err := copyThriftArg2KVIterator(f)
	return FakeCallFrame{
		TTLF:               f.TTL(),
		ServiceF:           string(f.Service()),
		MethodF:            string(f.Method()),
		CallerF:            string(f.Caller()),
		RoutingKeyF:        string(f.RoutingKey()),
		RoutingDelegateF:   string(f.RoutingDelegate()),
		Arg2StartOffsetVal: f.Arg2StartOffset(),
		Arg2EndOffsetVal:   endOffset,
		IsArg2Fragmented:   hasMore,
		Arg2KVIterator:     copyIterator,
		hasArg2KVIterator:  err,
	}
}

// copyThriftArg2KVIterator uses the CallFrame Arg2Iterator to make a
// deep-copy KeyValIterator.
func copyThriftArg2KVIterator(f relay.CallFrame) (arg2.KeyValIterator, error) {
	kv := make(map[string]string)
	for iter, err := f.Arg2Iterator(); err == nil; iter, err = iter.Next() {
		kv[string(iter.Key())] = string(iter.Value())
	}
	return arg2.NewKeyValIterator(thriftarg2test.BuildKVBuffer(kv))
}
