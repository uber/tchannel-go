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
	"sync"
	"time"
)

const (
	// defaultMaxIdleTime is the duration a connection is allowed to remain idle
	// before it is automatically closed.
	defaultMaxIdleTime = 3 * time.Minute

	// defaultIdleCheckInterval is the frequency in which the channel checks for
	// idle connections.
	defaultIdleCheckInterval = 30 * time.Second
)

// IdleSweep controls a periodic task that looks for idle connections and clears
// them from the peer list.
// NOTE: This struct is not thread-safe on its own. Calls to Start() and Stop()
// should be guarded by locking ch.mutable
type IdleSweep struct {
	ch                *Channel
	maxIdleTime       time.Duration
	idleCheckInterval time.Duration
	stopCh            chan struct{}
	wg                sync.WaitGroup
	started           bool
}

// newIdleSweep starts a timer that checks for idle connections at given
// intervals.
func newIdleSweep(ch *Channel, opts *ChannelOptions) *IdleSweep {
	maxIdleTime := defaultMaxIdleTime
	if opts.MaxIdleTime != nil {
		maxIdleTime = *opts.MaxIdleTime
	}

	idleCheckInterval := defaultIdleCheckInterval
	if opts.IdleCheckInterval != nil {
		idleCheckInterval = *opts.IdleCheckInterval
	}

	is := &IdleSweep{
		ch:                ch,
		maxIdleTime:       maxIdleTime,
		idleCheckInterval: idleCheckInterval,
		stopCh:            make(chan struct{}),
		started:           false,
	}

	return is
}

// Start runs the goroutine responsible for checking idle connections.
func (is *IdleSweep) Start() {
	if is.started {
		return
	}

	is.ch.log.Info("Starting idle connections timer")

	is.started = true
	is.wg.Add(1)
	go is.timerLoop()
}

// Stop kills the timer checking for idle connections.
func (is *IdleSweep) Stop() {
	if !is.started {
		return
	}

	is.ch.log.Info("Stopping idle connections timer")

	is.started = false
	is.stopCh <- struct{}{}
	//is.wg.Wait()
	is.ch.log.Info("Idle connections timer stopped")
}

func (is *IdleSweep) timerLoop() {
	defer is.wg.Done()
	ticker := is.ch.timeTicker(is.idleCheckInterval, "idle sweep")

	for {
		select {
		case <-ticker.C:
			is.checkIdleConnections()
		case <-is.stopCh:
			ticker.Stop()
			return
		}
	}
}

func (is *IdleSweep) checkIdleConnections() {
	// Make a copy of the peer list to reduce the time we keep the lock.
	rootPeers := is.ch.RootPeers().Copy()
	now := is.ch.timeNow()

	for hostPort, peer := range rootPeers {
		// Check idle time on both inbound and outbound connections.
		for _, conn := range *peer.connectionsFor(inbound) {
			if now.Sub(conn.lastActivity) >= is.maxIdleTime {
				is.ch.log.WithFields(
					LogField{"hostPort", hostPort}).Info("Closing idle inbound connection")
				conn.close(LogField{"reason", "Idle connection closed"})
			}
		}

		for _, conn := range *peer.connectionsFor(outbound) {
			if now.Sub(conn.lastActivity) >= is.maxIdleTime {
				is.ch.log.WithFields(
					LogField{"hostPort", hostPort}).Info("Closing idle outbound connection")
				conn.close(LogField{"reason", "Idle connection closed"})
			}
		}
	}
}
