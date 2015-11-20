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

package tchannel_test

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"
)

func TestGetPeerNoPeer(t *testing.T) {
	ch := testutils.NewClient(t, nil)
	peer, err := ch.Peers().Get(nil)
	assert.Equal(t, ErrNoPeers, err, "Empty peer list should return error")
	assert.Nil(t, peer, "should not return peer")
}

func TestGetPeerSinglePeer(t *testing.T) {
	ch := testutils.NewClient(t, nil)
	ch.Peers().Add("1.1.1.1:1234")

	peer, err := ch.Peers().Get(nil)
	assert.NoError(t, err, "peer list should return contained element")
	assert.Equal(t, "1.1.1.1:1234", peer.HostPort(), "returned peer mismatch")
}

func TestGetPeerAvoidPrevSelected(t *testing.T) {
	const (
		peer1 = "1.1.1.1:1"
		peer2 = "2.2.2.2:2"
		peer3 = "3.3.3.3:3"
	)

	ch := testutils.NewClient(t, nil)
	a, m := testutils.StrArray, testutils.StrMap
	tests := []struct {
		peers        []string
		prevSelected map[string]struct{}
		expected     map[string]struct{}
	}{
		{
			peers:    a(peer1),
			expected: m(peer1),
		},
		{
			peers:        a(peer1, peer2),
			prevSelected: m(peer1),
			expected:     m(peer2),
		},
		{
			peers:        a(peer1, peer2, peer3),
			prevSelected: m(peer1, peer2),
			expected:     m(peer3),
		},
		{
			peers:        a(peer1),
			prevSelected: m(peer1),
			expected:     m(peer1),
		},
		{
			peers:        a(peer1, peer2, peer3),
			prevSelected: m(peer1, peer2, peer3),
			expected:     m(peer1, peer2, peer3),
		},
	}

	for i, tt := range tests {
		peers := ch.GetSubChannel(fmt.Sprintf("test-%d", i), Isolated).Peers()
		for _, p := range tt.peers {
			peers.Add(p)
		}

		gotPeer, err := peers.Get(tt.prevSelected)
		if err != nil {
			t.Errorf("Got unexpected error selecting peer: %v", err)
			continue
		}

		got := gotPeer.HostPort()
		if _, ok := tt.expected[got]; !ok {
			t.Errorf("Got unexpected peer, expected one of %v got %v\n  Peers = %v PrevSelected = %v",
				tt.expected, got, tt.peers, tt.prevSelected)
		}
	}
}

func TestInboundEphemeralPeerRemoved(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	WithVerifiedServer(t, nil, func(ch *Channel, hostPort string) {
		client := testutils.NewClient(t, nil)
		assert.NoError(t, client.Ping(ctx, hostPort), "Ping to server failed")

		// Server should have a host:port in the root peers for the client.
		var clientHP string
		peers := ch.RootPeers().Copy()
		for k := range peers {
			clientHP = k
		}

		// Close the connection, which should remove the peer from the server channel.
		client.Close()
		runtime.Gosched()
		assert.Equal(t, ChannelClosed, client.State(), "Client should be closed")

		// Wait for the channel to see the connection as closed and update the peer list.
		time.Sleep(time.Millisecond)

		_, ok := ch.RootPeers().Get(clientHP)
		assert.False(t, ok, "server's root peers should remove peer for client on close")
	})
}

func TestOutboundPeerNotAdded(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	WithVerifiedServer(t, nil, func(server *Channel, hostPort string) {
		server.Register(raw.Wrap(newTestHandler(t)), "echo")

		ch := testutils.NewClient(t, nil)
		defer ch.Close()

		ch.Ping(ctx, hostPort)
		raw.Call(ctx, ch, hostPort, server.PeerInfo().ServiceName, "echo", nil, nil)

		peer, err := ch.Peers().Get(nil)
		assert.Equal(t, ErrNoPeers, err, "Ping should not add peers")
		assert.Nil(t, peer, "Expected no peer to be returned")
	})
}

