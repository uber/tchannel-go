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
	"errors"
	"sync"
	"time"

	"golang.org/x/net/context"
)

var (
	// ErrInvalidConnectionState indicates that the connection is not in a valid state.
	ErrInvalidConnectionState = errors.New("connection is in an invalid state")

	// ErrNoPeers indicates that there are no peers.
	ErrNoPeers = errors.New("no peers available")

	peerRng = NewRand(time.Now().UnixNano())
)

type Connectable interface {
	Connect(context.Context, string, *ConnectionOptions) (*Connection, error)
	ConnectionOptions() *ConnectionOptions
}

// PeerList maintains a list of Peers.
type PeerList struct {
	sync.RWMutex

	parent          *RootPeerList
	peersByHostPort map[string]*peerScore
	peerHeap        *PeerHeap
	scoreCalculator ScoreCalculator
}

func newPeerList(root *RootPeerList) *PeerList {
	return &PeerList{
		parent:          root,
		peersByHostPort: make(map[string]*peerScore),
		scoreCalculator: newRandCalculator(),
		peerHeap:        newPeerHeap(),
	}
}

// SetStrategy sets customized peer selection stratedgy.
func (l *PeerList) SetStrategy(sc ScoreCalculator) {
	l.scoreCalculator = sc
}

// Siblings don't share peer lists (though they take care not to double-connect
// to the same hosts).
func (l *PeerList) newSibling() *PeerList {
	sib := newPeerList(l.parent)
	return sib
}

// Add adds a peer to the list if it does not exist, or returns any existing peer.
func (l *PeerList) Add(hostPort string) *Peer {
	if ps, ok := l.exists(hostPort); ok {
		return ps.Peer
	}
	l.Lock()
	defer l.Unlock()

	if p, ok := l.peersByHostPort[hostPort]; ok {
		return p.Peer
	}

	p := l.parent.Add(hostPort)
	ps := newPeerScore(p)

	l.peersByHostPort[hostPort] = ps
	l.peerHeap.push(ps)

	return p
}

// Get returns a peer from the peer list, or nil if none can be found.
func (l *PeerList) Get() (*Peer, error) {
	l.RLock()

	if l.peerHeap.Len() == 0 {
		l.RUnlock()
		return nil, ErrNoPeers
	}

	peer := l.choosePeer()
	l.RUnlock()

	return peer, nil
}

func (l *PeerList) choosePeer() *Peer {
	return l.peerHeap.peek().Peer
}

// GetOrAdd returns a peer for the given hostPort, creating one if it doesn't yet exist.
func (l *PeerList) GetOrAdd(hostPort string) *Peer {
	if ps, ok := l.exists(hostPort); ok {
		return ps.Peer
	}
	return l.Add(hostPort)
}

// Copy returns a map of the peer list. This method should only be used for testing.
func (l *PeerList) Copy() map[string]*Peer {
	l.RLock()
	defer l.RUnlock()

	listCopy := make(map[string]*Peer)
	for k, v := range l.peersByHostPort {
		listCopy[k] = v.Peer
	}
	return listCopy
}

// exists checks if a hostport exists in the peer list.
func (l *PeerList) exists(hostPort string) (*peerScore, bool) {
	l.RLock()
	ps, ok := l.peersByHostPort[hostPort]
	l.RUnlock()

	return ps, ok
}

func (l *PeerList) updatePeerHeap(p *Peer) {
	if ps, ok := l.exists(p.hostPort); ok {
		ps.score = l.scoreCalculator.GetScore(p)
		l.peerHeap.update(ps)
	}
}

type peerScore struct {
	*Peer

	score uint64
	index int
}

func newPeerScore(p *Peer) *peerScore {
	return &peerScore{Peer: p}
}

// Peer represents a single autobahn service or client with a unique host:port.
type Peer struct {
	channel  Connectable
	hostPort string

	mut                 sync.RWMutex // mut protects connections.
	inboundConnections  []*Connection
	outboundConnections []*Connection
}

func newPeer(channel Connectable, hostPort string) *Peer {
	if hostPort == "" {
		panic("Cannot create peer with blank hostPort")
	}
	return &Peer{
		channel:  channel,
		hostPort: hostPort,
	}
}

// HostPort returns the host:port used to connect to this peer.
func (p *Peer) HostPort() string {
	return p.hostPort
}

// getActive returns a list of active connections.
// TODO(prashant): Should we clear inactive connections?
func (p *Peer) getActive() []*Connection {
	p.mut.RLock()

	var active []*Connection
	p.runWithConnections(func(c *Connection) {
		if c.IsActive() {
			active = append(active, c)
		}
	})

	p.mut.RUnlock()
	return active
}

func randConn(conns []*Connection) *Connection {
	return conns[peerRng.Intn(len(conns))]
}

// GetConnection returns an active connection to this peer. If no active connections
// are found, it will create a new outbound connection and return it.
func (p *Peer) GetConnection(ctx context.Context) (*Connection, error) {
	// TODO(prashant): Use some sort of scoring to pick a connection.
	if activeConns := p.getActive(); len(activeConns) > 0 {
		return randConn(activeConns), nil
	}

	// No active connections, make a new outgoing connection.
	c, err := p.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// AddInboundConnection adds an active inbound connection to the peer's connection list.
// If a connection is not active, ErrInvalidConnectionState will be returned.
func (p *Peer) AddInboundConnection(c *Connection) error {
	switch c.readState() {
	case connectionActive, connectionStartClose:
		// TODO(prashantv): Block inbound connections when the connection is not active.
		break
	default:
		return ErrInvalidConnectionState
	}

	p.mut.Lock()
	defer p.mut.Unlock()

	p.inboundConnections = append(p.inboundConnections, c)
	return nil
}

// AddOutboundConnection adds an active outbound connection to the peer's connection list.
// If a connection is not active, ErrInvalidConnectionState will be returned.
func (p *Peer) AddOutboundConnection(c *Connection) error {
	switch c.readState() {
	case connectionActive, connectionStartClose:
		break
	default:
		return ErrInvalidConnectionState
	}

	p.mut.Lock()
	defer p.mut.Unlock()

	p.outboundConnections = append(p.outboundConnections, c)
	return nil
}

// Connect adds a new outbound connection to the peer.
func (p *Peer) Connect(ctx context.Context) (*Connection, error) {
	c, err := p.channel.Connect(ctx, p.hostPort, p.channel.ConnectionOptions())
	if err != nil {
		return nil, err
	}

	if err := p.AddOutboundConnection(c); err != nil {
		return nil, err
	}

	return c, nil
}

// BeginCall starts a new call to this specific peer, returning an OutboundCall that can
// be used to write the arguments of the call.
func (p *Peer) BeginCall(ctx context.Context, serviceName string, operationName string, callOptions *CallOptions) (*OutboundCall, error) {
	conn, err := p.GetConnection(ctx)
	if err != nil {
		return nil, err
	}

	if callOptions == nil {
		callOptions = defaultCallOptions
	}
	call, err := conn.beginCall(ctx, serviceName, callOptions, operationName)
	if err != nil {
		return nil, err
	}

	return call, err
}

func (p *Peer) runWithConnections(f func(*Connection)) {
	for _, c := range p.inboundConnections {
		f(c)
	}

	for _, c := range p.outboundConnections {
		f(c)
	}
}

// Close closes all connections to this peer.
func (p *Peer) Close() {
	p.mut.RLock()
	defer p.mut.RUnlock()

	p.runWithConnections(func(c *Connection) {
		c.Close()
	})
}
