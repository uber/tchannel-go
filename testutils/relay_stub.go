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
	"math/rand"
	"sync"

	"github.com/uber/tchannel-go/relay"
	"github.com/uber/tchannel-go/trand"
)

// SimpleRelayHosts is a simple stub that satisfies the RelayHosts interface.
type SimpleRelayHosts struct {
	sync.RWMutex
	r     *rand.Rand
	peers map[string][]relay.Peer
}

// NewSimpleRelayHosts wraps a map in the RelayHosts interface.
func NewSimpleRelayHosts(peerHostPorts map[string][]string) *SimpleRelayHosts {
	peers := make(map[string][]relay.Peer)
	for key, hosts := range peerHostPorts {
		peerList := make([]relay.Peer, len(hosts))
		for i, host := range hosts {
			peerList[i] = relay.Peer{HostPort: host}
		}
		peers[key] = peerList
	}

	// Use a known seed for repeatable tests.
	return &SimpleRelayHosts{
		r:     trand.New(1),
		peers: peers,
	}
}

// Get takes a routing key and returns the best host:port for that key.
func (rh *SimpleRelayHosts) Get(frame relay.CallFrame) relay.Peer {
	rh.RLock()
	defer rh.RUnlock()

	available, ok := rh.peers[string(frame.Service())]
	if !ok || len(available) == 0 {
		return relay.Peer{}
	}
	i := rh.r.Intn(len(available))
	return available[i]
}

// Add adds a host:port for a routing key.
func (rh *SimpleRelayHosts) Add(service, hostPort string) {
	rh.AddAssignment(service, hostPort, "")
}

// AddAssignment adds a host:port with an assignment for a routing key.
func (rh *SimpleRelayHosts) AddAssignment(service, hostPort, assignment string) {
	rh.Lock()
	rh.peers[service] = append(rh.peers[service], relay.Peer{
		HostPort:   hostPort,
		Assignment: assignment,
	})
	rh.Unlock()
}