func TestPeerSelectionPreferIncoming(t *testing.T) {
	var allChannels []*Channel

	ctx, cancel := NewContext(time.Second)
	defer cancel()

	WithVerifiedServer(t, nil, func(ch *Channel, hostPort string) {
		expected := make(map[string]bool)

		// 5 peers that make incoming connections to ch.
		for i := 0; i < 5; i++ {
			incoming, _, incomingHP := NewServer(t, &testutils.ChannelOpts{ServiceName: fmt.Sprintf("hyperbahn-%d", i)})
			allChannels = append(allChannels, incoming)
			assert.NoError(t, incoming.Ping(ctx, ch.PeerInfo().HostPort), "Ping failed")
			ch.Peers().Add(incomingHP)
			expected[incomingHP] = true
		}

		// 5 random peers that don't have any connections.
		for i := 0; i < 5; i++ {
			ch.Peers().Add(fmt.Sprintf("1.1.1.1:1%d", i))
		}

		// 5 random peers that we have outgoing connections to.
		for i := 0; i < 5; i++ {
			outgoing, _, outgoingHP := NewServer(t, &testutils.ChannelOpts{ServiceName: fmt.Sprintf("outgoing-%d", i)})
			allChannels = append(allChannels, outgoing)
			assert.NoError(t, ch.Ping(ctx, outgoingHP), "Ping failed")
			ch.Peers().Add(outgoingHP)
		}

		// Now select peers in parallel
		selected := make([]string, 1000)
		var selectedIndex int32
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 100; i++ {
					peer, err := ch.Peers().Get(nil)
					if assert.NoError(t, err, "Peers.Get failed") {
						selected[int(atomic.AddInt32(&selectedIndex, 1))-1] = peer.HostPort()
					}
				}
			}()
		}
		wg.Wait()

		for _, v := range selected {
			assert.True(t, expected[v], "Peers.Get got unexpected peer: %v", v)
		}
	})

	// Clean up allChannels
	for _, c := range allChannels {
		c.Close()
	}
}

type peerTest struct {
	t        testing.TB
	channels []*Channel
}

// NewService will return a new server channel and the host port.
func (pt *peerTest) NewService(t testing.TB, svcName string) (*Channel, string) {
	ch := testutils.NewServer(t, &testutils.ChannelOpts{ServiceName: svcName})
	pt.channels = append(pt.channels, ch)
	return ch, ch.PeerInfo().HostPort
}

// CleanUp will clean up all channels started as part of the peer test.
func (pt *peerTest) CleanUp() {
	for _, ch := range pt.channels {
		ch.Close()
	}
}

type peerSelectionTest struct {
	peerTest

	// numPeers is the number of peers added to the channel
	numPeers int
	// numCalls is the number of calls to make concurrently.
	numCalls int
	// numAffinity is the number of affinity nodes.
	numAffinity int
	// numConcurrent is the number of concurrent gocoroutine.
	numConcurrent int
	// hasInboundCall is the bool flag to tell whether to have inbound calls from affinity nodes
	hasInboundCall bool

	servers  []*Channel
	affinity []*Channel
	client   *Channel
}

// setupServers will create numPeer servers, and register handlers on them.
func (pt *peerSelectionTest) setupServers(t testing.TB) {
	pt.servers = make([]*Channel, pt.numPeers)

	// Set up numPeers servers.
	for i := 0; i < pt.numPeers; i++ {
		pt.servers[i], _ = pt.NewService(t, "hyperbahn")
		pt.servers[i].Register(raw.Wrap(newTestHandler(pt.t)), "echo")
	}
}

func (pt *peerSelectionTest) setupAffinity(t testing.TB) {
	var wg sync.WaitGroup
	pt.affinity = make([]*Channel, pt.numAffinity)
	for i := range pt.affinity {
		pt.affinity[i] = pt.servers[i]
	}

	wg.Add(pt.numAffinity)
	// Connect from the affinity nodes to the service.
	for _, affinity := range pt.affinity {
		affinity.Peers().Add(pt.client.PeerInfo().HostPort)
		go func(affinity *Channel) {
			pt.makeCall(affinity.GetSubChannel(pt.client.PeerInfo().ServiceName))
			wg.Done()
		}(affinity)
	}
	wg.Wait()
}

func (pt *peerSelectionTest) setupClient(t testing.TB) {
	pt.client, _ = pt.NewService(t, "client")
	pt.client.Register(raw.Wrap(newTestHandler(pt.t)), "echo")
	for _, server := range pt.servers {
		pt.client.Peers().Add(server.PeerInfo().HostPort)
	}
}

func (pt *peerSelectionTest) makeCall(sc *SubChannel) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()
	_, _, _, err := raw.CallSC(ctx, sc, "echo", nil, nil)
	require.NoError(pt.t, err, "raw.Call failed")
}

func (pt *peerSelectionTest) runStressSimple(b *testing.B) {
	var wg sync.WaitGroup

	// helper that will make a request every n ticks.
	reqEveryNTicks := func(n int, sc *SubChannel) {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			pt.makeCall(sc)
		}
	}

	wg.Add(pt.numConcurrent)

	// server outbound request
	sc := pt.client.GetSubChannel("hyperbahn")
	for i := 0; i < pt.numConcurrent; i++ {
		go reqEveryNTicks(1, sc)
	}

	wg.Wait()
}

// Run these commands before run the benchmark.
// sudo sysctl -w kern.maxfiles=50000
// ulimit -n 50000
func BenchmarkSimplePeersHeapPerf(b *testing.B) {

	pt := &peerSelectionTest{
		peerTest:      peerTest{t: b},
		numPeers:      1000,
		numConcurrent: 100,
	}
	defer pt.CleanUp()

	pt.setupServers(b)
	pt.setupClient(b)
	pt.setupAffinity(b)
	b.ResetTimer()
	pt.runStressSimple(b)
}
