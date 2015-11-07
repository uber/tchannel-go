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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/stretchr/testify/assert"
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
