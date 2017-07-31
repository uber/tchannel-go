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

	"github.com/uber/tchannel-go/typed"
	"golang.org/x/net/context"
	"io"
)

// MexChannelBufferSize is the size of the message exchange channel buffer.
const MexChannelBufferSize = mexChannelBufferSize

// SetOnUpdate sets onUpdate for a peer, which is called when the peer's score is
// updated in all peer lists.
func (p *Peer) SetOnUpdate(f func(*Peer)) {
	p.Lock()
	p.onUpdate = f
	p.Unlock()
}

// GetConnectionRelay exports the getConnectionRelay for tests.
func (p *Peer) GetConnectionRelay(timeout time.Duration) (*Connection, error) {
	return p.getConnectionRelay(timeout)
}

// SetRandomSeed seeds all the random number generators in the channel so that
// tests will be deterministic for a given seed.
func (ch *Channel) SetRandomSeed(seed int64) {
	ch.Peers().peerHeap.rng.Seed(seed)
	peerRng.Seed(seed)
	for _, sc := range ch.subChannels.subchannels {
		sc.peers.peerHeap.rng.Seed(seed + int64(len(sc.peers.peersByHostPort)))
	}
}

// Ping sends a ping on the specific connection.
func (c *Connection) Ping(ctx context.Context) error {
	return c.ping(ctx)
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

// DoInitHandshake performs the initReq/initResp handshake
// using the given reader/writer stream
func DoInitHandshake(rw io.ReadWriter, msgID int, clientHostPort string) error {

	req := &initReq{
		initMessage{
			id:      uint32(msgID),
			Version: CurrentProtocolVersion,
			initParams: initParams{
				InitParamHostPort:         clientHostPort,
				InitParamProcessName:      "test",
				InitParamTChannelLanguage: "go",
			},
		},
	}

	reqFrame := NewFrame(MaxFramePayloadSize)
	if err := reqFrame.write(req); err != nil {
		return err
	}
	if err := reqFrame.WriteOut(rw); err != nil {
		return err
	}

	respFrame := NewFrame(MaxFramePayloadSize)
	if err := respFrame.ReadIn(rw); err != nil {
		return err
	}

	var resp initRes
	if err := respFrame.read(&resp); err != nil {
		return err
	}

	return nil
}

// SendCallReq sends a call request to the remote server
// Use this method when you are managing your own tcp
// conn instead of using a client channel
func SendCallReq(writer io.Writer, msgID int, ttl time.Duration, service string, arg1, arg2, arg3 string) error {

	callReq := &callReq{
		id:         uint32(msgID),
		TimeToLive: ttl,
		Headers: transportHeaders{
			CallerName: "test-client",
		},
		Service: service,
	}

	var wbuf typed.WriteBuffer

	frame := NewFrame(MaxFramePayloadSize)
	wbuf.Wrap(frame.Payload[:])
	wbuf.WriteSingleByte(0x00) // flags: 0, last fragment
	callReq.write(&wbuf)       // write the payload
	wbuf.WriteSingleByte(0x00) // no checksum option
	wbuf.WriteLen16String(arg1)
	wbuf.WriteLen16String(arg2)
	wbuf.WriteLen16String(arg3)

	frame.Header.ID = uint32(msgID)
	frame.Header.reserved1 = 0
	frame.Header.messageType = messageTypeCallReq
	frame.Header.SetPayloadSize(uint16(wbuf.BytesWritten()))

	return frame.WriteOut(writer)
}
