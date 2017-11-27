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

package tchannel_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func tagsForOutboundCall(serverCh *Channel, clientCh *Channel, method string) map[string]string {
	host, _ := os.Hostname()
	return map[string]string{
		"app":             clientCh.PeerInfo().ProcessName,
		"host":            host,
		"service":         clientCh.PeerInfo().ServiceName,
		"target-service":  serverCh.PeerInfo().ServiceName,
		"target-endpoint": method,
	}
}

func tagsForInboundCall(serverCh *Channel, clientCh *Channel, method string) map[string]string {
	host, _ := os.Hostname()
	return map[string]string{
		"app":             serverCh.PeerInfo().ProcessName,
		"host":            host,
		"service":         serverCh.PeerInfo().ServiceName,
		"calling-service": clientCh.PeerInfo().ServiceName,
		"endpoint":        method,
	}
}

// statsHandler increments the server and client timers when handling requests.
type statsHandler struct {
	*testHandler
	clientClock *testutils.StubClock
	serverClock *testutils.StubClock
}

func (h *statsHandler) Handle(ctx context.Context, args *raw.Args) (*raw.Res, error) {
	h.clientClock.Elapse(100 * time.Millisecond)
	h.serverClock.Elapse(70 * time.Millisecond)
	return h.testHandler.Handle(ctx, args)
}

