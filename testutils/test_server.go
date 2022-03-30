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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/relay/relaytest"
	"github.com/uber/tchannel-go/testutils/goroutines"
	"go.uber.org/multierr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"golang.org/x/net/context"
)

// Has a previous test already leaked a goroutine?
var _leakedGoroutine atomic.Bool

// A TestServer encapsulates a TChannel server, a client factory, and functions
// to ensure that we're not leaking resources.
type TestServer struct {
	testing.TB

	// References to specific channels (if any, as they can be disabled)
	relayCh  *tchannel.Channel
	serverCh *tchannel.Channel

	// relayHost is the relayer's StubRelayHost (if any).
	relayHost *relaytest.StubRelayHost

	// relayStats is the backing stats for the relay.
	// Note: if a user passes a custom RelayHosts that does not implement
	// relayStatter, then this will be nil, and relay stats cannot be verified.
	relayStats *relaytest.MockStats

	// channels is the list of channels created for this TestServer. The first
	// element is always the initial server.
	channels []*tchannel.Channel

	// channelState the initial runtime state for all channels created
	// as part of the TestServer (including the server).
	channelStates map[*tchannel.Channel]*tchannel.RuntimeState

	introspectOpts *tchannel.IntrospectionOptions
	verifyOpts     *goroutines.VerifyOpts
	postFns        []func()
}

type relayStatter interface {
	Stats() *relaytest.MockStats
}

// NewTestServer constructs a TestServer.
func NewTestServer(t testing.TB, opts *ChannelOpts) *TestServer {
	ts := &TestServer{
		TB:            t,
		channelStates: make(map[*tchannel.Channel]*tchannel.RuntimeState),
		introspectOpts: &tchannel.IntrospectionOptions{
			IncludeExchanges:  true,
			IncludeTombstones: true,
		},
	}

	if !opts.DisableServer {
		// Remove any relay options, since those should only be applied to addRelay.
		serverOpts := opts.Copy()
		serverOpts.RelayHost = nil
		ts.serverCh = ts.NewServer(serverOpts)
	}

	if opts == nil || !opts.DisableRelay {
		ts.addRelay(opts)
	}

	return ts
}

// runSubTest runs the specified function as a sub-test of a testing.T or
// testing.B if the types match.
func runSubTest(t testing.TB, name string, f func(testing.TB)) {
	switch t := t.(type) {
	case *testing.T:
		t.Run(name, func(t *testing.T) {
			f(t)
		})
	case *testing.B:
		t.Run(name, func(b *testing.B) {
			f(b)
		})
	default:
		f(t)
	}
}

// WithTestServer creates a new TestServer, runs the passed function, and then
// verifies that no resources were leaked.
func WithTestServer(t testing.TB, chanOpts *ChannelOpts, f func(testing.TB, *TestServer)) {
	runTest := func(t testing.TB, chanOpts *ChannelOpts) {
		runCount := chanOpts.RunCount
		if runCount < 1 {
			runCount = 1
		}

		for i := 0; i < runCount; i++ {
			if t.Failed() {
				return
			}

			// Run without the relay, unless OnlyRelay was set.
			if !chanOpts.OnlyRelay {
				runSubTest(t, "no relay", func(t testing.TB) {
					noRelayOpts := chanOpts.Copy()
					noRelayOpts.DisableRelay = true
					withServer(t, noRelayOpts, f)
				})
			}

			// Run with the relay, unless the user has disabled it.
			if !chanOpts.DisableRelay {
				runSubTest(t, "with relay", func(t testing.TB) {
					withServer(t, chanOpts.Copy(), f)
				})

				// Re-run the same test with timer verification if this is a relay-only test.
				if chanOpts.OnlyRelay {
					runSubTest(t, "with relay and timer verification", func(t testing.TB) {
						verifyOpts := chanOpts.Copy()
						verifyOpts.RelayTimerVerification = true
						withServer(t, verifyOpts, f)
					})
				}
			}
		}
	}

	chanOptsCopy := chanOpts.Copy()
	runTest(t, chanOptsCopy)

	if os.Getenv("DISABLE_FRAME_POOLING_CHECKS") == "" && chanOptsCopy.CheckFramePooling {
		runSubTest(t, "check frame leaks", func(t testing.TB) {
			pool := tchannel.NewCheckedFramePoolForTest()

			runTest(t, chanOpts.Copy().SetFramePool(pool))

			result := pool.CheckEmpty()
			if len(result.Unreleased) > 0 {
				t.Errorf("Frame pool has %v unreleased frames, errors:\n%v\n",
					len(result.Unreleased), strings.Join(result.Unreleased, "\n"))
			}
			if len(result.BadReleases) > 0 {
				t.Errorf("Frame pool has %v bad releases, errors:\n%v\n",
					len(result.BadReleases), strings.Join(result.BadReleases, "\n"))
			}
		})
	}
}

// SetVerifyOpts specifies the options we'll use during teardown to verify that
// no goroutines were leaked.
func (ts *TestServer) SetVerifyOpts(opts *goroutines.VerifyOpts) {
	ts.verifyOpts = opts
}

