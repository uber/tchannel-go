// Copyright (c) 2015 Uber Technologies, Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package relay

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

type noopStats struct{}

// NewNoopStats returns a no-op implementation of Stats.
func NewNoopStats() Stats {
	return noopStats{}
}

func (n noopStats) Begin(_ CallFrame) CallStats {
	return noopCallStats{}
}

type noopCallStats struct{}

func (n noopCallStats) Succeeded()      {}
func (n noopCallStats) Failed(_ string) {}
func (n noopCallStats) End()            {}

// MockCallStats is a testing spy for the CallStats interface.
type MockCallStats struct {
	sync.Mutex

	t          testing.TB
	succeeded  int
	failedMsgs []string
	ended      int
}

// Succeeded marks the RPC as succeeded.
func (m *MockCallStats) Succeeded() {
	m.Lock()
	m.succeeded++
	m.Unlock()
}

// Failed marks the RPC as failed for the provided reason.
func (m *MockCallStats) Failed(reason string) {
	m.Lock()
	m.failedMsgs = append(m.failedMsgs, reason)
	m.Unlock()
}

// End halts timer and metric collection for the RPC.
func (m *MockCallStats) End() {
	m.Lock()
	m.ended++
	m.Unlock()
}

// AssertSucceeded asserts the number of successes recorded.
func (m *MockCallStats) AssertSucceeded(n int) {
	m.Lock()
	assert.Equal(m.t, n, m.succeeded, "Unexpected number of successes.")
	m.Unlock()
}

// AssertFailed asserts that the RPC failed for the specified reason(s).
func (m *MockCallStats) AssertFailed(reasons ...string) {
	m.Lock()
	assert.Equal(m.t, reasons, m.failedMsgs, "Unexpected reasons for RPC failure.")
	m.Unlock()
}

// AssertEnded asserts that metrics collection was stopped exactly once.
func (m *MockCallStats) AssertEnded() {
	m.Lock()
	if assert.Equal(m.t, 1, m.ended, "Expected metric collection for RPC to have stopped.") {
		assert.False(m.t, m.succeeded == 0 && len(m.failedMsgs) == 0, "Expected stopped RPC to be marked either succeeded or failed.")
	}
	m.Unlock()
}

// AssertCalled is a convenience wrapper for asserting success and failure.
func (m *MockCallStats) AssertCalled(successes int, failReasons ...string) {
	m.AssertSucceeded(successes)
	m.AssertFailed(failReasons...)
	m.AssertEnded()
}

// MockStats is a testing spy for the Stats interface.
type MockStats struct {
	t     testing.TB
	mu    sync.Mutex
	stats map[string][]*MockCallStats
}

// NewMockStats constructs a MockStats.
func NewMockStats(t testing.TB) *MockStats {
	return &MockStats{
		t:     t,
		stats: make(map[string][]*MockCallStats),
	}
}

// Begin starts collecting metrics for an RPC.
func (m *MockStats) Begin(f CallFrame) CallStats {
	cs := &MockCallStats{t: m.t}
	key := m.tripleToKey(f.Caller(), f.Service(), f.Method())
	m.mu.Lock()
	m.stats[key] = append(m.stats[key], cs)
	m.mu.Unlock()
	return cs
}

// Get returns the collected call stats for all RPCs along this edge.
func (m *MockStats) Get(caller, callee, procedure string) []*MockCallStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats[m.tripleToKey(caller, callee, procedure)]
}

// AssertEdges asserts the number of caller-callee::procedure edges we expect to
// have calls along.
func (m *MockStats) AssertEdges(n int) {
	m.mu.Lock()
	assert.Equal(m.t, n, len(m.stats), "Unexpected number of edges in call graph.")
	m.mu.Unlock()
}

func (m *MockStats) tripleToKey(caller, callee, procedure string) string {
	return fmt.Sprintf("%s->%s::%s", caller, callee, procedure)
}
