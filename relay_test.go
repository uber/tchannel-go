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

func assertRelaysReleased(t testing.TB, ch *Channel) {
	for _, peerState := range ch.IntrospectState(nil).RootPeers {
		var connStates []ConnectionRuntimeState
		connStates = append(connStates, peerState.InboundConnections...)
		connStates = append(connStates, peerState.OutboundConnections...)
		for _, connState := range connStates {
			n := connState.Relayer.NumItems
			assert.Equal(t, 0, n, "Found %v left-over items in relayer for %v.", n, connState.LocalHostPort)
		}
	}
}

func withRelayedEcho(t testing.TB, f func(relay, server, client *Channel)) {
	relay, err := NewChannel("relay", &ChannelOptions{
		RelayHosts: NewSimpleRelayHosts(map[string][]string{}),
	})
	require.NoError(t, err, "Failed to create a relay channel.")
	defer assertRelaysReleased(t, relay)
	defer relay.Close()
	require.NoError(t, relay.ListenAndServe("127.0.0.1:0"), "Relay failed to listen.")

	server := testutils.NewServer(t, testutils.NewOpts().SetServiceName("test"))
	defer server.Close()
	server.Register(raw.Wrap(newTestHandler(t)), "echo")
	relay.RelayHosts().(*SimpleRelayHosts).Add("test", server.PeerInfo().HostPort)

	client := testutils.NewServer(t, nil)
	defer client.Close()
	client.Peers().Add(relay.PeerInfo().HostPort)

	f(relay, server, client)
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
			require.NoError(t, err, "TODO")
			conn.Close()
		})

		_, _, _, err := raw.CallSC(ctx, client.GetSubChannel("test"), "echo", []byte("fake-header"), []byte("fake-body"))
		require.NoError(t, err, "Relayed call failed.")
	})

	goroutines.VerifyNoLeaks(t, nil)
}
