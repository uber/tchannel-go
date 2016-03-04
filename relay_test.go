package tchannel_test

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	. "github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"
)

func getUnusedHostPort(t testing.TB) net.Addr {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Errorf("net.Listen failed: %v", err)
		return nil
	}

	if err := listener.Close(); err != nil {
		t.Errorf("listener.Close failed")
		return nil
	}

	return listener.Addr()
}

func TestRelay(t *testing.T) {
	relay, err := NewChannel("relay", &ChannelOptions{
		RelayHosts: NewSimpleRelayHosts(map[string][]string{}),
	})
	require.NoError(t, err, "Failed to create a relay channel.")
	defer relay.Close()
	require.NoError(t, relay.ListenAndServe("127.0.0.1:0"), "Relay failed to listen.")

	data := []byte("fake-body")

	server := testutils.NewServer(t, testutils.NewOpts().SetServiceName("test"))
	server.Register(raw.Wrap(newTestHandler(t)), "echo")
	relay.RelayHosts().(*SimpleRelayHosts).Add("test", server.PeerInfo().HostPort)

	client := testutils.NewClient(t, nil)
	defer client.Close()
	client.Peers().Add(relay.PeerInfo().HostPort)

	var wg sync.WaitGroup
	wg.Add(1)

	go func(c *Channel) {
		sc := client.GetSubChannel("test")
		ctx, cancel := NewContext(time.Second)
		defer cancel()

		arg2, arg3, _, err := raw.CallSC(ctx, sc, "echo", []byte("fake-header"), data)
		t.Log(string(arg3))
		require.NoError(t, err, "Relayed call failed.")
		assert.Equal(t, "fake-header", string(arg2), "Header was mangled during relay.")
		assert.Equal(t, "fake-body", string(arg3), "Body was mangled during relay.")

		wg.Done()
	}(client)

	wg.Wait()
}

func TestRelayIgnoresDeadConnections(t *testing.T) {
	relay, err := NewChannel("relay", &ChannelOptions{
		RelayHosts: NewSimpleRelayHosts(map[string][]string{}),
	})
	require.NoError(t, err, "Failed to create a relay channel.")
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
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	_, _, _, err = raw.CallSC(ctx, sc, "echo", []byte("fake-header"), []byte("fake-body"))
	require.NoError(t, err, "Relayed call failed.")

	peer, err := client.Peers().Get(nil)
	require.NoError(t, err, "Expected at least one peer.")
	conn, err := peer.GetConnection(nil)
	require.NoError(t, err, "Expected at least one connection.")
	_ = conn
	// conn.Close()
	// time.Sleep(time.Second)

	server.Close()

	_, _, _, err = raw.CallSC(ctx, sc, "echo", []byte("fake-header"), []byte("fake-body"))
	require.NoError(t, err, "Expected new connections to be created on demand.")
}
