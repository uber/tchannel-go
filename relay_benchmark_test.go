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

func BenchmarkRelay(b *testing.B) {
	const (
		numServers = 5
		numClients = 5
	)
	data := testutils.RandBytes(1000)

	ch, err := NewChannel("relay", &ChannelOptions{
		RelayHosts: NewSimpleRelayHosts(map[string][]string{}),
	})
	require.NoError(b, err, "Failed to create relay")
	require.NoError(b, ch.ListenAndServe("127.0.0.1:0"), "relay listen failed")

	servers := make([]*Channel, numServers)
	for i := 0; i < numServers; i++ {
		servers[i] = testutils.NewServer(b, testutils.NewOpts().SetServiceName("test"))
		servers[i].Register(raw.Wrap(newTestHandler(b)), "echo")
		ch.RelayHosts().(*SimpleRelayHosts).Add("test", servers[i].PeerInfo().HostPort)
	}

	clients := make([]*Channel, numServers)
	for i := 0; i < numClients; i++ {
		clients[i] = testutils.NewClient(b, nil)
		clients[i].Peers().Add(ch.PeerInfo().HostPort)
	}

	b.ResetTimer()

	benchMore := testutils.Decrementor(b.N)

	var wg sync.WaitGroup
	wg.Add(numClients)
	for i := 0; i < numClients; i++ {
		go func(c *Channel) {
			defer wg.Done()

			sc := c.GetSubChannel("test")
			for benchMore() {
				ctx, cancel := NewContext(time.Second)
				defer cancel()

				_, _, _, err := raw.CallSC(ctx, sc, "echo", nil, data)
				assert.NoError(b, err, "call failed")
			}

		}(clients[i])
	}
	wg.Wait()
}
