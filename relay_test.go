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
	"github.com/uber/tchannel-go/testutils/goroutines"
)

type relayTest struct {
	testing.TB

	relay      *Channel
	relayHosts *testutils.SimpleRelayHosts
	servers    []*Channel
	clients    []*Channel
}

func (t *relayTest) checkRelaysEmpty() {
	for _, peerState := range t.relay.IntrospectState(nil).RootPeers {
		var connStates []ConnectionRuntimeState
		connStates = append(connStates, peerState.InboundConnections...)
		connStates = append(connStates, peerState.OutboundConnections...)
		for _, connState := range connStates {
			n := connState.Relayer.NumItems
			assert.Equal(t, 0, n, "Found %v left-over items in relayer for %v.", n, connState.LocalHostPort)
		}
	}
}

func (t *relayTest) newServer(serviceName string, opts *testutils.ChannelOpts) *Channel {
	if opts == nil {
		opts = testutils.NewOpts()
	}
	opts.SetServiceName(serviceName)

	server := testutils.NewServer(t, opts)
	server.Peers().Add(t.relay.PeerInfo().HostPort)
	t.relayHosts.Add(serviceName, server.PeerInfo().HostPort)
	t.servers = append(t.servers, server)
	return server
}

func (t *relayTest) newClient(clientName string, opts *testutils.ChannelOpts) *Channel {
	if opts == nil {
		opts = testutils.NewOpts()
	}

	opts.SetServiceName(clientName)

	client := testutils.NewClient(t, opts)
	client.Peers().Add(t.relay.PeerInfo().HostPort)
	t.clients = append(t.clients, client)
	return client
}

func (t *relayTest) closeChannels() {
	t.relay.Close()
	for _, ch := range t.servers {
		ch.Close()
	}
	for _, ch := range t.clients {
		ch.Close()
	}
	// TODO: we should find some way to validate the Close, similar to testutils.WithTestServer
}

func withRelayTest(t testing.TB, f func(rt *relayTest)) {
	relayHosts := testutils.NewSimpleRelayHosts(map[string][]string{})
	relay, err := NewChannel("relay", &ChannelOptions{
		RelayHosts: relayHosts,
	})
	require.NoError(t, err, "Failed to create a relay channel.")
	defer relay.Close()
	require.NoError(t, relay.ListenAndServe("127.0.0.1:0"), "Relay failed to listen.")

	rt := &relayTest{
		TB:         t,
		relay:      relay,
		relayHosts: relayHosts,
	}
	defer rt.closeChannels()

	f(rt)

	// Only check relays are empty if the test hasn't failed.
	if !t.Failed() {
		rt.checkRelaysEmpty()
	}
}

func withRelayedEcho(t testing.TB, f func(relay, server, client *Channel)) {
	withRelayTest(t, func(rt *relayTest) {
		server := rt.newServer("test", nil)
		testutils.RegisterEcho(server, nil)

		client := rt.newClient("client", nil)
		f(rt.relay, server, client)
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
	withRelayTest(t, func(rt *relayTest) {
		ctx, cancel := NewContext(time.Second)
		defer cancel()

		s1 := rt.newServer("s1", nil)
		s2 := rt.newServer("s2", nil)

		s2HP := s2.PeerInfo().HostPort
		testutils.RegisterEcho(s1, func() {
			// When s1 gets called, it calls Close on the connection from the relay to s2.
			conn, err := rt.relay.Peers().GetOrAdd(s2HP).GetConnection(ctx)
			require.NoError(t, err, "Unexpected failure getting connection between s1 and relay")
			conn.Close()
		})

		testutils.AssertEcho(t, s2, rt.relay.PeerInfo().HostPort, "s1")
	})

	goroutines.VerifyNoLeaks(t, nil)
}

func TestRelayIDClash(t *testing.T) {
	withRelayTest(t, func(rt *relayTest) {
		s1 := rt.newServer("s1", nil)
		s2 := rt.newServer("s2", nil)

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
				testutils.AssertEcho(t, s2, rt.relay.PeerInfo().HostPort, s1.ServiceName())
			}()
		}

		for i := 0; i < 5; i++ {
			testutils.AssertEcho(t, s1, rt.relay.PeerInfo().HostPort, s2.ServiceName())
		}

		close(unblock)
		wg.Wait()
	})
}

func TestRelayErrorUnknownPeer(t *testing.T) {
	withRelayTest(t, func(rt *relayTest) {
		client := rt.newClient("client", nil)

		err := testutils.CallEcho(client, rt.relay.PeerInfo().HostPort, "random-service", nil)
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
	// withRelayTest validates that there are no relay items left after the given func.
	withRelayTest(t, func(rt *relayTest) {
		rt.newServer("svc", testutils.NewOpts().AddLogFilter("Couldn't find handler", 1))
		client := rt.newClient("client", nil)

		err := testutils.CallEcho(client, rt.relay.PeerInfo().HostPort, "svc", nil)
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
