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
	"github.com/stretchr/testify/require"
)

// peerStatusListener is a test tool used to wait for connections to drop by
// listening to status events from a channel.
type peerStatusListener struct {
	changes chan struct{}
}

func newPeerStatusListener() *peerStatusListener {
	return &peerStatusListener{
		changes: make(chan struct{}, 10),
	}
}

func (pl *peerStatusListener) onStatusChange(p *Peer) {
	pl.changes <- struct{}{}
}

func (pl *peerStatusListener) waitForZeroConnections(t testing.TB, channels ...*Channel) bool {
	for {
		select {
		case <-pl.changes:
			if allConnectionsClosed(channels) {
				return true
			}

		case <-time.After(testutils.Timeout(500 * time.Millisecond)):
			t.Fatalf("Some connections are still open: %s", connectionStatus(channels))
			return false
		}
	}
}

func allConnectionsClosed(channels []*Channel) bool {
	for _, ch := range channels {
		if numConnections(ch) != 0 {
			return false
		}
	}

	return true
}

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

// Validates that inbound idle connections are dropped.
func TestServerBasedSweep(t *testing.T) {
	listener := newPeerStatusListener()
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	serverTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	serverOpts := testutils.NewOpts().
		SetTimeTicker(serverTicker.New).
		SetIdleCheckInterval(30 * time.Second).
		SetMaxIdleTime(3 * time.Minute).
		SetOnPeerStatusChanged(listener.onStatusChange).
		SetTimeNow(clock.Now).
		NoRelay()

	clientOpts := testutils.NewOpts().
		SetOnPeerStatusChanged(listener.onStatusChange)

	testutils.WithTestServer(t, serverOpts, func(t testing.TB, ts *testutils.TestServer) {
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
		listener.waitForZeroConnections(t, ts.Server(), client)
	})
}

// Validates that outbound idle connections are dropped.
func TestClientBasedSweep(t *testing.T) {
	listener := newPeerStatusListener()
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	clientTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	serverOpts := testutils.NewOpts().
		SetOnPeerStatusChanged(listener.onStatusChange).
		NoRelay()

	clientOpts := testutils.NewOpts().
		SetTimeNow(clock.Now).
		SetTimeTicker(clientTicker.New).
		SetMaxIdleTime(3 * time.Minute).
		SetOnPeerStatusChanged(listener.onStatusChange).
		SetIdleCheckInterval(30 * time.Second)

	testutils.WithTestServer(t, serverOpts, func(t testing.TB, ts *testutils.TestServer) {
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
		listener.waitForZeroConnections(t, ts.Server(), client)
	})
}

// Validates that a relay also disconnects idle connections - both inbound and
// outbound.
func TestRelayBasedSweep(t *testing.T) {
	listener := newPeerStatusListener()
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	relayTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	opts := testutils.NewOpts().
		SetOnPeerStatusChanged(listener.onStatusChange)

	relayOpts := testutils.NewOpts().
		SetTimeNow(clock.Now).
		SetTimeTicker(relayTicker.New).
		SetMaxIdleTime(3 * time.Minute).
		SetIdleCheckInterval(30 * time.Second).
		SetOnPeerStatusChanged(listener.onStatusChange).
		SetRelayOnly()

	testutils.WithTestServer(t, relayOpts, func(t testing.TB, ts *testutils.TestServer) {
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
		listener.waitForZeroConnections(t, ts.Relay(), server, client)
	})
}

// Validates that pings do not keep the connection alive.
func TestIdleSweepWithPings(t *testing.T) {
	listener := newPeerStatusListener()
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	clientTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	serverOpts := testutils.NewOpts().
		SetOnPeerStatusChanged(listener.onStatusChange).
		NoRelay()

	clientOpts := testutils.NewOpts().
		SetTimeNow(clock.Now).
		SetTimeTicker(clientTicker.New).
		SetMaxIdleTime(3 * time.Minute).
		SetIdleCheckInterval(30 * time.Second).
		SetOnPeerStatusChanged(listener.onStatusChange)

	testutils.WithTestServer(t, serverOpts, func(t testing.TB, ts *testutils.TestServer) {
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
		listener.waitForZeroConnections(t, ts.Server(), client)
	})
}

// Validates that when MaxIdleTime isn't set, NewChannel returns an error.
func TestIdleSweepMisconfiguration(t *testing.T) {
	ch, err := NewChannel("svc", &ChannelOptions{
		IdleCheckInterval: time.Duration(30 * time.Second),
	})

	assert.Nil(t, ch, "NewChannel should not return a channel")
	assert.Error(t, err, "NewChannel should fail")
}

func TestIdleSweepIgnoresConnectionsWithCalls(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	clientTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	listener := newPeerStatusListener()
	// TODO: Log filtering doesn't require the message to be seen.
	serverOpts := testutils.NewOpts().
		AddLogFilter("Skip closing idle Connection as it has pending calls.", 2).
		SetOnPeerStatusChanged(listener.onStatusChange).
		SetTimeNow(clock.Now).
		SetTimeTicker(clientTicker.New).
		SetRelayMaxTimeout(time.Hour).
		SetMaxIdleTime(3 * time.Minute).
		SetIdleCheckInterval(30 * time.Second)

	testutils.WithTestServer(t, serverOpts, func(t testing.TB, ts *testutils.TestServer) {
		var (
			gotCall = make(chan struct{})
			block   = make(chan struct{})
		)
		testutils.RegisterEcho(ts.Server(), func() {
			close(gotCall)
			<-block
		})

		clientOpts := testutils.NewOpts().SetOnPeerStatusChanged(listener.onStatusChange)

		// Client 1 will just ping, so we create a connection that should be closed.
		c1 := ts.NewClient(clientOpts)
		require.NoError(t, c1.Ping(ctx, ts.HostPort()), "Ping failed")

		// Client 2 will make a call that will be blocked. Wait for the call to be received.
		c2CallComplete := make(chan struct{})
		c2 := ts.NewClient(clientOpts)
		go func() {
			testutils.AssertEcho(t, c2, ts.HostPort(), ts.ServiceName())
			close(c2CallComplete)
		}()
		<-gotCall

		// If we are in no-relay mode, we expect 2 connections to the server (from each client).
		// If we are in relay mode, the relay will have the 2 connections from clients + 1 connection to the server.
		check := struct {
			ch            *Channel
			preCloseConns int
			tick          func()
		}{
			ch:            ts.Server(),
			preCloseConns: 2,
			tick: func() {
				clock.Elapse(5 * time.Minute)
				clientTicker.Tick()
			},
		}

		if relay := ts.Relay(); relay != nil {
			check.ch = relay
			check.preCloseConns++
			oldTick := check.tick
			check.tick = func() {
				oldTick()

				// The same ticker is being used by the server and the relay
				// so we need to tick it twice.
				clientTicker.Tick()
			}
		}

		assert.Equal(t, check.preCloseConns, check.ch.IntrospectNumConnections(), "Expect connection to client 1 and client 2")

		// Let the idle checker close client 1's connection.
		check.tick()
		listener.waitForZeroConnections(t, c1)

		// Make sure we have only a connection for client 2, which is active.
		assert.Equal(t, check.preCloseConns-1, check.ch.IntrospectNumConnections(), "Expect connection only to client 2")
		state := check.ch.IntrospectState(nil)
		require.Empty(t, state.InactiveConnections, "Ensure all connections are active")

		// Unblock the call.
		close(block)
		<-c2CallComplete
		check.tick()
		listener.waitForZeroConnections(t, ts.Server(), c2)
	})
}