func TestStatsCalls(t *testing.T) {
	defer testutils.SetTimeout(t, 2*time.Second)()

	tests := []struct {
		method  string
		wantErr bool
	}{
		{
			method: "echo",
		},
		{
			method:  "app-error",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		initialTime := time.Date(2015, 2, 1, 10, 10, 0, 0, time.UTC)
		clientClock := testutils.NewStubClock(initialTime)
		serverClock := testutils.NewStubClock(initialTime)
		handler := &statsHandler{
			testHandler: newTestHandler(t),
			clientClock: clientClock,
			serverClock: serverClock,
		}

		clientStats := newRecordingStatsReporter()
		serverStats := newRecordingStatsReporter()
		serverOpts := testutils.NewOpts().
			SetStatsReporter(serverStats).
			SetTimeNow(serverClock.Now)
		WithVerifiedServer(t, serverOpts, func(serverCh *Channel, hostPort string) {
			handler := raw.Wrap(handler)
			serverCh.Register(handler, "echo")
			serverCh.Register(handler, "app-error")

			ch := testutils.NewClient(t, testutils.NewOpts().
				SetStatsReporter(clientStats).
				SetTimeNow(clientClock.Now))
			defer ch.Close()

			ctx, cancel := NewContext(time.Second * 5)
			defer cancel()

			_, _, resp, err := raw.Call(ctx, ch, hostPort, testutils.DefaultServerName, tt.method, nil, nil)
			require.NoError(t, err, "Call(%v) should fail", tt.method)
			assert.Equal(t, tt.wantErr, resp.ApplicationError(), "Call(%v) check application error")

			outboundTags := tagsForOutboundCall(serverCh, ch, tt.method)
			inboundTags := tagsForInboundCall(serverCh, ch, tt.method)

			clientStats.Expected.IncCounter("outbound.calls.send", outboundTags, 1)
			clientStats.Expected.RecordTimer("outbound.calls.per-attempt.latency", outboundTags, 100*time.Millisecond)
			clientStats.Expected.RecordTimer("outbound.calls.latency", outboundTags, 100*time.Millisecond)
			serverStats.Expected.IncCounter("inbound.calls.recvd", inboundTags, 1)
			serverStats.Expected.RecordTimer("inbound.calls.latency", inboundTags, 70*time.Millisecond)

			if tt.wantErr {
				clientStats.Expected.IncCounter("outbound.calls.per-attempt.app-errors", outboundTags, 1)
				clientStats.Expected.IncCounter("outbound.calls.app-errors", outboundTags, 1)
				serverStats.Expected.IncCounter("inbound.calls.app-errors", inboundTags, 1)
			} else {
				clientStats.Expected.IncCounter("outbound.calls.success", outboundTags, 1)
				serverStats.Expected.IncCounter("inbound.calls.success", inboundTags, 1)
			}
		})

		clientStats.Validate(t)
		serverStats.Validate(t)
	}
}

func TestStatsWithRetries(t *testing.T) {
	defer testutils.SetTimeout(t, 2*time.Second)()
	a := testutils.DurationArray

	initialTime := time.Date(2015, 2, 1, 10, 10, 0, 0, time.UTC)
	clientClock := testutils.NewStubClock(initialTime)

	clientStats := newRecordingStatsReporter()
	ch := testutils.NewClient(t, testutils.NewOpts().
		SetStatsReporter(clientStats).
		SetTimeNow(clientClock.Now))
	defer ch.Close()

	ctx, cancel := NewContext(time.Second)
	defer cancel()

	// TODO why do we need this??
	opts := testutils.NewOpts().NoRelay()
	WithVerifiedServer(t, opts, func(serverCh *Channel, hostPort string) {
		const (
			perAttemptServer = 10 * time.Millisecond
			perAttemptClient = time.Millisecond
			perAttemptTotal  = perAttemptServer + perAttemptClient
		)

		respErr := make(chan error, 1)
		testutils.RegisterFunc(serverCh, "req", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			clientClock.Elapse(perAttemptServer)
			return &raw.Res{Arg2: args.Arg2, Arg3: args.Arg3}, <-respErr
		})
		ch.Peers().Add(serverCh.PeerInfo().HostPort)

		tests := []struct {
			expectErr           error
			numFailures         int
			numAttempts         int
			overallLatency      time.Duration
			perAttemptLatencies []time.Duration
		}{
			{
				numFailures:         0,
				numAttempts:         1,
				perAttemptLatencies: a(perAttemptServer),
				overallLatency:      perAttemptTotal,
			},
			{
				numFailures:         1,
				numAttempts:         2,
				perAttemptLatencies: a(perAttemptServer, perAttemptServer),
				overallLatency:      2 * perAttemptTotal,
			},
			{
				numFailures:         4,
				numAttempts:         5,
				perAttemptLatencies: a(perAttemptServer, perAttemptServer, perAttemptServer, perAttemptServer, perAttemptServer),
				overallLatency:      5 * perAttemptTotal,
			},
			{
				numFailures:         5,
				numAttempts:         5,
				expectErr:           ErrServerBusy,
				perAttemptLatencies: a(perAttemptServer, perAttemptServer, perAttemptServer, perAttemptServer, perAttemptServer),
				overallLatency:      5 * perAttemptTotal,
			},
		}

		for _, tt := range tests {
			clientStats.Reset()
			err := ch.RunWithRetry(ctx, func(ctx context.Context, rs *RequestState) error {
				clientClock.Elapse(perAttemptClient)
				if rs.Attempt > tt.numFailures {
					respErr <- nil
				} else {
					respErr <- ErrServerBusy
				}

				sc := ch.GetSubChannel(serverCh.ServiceName())
				_, err := raw.CallV2(ctx, sc, raw.CArgs{
					Method:      "req",
					CallOptions: &CallOptions{RequestState: rs},
				})
				return err
			})
			assert.Equal(t, tt.expectErr, err, "RunWithRetry unexpected error")

			outboundTags := tagsForOutboundCall(serverCh, ch, "req")
			if tt.expectErr == nil {
				clientStats.Expected.IncCounter("outbound.calls.success", outboundTags, 1)
			}
			clientStats.Expected.IncCounter("outbound.calls.send", outboundTags, int64(tt.numAttempts))
			for i, latency := range tt.perAttemptLatencies {
				clientStats.Expected.RecordTimer("outbound.calls.per-attempt.latency", outboundTags, latency)
				if i > 0 {
					tags := tagsForOutboundCall(serverCh, ch, "req")
					tags["retry-count"] = fmt.Sprint(i)
					clientStats.Expected.IncCounter("outbound.calls.retries", tags, 1)
				}
			}
			clientStats.Expected.RecordTimer("outbound.calls.latency", outboundTags, tt.overallLatency)
			clientStats.Validate(t)
		}
	})
}
