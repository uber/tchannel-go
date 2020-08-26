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
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
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
	"github.com/uber/tchannel-go/testutils/testreader"
	"github.com/uber/tchannel-go/testutils/thriftarg2test"
	"github.com/uber/tchannel-go/thrift"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
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
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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
		for range tests {
			calls.Add("client", "test", "echo").Succeeded().End()
		}
		ts.AssertRelayStats(calls)
	})
}

func TestRelaySetHost(t *testing.T) {
	rh := relaytest.NewStubRelayHost()
	opts := serviceNameOpts("test").SetRelayHost(rh).SetRelayOnly()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)

		client := ts.NewClient(serviceNameOpts("client"))
		client.Peers().Add(ts.HostPort())
		testutils.AssertEcho(t, client, ts.HostPort(), ts.Server().ServiceName())
	})
}

func TestRelayHandlesClosedPeers(t *testing.T) {
	opts := serviceNameOpts("test").SetRelayOnly().
		// Disable logs as we are closing connections that can error in a lot of places.
		DisableLogVerification()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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
		desc       string
		returnPeer string
		returnErr  error
		statsKey   string
		wantErr    error
	}{
		{
			desc:       "No peer and no error",
			returnPeer: "",
			returnErr:  nil,
			statsKey:   "relay-bad-relay-host",
			wantErr:    NewSystemError(ErrCodeDeclined, `bad relay host implementation`),
		},
		{
			desc:      "System error getting peer",
			returnErr: busyErr,
			statsKey:  "relay-busy",
			wantErr:   busyErr,
		},
		{
			desc:      "Unknown error getting peer",
			returnErr: errors.New("unknown"),
			statsKey:  "relay-declined",
			wantErr:   NewSystemError(ErrCodeDeclined, "unknown"),
		},
	}

	for _, tt := range tests {
		f := func(relay.CallFrame, *relay.Conn) (string, error) {
			return tt.returnPeer, tt.returnErr
		}

		opts := testutils.NewOpts().
			SetRelayHost(relaytest.HostFunc(f)).
			SetRelayOnly().
			DisableLogVerification() // some of the test cases cause warnings.
		testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
			client := ts.NewClient(nil)
			err := testutils.CallEcho(client, ts.HostPort(), "svc", nil)
			if !assert.Error(t, err, "Call to unknown service should fail") {
				return
			}

			assert.Equal(t, tt.wantErr, err, "%v: unexpected error", tt.desc)

			calls := relaytest.NewMockStats()
			calls.Add(client.PeerInfo().ServiceName, "svc", "echo").
				Failed(tt.statsKey).End()
			ts.AssertRelayStats(calls)
		})
	}
}

func TestErrorFrameEndsRelay(t *testing.T) {
	// TestServer validates that there are no relay items left after the given func.
	opts := serviceNameOpts("svc").SetRelayOnly().DisableLogVerification()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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

func TestLargeTimeoutsAreClamped(t *testing.T) {
	const (
		clampTTL = time.Millisecond
		longTTL  = time.Minute
	)

	opts := serviceNameOpts("echo-service").
		SetRelayOnly().
		SetRelayMaxTimeout(clampTTL).
		DisableLogVerification() // handler returns after deadline

	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		srv := ts.Server()
		client := ts.NewClient(nil)

		unblock := make(chan struct{})
		defer close(unblock) // let server shut down cleanly
		testutils.RegisterFunc(srv, "echo", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			now := time.Now()
			deadline, ok := ctx.Deadline()
			assert.True(t, ok, "Expected deadline to be set in handler.")
			assert.True(t, deadline.Sub(now) <= clampTTL, "Expected relay to clamp TTL sent to backend.")
			<-unblock
			return &raw.Res{Arg2: args.Arg2, Arg3: args.Arg3}, nil
		})

		done := make(chan struct{})
		go func() {
			ctx, cancel := NewContext(longTTL)
			defer cancel()
			_, _, _, err := raw.Call(ctx, client, ts.HostPort(), "echo-service", "echo", nil, nil)
			require.Error(t, err)
			code := GetSystemErrorCode(err)
			assert.Equal(t, ErrCodeTimeout, code)
			close(done)
		}()

		select {
		case <-time.After(testutils.Timeout(10 * clampTTL)):
			t.Fatal("Failed to clamp timeout.")
		case <-done:
		}
	})
}

// TestRelayConcurrentCalls makes many concurrent calls and ensures that
// we don't try to reuse any frames once they've been released.
func TestRelayConcurrentCalls(t *testing.T) {
	pool := NewProtectMemFramePool()
	opts := testutils.NewOpts().SetRelayOnly().SetFramePool(pool)
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		server := benchmark.NewServer(
			benchmark.WithNoLibrary(),
			benchmark.WithServiceName("s1"),
		)
		defer server.Close()
		ts.RelayHost().Add("s1", server.HostPort())

		client := benchmark.NewClient([]string{ts.HostPort()},
			benchmark.WithNoDurations(),
			// TODO(prashant): Enable once we have control over concurrency with NoLibrary.
			// benchmark.WithNoLibrary(),
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
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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

		calls := relaytest.NewMockStats()
		calls.Add(client.ServiceName(), "s2", "echo").Succeeded().End()
		calls.Add(client.ServiceName(), "unknown", "echo").Failed("relay-declined").End()
		ts.AssertRelayStats(calls)
	})
}

func TestRelayHandleLargeLocalCall(t *testing.T) {
	opts := testutils.NewOpts().SetRelayOnly().
		SetRelayLocal("relay").
		AddLogFilter("Received fragmented callReq", 1).
		// Expect 4 callReqContinues for 256 kb payload that we cannot relay.
		AddLogFilter("Failed to relay frame.", 4)
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
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

func TestRelayInboundConnContext(t *testing.T) {
	rh := relaytest.NewStubRelayHost()
	rh.SetFrameFn(func(f relay.CallFrame, conn *relay.Conn) {
		// Verify that the relay gets the base context set in the server's ConnContext
		assert.Equal(t, "bar", conn.Context.Value("foo"), "Unexpected value set in base context")
	})

	opts := testutils.NewOpts().SetRelayOnly().SetRelayHost(rh).SetConnContext(func(ctx context.Context, conn net.Conn) context.Context {
		return context.WithValue(ctx, "foo", "bar")
	})
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		rly := ts.Relay()
		svr := ts.Server()
		testutils.RegisterEcho(svr, nil)

		client := testutils.NewClient(t, nil)
		testutils.AssertEcho(t, client, rly.PeerInfo().HostPort, ts.ServiceName())
	})
}