// HasServer returns whether this TestServer has a TChannel server, as
// the server may have been disabled with the DisableServer option.
func (ts *TestServer) HasServer() bool {
	return ts.serverCh != nil
}

// Server returns the underlying TChannel for the server (i.e., the channel on
// which we're registering handlers).
//
// To support test cases with relays interposed between clients and servers,
// callers should use the Client(), HostPort(), ServiceName(), and Register()
// methods instead of accessing the server channel explicitly.
func (ts *TestServer) Server() *tchannel.Channel {
	require.True(ts, ts.HasServer(), "Cannot use Server as it was disabled")
	return ts.serverCh
}

// HasRelay indicates whether this TestServer has a relay interposed between the
// server and clients.
func (ts *TestServer) HasRelay() bool {
	return ts.relayCh != nil
}

// Relay returns the relay channel, if one is present.
func (ts *TestServer) Relay() *tchannel.Channel {
	require.True(ts, ts.HasRelay(), "Cannot use Relay, not present in current test")
	return ts.relayCh
}

// RelayHost returns the stub RelayHost for mapping service names to peers.
func (ts *TestServer) RelayHost() *relaytest.StubRelayHost {
	return ts.relayHost
}

// HostPort returns the host:port for clients to connect to. Note that this may
// not be the same as the host:port of the server channel.
func (ts *TestServer) HostPort() string {
	if ts.HasRelay() {
		return ts.Relay().PeerInfo().HostPort
	}
	return ts.Server().PeerInfo().HostPort
}

// ServiceName returns the service name of the server channel.
func (ts *TestServer) ServiceName() string {
	return ts.Server().PeerInfo().ServiceName
}

// Register registers a handler on the server channel.
func (ts *TestServer) Register(h tchannel.Handler, methodName string) {
	ts.Server().Register(h, methodName)
}

// RegisterFunc registers a function as a handler for the given method name.
//
// TODO: Delete testutils.RegisterFunc in favor of this test server.
func (ts *TestServer) RegisterFunc(name string, f func(context.Context, *raw.Args) (*raw.Res, error)) {
	ts.Register(raw.Wrap(rawFuncHandler{ts.Server(), f}), name)
}

// CloseAndVerify closes all channels verifying each channel as it is closed.
// It then verifies that no goroutines were leaked.
func (ts *TestServer) CloseAndVerify() {
	// Verify channels before they are closed to ensure that we catch any
	// unexpected pending exchanges.
	for i := len(ts.channels) - 1; i >= 0; i-- {
		ch := ts.channels[i]
		ch.Logger().Debugf("TEST: TestServer is verifying channel")
		ts.verify(ch)
	}

	// Close the connection, then verify again to ensure connection close didn't
	// cause any unexpected issues.
	for i := len(ts.channels) - 1; i >= 0; i-- {
		ch := ts.channels[i]
		ch.Logger().Debugf("TEST: TestServer is closing and verifying channel")
		ts.close(ch)
		ts.verify(ch)
	}

	if ts.relayCh != nil {
		ts.close(ts.relayCh)
	}

	// Verify that there's no goroutine leaks after all tests are complete.
	ts.verifyNoGoroutinesLeaked()
}

// AssertRelayStats checks that the relayed call graph matches expectations. If
// there's no relay, AssertRelayStats is a no-op.
func (ts *TestServer) AssertRelayStats(expected *relaytest.MockStats) {
	if !ts.HasRelay() {
		return
	}

	if ts.relayStats == nil {
		ts.TB.Error("Cannot verify relay stats, passed in RelayStats does not implement relayStatter")
		return
	}

	ts.relayStats.AssertEqual(ts, expected)
}

// NewClient returns a client that with log verification.
// TODO: Verify message exchanges and leaks for client channels as well.
func (ts *TestServer) NewClient(opts *ChannelOpts) *tchannel.Channel {
	return ts.addChannel(newClient, opts.Copy())
}

// NewServer returns a server with log and channel state verification.
//
// Note: The same default service name is used if one isn't specified.
func (ts *TestServer) NewServer(opts *ChannelOpts) *tchannel.Channel {
	ch := ts.addChannel(newServer, opts.Copy())
	if ts.relayHost != nil {
		ts.relayHost.Add(ch.ServiceName(), ch.PeerInfo().HostPort)
	}
	return ch
}

// addRelay adds a relay in front of the test server, altering public methods as
// necessary to route traffic through the relay.
func (ts *TestServer) addRelay(parentOpts *ChannelOpts) {
	opts := parentOpts.Copy()

	relayHost := opts.ChannelOptions.RelayHost
	if relayHost == nil {
		ts.relayHost = relaytest.NewStubRelayHost()
		relayHost = ts.relayHost
	} else if relayHost, ok := relayHost.(*relaytest.StubRelayHost); ok {
		ts.relayHost = relayHost
	}

	opts.ServiceName = "relay"
	opts.ChannelOptions.RelayHost = relayHost

	ts.relayCh = ts.addChannel(newServer, opts)
	if ts.relayHost != nil && ts.HasServer() {
		ts.relayHost.Add(ts.Server().ServiceName(), ts.Server().PeerInfo().HostPort)
	}

	if statter, ok := relayHost.(relayStatter); ok {
		ts.relayStats = statter.Stats()
	}
}

