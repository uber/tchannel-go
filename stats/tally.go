package stats

import (
	"sync"
	"time"

	"github.com/uber/tchannel-go"

	"github.com/uber-go/tally"
)

type wrapper struct {
	sync.RWMutex

	scope  tally.Scope
	byTags map[knownTags]*taggedScope
}

type knownTags struct {
	dest       string
	source     string
	procedure  string
	retryCount string
}

type taggedScope struct {
	sync.RWMutex

	scope tally.Scope // already tagged with some set of tags

	counters map[string]tally.Counter
	gauges   map[string]tally.Gauge
	timers   map[string]tally.Timer
}

// NewTallyReporter takes a tally.Scope and wraps it so it ca be used as a
// StatsReporter. The list of metrics emitted is documented on:
// https://tchannel.readthedocs.io/en/latest/metrics/
// The metrics emitted are similar to YARPC, the tags emitted are:
// source, dest, procedure, and retry-count.
func NewTallyReporter(scope tally.Scope) tchannel.StatsReporter {
	return &wrapper{
		scope:  scope,
		byTags: make(map[knownTags]*taggedScope),
	}
}

func (w *wrapper) IncCounter(name string, tags map[string]string, value int64) {
	ts := w.getTaggedScope(tags)
	ts.getCounter(name).Inc(value)
}

func (w *wrapper) UpdateGauge(name string, tags map[string]string, value int64) {
	ts := w.getTaggedScope(tags)
	ts.getGauge(name).Update(float64(value))
}

func (w *wrapper) RecordTimer(name string, tags map[string]string, d time.Duration) {
	ts := w.getTaggedScope(tags)
	ts.getTimer(name).Record(d)
}

func (w *wrapper) getTaggedScope(tags map[string]string) *taggedScope {
	kt := convertTags(tags)

	w.RLock()
	ts, ok := w.byTags[kt]
	w.RUnlock()
	if ok {
		return ts
	}

	w.Lock()
	defer w.Unlock()

	// Always double-check under the write-lock.
	if ts, ok := w.byTags[kt]; ok {
		return ts
	}

	ts = &taggedScope{
		scope:    w.scope.Tagged(kt.tallyTags()),
		counters: make(map[string]tally.Counter),
		gauges:   make(map[string]tally.Gauge),
		timers:   make(map[string]tally.Timer),
	}
	w.byTags[kt] = ts
	return ts
}

func convertTags(tags map[string]string) knownTags {
	if ts, ok := tags["target-service"]; ok {
		// Outbound call.
		return knownTags{
			dest:       ts,
			source:     tags["service"],
			procedure:  tags["target-endpoint"],
			retryCount: tags["retry-count"],
		}
	}

	if cs, ok := tags["calling-service"]; ok {
		// Inbound call.
		return knownTags{
			dest:       tags["service"],
			source:     cs,
			procedure:  tags["endpoint"],
			retryCount: tags["retry-count"],
		}
	}

	// TChannel doesn't use any other tags, so ignore all others for now.
	return knownTags{}
}

// Create a sub-scope for this set of known tags.
func (kt knownTags) tallyTags() map[string]string {
	tallyTags := make(map[string]string, 5)

	if kt.dest != "" {
		tallyTags["dest"] = kt.dest
	}
	if kt.source != "" {
		tallyTags["source"] = kt.source
	}
	if kt.procedure != "" {
		tallyTags["procedure"] = kt.procedure
	}
	if kt.retryCount != "" {
		tallyTags["retry-count"] = kt.retryCount
	}

	return tallyTags
}

func (ts *taggedScope) getCounter(name string) tally.Counter {
	ts.RLock()
	counter, ok := ts.counters[name]
	ts.RUnlock()
	if ok {
		return counter
	}

	ts.Lock()
	defer ts.Unlock()

	// No double-check under the lock, as overwriting the counter has
	// no impact.
	counter = ts.scope.Counter(name)
	ts.counters[name] = counter
	return counter
}

func (ts *taggedScope) getGauge(name string) tally.Gauge {
	ts.RLock()
	gauge, ok := ts.gauges[name]
	ts.RUnlock()
	if ok {
		return gauge
	}

	ts.Lock()
	defer ts.Unlock()

	// No double-check under the lock, as overwriting the counter has
	// no impact.
	gauge = ts.scope.Gauge(name)
	ts.gauges[name] = gauge
	return gauge
}

func (ts *taggedScope) getTimer(name string) tally.Timer {
	ts.RLock()
	timer, ok := ts.timers[name]
	ts.RUnlock()
	if ok {
		return timer
	}

	ts.Lock()
	defer ts.Unlock()

	// No double-check under the lock, as overwriting the counter has
	// no impact.
	timer = ts.scope.Timer(name)
	ts.timers[name] = timer
	return timer
}
