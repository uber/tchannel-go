package stats

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uber-go/tally"
	"github.com/uber/tchannel-go/testutils"
)

func TestConvertTags(t *testing.T) {
	tests := []struct {
		tags map[string]string
		want map[string]string
	}{
		{
			tags: nil,
			want: map[string]string{},
		},
		{
			// unknown tags are ignored.
			tags: map[string]string{"foo": "bar"},
			want: map[string]string{},
		},
		{
			// Outbound call
			tags: map[string]string{
				"target-service":  "tsvc",
				"service":         "foo",
				"target-endpoint": "te",
				"retry-count":     "4",

				// ignored tag
				"foo": "bar",
			},
			want: map[string]string{
				"dest":        "tsvc",
				"source":      "foo",
				"procedure":   "te",
				"retry-count": "4",
			},
		},
		{
			// Inbound call
			tags: map[string]string{
				"service":         "foo",
				"calling-service": "bar",
				"endpoint":        "ep",
			},
			want: map[string]string{
				"dest":      "foo",
				"source":    "bar",
				"procedure": "ep",
			},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.tags), func(t *testing.T) {
			got := convertTags(tt.tags)
			assert.Equal(t, tt.want, got.tallyTags())
		})
	}
}

func TestNewTallyReporter(t *testing.T) {
	want := tally.NewTestScope("" /* prefix */, nil /* tags */)
	scope := tally.NewTestScope("" /* prefix */, nil /* tags */)
	wrapped := NewTallyReporter(scope)

	for i := 0; i < 10; i++ {
		wrapped.IncCounter("outbound.calls", map[string]string{
			"target-service":  "tsvc",
			"service":         "foo",
			"target-endpoint": "te",
		}, 3)
		want.Tagged(map[string]string{
			"dest":      "tsvc",
			"source":    "foo",
			"procedure": "te",
		}).Counter("outbound.calls").Inc(3)

		wrapped.UpdateGauge("num-connections", map[string]string{
			"service": "foo",
		}, 3)
		want.Gauge("num-connections").Update(3)

		wrapped.RecordTimer("inbound.call.latency", map[string]string{
			"service":         "foo",
			"calling-service": "bar",
			"endpoint":        "ep",
		}, time.Second)
		want.Tagged(map[string]string{
			"dest":      "foo",
			"source":    "bar",
			"procedure": "ep",
		}).Timer("inbound.call.latency").Record(time.Second)
	}

	assert.Equal(t, want.Snapshot(), scope.Snapshot())
}

func TestTallyIntegration(t *testing.T) {
	clientScope := tally.NewTestScope("" /* prefix */, nil /* tags */)
	serverScope := tally.NewTestScope("" /* prefix */, nil /* tags */)

	// Verify the tagged metrics from that call.
	tests := []struct {
		msg      string
		scope    tally.TestScope
		counters []string
		timers   []string
	}{
		{
			msg:   "client metrics",
			scope: clientScope,
			counters: []string{
				"outbound.calls.send+dest=testService,procedure=echo,source=testService-client",
				"outbound.calls.success+dest=testService,procedure=echo,source=testService-client",
			},
			timers: []string{
				"outbound.calls.per-attempt.latency+dest=testService,procedure=echo,source=testService-client",
				"outbound.calls.latency+dest=testService,procedure=echo,source=testService-client",
			},
		},
		{
			msg:   "server metrics",
			scope: serverScope,
			counters: []string{
				"inbound.calls.recvd+dest=testService,procedure=echo,source=testService-client",
				"inbound.calls.success+dest=testService,procedure=echo,source=testService-client",
			},
			timers: []string{
				"inbound.calls.latency+dest=testService,procedure=echo,source=testService-client",
			},
		},
	}

	// Use a closure so that the server/client are closed before we verify metrics.
	// Otherwise, we may attempt to verify metrics before they've been flushed by TChannel.
	func() {
		server := testutils.NewServer(t, testutils.NewOpts().SetStatsReporter(NewTallyReporter(serverScope)))
		defer server.Close()
		testutils.RegisterEcho(server, nil)

		client := testutils.NewClient(t, testutils.NewOpts().SetStatsReporter(NewTallyReporter(clientScope)))
		defer client.Close()

		testutils.AssertEcho(t, client, server.PeerInfo().HostPort, server.ServiceName())
	}()

	for _, tt := range tests {
		snapshot := tt.scope.Snapshot()
		for _, counter := range tt.counters {
			assert.Contains(t, snapshot.Counters(), counter, "missing counter")
		}
		for _, timer := range tt.timers {
			assert.Contains(t, snapshot.Timers(), timer, "missing timer")
		}
	}
}

func BenchmarkTallyCounter(b *testing.B) {
	scope := tally.NewTestScope("" /* prefix */, nil /* tags */)
	wrapped := NewTallyReporter(scope)

	tags := map[string]string{
		"target-service":  "tsvc",
		"service":         "foo",
		"target-endpoint": "te",
	}
	for i := 0; i < b.N; i++ {
		wrapped.IncCounter("outbound.calls", tags, 1)
	}
}
