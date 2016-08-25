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
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/uber/tchannel-go/benchmark"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/relay"
	"github.com/uber/tchannel-go/relay/relaytest"
	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber-go/atomic"
	"golang.org/x/net/context"
)

type relayTest struct {
	testutils.TestServer
}

func serviceNameOpts(s string) *testutils.ChannelOpts {
	return testutils.NewOpts().SetServiceName(s)
}

func withRelayedEcho(t testing.TB, f func(relay, server, client *Channel, ts *testutils.TestServer)) {
	opts := serviceNameOpts("test").SetRelayOnly()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)
		client := ts.NewClient(serviceNameOpts("client"))
		client.Peers().Add(ts.HostPort())
		f(ts.Relay(), ts.Server(), client, ts)
	})
}

func TestRelay(t *testing.T) {
	withRelayedEcho(t, func(_, _, client *Channel, ts *testutils.TestServer) {
		tests := []struct {
			header string
			body   string
		}{
			{"fake-header", "fake-body"},                        // fits in one frame
			{"fake-header", strings.Repeat("fake-body", 10000)}, // requires continuation
		}
		sc := client.GetSubChannel("test")
		for _, tt := range tests {
			ctx, cancel := NewContext(time.Second)
			defer cancel()

			arg2, arg3, _, err := raw.CallSC(ctx, sc, "echo", []byte(tt.header), []byte(tt.body))
			require.NoError(t, err, "Relayed call failed.")
			assert.Equal(t, tt.header, string(arg2), "Header was mangled during relay.")
			assert.Equal(t, tt.body, string(arg3), "Body was mangled during relay.")
		}

		calls := relaytest.NewMockStats()
		peer := relay.Peer{HostPort: ts.Server().PeerInfo().HostPort}
		for _ = range tests {
			calls.Add("client", "test", "echo").SetPeer(peer).Succeeded().End()
		}
		ts.AssertRelayStats(calls)
	})
}

func TestRelayHandlesClosedPeers(t *testing.T) {
	opts := serviceNameOpts("test").SetRelayOnly().
		// Disable logs as we are closing connections that can error in a lot of places.
		DisableLogVerification()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		ctx, cancel := NewContext(300 * time.Millisecond)
		defer cancel()

		testutils.RegisterEcho(ts.Server(), nil)
		client := ts.NewClient(serviceNameOpts("client"))
		client.Peers().Add(ts.HostPort())

		sc := client.GetSubChannel("test")
		_, _, _, err := raw.CallSC(ctx, sc, "echo", []byte("fake-header"), []byte("fake-body"))
		require.NoError(t, err, "Relayed call failed.")

		ts.Server().Close()
		require.NotPanics(t, func() {
			raw.CallSC(ctx, sc, "echo", []byte("fake-header"), []byte("fake-body"))
		})
	})
}

func TestRelayConnectionCloseDrainsRelayItems(t *testing.T) {
	opts := serviceNameOpts("s1").SetRelayOnly()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		ctx, cancel := NewContext(time.Second)
		defer cancel()

		s1 := ts.Server()
		s2 := ts.NewServer(serviceNameOpts("s2"))

		s2HP := s2.PeerInfo().HostPort
		testutils.RegisterEcho(s1, func() {
			// When s1 gets called, it calls Close on the connection from the relay to s2.
			conn, err := ts.Relay().Peers().GetOrAdd(s2HP).GetConnection(ctx)
			require.NoError(t, err, "Unexpected failure getting connection between s1 and relay")
			conn.Close()
		})

		testutils.AssertEcho(t, s2, ts.HostPort(), "s1")

		calls := relaytest.NewMockStats()
		calls.Add("s2", "s1", "echo").Succeeded().End()
		ts.AssertRelayStats(calls)
	})
}

