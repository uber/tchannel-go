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

package tchannel

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/opentracing/opentracing-go/mocktracer"
)

type statsReporter struct{}

func (w *statsReporter) IncCounter(name string, tags map[string]string, value int64) {
}

func (w *statsReporter) UpdateGauge(name string, tags map[string]string, value int64) {
}

func (w *statsReporter) RecordTimer(name string, tags map[string]string, d time.Duration) {
}

func TestTracingSpanApplicationError(t *testing.T) {

	tracer := mocktracer.New()

	_, cancel := context.WithCancel(context.TODO())

	callResp := &InboundCallResponse{
		span: tracer.StartSpan("test"),
		statsReporter: &statsReporter{},
		reqResWriter: reqResWriter{
			err: fmt.Errorf("application"),
		},
		applicationError: true,
		timeNow: time.Now,
		cancel: cancel,
	}

	callResp.doneSending()

	parsedSpan := callResp.span.(*mocktracer.MockSpan)

	assert.True(t, parsedSpan.Tag("error").(bool))
	assert.Nil(t, parsedSpan.Tag("rpc.tchannel.system_error_code"))
	assert.Equal(t, "application", parsedSpan.Tag("rpc.tchannel.error_type").(string))

}

func TestTracingSpanSystemError(t *testing.T) {

	tracer := mocktracer.New()

	_, cancel := context.WithCancel(context.TODO())

	injectedErr := NewSystemError(ErrCodeBusy, "foo")

	callResp := &InboundCallResponse{
		span: tracer.StartSpan("test"),
		statsReporter: &statsReporter{},
		reqResWriter: reqResWriter{
			err: injectedErr,
		},
		systemError: true,
		timeNow: time.Now,
		cancel: cancel,
	}

	callResp.doneSending()

	parsedSpan := callResp.span.(*mocktracer.MockSpan)

	assert.True(t, parsedSpan.Tag("error").(bool))
	assert.Equal(t, GetSystemErrorCode(injectedErr).MetricsKey(), parsedSpan.Tag("rpc.tchannel.system_error_code").(string))
	assert.Equal(t, "system", parsedSpan.Tag("rpc.tchannel.error_type").(string))

}
