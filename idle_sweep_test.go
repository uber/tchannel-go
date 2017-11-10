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

func doubleTick(t1, t2 *testutils.FakeTicker) {
	// By calling Tick() twice we ensure that the first one completed, since the
	// send channel will not allow another message in before the first was
	// processed.
	t1.Tick()
	t2.Tick()
	t1.Tick()
	t2.Tick()
}

// Validates that closing connections from the IdleSweep goroutine isn't causing
// any deadlocks.
func TestTickerBasedSweep(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	serverTickers := testutils.Tickers()
	serverTicker := serverTickers.Fake("idle sweep")
	clientTickers := testutils.Tickers()
	clientTicker := clientTickers.Fake("idle sweep")
	clock := testutils.NewStubClock(time.Now())
	connectionEstablished := make(chan struct{})

	serverOpts := testutils.NewOpts().
		SetTimeTicker(serverTickers.Get).
		SetTimeNow(clock.Now).
		NoRelay()

	clientOpts := testutils.NewOpts().
		SetTimeTicker(clientTickers.Get).
		SetTimeNow(clock.Now)

	testutils.WithTestServer(t, serverOpts, func(ts *testutils.TestServer) {
		ts.RegisterFunc("echo", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			close(connectionEstablished)
			return &raw.Res{}, nil
		})

		client := ts.NewClient(clientOpts)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)
		<-connectionEstablished

		// Both server and client now have an active connection. After 3 minutes they
		// should be cleared out by the idle sweep.
		for i := 0; i < 2; i++ {
			clock.Elapse(1 * time.Minute)
			ts.Server().Logger().Info("TICK")
			doubleTick(serverTicker, clientTicker)

			assert.Equal(t, 1, numConnections(ts.Server()))
			assert.Equal(t, 1, numConnections(client))
		}

		clock.Elapse(90 * time.Second)
		doubleTick(serverTicker, clientTicker)

		assert.Equal(t, 0, numConnections(ts.Server()))
		assert.Equal(t, 0, numConnections(client))
	})
}

/*
// Validates that pings do not keep the connection alive.
func TestIdleSweepWithPings(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	tickers := testutils.Tickers()
	ft := tickers.Fake("idle sweep")
	clock := testutils.NewStubClock(time.Now())

	opts := testutils.NewOpts().
		SetTimeTicker(tickers.Get).
		SetTimeNow(clock.Now)

	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		ts.RegisterFunc("echo", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			return &raw.Res{}, nil
		})

		client := ts.NewClient(nil)
		raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)

		for i := 0; i < 2; i++ {
			clock.Elapse(1 * time.Minute)
			client.Ping(ctx, ts.HostPort())
			doubleTick(ft)

			assert.Equal(t, 1, numConnections(ts.Server()))
			assert.Equal(t, 1, numConnections(client))
		}

		clock.Elapse(90 * time.Second)
		doubleTick(ft)

		assert.Equal(t, 0, numConnections(ts.Server()))
		assert.Equal(t, 0, numConnections(client))
	})
}
*/