func TestRelayIDClash(t *testing.T) {
	opts := serviceNameOpts("s1").SetRelayOnly()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		s1 := ts.Server()
		s2 := ts.NewServer(serviceNameOpts("s2"))

		unblock := make(chan struct{})
		testutils.RegisterEcho(s1, func() {
			<-unblock
		})
		testutils.RegisterEcho(s2, nil)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				testutils.AssertEcho(t, s2, ts.HostPort(), s1.ServiceName())
			}()
		}

		for i := 0; i < 5; i++ {
			testutils.AssertEcho(t, s1, ts.HostPort(), s2.ServiceName())
		}

		close(unblock)
		wg.Wait()
	})
}

func TestRelayErrorsOnGetPeer(t *testing.T) {
	busyErr := NewSystemError(ErrCodeBusy, "busy")
	tests := []struct {
		desc      string
		addPeer   func(*testutils.SimpleRelayHosts)
		statsKey  string
		statsPeer relay.Peer
		wantErr   error
	}{
		{
			desc:     "No peer mappings, return empty Peer",
			addPeer:  func(_ *testutils.SimpleRelayHosts) {},
			statsKey: "relay-declined",
			wantErr:  NewSystemError(ErrCodeDeclined, `invalid peer for "svc"`),
		},
		{
			desc: "System error getting peer",
			addPeer: func(rh *testutils.SimpleRelayHosts) {
				rh.AddError("svc", busyErr)
			},
			statsKey: "relay-busy",
			wantErr:  busyErr,
		},
		{
			desc: "Unknown error getting peer",
			addPeer: func(rh *testutils.SimpleRelayHosts) {
				rh.AddError("svc", errors.New("unknown"))
			},
			statsKey: "relay-declined",
			wantErr:  NewSystemError(ErrCodeDeclined, "unknown"),
		},
		{
			desc: "No host:port on peer",
			addPeer: func(rh *testutils.SimpleRelayHosts) {
				rh.AddPeer("svc", "", "pool", "zone")
			},
			statsKey:  "relay-declined",
			statsPeer: relay.Peer{Zone: "zone", Pool: "pool"},
			wantErr:   NewSystemError(ErrCodeDeclined, `invalid peer for "svc"`),
		},
	}

	for _, tt := range tests {
		opts := testutils.NewOpts().SetRelayOnly()
		testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
			tt.addPeer(ts.RelayHosts())

			client := ts.NewClient(nil)
			err := testutils.CallEcho(client, ts.HostPort(), "svc", nil)
			if !assert.Error(t, err, "Call to unknown service should fail") {
				return
			}

			assert.Equal(t, tt.wantErr, err, "%v: unexpected error", tt.desc)

			calls := relaytest.NewMockStats()
			calls.Add(client.PeerInfo().ServiceName, "svc", "echo").
				SetPeer(tt.statsPeer).
				Failed(tt.statsKey).End()
			ts.AssertRelayStats(calls)
		})
	}
}

func TestErrorFrameEndsRelay(t *testing.T) {
	// TestServer validates that there are no relay items left after the given func.
	opts := serviceNameOpts("svc").SetRelayOnly().DisableLogVerification()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		client := ts.NewClient(nil)

		err := testutils.CallEcho(client, ts.HostPort(), "svc", nil)
		if !assert.Error(t, err, "Expected error due to unknown method") {
			return
		}

		se, ok := err.(SystemError)
		if !assert.True(t, ok, "err should be a SystemError, got %T", err) {
			return
		}

		assert.Equal(t, ErrCodeBadRequest, se.Code(), "Expected BadRequest error")

		calls := relaytest.NewMockStats()
		calls.Add(client.PeerInfo().ServiceName, "svc", "echo").Failed("bad-request").End()
		ts.AssertRelayStats(calls)
	})
}

