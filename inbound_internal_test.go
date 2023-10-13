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
	"fmt"
	"testing"
	"time"

	"github.com/opentracing/opentracing-go/mocktracer"
	"github.com/stretchr/testify/assert"
)

type statsReporter struct{}

func (w *statsReporter) IncCounter(name string, tags map[string]string, value int64) {
}

func (w *statsReporter) UpdateGauge(name string, tags map[string]string, value int64) {
}

func (w *statsReporter) RecordTimer(name string, tags map[string]string, d time.Duration) {
}

type testCase struct {
	name                   string
	injectedError          error
	systemError            bool
	applicationError       bool
	expectedSpanError      bool
	expectedSpanErrorType  string
	expectedSystemErrorKey string
}

func TestTracingSpanError(t *testing.T) {
	var (
		systemError      = NewSystemError(ErrCodeBusy, "foo")
		applicationError = fmt.Errorf("application")
	)

	testCases := []testCase{
		{
			name:                   "ApplicationError",
			injectedError:          applicationError,
			systemError:            false,
			applicationError:       true,
			expectedSpanError:      true,
			expectedSpanErrorType:  "application",
			expectedSystemErrorKey: "",
		},
		{
			name:                   "SystemError",
			injectedError:          systemError,
			systemError:            true,
			applicationError:       false,
			expectedSpanError:      true,
			expectedSpanErrorType:  "system",
			expectedSystemErrorKey: GetSystemErrorCode(systemError).MetricsKey(),
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {

			var (
				parsedSpan *mocktracer.MockSpan

				tracer   = mocktracer.New()
				callResp = &InboundCallResponse{
					span:          tracer.StartSpan("test"),
					statsReporter: &statsReporter{},
					reqResWriter: reqResWriter{
						err: tt.injectedError,
					},
					applicationError: tt.applicationError,
					systemError:      tt.systemError,
					timeNow:          time.Now,
					cancel:           func() {},
				}
			)

			callResp.doneSending()

			parsedSpan = callResp.span.(*mocktracer.MockSpan)

			assert.Equal(t, tt.expectedSpanError, parsedSpan.Tag("error").(bool))
			if tt.expectedSystemErrorKey == "" {
				assert.Nil(t, parsedSpan.Tag("rpc.tchannel.system_error_code"))
			} else {
				assert.Equal(t, tt.expectedSystemErrorKey, parsedSpan.Tag("rpc.tchannel.system_error_code").(string))
			}
			assert.Equal(t, tt.expectedSpanErrorType, parsedSpan.Tag("rpc.tchannel.error_type").(string))
		})
	}
}
