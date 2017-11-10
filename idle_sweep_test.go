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
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func numConnections(ch *Channel) int {
	rootPeers := ch.RootPeers().Copy()
	count := 0

	for _, peer := range rootPeers {
		in, out := peer.NumConnections()

		ch.Logger().Infof("numConnections IN: %d  OUT: %d", in, out)
		count += in + out
	}

	ch.Logger().Infof("numConnections TOTAL: %d", count)
	return count
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
		ts.RegisterFunc("echo", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			return &raw.Res{}, nil
		})

		client := ts.NewClient(clientOpts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		// Both server and client now have an active connection. After 3 minutes they
		// should be cleared out by the idle sweep.
		for i := 0; i < 2; i++ {
			clock.Elapse(1 * time.Minute)

			serverTicker.Tick()
			time.Sleep(testutils.Timeout(10 * time.Millisecond))

			assert.Equal(t, 1, numConnections(ts.Server()))
			assert.Equal(t, 1, numConnections(client))
		}

		clock.Elapse(90 * time.Second)
		serverTicker.Tick()

		// Allow some time for the disconnect to propagate
		time.Sleep(testutils.Timeout(10 * time.Millisecond))

		assert.Equal(t, 0, numConnections(ts.Server()))
		assert.Equal(t, 0, numConnections(client))
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
		ts.RegisterFunc("echo", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			return &raw.Res{}, nil
		})

		client := ts.NewClient(clientOpts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		// Both server and client now have an active connection. After 3 minutes they
		// should be cleared out by the idle sweep.
		clientTicker.Tick()
		time.Sleep(testutils.Timeout(10 * time.Millisecond))

		assert.Equal(t, 1, numConnections(ts.Server()))
		assert.Equal(t, 1, numConnections(client))

		clock.Elapse(180 * time.Second)
		clientTicker.Tick()

		// Allow some time for the disconnect to propagate
		time.Sleep(testutils.Timeout(10 * time.Millisecond))

		assert.Equal(t, 0, numConnections(ts.Server()))
		assert.Equal(t, 0, numConnections(client))
	})
}

// Validates that a relay also disconnects idle connections - both inbound and
// outbound.
func TestRelayBasedSweep(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	relayTicker := testutils.NewFakeTicker()
	clock := testutils.NewStubClock(time.Now())

	serverOpts := testutils.NewOpts().
		SetTimeNow(clock.Now).
		SetRelayOptionsFn(func(relayOpts *testutils.ChannelOpts) {
			relayOpts.
				SetTimeTicker(relayTicker.New).
				SetMaxIdleTime(3 * time.Minute).
				SetIdleCheckInterval(30 * time.Second)
		}).
		SetRelayOnly()

	clientOpts := testutils.NewOpts().
		SetTimeNow(clock.Now)

	testutils.WithTestServer(t, serverOpts, func(ts *testutils.TestServer) {
		ts.RegisterFunc("echo", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			return &raw.Res{}, nil
		})

		client := ts.NewClient(clientOpts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		relayTicker.Tick()
		time.Sleep(testutils.Timeout(10 * time.Millisecond))

		assert.Equal(t, 2, numConnections(ts.Relay()))
		assert.Equal(t, 1, numConnections(ts.Server()))
		assert.Equal(t, 1, numConnections(client))

		// The relay will drop both sides of the connection after 3 minutes of inactivity.
		clock.Elapse(180 * time.Second)
		relayTicker.Tick()
		time.Sleep(testutils.Timeout(10 * time.Millisecond))

		assert.Equal(t, 0, numConnections(ts.Relay()))
		assert.Equal(t, 0, numConnections(ts.Server()))
		assert.Equal(t, 0, numConnections(client))
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
		ts.RegisterFunc("echo", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			return &raw.Res{}, nil
		})

		client := ts.NewClient(clientOpts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		// Generate pings every minute.
		for i := 0; i < 2; i++ {
			clock.Elapse(60 * time.Second)
			client.Ping(ctx, ts.HostPort())

			clientTicker.Tick()
			time.Sleep(testutils.Timeout(10 * time.Millisecond))

			assert.Equal(t, 1, numConnections(ts.Server()))
			assert.Equal(t, 1, numConnections(client))
		}

		clock.Elapse(60 * time.Second)
		clientTicker.Tick()
		time.Sleep(testutils.Timeout(10 * time.Millisecond))

		// Connections should still drop, regardless of the ping.
		assert.Equal(t, 0, numConnections(ts.Server()))
		assert.Equal(t, 0, numConnections(client))
	})
}
