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

// Package trace provides methods to submit Zipkin style spans to TCollector.
package trace

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	tc "github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/testutils"
)

func TestSamplingReporter(t *testing.T) {
	rand.Seed(10)

	dummyData, _ := submitArgs(t)
	var reports []tc.TraceData
	mock := tc.TraceReporterFunc(func(data tc.TraceData) {
		assert.Equal(t, *dummyData, data, "Reported data does not match")
		reports = append(reports, data)
	})

	tests := []struct {
		sampleRate  float64
		count       int
		expectedMin int
		expectedMax int
	}{
		{1.0, 100, 100, 100},
		{0.5, 100, 40, 60},
		{0.1, 100, 5, 15},
		{0, 100, 0, 0},
	}

	for _, tt := range tests {
		reports = nil
		reporter := NewSamplingReporter(tt.sampleRate, mock)
		for i := 0; i < 100; i++ {
			reporter.Report(*dummyData)
		}

		assert.True(t, len(reports) >= tt.expectedMin,
			"Number of reports (%v) expected to be greater than %v", len(reports), tt.expectedMin)
		assert.True(t, len(reports) <= tt.expectedMax,
			"Number of reports (%v) expected to be less than %v", len(reports), tt.expectedMax)
	}
}

func TestSamplingReporterFactory(t *testing.T) {
	ch := testutils.NewClient(t, nil)
	called := false
	factory := SamplingReporterFactory(0.1, func(gotCh *tc.Channel) tc.TraceReporter {
		assert.Equal(t, ch, gotCh, "Received wrong channel")
		called = true
		return nil
	})

	reporter := factory(ch)
	if assert.NotNil(t, reporter, "Expected reporter not to be nil") {
		assert.Equal(t, &samplingReporter{sampleRate: 0.1}, reporter.(*samplingReporter),
			"Unexpected sampling reporter")
	}
}
