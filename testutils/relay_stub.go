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
	"testing"

	"github.com/uber/tchannel-go/relay"
	"github.com/uber/tchannel-go/trand"
)

const _noConnMsg = "SimpleRelayHosts implementation was passed a nil relay.Conn."

// SimpleRelayHosts is a simple stub that satisfies the RelayHosts interface.
type SimpleRelayHosts struct {
	sync.RWMutex

	t          testing.TB
	r          *rand.Rand
	verifyConn bool
	peers      map[string][]relay.Peer
	errors     map[string]error
}

// NewSimpleRelayHosts wraps a map in the RelayHosts interface.
func NewSimpleRelayHosts(t testing.TB, peerHostPorts map[string][]string) *SimpleRelayHosts {
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
		t:          t,
		verifyConn: true,
		r:          trand.New(1),
		peers:      peers,
		errors:     make(map[string]error),
	}
}

// Get takes a routing key and returns the best host:port for that key.
func (rh *SimpleRelayHosts) Get(frame relay.CallFrame, conn relay.Conn) (relay.Peer, error) {
	rh.RLock()
	defer rh.RUnlock()

	if rh.verifyConn && conn == nil {
		rh.t.Error(_noConnMsg)
	}

	if err, ok := rh.errors[string(frame.Service())]; ok {
		return relay.Peer{}, err
	}

	available, ok := rh.peers[string(frame.Service())]
	if !ok || len(available) == 0 {
		return relay.Peer{}, nil
	}
	i := rh.r.Intn(len(available))
	return available[i], nil
}

// Add adds a host:port for a routing key.
func (rh *SimpleRelayHosts) Add(service, hostPort string) {
	rh.AddPeer(service, hostPort, "", "")
}

// AddPeer adds a host:port with all the associated Peer information.
func (rh *SimpleRelayHosts) AddPeer(service, hostPort, pool, zone string) {
	rh.Lock()
	defer rh.Unlock()

	rh.peers[service] = append(rh.peers[service], relay.Peer{
		HostPort: hostPort,
		Pool:     pool,
		Zone:     zone,
	})
}

// AddError specifies an error to be returned for a given service.
func (rh *SimpleRelayHosts) AddError(service string, err error) {
	rh.Lock()
	defer rh.Unlock()

	rh.errors[service] = err
}

// DisableConnVerification disables nil checks on the relay.Conn passed to Get.
func (rh *SimpleRelayHosts) DisableConnVerification() {
	rh.verifyConn = false
}