func TestRelayContextInheritsFromOutboundConnection(t *testing.T) {
	rh := relaytest.NewStubRelayHost()
	rh.SetFrameFn(func(f relay.CallFrame, conn *relay.Conn) {
		// Verify that the relay gets the base context set by the outbound connection to the caller
		assert.Equal(t, "bar", conn.Context.Value("foo"), "Unexpected value set in base context")
	})
	opts := testutils.NewOpts().SetRelayOnly().SetRelayHost(rh)

	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		rly := ts.Relay()
		callee := ts.Server()
		testutils.RegisterEcho(callee, nil)

		caller := ts.NewServer(testutils.NewOpts())
		testutils.RegisterEcho(caller, nil)

		baseCtx := context.WithValue(context.Background(), "foo", "bar")
		ctx, cancel := NewContextBuilder(time.Second).SetConnectBaseContext(baseCtx).Build()
		defer cancel()

		require.NoError(t, rly.Ping(ctx, caller.PeerInfo().HostPort))
		testutils.AssertEcho(t, caller, ts.HostPort(), ts.ServiceName())
	})
}

func TestRelayConnection(t *testing.T) {
	var errTest = errors.New("test")
	var gotConn *relay.Conn

	getHost := func(_ relay.CallFrame, conn *relay.Conn) (string, error) {
		gotConn = conn
		return "", errTest
	}

	opts := testutils.NewOpts().
		SetRelayOnly().
		SetRelayHost(relaytest.HostFunc(getHost))
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		getConn := func(ch *Channel, outbound bool) ConnectionRuntimeState {
			state := ch.IntrospectState(nil)
			peer, ok := state.RootPeers[ts.HostPort()]
			require.True(t, ok, "Failed to find peer for relay")

			conns := peer.InboundConnections
			if outbound {
				conns = peer.OutboundConnections
			}

			require.Len(t, conns, 1, "Expect single connection from client to relay")
			return conns[0]
		}

		// Create a client that is listening so we can set the expected host:port.
		client := ts.NewClient(nil)

		err := testutils.CallEcho(client, ts.HostPort(), ts.ServiceName(), nil)
		require.Error(t, err, "Expected CallEcho to fail")
		assert.Contains(t, err.Error(), errTest.Error(), "Unexpected error")

		wantConn := &relay.Conn{
			RemoteAddr:        getConn(client, true /* outbound */).LocalHostPort,
			RemoteProcessName: client.PeerInfo().ProcessName,
			IsOutbound:        false,
			Context:           context.Background(),
		}
		assert.Equal(t, wantConn, gotConn, "Unexpected remote addr")

		// Verify something similar with a listening channel, ensuring that
		// we're not using the host:port of the listening server, but the
		// host:port of the outbound TCP connection.
		listeningC := ts.NewServer(nil)

		err = testutils.CallEcho(listeningC, ts.HostPort(), ts.ServiceName(), nil)
		require.Error(t, err, "Expected CallEcho to fail")
		assert.Contains(t, err.Error(), errTest.Error(), "Unexpected error")

		connHostPort := getConn(listeningC, true /* outbound */).LocalHostPort
		assert.NotEqual(t, connHostPort, listeningC.PeerInfo().HostPort, "Ensure connection host:port is not listening host:port")
		wantConn = &relay.Conn{
			RemoteAddr:        connHostPort,
			RemoteProcessName: listeningC.PeerInfo().ProcessName,
			Context:           context.Background(),
		}
		assert.Equal(t, wantConn, gotConn, "Unexpected remote addr")

		// Connections created when relaying hide the relay host:port to ensure
		// services don't send calls back over that same connection. However,
		// this is what happens in the hyperbahn emulation case, so create
		// an explicit connection to a new listening channel.
		listeningHBSvc := ts.NewServer(nil)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_, err = ts.Relay().Connect(ctx, listeningHBSvc.PeerInfo().HostPort)
		require.NoError(t, err, "Failed to connect from relay to listening host:port")

		// Now when listeningHBSvc makes a call, it should use the above connection.
		err = testutils.CallEcho(listeningHBSvc, ts.HostPort(), ts.ServiceName(), nil)
		require.Error(t, err, "Expected CallEcho to fail")
		assert.Contains(t, err.Error(), errTest.Error(), "Unexpected error")

		// We expect an inbound connection on listeningHBSvc.
		connHostPort = getConn(listeningHBSvc, false /* outbound */).LocalHostPort
		wantConn = &relay.Conn{
			RemoteAddr:        connHostPort,
			RemoteProcessName: listeningHBSvc.PeerInfo().ProcessName,
			IsOutbound:        true, // outbound connection according to relay.
			Context:           context.Background(),
		}
		assert.Equal(t, wantConn, gotConn, "Unexpected remote addr")
	})
}

func TestRelayConnectionClosed(t *testing.T) {
	protocolErr := NewSystemError(ErrCodeProtocol, "invalid service name")
	getHost := func(relay.CallFrame, *relay.Conn) (string, error) {
		return "", protocolErr
	}

	opts := testutils.NewOpts().
		SetRelayOnly().
		SetRelayHost(relaytest.HostFunc(getHost))
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		// The client receives a protocol error which causes the following logs.
		opts := testutils.NewOpts().
			AddLogFilter("Peer reported protocol error", 1).
			AddLogFilter("Connection error", 1)
		client := ts.NewClient(opts)

		err := testutils.CallEcho(client, ts.HostPort(), ts.ServiceName(), nil)
		assert.Equal(t, protocolErr, err, "Unexpected error on call")

		closedAll := testutils.WaitFor(time.Second, func() bool {
			return ts.Relay().IntrospectNumConnections() == 0
		})
		assert.True(t, closedAll, "Relay should close client connection")
	})
}

