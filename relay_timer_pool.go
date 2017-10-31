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
package tchannel

import (
	"math"
	"sync"
	"time"
)

type relayTimerTrigger func(items *relayItems, id uint32, isOriginator bool)

type relayTimerPool struct {
	pool    sync.Pool
	trigger relayTimerTrigger
}

type relayTimer struct {
	pool  *relayTimerPool
	timer *time.Timer

	// Per-timer paramters passed back when the timer is triggered.
	items        *relayItems
	id           uint32
	isOriginator bool
}

func (rt *relayTimer) OnTimer() {
	rt.pool.trigger(rt.items, rt.id, rt.isOriginator)
}

func newRelayTimerPool(trigger relayTimerTrigger) *relayTimerPool {
	return &relayTimerPool{
		trigger: trigger,
	}
}

// Get returns a Timer that has not started. Timers must be started explicitly
// using the Start function.
func (tp *relayTimerPool) Get() *relayTimer {
	timer, ok := tp.pool.Get().(*relayTimer)
	if ok {
		return timer
	}

	rt := &relayTimer{
		pool: tp,
	}
	rt.timer = time.AfterFunc(time.Duration(math.MaxInt64), rt.OnTimer)
	if !rt.timer.Stop() {
		panic("failed to stop timer set for 1 hour")
	}
	return rt
}

// Stop stops the timer and returns whether the timer was stopped. It returns
// the same behaviour as https://golang.org/pkg/time/#Timer.Stop.
func (rt *relayTimer) Stop() bool {
	return rt.timer.Stop()
}

// Release releases a timer back to the timer pool. The timer MUST be stopped
// before being released.
func (rt *relayTimer) Release() {
	if rt.Stop() {
		panic("tried to release unstopped timer")
	}
	rt.id = 0
	rt.pool.pool.Put(rt)
}

// Start starts a timer with the given duration for the specified ID.
func (rt *relayTimer) Start(d time.Duration, items *relayItems, id uint32, isOriginator bool) {
	if rt.id != 0 {
		panic("ID mismatch")
	}

	rt.items = items
	rt.id = id
	rt.isOriginator = isOriginator
	rt.timer.Reset(d)
}
