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
	"errors"
	"fmt"
	"io"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorMetricKeys(t *testing.T) {
	codes := []SystemErrCode{
		ErrCodeInvalid,
		ErrCodeTimeout,
		ErrCodeCancelled,
		ErrCodeBusy,
		ErrCodeDeclined,
		ErrCodeUnexpected,
		ErrCodeBadRequest,
		ErrCodeNetwork,
		ErrCodeProtocol,
	}

	// Metrics keys should be all lowercase letters and dashes. No spaces,
	// underscores, or other characters.
	expected := regexp.MustCompile(`^[[:lower:]-]+$`)
	for _, c := range codes {
		assert.True(t, expected.MatchString(c.MetricsKey()), "Expected metrics key for code %s to be well-formed.", c.String())
	}

	// Unexpected codes may have poorly-formed keys.
	assert.Equal(t, "SystemErrCode(13)", SystemErrCode(13).MetricsKey(), "Expected invalid error codes to use a fallback metrics key format.")
}

func TestInvalidError(t *testing.T) {
	code := GetSystemErrorCode(nil)
	assert.Equal(t, ErrCodeInvalid, code, "nil error should produce ErrCodeInvalid")
}

func TestUnexpectedError(t *testing.T) {
	code := GetSystemErrorCode(io.EOF)
	assert.Equal(t, ErrCodeUnexpected, code, "non-tchannel SystemError should produce ErrCodeUnexpected")
}

func TestSystemError(t *testing.T) {
	code := GetSystemErrorCode(ErrTimeout)
	assert.Equal(t, ErrCodeTimeout, code, "tchannel timeout error produces ErrCodeTimeout")
}

func TestRelayMetricsKey(t *testing.T) {
	for i := 0; i <= 256; i++ {
		code := SystemErrCode(i)
		assert.Equal(t, "relay-"+code.MetricsKey(), code.relayMetricsKey(), "Unexpected relay metrics key for %v", code)
	}
}

func TestSystemErrorIs(t *testing.T) {
	// These targets come from the standard library's context package on purpose:
	// callers use errors.Is(err, context.DeadlineExceeded) with the stdlib
	// sentinels, and this test proves a SystemError matches them.
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{"timeout sentinel matches DeadlineExceeded", ErrTimeout, context.DeadlineExceeded, true},
		{"cancelled sentinel matches Canceled", ErrRequestCancelled, context.Canceled, true},
		{"timeout does not match Canceled", ErrTimeout, context.Canceled, false},
		{"cancelled does not match DeadlineExceeded", ErrRequestCancelled, context.DeadlineExceeded, false},

		// Matching is keyed on the wire error code, not the message, so timeouts
		// and cancellations rebuilt from the wire (including from non-Go peers
		// that send a different message) are still recognized.
		{"wire timeout with custom message matches DeadlineExceeded", NewSystemError(ErrCodeTimeout, "connection timed out"), context.DeadlineExceeded, true},
		{"wire cancel with custom message matches Canceled", NewSystemError(ErrCodeCancelled, "peer cancelled"), context.Canceled, true},

		// Other codes never match the context sentinels.
		{"busy does not match DeadlineExceeded", ErrServerBusy, context.DeadlineExceeded, false},
		{"busy does not match Canceled", ErrServerBusy, context.Canceled, false},
		{"bad request does not match DeadlineExceeded", ErrTimeoutRequired, context.DeadlineExceeded, false},
		{"timeout does not match an unrelated error", ErrTimeout, io.EOF, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errors.Is(tt.err, tt.target))
		})
	}
}

func TestSystemErrorIsThroughWrap(t *testing.T) {
	// errors.Is must find the context sentinel when a SystemError is wrapped
	// further up the chain with %w.
	err := fmt.Errorf("call to service failed: %w", ErrTimeout)
	assert.True(t, errors.Is(err, context.DeadlineExceeded),
		"errors.Is should see context.DeadlineExceeded through a wrapped timeout")

	err = fmt.Errorf("call to service failed: %w", NewSystemError(ErrCodeCancelled, "peer cancelled"))
	assert.True(t, errors.Is(err, context.Canceled),
		"errors.Is should see context.Canceled through a wrapped cancellation")
}

func TestSystemErrorIdentityUnchanged(t *testing.T) {
	// Is() is purely additive: it changes no SystemError values, so existing
	// equality-based comparisons and code extraction keep working. A timeout
	// rebuilt from the wire still equals the ErrTimeout sentinel by value, and
	// the sentinels still report their codes.
	assert.Equal(t, ErrTimeout, NewSystemError(ErrCodeTimeout, "timeout"),
		"a rebuilt wire timeout must still equal the ErrTimeout sentinel by value")
	assert.Equal(t, ErrRequestCancelled, NewSystemError(ErrCodeCancelled, "request cancelled"),
		"a rebuilt wire cancellation must still equal the ErrRequestCancelled sentinel by value")
	assert.Equal(t, ErrCodeTimeout, GetSystemErrorCode(ErrTimeout))
	assert.Equal(t, ErrCodeCancelled, GetSystemErrorCode(ErrRequestCancelled))
}