func TestRelayUsesRootPeers(t *testing.T) {
	opts := testutils.NewOpts().SetRelayOnly()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)
		client := testutils.NewClient(t, nil)
		err := testutils.CallEcho(client, ts.HostPort(), ts.ServiceName(), nil)
		assert.NoError(t, err, "Echo failed")
		assert.Len(t, ts.Relay().Peers().Copy(), 0, "Peers should not be modified by relay")
	})
}

// Ensure that if the relay recieves a call on a connection that is not active,
// it declines the call, and increments a relay-client-conn-inactive stat.
func TestRelayRejectsDuringClose(t *testing.T) {
	opts := testutils.NewOpts().SetRelayOnly().
		AddLogFilter("Failed to relay frame.", 1, "error", "incoming connection is not active: connectionStartClose")
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		gotCall := make(chan struct{})
		block := make(chan struct{})

		testutils.RegisterEcho(ts.Server(), func() {
			close(gotCall)
			<-block
		})

		client := ts.NewClient(nil)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			testutils.AssertEcho(t, client, ts.HostPort(), ts.ServiceName())
		}()

		<-gotCall
		// Close the relay so that it stops accepting more calls.
		ts.Relay().Close()
		err := testutils.CallEcho(client, ts.HostPort(), ts.ServiceName(), nil)
		require.Error(t, err, "Expect call to fail after relay is shutdown")
		assert.Contains(t, err.Error(), "incoming connection is not active")
		close(block)
		wg.Wait()

		// We have a successful call that ran in the goroutine
		// and a failed call that we just checked the error on.
		calls := relaytest.NewMockStats()
		calls.Add(client.PeerInfo().ServiceName, ts.ServiceName(), "echo").
			Succeeded().End()
		calls.Add(client.PeerInfo().ServiceName, ts.ServiceName(), "echo").
			// No peer is set since we rejected the call before selecting one.
			Failed("relay-client-conn-inactive").End()
		ts.AssertRelayStats(calls)
	})
}

func TestRelayRateLimitDrop(t *testing.T) {
	getHost := func(relay.CallFrame, *relay.Conn) (string, error) {
		return "", relay.RateLimitDropError{}
	}

	opts := testutils.NewOpts().
		SetRelayOnly().
		SetRelayHost(relaytest.HostFunc(getHost))
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		var gotCall bool
		testutils.RegisterEcho(ts.Server(), func() {
			gotCall = true
		})

		client := ts.NewClient(nil)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			// We want to use a low timeout here since the test waits for this
			// call to timeout.
			ctx, cancel := NewContext(testutils.Timeout(100 * time.Millisecond))
			defer cancel()
			_, _, _, err := raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)
			require.Equal(t, ErrTimeout, err, "Expected CallEcho to fail")
			defer wg.Done()
		}()

		wg.Wait()
		assert.False(t, gotCall, "Server should not receive a call")

		calls := relaytest.NewMockStats()
		calls.Add(client.PeerInfo().ServiceName, ts.ServiceName(), "echo").
			Failed("relay-dropped").End()
		ts.AssertRelayStats(calls)
	})
}

// Test that a stalled connection to a single server does not block all calls
// from that server, and we have stats to capture that this is happening.
func TestRelayStalledConnection(t *testing.T) {
	opts := testutils.NewOpts().
		AddLogFilter("Dropping call due to slow connection.", 1, "sendChCapacity", "32").
		SetSendBufferSize(32). // We want to hit the buffer size earlier, but also ensure we're only dropping once the sendCh is full.
		SetServiceName("s1").
		SetRelayOnly()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		s2 := ts.NewServer(testutils.NewOpts().SetServiceName("s2"))
		testutils.RegisterEcho(s2, nil)

		stall := make(chan struct{})
		stallComplete := make(chan struct{})
		stallHandler := func(ctx context.Context, call *InboundCall) {
			<-stall
			raw.ReadArgs(call)
			close(stallComplete)
		}
		ts.Register(HandlerFunc(stallHandler), "echo")

		ctx, cancel := NewContext(testutils.Timeout(300 * time.Millisecond))
		defer cancel()

		client := ts.NewClient(nil)
		call, err := client.BeginCall(ctx, ts.HostPort(), ts.ServiceName(), "echo", nil)
		require.NoError(t, err, "BeginCall failed")
		writer, err := call.Arg2Writer()
		require.NoError(t, err, "Arg2Writer failed")
		go io.Copy(writer, testreader.Looper([]byte("test")))

		// Try to read the response which might get an error.
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)

			_, err := call.Response().Arg2Reader()
			if assert.Error(t, err, "Expected error while reading") {
				assert.Contains(t, err.Error(), "frame was not sent to remote side")
			}
		}()

		// Wait for the reader to error out.
		select {
		case <-time.After(testutils.Timeout(10 * time.Second)):
			t.Fatalf("Test timed out waiting for reader to fail")
		case <-readDone:
		}

		// We should be able to make calls to s2 even if s1 is stalled.
		testutils.AssertEcho(t, client, ts.HostPort(), "s2")

		// Verify the sendCh is full, and the buffers are utilized.
		state := ts.Relay().IntrospectState(&IntrospectionOptions{})
		connState := state.RootPeers[ts.Server().PeerInfo().HostPort].OutboundConnections[0]
		assert.Equal(t, 32, connState.SendChCapacity, "unexpected SendChCapacity")
		assert.NotZero(t, connState.SendChQueued, "unexpected SendChQueued")
		assert.NotZero(t, connState.SendBufferUsage, "unexpected SendBufferUsage")
		assert.NotZero(t, connState.SendBufferSize, "unexpected SendBufferSize")

		// Cancel the call and unblock the stall handler.
		cancel()
		close(stall)

		// The server channel will not close until the stall handler receives
		// an error. Since we don't propagate cancels, the handler will keep
		// trying to read arguments till the timeout.
		select {
		case <-stallComplete:
		case <-time.After(testutils.Timeout(300 * time.Millisecond)):
			t.Fatalf("Stall handler did not complete")
		}

		calls := relaytest.NewMockStats()
		calls.Add(client.PeerInfo().ServiceName, ts.ServiceName(), "echo").
			Failed("relay-dest-conn-slow").End()
		calls.Add(client.PeerInfo().ServiceName, "s2", "echo").
			Succeeded().End()
		ts.AssertRelayStats(calls)
	})
}

