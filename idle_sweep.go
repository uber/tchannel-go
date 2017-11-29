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
	"time"
)

// idleSweep controls a periodic task that looks for idle connections and clears
// them from the peer list.
// NOTE: This struct is not thread-safe on its own. Calls to Start() and Stop()
// should be guarded by locking ch.mutable
type idleSweep struct {
	ch                *Channel
	maxIdleTime       time.Duration
	idleCheckInterval time.Duration
	stopCh            chan struct{}
	started           bool
}

// newIdleSweep starts a poller that checks for idle connections at given
// intervals.
func newIdleSweep(ch *Channel, opts *ChannelOptions) *idleSweep {
	is := &idleSweep{
		ch:                ch,
		maxIdleTime:       opts.MaxIdleTime,
		idleCheckInterval: opts.IdleCheckInterval,
		started:           false,
	}

	return is
}

// Start runs the goroutine responsible for checking idle connections.
func (is *idleSweep) Start() {
	if is.started || is.idleCheckInterval <= time.Duration(0) {
		return
	}

	if is.maxIdleTime <= time.Duration(0) {
		is.ch.log.Warn("To enable automatically removing idle connections, you must " +
			"set both IdleCheckInterval and MaxIdleTime.")
		return
	}

	is.ch.log.WithFields(
		LogField{"idleCheckInterval", is.idleCheckInterval},
		LogField{"maxIdleTime", is.maxIdleTime},
	).Info("Starting idle connections poller.")

	is.stopCh = make(chan struct{})
	is.started = true
	go is.pollerLoop()
}

// Stop kills the poller checking for idle connections.
func (is *idleSweep) Stop() {
	if !is.started {
		return
	}

	is.ch.log.Info("Stopping idle connections poller.")

	is.started = false
	close(is.stopCh)
	is.ch.log.Info("Idle connections poller stopped.")
}

func (is *idleSweep) pollerLoop() {
	ticker := is.ch.timeTicker(is.idleCheckInterval)

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

func (is *idleSweep) checkIdleConnections() {
	now := is.ch.timeNow()

	// Acquire the read lock and examine which connections are idle.
	idleConnections := make([]*Connection, 0, 10)
	is.ch.mutable.RLock()

	for _, conn := range is.ch.mutable.conns {
		idleTime := now.Sub(conn.getLastActivityTime())
		if idleTime >= is.maxIdleTime {
			idleConnections = append(idleConnections, conn)
		}
	}

	is.ch.mutable.RUnlock()

	for _, conn := range idleConnections {
		is.ch.log.WithFields(
			LogField{"remotePeer", conn.remotePeerInfo}).Info("Closing idle inbound connection.")
		conn.close(LogField{"reason", "Idle connection closed"})
	}
}
