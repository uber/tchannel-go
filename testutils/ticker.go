// Copyright (c) 2017 Uber Technologies, Inc.

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

import "time"

// FakeTicker is a ticker for unit tests that can be controlled
// deterministically.
type FakeTicker struct {
	c chan time.Time
}

// NewFakeTicker returns a new instance of FakeTicker
func NewFakeTicker() *FakeTicker {
	return &FakeTicker{
		c: make(chan time.Time, 1),
	}
}

// Tick sends an immediate tick call to the receiver
func (ft *FakeTicker) Tick() {
	ft.c <- time.Now()
}

// TryTick attempts to send a tick, if the channel isn't blocked.
func (ft *FakeTicker) TryTick() bool {
	select {
	case ft.c <- time.Time{}:
		return true
	default:
		return false
	}
}

// New can be used in tests as a factory method for tickers, by passing it to
// ChannelOptions.TimeTicker
func (ft *FakeTicker) New(d time.Duration) *time.Ticker {
	t := time.NewTicker(time.Hour)
	t.C = ft.c
	return t
}