// Test that a stalled connection to the client does not cause stuck calls
// See https://github.com/uber/tchannel-go/issues/700 for more info.
func TestRelayStalledClientConnection(t *testing.T) {
	// This needs to be large enough to fill up the client TCP buffer.
	const _calls = 100

	opts := testutils.NewOpts().
		// Expect errors from dropped frames.
		AddLogFilter("Dropping call due to slow connection.", _calls).
		SetSendBufferSize(10). // We want to hit the buffer size earlier.
		SetServiceName("s1").
		SetRelayOnly()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		// Track when the server receives calls
		gotCall := make(chan struct{}, _calls)
		testutils.RegisterEcho(ts.Server(), func() {
			gotCall <- struct{}{}
		})

		// Create a frame relay that will block all client inbound frames.
		unblockClientInbound := make(chan struct{})
		blockerHostPort, relayCancel := testutils.FrameRelay(t, ts.HostPort(), func(outgoing bool, f *Frame) *Frame {
			if !outgoing && f.Header.ID > 1 {
				// Block all inbound frames except the initRes
				<-unblockClientInbound
			}

			return f
		})
		defer relayCancel()
		defer close(unblockClientInbound)

		client := ts.NewClient(nil)

		ctx, cancel := NewContext(testutils.Timeout(time.Second))
		defer cancel()

		var calls []*OutboundCall

		// Data to fit one frame fully, but large enough that a number of these frames will fill
		// all the buffers and cause the relay to drop the response frame. Buffers are:
		// 1. Relay's sendCh on the connection to the client (set to 10 frames explicitly)
		// 2. Relay's TCP send buffer for the connection to the client.
		// 3. Client's TCP receive buffer on the connection to the relay.
		data := bytes.Repeat([]byte("test"), 256*60)
		for i := 0; i < _calls; i++ {
			call, err := client.BeginCall(ctx, blockerHostPort, ts.ServiceName(), "echo", nil)
			require.NoError(t, err, "BeginCall failed")

			require.NoError(t, NewArgWriter(call.Arg2Writer()).Write(nil), "arg2 write failed")
			require.NoError(t, NewArgWriter(call.Arg3Writer()).Write(data), "arg2 write failed")

			// Wait for server to receive the call
			<-gotCall

			calls = append(calls, call)
		}

		// Wait for all calls to end on the relay, and ensure we got failures from the slow client.
		stats := ts.RelayHost().Stats()
		stats.WaitForEnd()
		assert.Contains(t, stats.Map(), "testService-client->s1::echo.failed-relay-source-conn-slow", "Expect at least 1 failed call due to slow client")

		// We don't read the responses, as we want the client's TCP buffers to fill up
		// and the relay to drop calls. However, we should unblock the client reader
		// to make sure the client channel can close.
		// Unblock the client so it can close.
		cancel()
		for _, call := range calls {
			require.Error(t, NewArgReader(call.Response().Arg2Reader()).Read(&data), "should fail to read response")
		}
	})
}
func TestRelayThroughSeparateRelay(t *testing.T) {
	opts := testutils.NewOpts().
		SetRelayOnly()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		serverHP := ts.Server().PeerInfo().HostPort
		dummyFactory := func(relay.CallFrame, *relay.Conn) (string, error) {
			panic("should not get invoked")
		}
		relay2Opts := testutils.NewOpts().SetRelayHost(relaytest.HostFunc(dummyFactory))
		relay2 := ts.NewServer(relay2Opts)

		// Override where the peers come from.
		ts.RelayHost().SetChannel(relay2)
		relay2.GetSubChannel(ts.ServiceName(), Isolated).Peers().Add(serverHP)

		testutils.RegisterEcho(ts.Server(), nil)
		client := ts.NewClient(nil)
		testutils.AssertEcho(t, client, ts.HostPort(), ts.ServiceName())

		numConns := func(p PeerRuntimeState) int {
			return len(p.InboundConnections) + len(p.OutboundConnections)
		}

		// Verify that there are no connections from ts.Relay() to the server.
		introspected := ts.Relay().IntrospectState(nil)
		assert.Zero(t, numConns(introspected.RootPeers[serverHP]), "Expected no connections from relay to server")

		introspected = relay2.IntrospectState(nil)
		assert.Equal(t, 1, numConns(introspected.RootPeers[serverHP]), "Expected 1 connection from relay2 to server")
	})
}

func TestRelayConcurrentNewConnectionAttempts(t *testing.T) {
	opts := testutils.NewOpts().SetRelayOnly()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		// Create a server that is slow to accept connections by using
		// a frame relay to slow down the initial message.
		slowServer := testutils.NewServer(t, serviceNameOpts("slow-server"))
		defer slowServer.Close()
		testutils.RegisterEcho(slowServer, nil)

		var delayed atomic.Bool
		relayFunc := func(outgoing bool, f *Frame) *Frame {
			if !delayed.Load() {
				time.Sleep(testutils.Timeout(50 * time.Millisecond))
				delayed.Store(true)
			}
			return f
		}

		slowHP, close := testutils.FrameRelay(t, slowServer.PeerInfo().HostPort, relayFunc)
		defer close()
		ts.RelayHost().Add("slow-server", slowHP)

		// Make concurrent calls to trigger concurrent getConnectionRelay calls.
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			// Create client and get dest host:port in the main goroutine to avoid races.
			client := ts.NewClient(nil)
			relayHostPort := ts.HostPort()
			go func() {
				defer wg.Done()
				testutils.AssertEcho(t, client, relayHostPort, "slow-server")
			}()
		}
		wg.Wait()

		// Verify that the slow server only received a single connection.
		inboundConns := 0
		for _, state := range slowServer.IntrospectState(nil).RootPeers {
			inboundConns += len(state.InboundConnections)
		}
		assert.Equal(t, 1, inboundConns, "Expected a single inbound connection to the server")
	})
}

