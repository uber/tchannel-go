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

func withRelayedEcho(t testing.TB, f func(relay, server *Channel, client *SubChannel)) {
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

	client := testutils.NewClient(t, nil)
	defer client.Close()
	client.Peers().Add(relay.PeerInfo().HostPort)
	sc := client.GetSubChannel("test")

	f(relay, server, sc)
}

func TestRelay(t *testing.T) {
	withRelayedEcho(t, func(_, _ *Channel, sc *SubChannel) {
		tests := []struct {
			header string
			body   string
		}{
			{"fake-header", "fake-body"},                        // fits in one frame
			{"fake-header", strings.Repeat("fake-body", 10000)}, // requires continuation
		}
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

func TestRelayHandlesCrashedPeers(t *testing.T) {
	withRelayedEcho(t, func(_, server *Channel, sc *SubChannel) {
		ctx, cancel := NewContext(time.Second)
		defer cancel()

		_, _, _, err := raw.CallSC(ctx, sc, "echo", []byte("fake-header"), []byte("fake-body"))
		require.NoError(t, err, "Relayed call failed.")

		// Simulate a server crash.
		server.Close()
		require.NotPanics(t, func() {
			raw.CallSC(ctx, sc, "echo", []byte("fake-header"), []byte("fake-body"))
		})
	})
}
