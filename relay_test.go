package tchannel_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	. "github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"
)

type relayTest struct {
	testutils.TestServer
}

func serviceNameOpts(s string) *testutils.ChannelOpts {
	return testutils.NewOpts().SetServiceName(s)
}

func withRelayedEcho(t testing.TB, f func(relay, server, client *Channel)) {
	opts := serviceNameOpts("test").SetRelayOnly()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		testutils.RegisterEcho(ts.Server(), nil)
		client := ts.NewClient(serviceNameOpts("client"))
		client.Peers().Add(ts.HostPort())
		f(ts.Relay(), ts.Server(), client)
	})
}

func TestRelay(t *testing.T) {
	withRelayedEcho(t, func(_, _, client *Channel) {
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
	})
}

func DisabledTestRelayHandlesCrashedPeers(t *testing.T) {
	withRelayedEcho(t, func(_, server, client *Channel) {
		ctx, cancel := NewContext(time.Second)
		defer cancel()

		sc := client.GetSubChannel("test")
		_, _, _, err := raw.CallSC(ctx, sc, "echo", []byte("fake-header"), []byte("fake-body"))
		require.NoError(t, err, "Relayed call failed.")

		// Simulate a server crash.
		server.Close()
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

func TestRelayErrorUnknownPeer(t *testing.T) {
	opts := testutils.NewOpts().SetRelayOnly()
	testutils.WithTestServer(t, opts, func(ts *testutils.TestServer) {
		client := ts.NewClient(nil)

		err := testutils.CallEcho(client, ts.HostPort(), "random-service", nil)
		if !assert.Error(t, err, "Call to unknown service should fail") {
			return
		}

		se, ok := err.(SystemError)
		if !assert.True(t, ok, "err should be a SystemError, got %T", err) {
			return
		}

		assert.Equal(t, ErrCodeDeclined, se.Code(), "Expected Declined error")
		assert.Contains(t, err.Error(), `no peers for "random-service"`, "Unexpected error")
	})
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
	})
}
