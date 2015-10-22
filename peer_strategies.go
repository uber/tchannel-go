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
	"time"
)

type scoreCalculator interface {
	GetScore(p *Peer) uint64
}

type randCalculator struct {
	rng *rand.Rand
}

func (r *randCalculator) GetScore(p *Peer) uint64 {
	return uint64(r.rng.Int63())
}

func newRandCalculator() *randCalculator {
	return &randCalculator{rng: NewRand(time.Now().UnixNano())}
}

type preferIncomingCalculator struct {
}

func newPreferIncomingCalculator() *preferIncomingCalculator {
	return &preferIncomingCalculator{}
}

func (r *preferIncomingCalculator) GetScore(p *Peer) uint64 {
	return 0
}
