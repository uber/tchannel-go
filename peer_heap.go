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
	"container/heap"
	"math/rand"
	"time"
)

// PeerHeap maintains a MIN heap of peers based on the peers' score.
type PeerHeap struct {
	PeerScores []*peerScore
	rng        *rand.Rand
}

func newPeerHeap() *PeerHeap {
	return &PeerHeap{rng: NewRand(time.Now().UnixNano())}
}

func (ph PeerHeap) Len() int { return len(ph.PeerScores) }

func (ph PeerHeap) Less(i, j int) bool {

	// We use random to avoid a deterministic round robin which can cause load on the same nodes from multiple clients
	if ph.PeerScores[i].score == ph.PeerScores[j].score {
		// return random true or false when scores are the same.
		return ph.rng.Intn(2) < 1
	}

	// We want Pop to give us the lowest, not highest, score so we use less than here.
	return ph.PeerScores[i].score < ph.PeerScores[j].score
}

func (ph PeerHeap) Swap(i, j int) {
	ph.PeerScores[i], ph.PeerScores[j] = ph.PeerScores[j], ph.PeerScores[i]
	ph.PeerScores[i].index = i
	ph.PeerScores[j].index = j
}

// Push implements heap Push interface
func (ph *PeerHeap) Push(x interface{}) {
	n := len(ph.PeerScores)
	item := x.(*peerScore)
	item.index = n
	ph.PeerScores = append(ph.PeerScores, item)
}

// Pop implements heap Pop interface
func (ph *PeerHeap) Pop() interface{} {
	old := *ph
	n := len(old.PeerScores)
	item := old.PeerScores[n-1]
	item.index = -1 // for safety
	ph.PeerScores = old.PeerScores[:n-1]
	return item
}

func (ph *PeerHeap) update(peer *peerScore) {
	heap.Fix(ph, peer.index)
}

// PopPeer pops the top peer of the heap.
func (ph *PeerHeap) PopPeer() *peerScore {
	return heap.Pop(ph).(*peerScore)
}

// PushPeer pushes the new peer into the heap.
func (ph *PeerHeap) PushPeer(peerScore *peerScore) {
	heap.Push(ph, peerScore)
}

func (ph *PeerHeap) peek() *peerScore {
	return ph.PeerScores[0]
}
