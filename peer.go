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
	"sync/atomic"
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

// Connectable is the interface used by peers to create connections.
type Connectable interface {
	// Connect tries to connect to the given hostPort.
	Connect(ctx context.Context, hostPort string) (*Connection, error)
	// Logger returns the logger to use.
	Logger() Logger
}

// PeerList maintains a list of Peers.
type PeerList struct {
	sync.RWMutex

	parent          *RootPeerList
	peersByHostPort map[string]*Peer
	peers           []*Peer
	scoreCalculator scoreCalculator
}

func newPeerList(root *RootPeerList) *PeerList {
	return &PeerList{
		parent:          root,
		peersByHostPort: make(map[string]*Peer),
		scoreCalculator: newPreferIncoming(),
	}
}

// Siblings don't share peer lists (though they take care not to double-connect
// to the same hosts).
func (l *PeerList) newSibling() *PeerList {
	sib := newPeerList(l.parent)
	return sib
}

// Add adds a peer to the list if it does not exist, or returns any existing peer.
func (l *PeerList) Add(hostPort string) *Peer {
	l.RLock()

	if p, ok := l.peersByHostPort[hostPort]; ok {
		l.RUnlock()
		return p
	}

	l.RUnlock()
	l.Lock()
	defer l.Unlock()

	if p, ok := l.peersByHostPort[hostPort]; ok {
		return p
	}

	p := l.parent.Add(hostPort)
	atomic.AddUint32(&p.scCount, 1)
	l.peersByHostPort[hostPort] = p
	l.peers = append(l.peers, p)
	return p
}

// Get returns a peer from the peer list, or nil if none can be found.
// Get will avoid peers in prevSelected unless there are no other peers left.
func (l *PeerList) Get(prevSelected map[string]struct{}) (*Peer, error) {
	l.RLock()

	if len(l.peers) == 0 {
		l.RUnlock()
		return nil, ErrNoPeers
	}

	// Select a peer, avoiding previously selected peers. If all peers have been previously
	// selected, then it's OK to repick them.
	peer := l.choosePeer(l.peers, prevSelected)
	if peer == nil {
		peer = l.choosePeer(l.peers, nil)
	}
	l.RUnlock()

	return peer, nil
}

func (l *PeerList) choosePeer(peers []*Peer, prevSelected map[string]struct{}) *Peer {
	var maxScore uint64
	var bestPeer *Peer

	// pick peer with highest score that is not in prevSelected.
	for _, peer := range peers {
		if _, ok := prevSelected[peer.HostPort()]; ok {
			continue
		}

		score := l.scoreCalculator.GetScore(peer)
		if score > maxScore {
			maxScore = score
			bestPeer = peer
		}
	}

	if bestPeer != nil {
		atomic.AddUint64(&bestPeer.chosenCount, 1)
	}
	return bestPeer
}

// GetOrAdd returns a peer for the given hostPort, creating one if it doesn't yet exist.
func (l *PeerList) GetOrAdd(hostPort string) *Peer {
	l.RLock()
	if p, ok := l.peersByHostPort[hostPort]; ok {
		l.RUnlock()
		return p
	}

	l.RUnlock()
	return l.Add(hostPort)
}

// Copy returns a map of the peer list. This method should only be used for testing.
func (l *PeerList) Copy() map[string]*Peer {
	l.RLock()
	defer l.RUnlock()

	listCopy := make(map[string]*Peer)
	for k, v := range l.peersByHostPort {
		listCopy[k] = v
	}
	return listCopy
}

// Peer represents a single autobahn service or client with a unique host:port.
type Peer struct {
	channel      Connectable
	hostPort     string
	onConnChange func(*Peer)

	// scCount is the number of subchannels that this peer is added to.
	scCount uint32

	mut                 sync.RWMutex // mut protects connections.
	inboundConnections  []*Connection
	outboundConnections []*Connection
	chosenCount         uint64
}

func newPeer(channel Connectable, hostPort string, onConnChange func(*Peer)) *Peer {
	if hostPort == "" {
		panic("Cannot create peer with blank hostPort")
	}
	return &Peer{
		channel:      channel,
		hostPort:     hostPort,
		onConnChange: onConnChange,
	}
}

// HostPort returns the host:port used to connect to this peer.
func (p *Peer) HostPort() string {
	return p.hostPort
}

// getActive returns a list of active connections.
// TODO(prashant): Should we clear inactive connections?
func (p *Peer) getActive() []*Connection {
	var active []*Connection
	p.runWithConnections(func(c *Connection) {
		if c.IsActive() {
			active = append(active, c)
		}
	})
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
	p.inboundConnections = append(p.inboundConnections, c)
	p.mut.Unlock()

	p.connectionStateChanged(c)
	return nil
}

// canRemove returns whether this peer can be safely removed from the root peer list.
func (p *Peer) canRemove() bool {
	p.mut.RLock()
	count := len(p.inboundConnections) + len(p.outboundConnections) + int(p.scCount)
	p.mut.RUnlock()
	return count == 0
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
	p.outboundConnections = append(p.outboundConnections, c)
	p.mut.Unlock()

	p.connectionStateChanged(c)
	return nil
}

// checkInboundConnection will check whether the changed connection is an inbound
// connection, and will remove any closed connections.
func (p *Peer) checkInboundConnection(changed *Connection) (updated bool, isInbound bool) {
	newConns := p.inboundConnections[:0]
	for _, c := range p.inboundConnections {
		if c == changed {
			isInbound = true
		}

		if c.readState() != connectionClosed {
			newConns = append(newConns, c)
		} else {
			updated = true
		}
	}
	if updated {
		p.inboundConnections = newConns
	}

	return updated, isInbound
}

// connectionStateChanged is called when one of the peers' connections states changes.
func (p *Peer) connectionStateChanged(changed *Connection) {
	p.mut.Lock()
	updated, _ := p.checkInboundConnection(changed)
	p.mut.Unlock()

	if updated {
		p.onConnChange(p)
	}
}

// Connect adds a new outbound connection to the peer.
func (p *Peer) Connect(ctx context.Context) (*Connection, error) {
	c, err := p.channel.Connect(ctx, p.hostPort)
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
	if callOptions == nil {
		callOptions = defaultCallOptions
	}
	callOptions.RequestState.AddSelectedPeer(p.HostPort())

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

// NumInbound returns the number of inbound connections to this node.
func (p *Peer) NumInbound() int {
	p.mut.RLock()
	count := len(p.inboundConnections)
	p.mut.RUnlock()
	return count
}

func (p *Peer) runWithConnections(f func(*Connection)) {
	p.mut.RLock()
	inboundConns := p.inboundConnections
	outboundConns := p.outboundConnections
	p.mut.RUnlock()

	for _, c := range inboundConns {
		f(c)
	}

	for _, c := range outboundConns {
		f(c)
	}
}
