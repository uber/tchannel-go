package tchannel_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/uber/tchannel-go/atomic"
	"github.com/uber/tchannel-go/benchmark"
	"github.com/uber/tchannel-go/testutils"

	"github.com/bmizerany/perks/quantile"
	"github.com/stretchr/testify/require"
)

type benchmarkParams struct {
	servers, clients int
	requestSize      int
}

type workerControl struct {
	start        sync.WaitGroup
	unblockStart chan struct{}
	done         sync.WaitGroup
}

func init() {
	benchmark.BenchmarkDir = "./benchmark/"
}

func newWorkerControl(numWorkers int) *workerControl {
	wc := &workerControl{
		unblockStart: make(chan struct{}),
	}
	wc.start.Add(numWorkers)
	wc.done.Add(numWorkers)
	return wc
}

func (c *workerControl) WaitForStart(f func()) {
	c.start.Wait()
	f()
	close(c.unblockStart)
}

func (c *workerControl) WaitForEnd() {
	c.done.Wait()
}

func (c *workerControl) WorkerStart() {
	c.start.Done()
	<-c.unblockStart
}

func (c *workerControl) WorkerDone() {
	c.done.Done()
}

func defaultParams() benchmarkParams {
	return benchmarkParams{
		servers:     2,
		clients:     2,
		requestSize: 1024,
	}
}

func closeAndVerify(b *testing.B, ch *Channel) {
	ch.Close()
	isChanClosed := func() bool {
		return ch.State() == ChannelClosed
	}
	if !testutils.WaitFor(time.Second, isChanClosed) {
		b.Errorf("Timed out waiting for channel to close, state: %v", ch.State())
	}
}

func benchmarkRelay(b *testing.B, p benchmarkParams) {
	b.SetBytes(int64(p.requestSize))
	b.ReportAllocs()

	relayHosts := testutils.NewSimpleRelayHosts(map[string][]string{})
	ch, err := NewChannel("relay", &ChannelOptions{
		RelayHosts: relayHosts,
	})
	defer closeAndVerify(b, ch)
	require.NoError(b, err, "Failed to create relay")
	require.NoError(b, ch.ListenAndServe("127.0.0.1:0"), "relay listen failed")

	servers := make([]benchmark.Server, p.servers)
	for i := range servers {
		servers[i] = benchmark.NewServer(
			benchmark.WithServiceName("svc"),
			benchmark.WithRequestSize(p.requestSize),
			benchmark.WithExternalProcess(),
		)
		defer servers[i].Close()
		relayHosts.Add("svc", servers[i].HostPort())
	}

	clients := make([]benchmark.Client, p.clients)
	for i := range clients {
		clients[i] = benchmark.NewClient([]string{ch.PeerInfo().HostPort},
			benchmark.WithServiceName("svc"),
			benchmark.WithRequestSize(p.requestSize),
			benchmark.WithExternalProcess(),
		)
		defer clients[i].Close()
	}

	quantileVals := []float64{0.50, 0.95, 0.99, 1.0}
	quantiles := make([]*quantile.Stream, p.clients)
	for i := range quantiles {
		quantiles[i] = quantile.NewTargeted(quantileVals...)
	}

	var errors atomic.Int64
	var success atomic.Int64

	wc := newWorkerControl(p.clients)
	benchMore := testutils.Decrementor(b.N)

	for i, c := range clients {
		go func(i int, c benchmark.Client) {
			// Do a warm up call.
			c.RawCall()

			wc.WorkerStart()
			defer wc.WorkerDone()

			for benchMore() {
				d, err := c.RawCall()
				if err == nil {
					quantiles[i].Insert(float64(d))
					success.Inc()
				} else {
					errors.Inc()
				}
			}
		}(i, c)
	}

	var started time.Time
	wc.WaitForStart(func() {
		b.ResetTimer()
		started = time.Now()
	})
	wc.WaitForEnd()
	duration := time.Since(started)
	if errors.Load() > 0 {
		b.Errorf("Errors during benchmarl: %v", errors.Load())
	}

	fmt.Printf("\nb.N: %v Duration: %v Requests = %v RPS = %0.2f\n", b.N, duration, success.Load(), float64(success.Load())/duration.Seconds())

	// Merge all the quantiles into 1
	for _, q := range quantiles[1:] {
		quantiles[0].Merge(q.Samples())
	}

	for _, q := range quantileVals {
		fmt.Printf("  %0.4f = %v\n", q, time.Duration(quantiles[0].Query(q)))
	}
	fmt.Println()
}

func BenchmarkRelay2Servers5Clients1k(b *testing.B) {
	p := defaultParams()
	p.clients = 5
	p.servers = 2
	benchmarkRelay(b, p)
}

func BenchmarkRelay2Servers10Clients1k(b *testing.B) {
	p := defaultParams()
	p.clients = 10
	p.servers = 2
	benchmarkRelay(b, p)
}

func BenchmarkRelay2Servers5Clients4k(b *testing.B) {
	p := defaultParams()
	p.requestSize = 4 * 1024
	p.clients = 5
	p.servers = 2
	benchmarkRelay(b, p)
}
