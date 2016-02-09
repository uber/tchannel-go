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
	defer relay.Close()
	require.NoError(t, relay.ListenAndServe("127.0.0.1:0"), "Relay failed to listen.")

	data := []byte("fake-body")

	server := testutils.NewServer(t, testutils.NewOpts().SetServiceName("test"))
	defer server.Close()
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
