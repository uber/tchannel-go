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
	"strings"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTicker struct {
	c chan time.Time
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{
		c: make(chan time.Time, 1),
	}
}

func (ft *fakeTicker) tick() {
	ft.c <- time.Now()
}

func (ft *fakeTicker) tryTick() bool {
	select {
	case ft.c <- time.Time{}:
		return true
	default:
		return false
	}
}

func (ft *fakeTicker) New(d time.Duration) *time.Ticker {
	t := time.NewTicker(time.Hour)
	t.C = ft.c
	return t
}

func TestHealthCheckStopBeforeStart(t *testing.T) {
	opts := testutils.NewOpts().NoRelay()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {

		var pingCount int
		frameRelay, cancel := testutils.FrameRelay(t, ts.HostPort(), func(outgoing bool, f *Frame) *Frame {
			if strings.Contains(f.Header.String(), "PingRes") {
				pingCount++
			}
			return f
		})
		defer cancel()

		ft := newFakeTicker()
		opts := testutils.NewOpts().
			SetTimeTicker(ft.New).
			SetHealthChecks(HealthCheckOptions{Interval: time.Second})
		client := ts.NewClient(opts)

		ctx, cancel := NewContext(time.Second)
		defer cancel()

		conn, err := client.RootPeers().GetOrAdd(frameRelay).GetConnection(ctx)
		require.NoError(t, err, "Failed to get connection")

		conn.StopHealthCheck()

		// Should be no ping messages sent.
		for i := 0; i < 10; i++ {
			ft.tryTick()
		}
		assert.Equal(t, 0, pingCount, "No pings when health check is stopped")
	})
}

func TestHealthCheckStopNoError(t *testing.T) {
	opts := testutils.NewOpts().NoRelay()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {

		var pingCount int
		frameRelay, cancel := testutils.FrameRelay(t, ts.HostPort(), func(outgoing bool, f *Frame) *Frame {
			if strings.Contains(f.Header.String(), "PingRes") {
				pingCount++
			}
			return f
		})
		defer cancel()

		ft := newFakeTicker()
		opts := testutils.NewOpts().
			SetTimeTicker(ft.New).
			SetHealthChecks(HealthCheckOptions{Interval: time.Second}).
			AddLogFilter("Unexpected ping response.", 1)
		client := ts.NewClient(opts)

		ctx, cancel := NewContext(time.Second)
		defer cancel()

		conn, err := client.RootPeers().GetOrAdd(frameRelay).GetConnection(ctx)
		require.NoError(t, err, "Failed to get connection")

		for i := 0; i < 10; i++ {
			ft.tick()
			waitForNHealthChecks(t, conn, i+1)
		}
		conn.StopHealthCheck()

		// We stop the health check, so the ticks channel is no longer read, so
		// we can't use the synchronous tick here.
		for i := 0; i < 10; i++ {
			ft.tryTick()
		}

		assert.Equal(t, 10, pingCount, "Pings should stop after health check is stopped")
	})
}

func TestHealthCheckIntegration(t *testing.T) {
	tests := []struct {
		msg                 string
		disable             bool
		failuresToClose     int
		pingResponses       []bool
		wantActive          bool
		wantHealthCheckLogs int
	}{
		{
			msg:             "no failures with failuresToClose=0",
			failuresToClose: 1,
			pingResponses:   []bool{true, true, true, true},
			wantActive:      true,
		},
		{
			msg:                 "single failure with failuresToClose=1",
			failuresToClose:     1,
			pingResponses:       []bool{true, false},
			wantActive:          false,
			wantHealthCheckLogs: 1,
		},
		{
			msg:                 "single failure with failuresToClose=2",
			failuresToClose:     2,
			pingResponses:       []bool{true, false, true, false, true},
			wantActive:          true,
			wantHealthCheckLogs: 2,
		},
		{
			msg:                 "up to 2 consecutive failures with failuresToClose=3",
			failuresToClose:     3,
			pingResponses:       []bool{true, false, true, false, true, false, false, true, false, false, true},
			wantActive:          true,
			wantHealthCheckLogs: 6,
		},
		{
			msg:                 "3 consecutive failures with failuresToClose=3",
			failuresToClose:     3,
			pingResponses:       []bool{true, false, true, false, true, false, false, true, false, false, false},
			wantActive:          false,
			wantHealthCheckLogs: 7,
		},
	}

	errFrame := getErrorFrame(t)
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			opts := testutils.NewOpts().NoRelay()
			testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
				var pingCount int
				frameRelay, cancel := testutils.FrameRelay(t, ts.HostPort(), func(outgoing bool, f *Frame) *Frame {
					if strings.Contains(f.Header.String(), "PingRes") {
						success := tt.pingResponses[pingCount]
						pingCount++
						if !success {
							errFrame.Header.ID = f.Header.ID
							f = errFrame
						}
					}
					return f
				})
				defer cancel()

				ft := newFakeTicker()
				opts := testutils.NewOpts().
					SetTimeTicker(ft.New).
					SetHealthChecks(HealthCheckOptions{Interval: time.Second, FailuresToClose: tt.failuresToClose}).
					AddLogFilter("Failed active health check.", uint(tt.wantHealthCheckLogs)).
					AddLogFilter("Unexpected ping response.", 1)
				client := ts.NewClient(opts)

				ctx, cancel := NewContext(time.Second)
				defer cancel()

				conn, err := client.RootPeers().GetOrAdd(frameRelay).GetConnection(ctx)
				require.NoError(t, err, "Failed to get connection")

				for i := 0; i < len(tt.pingResponses); i++ {
					ft.tryTick()

					waitForNHealthChecks(t, conn, i+1)
					assert.Equal(t, tt.pingResponses[:i+1], introspectConn(conn).HealthChecks, "Unexpectd health check history")
				}

				// Once the health check is done, we trigger a Close, it's possible we are still
				// waiting for the connection to close.
				if tt.wantActive == false {
					testutils.WaitFor(time.Second, func() bool { return !conn.IsActive() })
				}
				assert.Equal(t, tt.wantActive, conn.IsActive(), "Connection active mismatch")
			})
		})
	}
}

func waitForNHealthChecks(t *testing.T, conn *Connection, n int) {
	require.True(t, testutils.WaitFor(time.Second, func() bool {
		return len(introspectConn(conn).HealthChecks) >= n
	}), "Failed while waiting for %v health checks", n)
}

func introspectConn(c *Connection) ConnectionRuntimeState {
	return c.IntrospectState(&IntrospectionOptions{})
}
