package tchannel_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	. "github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"
)

func TestRelay(t *testing.T) {
	relay, err := NewChannel("relay", &ChannelOptions{
		RelayHosts: NewSimpleRelayHosts(map[string][]string{}),
	})
	require.NoError(t, err, "Failed to create a relay channel.")
	require.NoError(t, relay.ListenAndServe("127.0.0.1:0"), "Relay failed to listen.")
	defer relay.Close()

	data := []byte("fake-body")

	server := testutils.NewServer(t, testutils.NewOpts().SetServiceName("test"))
	defer server.Close()
	server.Register(raw.Wrap(newTestHandler(t)), "echo")
	relay.RelayHosts().(*SimpleRelayHosts).Add("test", server.PeerInfo().HostPort)

	client := testutils.NewClient(t, nil)
	client.Peers().Add(relay.PeerInfo().HostPort)
	defer client.Close()

	var wg sync.WaitGroup

	defer func(c *Channel) {
		sc := client.GetSubChannel("test")
		ctx, cancel := NewContext(10 * time.Millisecond)
		defer cancel()

		arg2, arg3, _, err := raw.CallSC(ctx, sc, "echo", []byte("fake-header"), data)
		require.NoError(t, err, "Relayed call failed.")
		assert.Equal(t, "fake-header", string(arg2), "Header was mangled during relay.")
		assert.Equal(t, "fake-body", string(arg3), "Body was mangled during relay.")
	}(client)

	wg.Wait()
}
