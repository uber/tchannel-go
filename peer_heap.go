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

import "container/heap"

// PeerHeap maintains a MAX heap of peers based on the peers' score.
type PeerHeap []*peerScore

func (ph PeerHeap) Len() int { return len(ph) }

func (ph PeerHeap) Less(i, j int) bool {
	// We want Pop to give us the highest, not lowest, score so we use greater than here.
	return ph[i].score > ph[j].score
}

func (ph PeerHeap) Swap(i, j int) {
	ph[i], ph[j] = ph[j], ph[i]
	ph[i].index = i
	ph[j].index = j
}

// Push implements heap Push interface
func (ph *PeerHeap) Push(x interface{}) {
	n := len(*ph)
	item := x.(*peerScore)
	item.index = n
	*ph = append(*ph, item)
}

// Pop implements heap Pop interface
func (ph *PeerHeap) Pop() interface{} {
	old := *ph
	n := len(old)
	item := old[n-1]
	item.index = -1 // for safety
	*ph = old[0 : n-1]
	return item
}

func (ph *PeerHeap) update(peer *peerScore) {
	heap.Fix(ph, peer.index)
}

func (ph *PeerHeap) pop() *peerScore {
	return heap.Pop(ph).(*peerScore)
}

func (ph *PeerHeap) push(peer *peerScore) {
	heap.Push(ph, peer)
}

func (ph *PeerHeap) peek() *peerScore {
	return (*ph)[0]
}
