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

package relaytest

import (
	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/relay"
)

// Ensure that the StubRelayHost implements tchannel.RelayHost.
var _ tchannel.RelayHost = (*StubRelayHost)(nil)

// StubRelayHost is a stub RelayHost for tests that backs peer selection to an
// underlying channel using isolated subchannels and the default peer selection.
type StubRelayHost struct {
	ch          *tchannel.Channel
	stats       *MockStats
	frameFn     func(relay.CallFrame, *relay.Conn)
	respFrameFn func(relay.RespFrame)
}

type stubCall struct {
	*MockCallStats

	peer        *tchannel.Peer
	respFrameFn func(relay.RespFrame)
}

// NewStubRelayHost creates a new stub RelayHost for tests.
func NewStubRelayHost() *StubRelayHost {
	return &StubRelayHost{
		stats:       NewMockStats(),
		respFrameFn: func(_ relay.RespFrame) {},
	}
}

// SetFrameFn sets a function to run on every frame.
func (rh *StubRelayHost) SetFrameFn(f func(relay.CallFrame, *relay.Conn)) {
	rh.frameFn = f
}

// SetRespFrameFn sets a function to run on every frame.
func (rh *StubRelayHost) SetRespFrameFn(f func(relay.RespFrame)) {
	rh.respFrameFn = f
}

// SetChannel is called by the channel after creation so we can
// get a reference to the channels' peers.
func (rh *StubRelayHost) SetChannel(ch *tchannel.Channel) {
	rh.ch = ch
}

// Start starts a new RelayCall for the given call on a specific connection.
func (rh *StubRelayHost) Start(cf relay.CallFrame, conn *relay.Conn) (tchannel.RelayCall, error) {
	if rh.frameFn != nil {
		rh.frameFn(cf, conn)
	}

	// Get a peer from the subchannel.
	peer, err := rh.ch.GetSubChannel(string(cf.Service())).Peers().Get(nil)
	return &stubCall{
		MockCallStats: rh.stats.Begin(cf),
		peer:          peer,
		respFrameFn:   rh.respFrameFn,
	}, err
}

// Add adds a service instance with the specified host:port.
func (rh *StubRelayHost) Add(service, hostPort string) {
	rh.ch.GetSubChannel(service, tchannel.Isolated).Peers().GetOrAdd(hostPort)
}

// Stats returns the *MockStats tracked for this channel.
func (rh *StubRelayHost) Stats() *MockStats {
	return rh.stats
}

// Destination returns the selected peer for this call.
func (c *stubCall) Destination() (*tchannel.Peer, bool) {
	return c.peer, c.peer != nil
}

func (c *stubCall) CallResponse(frame relay.RespFrame) {
	c.respFrameFn(frame)
}
