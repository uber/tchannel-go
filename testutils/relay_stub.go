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

	. "github.com/uber/tchannel-go"
)

type SimpleRelayHosts struct {
	sync.RWMutex
	r     *rand.Rand
	peers map[string][]string
}

func NewSimpleRelayHosts(peers map[string][]string) *SimpleRelayHosts {
	// Use a known seed for repeatable tests.
	return &SimpleRelayHosts{
		r:     NewRand(1),
		peers: peers,
	}
}

func (rh *SimpleRelayHosts) Get(service string) string {
	rh.RLock()
	defer rh.RUnlock()

	available, ok := rh.peers[service]
	if !ok || len(available) == 0 {
		return ""
	}
	i := rh.r.Intn(len(available))
	return available[i]
}

func (rh *SimpleRelayHosts) Add(service, hostPort string) {
	rh.Lock()
	rh.peers[service] = append(rh.peers[service], hostPort)
	rh.Unlock()
}
