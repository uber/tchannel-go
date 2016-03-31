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
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/atomic"
	"github.com/uber/tchannel-go/testutils/goroutines"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Has a previous test already leaked a goroutine?
var _leakedGoroutine = atomic.NewInt32(0)

// A TestServer encapsulates a TChannel server, a client factory, and functions
// to ensure that we're not leaking resources.
//
// TODO: Include an optional relay.
type TestServer struct {
	testing.TB

	// Don't embed the server, since we'll soon want to introduce a relay.
	server        *tchannel.Channel
	serverInitial *tchannel.RuntimeState
	verifyOpts    *goroutines.VerifyOpts
	postFns       []func()
}

// NewTestServer constructs a TestServer.
func NewTestServer(t testing.TB, opts *ChannelOpts) *TestServer {
	opts = getOptsForTest(t, opts)
	ch, err := NewServerChannel(opts)
	require.NoError(t, err, "WithTestServer failed to create Server")

	return &TestServer{
		TB:            t,
		server:        ch,
		serverInitial: comparableState(ch),
		postFns:       opts.postFns,
	}
}

// WithTestServer creates a new TestServer, runs the passed function, and then
// verifies that no resources were leaked.
//
// TODO: run function twice; once with a relay, once without.
func WithTestServer(t testing.TB, chanOpts *ChannelOpts, f func(*TestServer)) {
	ts := NewTestServer(t, chanOpts)
	// Note: We use defer, as we want the postFns to run even if the test
	// goroutine exits (e.g. user calls t.Fatalf).
	defer ts.post()

	f(ts)
	ts.CloseAndVerify()
}

// SetVerifyOpts specifies the options we'll use during teardown to verify that
// no goroutines were leaked.
func (ts *TestServer) SetVerifyOpts(opts *goroutines.VerifyOpts) {
	ts.verifyOpts = opts
}

// Server returns the underlying TChannel for the server (i.e., the channel on
// which we're registering handlers).
//
// To support test cases with relays interposed between clients and servers,
// callers should use the Client(), HostPort(), ServiceName(), and Register()
// methods instead of accessing the server channel explicitly.
func (ts *TestServer) Server() *tchannel.Channel {
	return ts.server
}

// HostPort returns the host:port for clients to connect to. Note that this may
// not be the same as the host:port of the server channel.
func (ts *TestServer) HostPort() string {
	// Use this wrapper to enable registering handlers on the server, but
	// connecting to a relay.
	return ts.server.PeerInfo().HostPort
}

// ServiceName returns the service name of the server channel.
func (ts *TestServer) ServiceName() string {
	return ts.server.PeerInfo().ServiceName
}

// Register registers a handler on the server channel.
func (ts *TestServer) Register(h tchannel.Handler, methodName string) {
	ts.server.Register(h, methodName)
}

// CloseAndVerify closes all channels, then verifies that no message exchanges
// or goroutines were leaked.
func (ts *TestServer) CloseAndVerify() {
	ts.close()
	ts.verify()
}

// NewClient is a convenience wrapper around testutils.NewClient.
func (ts *TestServer) NewClient(opts *ChannelOpts) *tchannel.Channel {
	return NewClient(ts, opts)
}

func (ts *TestServer) close() {
	ts.server.Close()
}

func (ts *TestServer) verify() {
	ts.waitForChannelClose(ts.server)

	// Verify no goroutines leaked first, which will wait for all runnable goroutines.
	ts.verifyNoGoroutinesLeaked()
	ts.verifyExchangesCleared()
	ts.verifyNoStateLeak()
}

func (ts *TestServer) post() {
	for _, fn := range ts.postFns {
		fn()
	}
}

func (ts *TestServer) waitForChannelClose(ch *tchannel.Channel) {
	if ts.Failed() {
		return
	}
	started := time.Now()

	var state tchannel.ChannelState
	for i := 0; i < 50; i++ {
		if state = ch.State(); state == tchannel.ChannelClosed {
			return
		}

		runtime.Gosched()
		if i < 5 {
			continue
		}

		sleepFor := time.Duration(i) * 100 * time.Microsecond
		time.Sleep(Timeout(sleepFor))
	}

	// Channel is not closing, fail the test.
	sinceStart := time.Since(started)
	ts.Errorf("Channel did not close after %v, last state: %v", sinceStart, state)
}

func (ts *TestServer) verifyNoStateLeak() {
	serverFinal := comparableState(ts.server)
	assert.Equal(ts.TB, ts.serverInitial, serverFinal, "Runtime state has leaks")
}

func (ts *TestServer) verifyExchangesCleared() {
	if ts.Failed() {
		return
	}
	// Ensure that all the message exchanges are empty.
	serverState := ts.server.IntrospectState(&tchannel.IntrospectionOptions{
		IncludeExchanges: true,
	})
	if exchangesLeft := describeLeakedExchanges(serverState); exchangesLeft != "" {
		ts.Errorf("Found uncleared message exchanges on server:\n%v", exchangesLeft)
	}
}

func (ts *TestServer) verifyNoGoroutinesLeaked() {
	if _leakedGoroutine.Load() == 1 {
		ts.Log("Skipping check for leaked goroutines because of a previous leak.")
		return
	}
	err := goroutines.IdentifyLeaks(ts.verifyOpts)
	if err == nil {
		// No leaks, nothing to do.
		return
	}
	if isFirstLeak := _leakedGoroutine.CAS(0, 1); !isFirstLeak {
		ts.Log("Skipping check for leaked goroutines because of a previous leak.")
		return
	}
	if ts.Failed() {
		// If we've already failed this test, don't pollute the test output with
		// more failures.
		return
	}
	ts.Error(err.Error())
}

func comparableState(ch *tchannel.Channel) *tchannel.RuntimeState {
	s := ch.IntrospectState(&tchannel.IntrospectionOptions{
		IncludeExchanges: true,
	})
	s.OtherChannels = nil
	s.SubChannels = nil
	s.Peers = nil
	return s
}

func describeLeakedExchanges(rs *tchannel.RuntimeState) string {
	var connections []*tchannel.ConnectionRuntimeState
	for _, peer := range rs.RootPeers {
		for _, conn := range peer.InboundConnections {
			connections = append(connections, &conn)
		}
		for _, conn := range peer.OutboundConnections {
			connections = append(connections, &conn)
		}
	}
	return describeLeakedExchangesConns(connections)
}

func describeLeakedExchangesConns(connections []*tchannel.ConnectionRuntimeState) string {
	var exchanges []string
	for _, c := range connections {
		if exch := describeLeakedExchangesSingleConn(c); exch != "" {
			exchanges = append(exchanges, exch)
		}
	}
	return strings.Join(exchanges, "\n")
}

func describeLeakedExchangesSingleConn(cs *tchannel.ConnectionRuntimeState) string {
	var exchanges []string
	checkExchange := func(e tchannel.ExchangeSetRuntimeState) {
		if e.Count > 0 {
			exchanges = append(exchanges, fmt.Sprintf(" %v leftover %v exchanges", e.Name, e.Count))
			for _, v := range e.Exchanges {
				exchanges = append(exchanges, fmt.Sprintf("  exchanges: %+v", v))
			}
		}
	}
	checkExchange(cs.InboundExchange)
	checkExchange(cs.OutboundExchange)
	if len(exchanges) == 0 {
		return ""
	}

	return fmt.Sprintf("Connection %d has leftover exchanges:\n\t%v", cs.ID, strings.Join(exchanges, "\n\t"))
}
