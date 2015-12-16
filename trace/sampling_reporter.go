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

	tc "github.com/uber/tchannel-go"
)

// SamplingReporterFactory returns a TraceReporterFactory that wraps the given reporterFactory.
// The sampling reporter will only call Report on the underlying reporter for a subset of reports.
// sampleRate should be between (0, 1].
func SamplingReporterFactory(sampleRate float64, reporterFactory tc.TraceReporterFactory) tc.TraceReporterFactory {
	return func(ch *tc.Channel) tc.TraceReporter {
		return NewSamplingReporter(sampleRate, reporterFactory(ch))
	}
}

// NewSamplingReporter returns a sampling reporter that will only call Report on the underlying
// reporter for a subset of reports. sampleRate should be between (0, 1].
func NewSamplingReporter(sampleRate float64, reporter tc.TraceReporter) tc.TraceReporter {
	return &samplingReporter{
		sampleRate: sampleRate,
		underlying: reporter,
	}
}

type samplingReporter struct {
	sampleRate float64
	underlying tc.TraceReporter
}

func (r *samplingReporter) Report(data tc.TraceData) {
	if rand.Float64() >= r.sampleRate {
		return
	}

	r.underlying.Report(data)
}
