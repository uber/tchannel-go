// Copyright (c) 2017 Uber Technologies, Inc.

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

package tchannel

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHealthCheckEnabled(t *testing.T) {
	hc := HealthCheckOptions{}
	assert.False(t, hc.enabled(), "Default struct should not have health checks enabled")

	hc.Interval = time.Second
	assert.True(t, hc.enabled(), "Setting interval should enable health checks")
}

func TestHealthCheckOptionsDefaults(t *testing.T) {
	tests := []struct {
		opts HealthCheckOptions
		want HealthCheckOptions
	}{
		{
			opts: HealthCheckOptions{},
			want: HealthCheckOptions{Timeout: _defaultHealthCheckTimeout, FailuresToClose: _defaultHealthCheckFailuresToClose},
		},
		{
			opts: HealthCheckOptions{Timeout: 2 * time.Second},
			want: HealthCheckOptions{Timeout: 2 * time.Second, FailuresToClose: _defaultHealthCheckFailuresToClose},
		},
		{
			opts: HealthCheckOptions{FailuresToClose: 3},
			want: HealthCheckOptions{Timeout: _defaultHealthCheckTimeout, FailuresToClose: 3},
		},
		{
			opts: HealthCheckOptions{Timeout: 2 * time.Second, FailuresToClose: 3},
			want: HealthCheckOptions{Timeout: 2 * time.Second, FailuresToClose: 3},
		},
	}

	for _, tt := range tests {
		got := tt.opts.withDefaults()
		assert.Equal(t, tt.want, got, "Unexpected defaults for %+v", tt.opts)
	}
}

func TestHealthHistory(t *testing.T) {
	hh := newHealthHistory()
	var want []bool
	for i := 0; i < 1000; i++ {
		assert.Equal(t, want, hh.asBools())
		b := rand.Intn(3) > 0
		hh.add(b)
		want = append(want, b)
		if len(want) > _healthHistorySize {
			want = want[1:]
		}
	}

}
