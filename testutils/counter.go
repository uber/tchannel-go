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
	"sync"

	"github.com/uber/tchannel-go/atomic"
)

// Decrement is the interface returned by Decrementor.
type Decrement interface {
	// Single returns whether any more tokens are remaining.
	Single() bool

	// Multiple tries to get n tokens. It returns the actual amount of tokens
	// available to use. If this is 0, it means there are no tokens left.
	Multiple(n int) int
}

type decrementor struct {
	n atomic.Int64
}

func (d *decrementor) Single() bool {
	return d.n.Dec() >= 0
}

func (d *decrementor) Multiple(n int) int {
	decBy := -1 * int64(n)
	decremented := d.n.Add(decBy)
	if decremented <= decBy {
		// Already out of tokens before this decrement.
		return 0
	} else if decremented < 0 {
		// Not enough tokens, return how many tokens we actually could decrement.
		return n + int(decremented)
	}

	return n
}

// Decrementor returns a function that can be called from multiple goroutines and ensures
// it will only return true n times.
func Decrementor(n int) Decrement {
	return &decrementor{
		n: *atomic.NewInt64(int64(n)),
	}
}

// RunN runs the given f n times (and passes the run's index) and waits till they complete.
// It starts n-1 goroutines, and runs one instance in the current goroutine.
func RunN(n int, f func(i int)) {
	var wg sync.WaitGroup
	for i := 0; i < n-1; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f(i)
		}(i)
	}
	f(n - 1)
	wg.Wait()
}