func TestRelayRaceTimerCausesStuckConnectionOnClose(t *testing.T) {
	const (
		concurrentClients = 15
		callsPerClient    = 100
	)
	opts := testutils.NewOpts().
		SetRelayOnly().
		SetSendBufferSize(concurrentClients * callsPerClient) // Avoid dropped frames causing unexpected logs.
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)
		// Create clients and ensure we can make a successful request.
		clients := make([]*Channel, concurrentClients)

		var callTime time.Duration
		for i := range clients {
			clients[i] = ts.NewClient(opts)
			started := time.Now()
			testutils.AssertEcho(t, clients[i], ts.HostPort(), ts.ServiceName())
			callTime = time.Since(started)
		}

		// Overwrite the echo method with one that times out for the test.
		ts.Server().Register(HandlerFunc(func(ctx context.Context, call *InboundCall) {
			call.Response().Blackhole()
		}), "echo")

		var wg sync.WaitGroup
		for i := 0; i < concurrentClients; i++ {
			wg.Add(1)
			go func(client *Channel) {
				defer wg.Done()

				for j := 0; j < callsPerClient; j++ {
					// Make many concurrent calls which, some of which should timeout.
					ctx, cancel := NewContext(callTime)
					raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)
					cancel()
				}
			}(clients[i])
		}

		wg.Wait()
	})
}

func TestRelayRaceCompletionAndTimeout(t *testing.T) {
	const numCalls = 10

	opts := testutils.NewOpts().
		AddLogFilter("simpleHandler OnError.", numCalls).
		SetRelayOnly()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)

		client := ts.NewClient(nil)
		started := time.Now()
		testutils.AssertEcho(t, client, ts.HostPort(), ts.ServiceName())
		callTime := time.Since(started)

		// Make many calls with the same timeout, with the goal of
		// timing out right as we process the response frame.
		var wg sync.WaitGroup
		for i := 0; i < numCalls; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				ctx, cancel := NewContext(callTime)
				raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), "echo", nil, nil)
				cancel()
			}()
		}

		// Some of those calls should triger the race.
		wg.Wait()
	})
}

func TestRelayArg2OffsetIntegration(t *testing.T) {
	ctx, cancel := NewContext(testutils.Timeout(time.Second))
	defer cancel()

	rh := relaytest.NewStubRelayHost()
	frameCh := inspectFrames(rh)
	opts := testutils.NewOpts().
		SetRelayOnly().
		SetRelayHost(rh)

	testutils.WithTestServer(t, opts, func(tb testing.TB, ts *testutils.TestServer) {
		const (
			testMethod = "echo"
			arg2Data   = "arg2-is"
			arg3Data   = "arg3-here"
		)

		var (
			wantArg2Start = len(ts.ServiceName()) + len(testMethod) + 70 /*data before arg1*/
			payloadLeft   = MaxFramePayloadSize - wantArg2Start
		)

		testutils.RegisterEcho(ts.Server(), nil)

		client := testutils.NewClient(t, nil /*opts*/)
		defer client.Close()

		tests := []struct {
			msg               string
			arg2Data          string
			arg2Flush         bool
			arg2PostFlushData string
			noArg3            bool
			wantEndOffset     int
			wantHasMore       bool
		}{
			{
				msg:           "all within a frame",
				arg2Data:      arg2Data,
				wantEndOffset: wantArg2Start + len(arg2Data),
				wantHasMore:   false,
			},
			{
				msg:           "arg2 flushed",
				arg2Data:      arg2Data,
				arg2Flush:     true,
				wantEndOffset: wantArg2Start + len(arg2Data),
				wantHasMore:   true,
			},
			{
				msg:               "arg2 flushed called then write again",
				arg2Data:          arg2Data,
				arg2Flush:         true,
				arg2PostFlushData: "more data",
				wantEndOffset:     wantArg2Start + len(arg2Data),
				wantHasMore:       true,
			},
			{
				msg:           "no arg2 but flushed",
				wantEndOffset: wantArg2Start,
				wantHasMore:   false,
			},
			{
				msg:           "XL arg2 which is fragmented",
				arg2Data:      string(make([]byte, MaxFrameSize+100)),
				wantEndOffset: wantArg2Start + payloadLeft,
				wantHasMore:   true,
			},
			{
				msg:           "large arg2 with 3 bytes left for arg3",
				arg2Data:      string(make([]byte, payloadLeft-3)),
				wantEndOffset: wantArg2Start + payloadLeft - 3,
				wantHasMore:   false,
			},
			{
				msg:           "large arg2, 2 bytes left",
				arg2Data:      string(make([]byte, payloadLeft-2)),
				wantEndOffset: wantArg2Start + payloadLeft - 2,
				wantHasMore:   true, // no arg3
			},
			{
				msg:           "large arg2, 2 bytes left, no arg3",
				arg2Data:      string(make([]byte, payloadLeft-2)),
				wantEndOffset: wantArg2Start + payloadLeft - 2,
				noArg3:        true,
				wantHasMore:   true, // no arg3 and still got CALL_REQ_CONTINUE
			},
			{
				msg:           "large arg2, 1 bytes left",
				arg2Data:      string(make([]byte, payloadLeft-1)),
				wantEndOffset: wantArg2Start + payloadLeft - 1,
				wantHasMore:   true, // no arg3
			},
		}

		for _, tt := range tests {
			t.Run(tt.msg, func(t *testing.T) {
				call, err := client.BeginCall(ctx, ts.HostPort(), ts.ServiceName(), testMethod, nil)
				require.NoError(t, err, "BeginCall failed")
				writer, err := call.Arg2Writer()
				require.NoError(t, err)
				_, err = writer.Write([]byte(tt.arg2Data))
				require.NoError(t, err)
				if tt.arg2Flush {
					writer.Flush()
					// tries to write after flush
					if tt.arg2PostFlushData != "" {
						_, err := writer.Write([]byte(tt.arg2PostFlushData))
						require.NoError(t, err)
					}
				}
				require.NoError(t, writer.Close())

				arg3DataToWrite := arg3Data
				if tt.noArg3 {
					arg3DataToWrite = ""
				}
				require.NoError(t, NewArgWriter(call.Arg3Writer()).Write([]byte(arg3DataToWrite)), "arg3 write failed")

				f := <-frameCh
				start := f.Arg2StartOffset()
				end, hasMore := f.Arg2EndOffset()
				assert.Equal(t, wantArg2Start, start, "arg2 start offset does not match expectation")
				assert.Equal(t, tt.wantEndOffset, end, "arg2 end offset does not match expectation")
				assert.Equal(t, tt.wantHasMore, hasMore, "arg2 hasMore bit does not match expectation")

				gotArg2, gotArg3, err := raw.ReadArgsV2(call.Response())
				assert.NoError(t, err)
				assert.Equal(t, tt.arg2Data+tt.arg2PostFlushData, string(gotArg2), "arg2 in response does not meet expectation")
				assert.Equal(t, arg3DataToWrite, string(gotArg3), "arg3 in response does not meet expectation")
			})
		}
	})
}

