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
	"testing"

	. "github.com/uber/tchannel-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go/testutils"
)

func TestGetPeerNoPeer(t *testing.T) {
	ch, err := testutils.NewClient(nil)
	require.NoError(t, err, "NewClient failed")

	peer, err := ch.Peers().Get(nil)
	assert.Equal(t, ErrNoPeers, err, "Empty peer list should return error")
	assert.Nil(t, peer, "should not return peer")
}

func TestGetPeerSinglePeer(t *testing.T) {
	ch, err := testutils.NewClient(nil)
	require.NoError(t, err, "NewClient failed")

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

	ch, err := testutils.NewClient(nil)
	require.NoError(t, err, "NewClient failed")

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
