package tchannel_test

import (
	"strings"
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
	relayHosts *SimpleRelayHosts
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
	relayHosts := NewSimpleRelayHosts(map[string][]string{})
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
	withRelayedEcho(t, func(relay, server, client *Channel) {
		ctx, cancel := NewContext(time.Second)
		defer cancel()

		clientHP := client.PeerInfo().HostPort
		testutils.RegisterEcho(server, func() {
			// Handler should close the connection to the relay.
			conn, err := relay.Peers().GetOrAdd(clientHP).GetConnection(ctx)
			require.NoError(t, err, "Unexpected failure getting a connection to the client.")
			conn.Close()
		})

		_, _, _, err := raw.CallSC(ctx, client.GetSubChannel("test"), "echo", []byte("fake-header"), []byte("fake-body"))
		require.NoError(t, err, "Relayed call failed.")
	})

	goroutines.VerifyNoLeaks(t, nil)
}
