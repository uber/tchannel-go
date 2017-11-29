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

package tchannel_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
)

func numConnections(ch *Channel) int {
	rootPeers := ch.RootPeers().Copy()
	count := 0

	for _, peer := range rootPeers {
		in, out := peer.NumConnections()
		count += in + out
	}

	return count
}

func connectionStatus(channels []*Channel) string {
	status := make([]string, 0)
	for _, ch := range channels {

		status = append(status,
			fmt.Sprintf("%s: %d open", ch.PeerInfo().ProcessName, numConnections(ch)))
	}

	return strings.Join(status, ", ")
}

func waitForZeroConnections(t *testing.T, channels ...*Channel) bool {
	return assert.True(t, testutils.WaitFor(10*time.Millisecond, func() bool {
		for _, ch := range channels {
			if numConnections(ch) != 0 {
				return false
			}
		}

		return true
	}), "Some connections are still open: %s", connectionStatus(channels))
}

// Validates that inbound idle connections are dropped.
func TestServerBasedSweep(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	serverTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	serverOpts := testutils.NewOpts().
		SetTimeTicker(serverTicker.New).
		SetIdleCheckInterval(30 * time.Second).
		SetMaxIdleTime(3 * time.Minute).
		SetTimeNow(clock.Now).
		NoRelay()

	clientOpts := testutils.NewOpts().
		SetTimeNow(clock.Now)

	testutils.WithTestServer(t, serverOpts, func(ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)

		client := ts.NewClient(clientOpts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		// Both server and client now have an active connection. After 3 minutes they
		// should be cleared out by the idle sweep.
		for i := 0; i < 2; i++ {
			clock.Elapse(1 * time.Minute)

			serverTicker.Tick()

			assert.Equal(t, 1, numConnections(ts.Server()))
			assert.Equal(t, 1, numConnections(client))
		}

		// Move the clock forward and trigger the idle poller.
		clock.Elapse(90 * time.Second)
		serverTicker.Tick()
		waitForZeroConnections(t, ts.Server(), client)
	})
}

// Validates that outbound idle connections are dropped.
func TestClientBasedSweep(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	clientTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	serverOpts := testutils.NewOpts().
		SetTimeNow(clock.Now).
		NoRelay()

	clientOpts := testutils.NewOpts().
		SetTimeNow(clock.Now).
		SetTimeTicker(clientTicker.New).
		SetMaxIdleTime(3 * time.Minute).
		SetIdleCheckInterval(30 * time.Second)

	testutils.WithTestServer(t, serverOpts, func(ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)

		client := ts.NewClient(clientOpts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		// Both server and client now have an active connection. After 3 minutes they
		// should be cleared out by the idle sweep.
		clientTicker.Tick()

		assert.Equal(t, 1, numConnections(ts.Server()))
		assert.Equal(t, 1, numConnections(client))

		// Move the clock forward and trigger the idle poller.
		clock.Elapse(180 * time.Second)
		clientTicker.Tick()
		waitForZeroConnections(t, ts.Server(), client)
	})
}

// Validates that a relay also disconnects idle connections - both inbound and
// outbound.
func TestRelayBasedSweep(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	relayTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	opts := testutils.NewOpts().
		SetTimeNow(clock.Now)

	relayOpts := testutils.NewOpts().
		SetTimeNow(clock.Now).
		SetTimeTicker(relayTicker.New).
		SetMaxIdleTime(3 * time.Minute).
		SetIdleCheckInterval(30 * time.Second).
		SetRelayOnly()

	testutils.WithTestServer(t, relayOpts, func(ts *testutils.TestServer) {
		// Replace the auto-created server with a new one that doesn't have
		// an idle-connection poller.
		ts.Relay().GetSubChannel(ts.ServiceName()).Peers().Remove(
			ts.Server().PeerInfo().HostPort)
		server := ts.NewServer(opts)
		testutils.RegisterEcho(server, nil)

		// Make a call to the server via relay, which will establish connections:
		// Client -> Relay -> Server
		client := ts.NewClient(opts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		relayTicker.Tick()

		// Relay has 1 inbound + 1 outbound
		assert.Equal(t, 2, numConnections(ts.Relay()))
		assert.Equal(t, 1, numConnections(server))
		assert.Equal(t, 1, numConnections(client))

		// The relay will drop both sides of the connection after 3 minutes of inactivity.
		clock.Elapse(180 * time.Second)
		relayTicker.Tick()
		waitForZeroConnections(t, ts.Relay(), server, client)
	})
}

// Validates that pings do not keep the connection alive.
func TestIdleSweepWithPings(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	clientTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	serverOpts := testutils.NewOpts().
		SetTimeNow(clock.Now).
		NoRelay()

	clientOpts := testutils.NewOpts().
		SetTimeNow(clock.Now).
		SetTimeTicker(clientTicker.New).
		SetMaxIdleTime(3 * time.Minute).
		SetIdleCheckInterval(30 * time.Second)

	testutils.WithTestServer(t, serverOpts, func(ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)

		client := ts.NewClient(clientOpts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		// Generate pings every minute.
		for i := 0; i < 2; i++ {
			clock.Elapse(60 * time.Second)
			client.Ping(ctx, ts.HostPort())

			clientTicker.Tick()

			assert.Equal(t, 1, numConnections(ts.Server()))
			assert.Equal(t, 1, numConnections(client))
		}

		clock.Elapse(60 * time.Second)
		clientTicker.Tick()

		// Connections should still drop, regardless of the ping.
		waitForZeroConnections(t, ts.Server(), client)
	})
}

// Validates that when MaxIdleTime isn't set, the sweep goroutine doesn't start.
func TestIdleSweepMisconfiguration(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	serverTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	serverOpts := testutils.NewOpts().
		SetTimeTicker(serverTicker.New).
		SetIdleCheckInterval(30*time.Second).
		AddLogFilter("set both IdleCheckInterval and MaxIdleTime", 1).
		SetTimeNow(clock.Now).
		NoRelay()

	clientOpts := testutils.NewOpts().
		SetTimeNow(clock.Now)

	testutils.WithTestServer(t, serverOpts, func(ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)

		client := ts.NewClient(clientOpts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		// Move the clock forward and trigger the idle poller.
		clock.Elapse(300 * time.Second)
		serverTicker.Tick()

		// Connections should still be active.
		assert.Equal(t, 1, numConnections(ts.Server()))
		assert.Equal(t, 1, numConnections(client))
	})
}
