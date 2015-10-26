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
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const numPeers = 10

func TestPeerHeap(t *testing.T) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	peerHeap := newPeerHeap()

	peerScores := make([]*peerScore, numPeers)
	minScore := uint64(numPeers)
	for i := 0; i < numPeers; i++ {
		peerScore := newPeerScore(&Peer{})
		peerScore.score = uint64(r.Intn(numPeers * 5))
		peerScores[i] = peerScore
		if peerScore.score < minScore {
			minScore = peerScore.score
		}
	}

	for i := 0; i < numPeers; i++ {
		peerHeap.push(peerScores[i])
	}

	assert.Equal(t, 10, peerHeap.Len(), "Incorrect peer heap numPeers.")
	assert.Equal(t, minScore, peerHeap.peek().score, "PeerHeap's peek value is not minium.")

	lastScore := peerHeap.pop().score
	for i := 1; i < numPeers; i++ {
		assert.Equal(t, numPeers-i, peerHeap.Len(), "Incorrect peer heap numPeers.")
		score := peerHeap.pop().score
		assert.True(t, score >= lastScore, "The order of the heap is invalid.")
		lastScore = score
	}
}