func TestRelayThriftArg2KeyValueIteration(t *testing.T) {
	ctx, cancel := NewContext(testutils.Timeout(time.Second))
	defer cancel()

	rh := relaytest.NewStubRelayHost()
	frameCh := inspectFrames(rh)
	opts := testutils.NewOpts().
		SetRelayOnly().
		SetRelayHost(rh)

	testutils.WithTestServer(t, opts, func(tb testing.TB, ts *testutils.TestServer) {
		kv := map[string]string{
			"key":     "val",
			"key2":    "valval",
			"longkey": "valvalvalval",
		}
		arg2Buf := thriftarg2test.BuildKVBuffer(kv)

		const (
			testMethod = "echo"
			arg3Data   = "arg3-here"
		)

		testutils.RegisterEcho(ts.Server(), nil)

		client := testutils.NewClient(t, nil /*opts*/)
		defer client.Close()

		call, err := client.BeginCall(ctx, ts.HostPort(), ts.ServiceName(), testMethod, &CallOptions{Format: Thrift})
		require.NoError(t, err, "BeginCall failed")
		require.NoError(t, NewArgWriter(call.Arg2Writer()).Write(arg2Buf), "arg2 write failed")
		require.NoError(t, NewArgWriter(call.Arg3Writer()).Write([]byte(arg3Data)), "arg3 write failed")

		f := <-frameCh
		iter, err := f.Arg2Iterator()
		gotKV := make(map[string]string)
		for err == nil {
			gotKV[string(iter.Key())] = string(iter.Value())
			iter, err = iter.Next()
		}
		assert.Equal(t, kv, gotKV)
		assert.Equal(t, io.EOF, err)

		gotArg2, gotArg3, err := raw.ReadArgsV2(call.Response())
		assert.NoError(t, err)
		assert.Equal(t, string(arg2Buf), string(gotArg2), "arg2 in response does not meet expectation")
		assert.Equal(t, arg3Data, string(gotArg3), "arg3 in response does not meet expectation")
	})
}

func TestRelayConnectionTimeout(t *testing.T) {
	var (
		minTimeout = testutils.Timeout(10 * time.Millisecond)
		maxTimeout = testutils.Timeout(time.Minute)
	)
	tests := []struct {
		msg            string
		callTimeout    time.Duration
		maxConnTimeout time.Duration
		minTime        time.Duration
	}{
		{
			msg:         "only call timeout is set",
			callTimeout: 2 * minTimeout,
		},
		{
			msg:            "call timeout < relay timeout",
			callTimeout:    2 * minTimeout,
			maxConnTimeout: 2 * maxTimeout,
		},
		{
			msg:            "relay timeout < call timeout",
			callTimeout:    2 * maxTimeout,
			maxConnTimeout: 2 * minTimeout,
		},
		{
			msg:            "relay timeout == call timeout",
			callTimeout:    2 * minTimeout,
			maxConnTimeout: 2 * minTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			opts := testutils.NewOpts().
				SetRelayOnly().
				SetRelayMaxConnectionTimeout(tt.maxConnTimeout).
				AddLogFilter("Failed during connection handshake.", 1).
				AddLogFilter("Failed to connect to relay host.", 1)
			testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err, "Failed to listen")
				defer ln.Close()

				// TCP listener will never complete the handshake and always timeout.
				ts.RelayHost().Add("blocked", ln.Addr().String())

				start := time.Now()

				ctx, cancel := NewContext(testutils.Timeout(tt.callTimeout))
				defer cancel()

				// We expect connection error logs from the client.
				client := ts.NewClient(nil /* opts */)
				_, _, _, err = raw.Call(ctx, client, ts.HostPort(), "blocked", "echo", nil, nil)
				assert.Equal(t, ErrTimeout, err)

				taken := time.Since(start)
				if taken < minTimeout || taken > maxTimeout {
					t.Errorf("Took %v, expected [%v, %v]", taken, minTimeout, maxTimeout)
				}
			})
		})
	}
}