// Trigger a race between receiving a new call and a connection closing
// by closing the relay while a lot of background calls are being made.
func TestRaceCloseWithNewCall(t *testing.T) {
	opts := serviceNameOpts("s1").SetRelayOnly().DisableLogVerification()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		s1 := ts.Server()
		s2 := ts.NewServer(serviceNameOpts("s2").DisableLogVerification())
		testutils.RegisterEcho(s1, nil)

		// signal to start closing the relay.
		var (
			closeRelay  sync.WaitGroup
			stopCalling atomic.Int32
			callers     sync.WaitGroup
		)

		for i := 0; i < 5; i++ {
			callers.Add(1)
			closeRelay.Add(1)

			go func() {
				defer callers.Done()

				calls := 0
				for stopCalling.Load() == 0 {
					testutils.CallEcho(s2, ts.HostPort(), "s1", nil)
					calls++
					if calls == 5 {
						closeRelay.Done()
					}
				}
			}()
		}

		closeRelay.Wait()

		// Close the relay, wait for it to close.
		ts.Relay().Close()
		closed := testutils.WaitFor(time.Second, func() bool {
			return ts.Relay().State() == ChannelClosed
		})
		assert.True(t, closed, "Relay did not close within timeout")

		// Now stop all calls, and wait for the calling goroutine to end.
		stopCalling.Inc()
		callers.Wait()
	})
}

func TestTimeoutCallsThenClose(t *testing.T) {
	// Test needs at least 2 CPUs to trigger race conditions.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(2))

	opts := serviceNameOpts("s1").SetRelayOnly().DisableLogVerification()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		s1 := ts.Server()
		s2 := ts.NewServer(serviceNameOpts("s2").DisableLogVerification())

		unblockEcho := make(chan struct{})
		testutils.RegisterEcho(s1, func() {
			<-unblockEcho
		})

		ctx, cancel := NewContext(testutils.Timeout(30 * time.Millisecond))
		defer cancel()

		var callers sync.WaitGroup
		for i := 0; i < 100; i++ {
			callers.Add(1)
			go func() {
				defer callers.Done()
				raw.Call(ctx, s2, ts.HostPort(), "s1", "echo", nil, nil)
			}()
		}

		close(unblockEcho)

		// Wait for all the callers to end
		callers.Wait()
	})
}

// TestRelayStress makes many concurrent calls and ensures that
// we don't try to reuse any frames once they've been released.
func TestRelayConcurrentCalls(t *testing.T) {
	pool := NewProtectMemFramePool()
	opts := testutils.NewOpts().SetRelayOnly().SetFramePool(pool)
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		server := benchmark.NewServer(
			benchmark.WithNoLibrary(),
			benchmark.WithServiceName("s1"),
		)
		defer server.Close()
		ts.RelayHosts().Add("s1", server.HostPort())

		client := benchmark.NewClient([]string{ts.HostPort()},
			benchmark.WithNoDurations(),
			benchmark.WithNoLibrary(),
			benchmark.WithNumClients(20),
			benchmark.WithServiceName("s1"),
			benchmark.WithTimeout(time.Minute),
		)
		defer client.Close()
		require.NoError(t, client.Warmup(), "Client warmup failed")

		_, err := client.RawCall(1000)
		assert.NoError(t, err, "RawCalls failed")
	})
}

// Ensure that any connections created in the relay path send the ephemeral
// host:port.
func TestRelayOutgoingConnectionsEphemeral(t *testing.T) {
	opts := testutils.NewOpts().SetRelayOnly()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		s2 := ts.NewServer(serviceNameOpts("s2"))
		testutils.RegisterFunc(s2, "echo", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			assert.True(t, CurrentCall(ctx).RemotePeer().IsEphemeral,
				"Connections created for the relay should send ephemeral host:port header")

			return &raw.Res{
				Arg2: args.Arg2,
				Arg3: args.Arg3,
			}, nil
		})

		require.NoError(t, testutils.CallEcho(ts.Server(), ts.HostPort(), "s2", nil), "CallEcho failed")
	})
}

func TestRelayHandleLocalCall(t *testing.T) {
	opts := testutils.NewOpts().SetRelayOnly().
		SetRelayLocal("relay", "tchannel", "test").
		// We make a call to "test" for an unknown method.
		AddLogFilter("Couldn't find handler.", 1)
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		s2 := ts.NewServer(serviceNameOpts("s2"))
		testutils.RegisterEcho(s2, nil)

		client := ts.NewClient(nil)
		testutils.AssertEcho(t, client, ts.HostPort(), "s2")

		testutils.RegisterEcho(ts.Relay(), nil)
		testutils.AssertEcho(t, client, ts.HostPort(), "relay")

		// Sould get a bad request for "test" since the channel does not handle it.
		err := testutils.CallEcho(client, ts.HostPort(), "test", nil)
		assert.Equal(t, ErrCodeBadRequest, GetSystemErrorCode(err), "Expected BadRequest for test")

		// But an unknown service causes declined
		err = testutils.CallEcho(client, ts.HostPort(), "unknown", nil)
		assert.Equal(t, ErrCodeDeclined, GetSystemErrorCode(err), "Expected Declined for unknown")
	})
}

