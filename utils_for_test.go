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

// This file contains functions for tests to access internal tchannel state.
// Since it has a _test.go suffix, it is only compiled with tests in this package.

import (
	"net"
	"time"
)

// MexChannelBufferSize is the size of the message exchange channel buffer.
const MexChannelBufferSize = mexChannelBufferSize

// RootPeers returns the root peer list from the Channel.
func (ch *Channel) RootPeers() *RootPeerList {
	return ch.rootPeers()
}

// OutboundConnection returns the underlying connection for an outbound call.
func OutboundConnection(call *OutboundCall) (*Connection, net.Conn) {
	conn := call.conn
	return conn, conn.conn
}

// InboundConnection returns the underlying connection for an incoming call.
func InboundConnection(call IncomingCall) (*Connection, net.Conn) {
	inboundCall, ok := call.(*InboundCall)
	if !ok {
		return nil, nil
	}

	conn := inboundCall.conn
	return conn, conn.conn
}

// NewSpan returns a Span for testing.
func NewSpan(traceID uint64, parentID uint64, spanID uint64) Span {
	return Span{traceID: traceID, parentID: parentID, spanID: spanID, flags: defaultTracingFlags}
}

// GetTimeNow returns the variable pointing to time.Now for stubbing.
func GetTimeNow() *func() time.Time {
	return &timeNow
}

func (l *PeerList) GetHeap() *PeerHeap {
	return l.peerHeap
}

func GetPendingRequests(p *Peer) int {
	count := 0
	for _, c := range p.outboundConnections {
		count = count + len(c.inbound.exchanges)
		count = count + len(c.outbound.exchanges)
	}

	for _, c := range p.inboundConnections {
		count = count + len(c.inbound.exchanges)
		count = count + len(c.outbound.exchanges)
	}
	return count
}