func TestRelayTransferredBytes(t *testing.T) {
	const (
		kb = 1024

		// The maximum delta between the payload size and the bytes on wire.
		protocolBuffer = kb
	)

	rh := relaytest.NewStubRelayHost()
	opts := testutils.NewOpts().
		SetRelayHost(rh).
		SetRelayOnly()

	testutils.WithTestServer(t, opts, func(tb testing.TB, ts *testutils.TestServer) {
		// Note: Upcast to testing.T so we can use t.Run.
		t := tb.(*testing.T)

		s1 := ts.NewServer(testutils.NewOpts().SetServiceName("s1"))
		s2 := ts.NewServer(testutils.NewOpts().SetServiceName("s2"))
		testutils.RegisterEcho(s1, nil)
		testutils.RegisterEcho(s2, nil)

		// Add a handler that always returns an empty payload.
		testutils.RegisterFunc(s2, "swallow", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			fmt.Println("swallow got", len(args.Arg2)+len(args.Arg3))
			return &raw.Res{}, nil
		})

		// Helper to make calls with specific payload sizes.
		makeCall := func(src, dst *Channel, method string, arg2Size, arg3Size int) {
			ctx, cancel := NewContext(testutils.Timeout(time.Second))
			defer cancel()

			arg2 := testutils.RandBytes(arg2Size)
			arg3 := testutils.RandBytes(arg3Size)

			_, _, _, err := raw.Call(ctx, src, ts.HostPort(), dst.ServiceName(), method, arg2, arg3)
			require.NoError(t, err)
		}

		t.Run("verify sent vs received", func(t *testing.T) {
			makeCall(s1, s2, "swallow", 4*1024, 4*1024)

			statsMap := rh.Stats().Map()
			assert.InDelta(t, 8*kb, statsMap["s1->s2::swallow.sent-bytes"], protocolBuffer, "Unexpected sent bytes")
			assert.InDelta(t, 0, statsMap["s1->s2::swallow.received-bytes"], protocolBuffer, "Unexpected sent bytes")
		})

		t.Run("verify sent and received", func(t *testing.T) {
			makeCall(s1, s2, "echo", 4*kb, 4*kb)

			statsMap := rh.Stats().Map()
			assert.InDelta(t, 8*kb, statsMap["s1->s2::echo.sent-bytes"], protocolBuffer, "Unexpected sent bytes")
			assert.InDelta(t, 8*kb, statsMap["s1->s2::echo.received-bytes"], protocolBuffer, "Unexpected sent bytes")
		})

		t.Run("verify large payload", func(t *testing.T) {
			makeCall(s1, s2, "echo", 128*1024, 128*1024)

			statsMap := rh.Stats().Map()
			assert.InDelta(t, 256*kb, statsMap["s1->s2::echo.sent-bytes"], protocolBuffer, "Unexpected sent bytes")
			assert.InDelta(t, 256*kb, statsMap["s1->s2::echo.received-bytes"], protocolBuffer, "Unexpected sent bytes")
		})

		t.Run("verify reverse call", func(t *testing.T) {
			makeCall(s2, s1, "echo", 0, 64*kb)

			statsMap := rh.Stats().Map()
			assert.InDelta(t, 64*kb, statsMap["s2->s1::echo.sent-bytes"], protocolBuffer, "Unexpected sent bytes")
			assert.InDelta(t, 64*kb, statsMap["s2->s1::echo.received-bytes"], protocolBuffer, "Unexpected sent bytes")
		})
	})
}

func TestRelayAppendArg2SentBytes(t *testing.T) {
	tests := []struct {
		msg           string
		appends       map[string]string
		arg3          []byte
		wantSentBytes int
	}{
		{
			msg:           "without appends",
			arg3:          []byte("hello, world"),
			wantSentBytes: 127,
		},
		{
			msg:           "with appends",
			arg3:          []byte("hello, world"),
			appends:       map[string]string{"baz": "qux"},
			wantSentBytes: 137, // 127 + 2 bytes size + 3 bytes key + 2 byts size + 3 bytes val = 137
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			rh := relaytest.NewStubRelayHost()
			rh.SetFrameFn(func(f relay.CallFrame, conn *relay.Conn) {
				for k, v := range tt.appends {
					f.Arg2Append([]byte(k), []byte(v))
				}
			})

			opts := testutils.NewOpts().SetRelayOnly().SetRelayHost(rh)
			testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
				rly := ts.Relay()
				svr := ts.Server()
				testutils.RegisterEcho(svr, nil)

				client := testutils.NewClient(t, nil)
				ctx, cancel := NewContext(testutils.Timeout(300 * time.Millisecond))
				defer cancel()

				sendArgs := &raw.Args{
					Arg2: thriftarg2test.BuildKVBuffer(map[string]string{"foo": "bar"}),
					Arg3: tt.arg3,
				}

				recvArg2, recvArg3, _, err := raw.Call(ctx, client, rly.PeerInfo().HostPort, ts.ServiceName(), "echo", sendArgs.Arg2, sendArgs.Arg3)
				if !assert.NoError(t, err, "Call from %v (%v) to %v (%v) failed", client.ServiceName(), client.PeerInfo().HostPort, ts.ServiceName(), rly.PeerInfo().HostPort) {
					return
				}

				wantArg2 := map[string]string{
					"foo": "bar",
				}
				for k, v := range tt.appends {
					wantArg2[k] = v
				}

				assert.Equal(t, wantArg2, thriftarg2test.MustReadKVBuffer(t, recvArg2), "Arg2 mismatch")
				assert.Equal(t, recvArg3, []byte("hello, world"), "Arg3 mismatch")

				sentBytes := rh.Stats().Map()["testService-client->testService::echo.sent-bytes"]
				assert.Equal(t, tt.wantSentBytes, sentBytes)
			})
		})
	}
}

func inspectFrames(rh *relaytest.StubRelayHost) chan relay.CallFrame {
	frameCh := make(chan relay.CallFrame, 1)
	rh.SetFrameFn(func(f relay.CallFrame, _ *relay.Conn) {
		frameCh <- testutils.CopyCallFrame(f)
	})
	return frameCh
}