func TestRelayHandleLargeLocalCall(t *testing.T) {
	opts := testutils.NewOpts().SetRelayOnly().
		SetRelayLocal("relay").
		AddLogFilter("Received fragmented callReq", 1).
		// Expect 4 callReqContinues for 256 kb payload that we cannot relay.
		AddLogFilter("Failed to relay frame.", 4)
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		client := ts.NewClient(nil)
		testutils.RegisterEcho(ts.Relay(), nil)

		// This large call should fail with a bad request.
		err := testutils.CallEcho(client, ts.HostPort(), "relay", &raw.Args{
			Arg2: testutils.RandBytes(128 * 1024),
			Arg3: testutils.RandBytes(128 * 1024),
		})
		if assert.Equal(t, ErrCodeBadRequest, GetSystemErrorCode(err), "Expected BadRequest for large call to relay") {
			assert.Contains(t, err.Error(), "cannot receive fragmented calls")
		}

		// We may get an error before the call is finished flushing.
		// Do a ping to ensure everything has been flushed.
		ctx, cancel := NewContext(time.Second)
		defer cancel()
		require.NoError(t, client.Ping(ctx, ts.HostPort()), "Ping failed")
	})
}

func TestRelayMakeOutgoingCall(t *testing.T) {
	opts := testutils.NewOpts().SetRelayOnly()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		svr1 := ts.Relay()
		svr2 := ts.NewServer(testutils.NewOpts().SetServiceName("svc2"))
		testutils.RegisterEcho(svr2, nil)

		sizes := []int{128, 1024, 128 * 1024}
		for _, size := range sizes {
			err := testutils.CallEcho(svr1, ts.HostPort(), "svc2", &raw.Args{
				Arg2: testutils.RandBytes(size),
				Arg3: testutils.RandBytes(size),
			})
			assert.NoError(t, err, "Echo with size %v failed", size)
		}
	})
}

type hostsFunc func(relay.CallFrame, relay.Conn) (relay.Peer, error)

func (f hostsFunc) Get(call relay.CallFrame, conn relay.Conn) (relay.Peer, error) {
	return f(call, conn)
}

func TestRelayConnection(t *testing.T) {
	var errTest = errors.New("test")
	var wantHostPort string
	getHost := func(call relay.CallFrame, conn relay.Conn) (relay.Peer, error) {
		matches := conn.RemoteProcessPrefixMatches()
		assert.Equal(t, []bool{true, true, true, false}, matches, "Unexpected prefix matches.")
		assert.Equal(t, wantHostPort, conn.RemoteHostPort(), "Unexpected RemoteHostPort")
		return relay.Peer{}, errTest
	}

	// Note: we cannot use WithTestServer since we override the RelayHosts.
	opts := testutils.NewOpts().
		SetServiceName("relay").
		SetRelayHosts(hostsFunc(getHost)).
		SetProcessPrefixes("nod", "nodejs-hyperbahn", "", "hyperbahn")
	relay := testutils.NewServer(t, opts)
	defer relay.Close()

	// Create a client that is listening so we can set the expected host:port.
	clientOpts := testutils.NewOpts().SetProcessName("nodejs-hyperbahn")
	client := testutils.NewServer(t, clientOpts)
	wantHostPort = client.PeerInfo().HostPort
	defer client.Close()

	err := testutils.CallEcho(client, relay.PeerInfo().HostPort, relay.ServiceName(), nil)
	require.Error(t, err, "Expected CallEcho to fail")
	assert.Contains(t, err.Error(), errTest.Error(), "Unexpected error")
}
