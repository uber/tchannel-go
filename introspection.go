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
	"encoding/json"
	"runtime"

	"golang.org/x/net/context"
)

// IntrospectionOptions are the options used when introspecting the Channel.
type IntrospectionOptions struct {
	// IncludeExchanges will include all the IDs in the message exchanges.
	IncludeExchanges bool `json:"includeExchanges"`
}

// RuntimeState is a snapshot of the runtime state for a channel.
type RuntimeState struct {
	// LocalPeer is the local peer information (service name, host-port, etc).
	LocalPeer LocalPeerInfo

	// SubChannels contains information about any subchannels.
	SubChannels map[string]SubChannelRuntimeState

	// Peers contains information about all the peers on this channel.
	Peers map[string]PeerRuntimeState

	// MemStats is the Go runtime memory stats.
	MemState runtime.MemStats
}

// SubChannelRuntimeState is the runtime state for a subchannel.
type SubChannelRuntimeState struct {
	Service string
}

// ConnectionRuntimeState is the runtime state for a single connection.
type ConnectionRuntimeState struct {
	ConnectionState  string
	LocalHostPort    string
	RemoteHostPort   string
	InboundExchange  ExchangeRuntimeState
	OutboundExchange ExchangeRuntimeState
}

// ExchangeRuntimeState is the runtime state for a message exchange set.
type ExchangeRuntimeState struct {
	Name      string
	Count     int
	Exchanges []uint32
}

// PeerRuntimeState is the runtime state for a single peer.
type PeerRuntimeState struct {
	HostPort    string
	Connections []ConnectionRuntimeState
}

// IntrospectState returns the RuntimeState for this channel.
// Note: this is purely for debugging and monitoring, and may slow down your Channel.
func (ch *Channel) IntrospectState(opts *IntrospectionOptions) *RuntimeState {
	return &RuntimeState{
		LocalPeer:   ch.PeerInfo(),
		SubChannels: ch.subChannels.IntrospectState(opts),
		Peers:       ch.peers.IntrospectState(opts),
	}
}

// IntrospectState returns the runtime state of the peer list.
func (l *PeerList) IntrospectState(opts *IntrospectionOptions) map[string]PeerRuntimeState {
	m := make(map[string]PeerRuntimeState)
	l.mut.RLock()
	for _, peer := range l.peers {
		m[peer.HostPort()] = peer.IntrospectState(opts)
	}

	l.mut.RUnlock()
	return m
}

// IntrospectState returns the runtime state of the subchannels.
func (subChMap *subChannelMap) IntrospectState(opts *IntrospectionOptions) map[string]SubChannelRuntimeState {
	m := make(map[string]SubChannelRuntimeState)
	subChMap.mut.RLock()
	for k := range subChMap.subchannels {
		m[k] = SubChannelRuntimeState{
			Service: k,
		}
	}
	subChMap.mut.RUnlock()
	return m
}

// IntrospectState returns the runtime state for this peer.
func (p *Peer) IntrospectState(opts *IntrospectionOptions) PeerRuntimeState {
	p.mut.RLock()

	hostPort := p.hostPort
	conns := make([]ConnectionRuntimeState, len(p.connections))
	for i, conn := range p.connections {
		conns[i] = conn.IntrospectState(opts)
	}
	p.mut.RUnlock()

	return PeerRuntimeState{
		HostPort:    hostPort,
		Connections: conns,
	}
}

// IntrospectState returns the runtime state for this connection.
func (c *Connection) IntrospectState(opts *IntrospectionOptions) ConnectionRuntimeState {
	return ConnectionRuntimeState{
		ConnectionState:  c.state.String(),
		LocalHostPort:    c.conn.LocalAddr().String(),
		RemoteHostPort:   c.conn.RemoteAddr().String(),
		InboundExchange:  c.inbound.IntrospectState(opts),
		OutboundExchange: c.outbound.IntrospectState(opts),
	}
}

// IntrospectState returns the runtime state for this messsage exchange set.
func (mexset *messageExchangeSet) IntrospectState(opts *IntrospectionOptions) ExchangeRuntimeState {
	mexset.mut.RLock()
	state := ExchangeRuntimeState{
		Name:  mexset.name,
		Count: len(mexset.exchanges),
	}

	if opts.IncludeExchanges {
		state.Exchanges = make([]uint32, 0, len(mexset.exchanges))
		for k := range mexset.exchanges {
			state.Exchanges = append(state.Exchanges, k)
		}
	}

	mexset.mut.RUnlock()

	return state
}

// registerIntrospection registers a handler to respond with the runtime state in JSON.
// The endpoint name is _gometa_introspect.
func (ch *Channel) registerIntrospection() {
	handler := func(ctx context.Context, call *InboundCall) {
		var arg2, arg3 []byte
		if err := NewArgReader(call.Arg2Reader()).Read(&arg2); err != nil {
			return
		}
		if err := NewArgReader(call.Arg3Reader()).Read(&arg3); err != nil {
			return
		}
		if err := NewArgWriter(call.Response().Arg2Writer()).Write(nil); err != nil {
			return
		}

		var opts IntrospectionOptions
		json.Unmarshal(arg3, &opts)
		state := ch.IntrospectState(&opts)
		NewArgWriter(call.Response().Arg3Writer()).WriteJSON(state)
	}
	ch.Register(HandlerFunc(handler), "_gometa_introspect")
}