func TestRelayModifyArg2(t *testing.T) {
	largeKey := testutils.RandString(378)
	largeVal := testutils.RandString(65000)
	largePayload := testutils.RandString(1024)

	modifyTests := []struct {
		msg         string
		skip        string
		modifyFrame func(relay.CallFrame, *relay.Conn)
		modifyArg2  func(arg2 map[string]string) map[string]string
	}{
		{
			msg:         "no change",
			modifyFrame: func(cf relay.CallFrame, _ *relay.Conn) {},
		},
		{
			msg: "add small key/value",
			modifyFrame: func(cf relay.CallFrame, _ *relay.Conn) {
				cf.Arg2Append([]byte("key"), []byte("value"))
			},
			modifyArg2: func(m map[string]string) map[string]string {
				m["key"] = "value"
				return m
			},
		},
		{
			msg: "add large key/value",
			modifyFrame: func(cf relay.CallFrame, _ *relay.Conn) {
				cf.Arg2Append([]byte(largeKey), []byte(largeVal))
			},
			modifyArg2: func(m map[string]string) map[string]string {
				m[largeKey] = largeVal
				return m
			},
		},
	}

	checksumTypes := []struct {
		msg          string
		checksumType ChecksumType
	}{
		{"none", ChecksumTypeNone},
		{"crc32", ChecksumTypeCrc32},
		{"farmhash", ChecksumTypeFarmhash},
		{"crc32c", ChecksumTypeCrc32C},
	}

	payloadTests := []struct {
		msg  string
		arg2 map[string]string
		arg3 []byte
	}{
		{
			msg:  "no payload",
			arg2: nil, // empty map
			arg3: []byte{},
		},
		{
			msg: "small payloads",
			arg2: map[string]string{
				"existingKey": "existingValue",
			},
			arg3: []byte("hello world"),
		},
		{
			msg: "large payloads",
			arg2: map[string]string{
				"existingKey": "existingValue",
			},
			arg3: []byte(largePayload),
		},
	}

	const (
		format      = Thrift
		noErrMethod = "EchoVerifyNoErr"
		errMethod   = "EchoVerifyErr"
	)

	appErrTests := []struct {
		msg        string
		method     string
		wantAppErr bool
	}{
		{
			msg:        "no app error bit",
			method:     noErrMethod,
			wantAppErr: false,
		},
		{
			msg:        "app error bit",
			method:     errMethod,
			wantAppErr: true,
		},
	}

	for _, mt := range modifyTests {
		for _, csTest := range checksumTypes {
			t.Run(mt.msg+"/checksum="+csTest.msg, func(t *testing.T) {
				// Create a relay that will modify the frame as per the test.
				relayHost := relaytest.NewStubRelayHost()
				relayHost.SetFrameFn(mt.modifyFrame)
				opts := testutils.NewOpts().
					SetRelayHost(relayHost).
					SetRelayOnly()
				testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
					// Create a client that uses a specific checksumType.
					clientOpts := testutils.NewOpts().SetChecksumType(csTest.checksumType)
					client := ts.NewClient(clientOpts)
					defer client.Close()

					// Create a server echo verify endpoints (optionally returning an error).
					for _, appErrTest := range appErrTests {
						handler := echoVerifyHandler{
							t:            t,
							verifyFormat: format,
							verifyCaller: client.ServiceName(),
							verifyMethod: appErrTest.method,
							appErr:       appErrTest.wantAppErr,
						}
						ts.Server().Register(raw.Wrap(handler), appErrTest.method)
					}

					ctx, cancel := NewContextBuilder(testutils.Timeout(time.Second)).
						SetFormat(format).Build()
					defer cancel()

					// Make calls with different payloads and expected errors.
					for _, aet := range appErrTests {
						for _, tt := range payloadTests {
							t.(*testing.T).Run(aet.msg+","+tt.msg, func(t *testing.T) {
								arg2Encoded := encodeThriftHeaders(t, tt.arg2)

								resArg2, resArg3, resp, err := raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), aet.method, arg2Encoded, tt.arg3)
								require.NoError(t, err, "%v: Received unexpected error", tt.msg)
								assert.Equal(t, format, resp.Format(), "%v: Unexpected error format")
								assert.Equal(t, aet.wantAppErr, resp.ApplicationError(), "%v: Unexpected app error")

								wantArg2 := copyHeaders(tt.arg2)
								if mt.modifyArg2 != nil {
									if wantArg2 == nil {
										wantArg2 = make(map[string]string)
									}
									wantArg2 = mt.modifyArg2(wantArg2)
								}

								gotArg2Map := decodeThriftHeaders(t, resArg2)
								assert.Equal(t, wantArg2, gotArg2Map, "%v: Unexpected arg2 headers", tt.msg)
								assert.Equal(t, resArg3, tt.arg3, "%v: Unexpected arg3", tt.msg)
							})
						}
					}
				})
			})
		}
	}
}

// echoVerifyHandler is an echo handler with some added verification of
// the call metadata (e.g., caller, format).
type echoVerifyHandler struct {
	t testing.TB

	appErr       bool
	verifyFormat Format
	verifyCaller string
	verifyMethod string
}

func (h echoVerifyHandler) Handle(ctx context.Context, args *raw.Args) (*raw.Res, error) {
	assert.Equal(h.t, h.verifyFormat, args.Format, "Unexpected format")
	assert.Equal(h.t, h.verifyCaller, args.Caller, "Unexpected caller")
	assert.Equal(h.t, h.verifyMethod, args.Method, "Unexpected method")

	return &raw.Res{
		Arg2:  args.Arg2,
		Arg3:  args.Arg3,
		IsErr: h.appErr,
	}, nil
}

func (h echoVerifyHandler) OnError(ctx context.Context, err error) {
	h.t.Errorf("unexpected OnError: %v", err)
}

func encodeThriftHeaders(t testing.TB, m map[string]string) []byte {
	var buf bytes.Buffer
	require.NoError(t, thrift.WriteHeaders(&buf, m), "Failed to write headers")
	return buf.Bytes()
}

func decodeThriftHeaders(t testing.TB, bs []byte) map[string]string {
	m, err := thrift.ReadHeaders(bytes.NewReader(bs))
	require.NoError(t, err, "Failed to read headers")
	return m
}

func copyHeaders(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}

	copied := make(map[string]string, len(m))
	for k, v := range m {
		copied[k] = v
	}
	return copied
}
