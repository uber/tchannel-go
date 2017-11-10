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

import (
	"time"
)

// FakeTicker mocks time's default Ticker
type FakeTicker struct {
	c chan time.Time
}

// Tick triggers the callback for this ticker
func (ft *FakeTicker) Tick() {
	ft.c <- time.Now()
}

// TryTick returns true if the channel does not block
func (ft *FakeTicker) TryTick() bool {
	select {
	case ft.c <- time.Time{}:
		return true
	default:
		return false
	}
}

// FakeTickerRegistry holds the map of tickers we want to mock in a test
type FakeTickerRegistry struct {
	tickers map[string]*FakeTicker
}

// Tickers returns a new FakeTickerRegistry
func Tickers() *FakeTickerRegistry {
	return &FakeTickerRegistry{
		tickers: map[string]*FakeTicker{},
	}
}

// Fake creates a new fake ticker in the registry
func (r *FakeTickerRegistry) Fake(name string) *FakeTicker {
	ft := &FakeTicker{
		c: make(chan time.Time),
	}
	r.tickers[name] = ft
	return ft
}

// Buffered creates a ticker with a buffered channel
func (r *FakeTickerRegistry) Buffered(name string, bufSize int) *FakeTicker {
	ft := &FakeTicker{
		c: make(chan time.Time, bufSize),
	}
	r.tickers[name] = ft
	return ft
}

// Get is the function to pass to SetTimeTicker when mocking tickers
func (r *FakeTickerRegistry) Get(d time.Duration, name string) *time.Ticker {
	ft := r.tickers[name]
	if ft != nil {
		t := time.NewTicker(time.Hour)
		t.C = ft.c
		return t
	}

	return time.NewTicker(d)
}