func (ts *TestServer) addChannel(createChannel func(t testing.TB, opts *ChannelOpts) *tchannel.Channel, opts *ChannelOpts) *tchannel.Channel {
	ch := createChannel(ts, opts)
	ts.postFns = append(ts.postFns, opts.postFns...)
	ts.channels = append(ts.channels, ch)
	ts.channelStates[ch] = comparableState(ch, ts.introspectOpts)
	return ch
}

// close closes all channels in most-recently-created order.
// it waits for the channels to close.
func (ts *TestServer) close(ch *tchannel.Channel) {
	ch.Close()

	timeout := Timeout(time.Second)
	select {
	case <-time.After(timeout):
		ts.Errorf("Channel %p did not close after %v, last state: %v", ch, timeout, ch.State())

		// The introspected state might help debug why the channel isn't closing.
		ts.Logf("Introspected state:\n%s", IntrospectJSON(ch, &tchannel.IntrospectionOptions{
			IncludeExchanges:  true,
			IncludeTombstones: true,
		}))
	case <-ch.ClosedChan():
	}
}

func (ts *TestServer) verify(ch *tchannel.Channel) {
	if ts.Failed() {
		return
	}

	// Tests may end with running background goroutines that are cleaning up, so give
	// them some time to finish before running verifications.
	var errs error
	WaitFor(time.Second, func() bool {
		errs = multierr.Combine(
			ts.verifyExchangesCleared(ch),
			ts.verifyRelaysEmpty(ch),
		)
		return errs == nil
	})

	if errs == nil {
		return
	}

	// If verification fails, get the marshalled state.
	assert.NoError(ts, errs, "Verification failed. Channel state:\n%v", IntrospectJSON(ch, nil /* opts */))
}

func (ts *TestServer) post() {
	if !ts.Failed() {
		for _, ch := range ts.channels {
			ts.verifyNoStateLeak(ch)
		}
	}
	for _, fn := range ts.postFns {
		fn()
	}
}

func (ts *TestServer) verifyNoStateLeak(ch *tchannel.Channel) {
	initial := ts.channelStates[ch]
	final := comparableState(ch, ts.introspectOpts)
	assert.Equal(ts.TB, initial, final, "Runtime state has leaks")
}

func (ts *TestServer) verifyExchangesCleared(ch *tchannel.Channel) error {
	// Ensure that all the message exchanges are empty.
	serverState := ch.IntrospectState(ts.introspectOpts)
	if exchangesLeft := describeLeakedExchanges(serverState); exchangesLeft != "" {
		return fmt.Errorf("found uncleared message exchanges on %q:\n%v", ch.ServiceName(), exchangesLeft)
	}

	return nil
}

func (ts *TestServer) verifyRelaysEmpty(ch *tchannel.Channel) error {
	var errs error
	state := ch.IntrospectState(ts.introspectOpts)
	for _, peerState := range state.RootPeers {
		var connStates []tchannel.ConnectionRuntimeState
		connStates = append(connStates, peerState.InboundConnections...)
		connStates = append(connStates, peerState.OutboundConnections...)
		for _, connState := range connStates {
			n := connState.Relayer.Count
			if n != 0 {
				errs = multierr.Append(errs, fmt.Errorf("found %v left-over items in relayer for %v", n, connState.LocalHostPort))
			}
		}
	}

	return errs
}

func (ts *TestServer) verifyNoGoroutinesLeaked() {
	if _leakedGoroutine.Load() {
		ts.Log("Skipping check for leaked goroutines because of a previous leak.")
		return
	}
	err := goroutines.IdentifyLeaks(ts.verifyOpts)
	if err == nil {
		// No leaks, nothing to do.
		return
	}
	if isFirstLeak := _leakedGoroutine.CAS(false, true); !isFirstLeak {
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

func comparableState(ch *tchannel.Channel, opts *tchannel.IntrospectionOptions) *tchannel.RuntimeState {
	s := ch.IntrospectState(opts)
	s.SubChannels = nil
	s.Peers = nil

	// Tests start with ChannelClient or ChannelListening, but end with ChannelClosed.
	s.ChannelState = ""
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

func withServer(t testing.TB, chanOpts *ChannelOpts, f func(testing.TB, *TestServer)) {
	ts := NewTestServer(t, chanOpts)
	// Note: We use defer, as we want the postFns to run even if the test
	// goroutine exits (e.g. user calls t.Fatalf).
	defer ts.post()
	defer ts.CloseAndVerify()

	f(t, ts)
	if ts.HasServer() {
		ts.Server().Logger().Debugf("TEST: Test function complete")
	}
}
