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
	"net"

	"golang.org/x/net/context"
)

// RetryOn represents the types of errors to retry on.
type RetryOn int

//go:generate stringer -type=RetryOn

const (
	// RetryDefault is currently the same as RetryConnectionError.
	RetryDefault RetryOn = iota

	// RetryConnectionError retries on busy frames, declined frames, and connection errors.
	RetryConnectionError

	// RetryNever never retries any errors.
	RetryNever

	// RetryNonIdempotent will retry errors that occur before a request has been picked up.
	// E.g. busy frames and declined frames.
	// This should be used when making calls to non-idempotent endpoints.
	RetryNonIdempotent

	// RetryUnexpected will retry busy frames, declined frames, and unenxpected frames.
	RetryUnexpected
)

func isNetError(err error) bool {
	// TODO(prashantv): Should TChannel internally these to ErrCodeNetwork before returning
	// them to the user?
	_, ok := err.(net.Error)
	return ok
}

func getErrCode(err error) SystemErrCode {
	code := GetSystemErrorCode(err)
	if isNetError(err) {
		code = ErrCodeNetwork
	}
	return code
}

// CanRetry returns whether an error can be retried for the given retry option.
func (r RetryOn) CanRetry(err error) bool {
	if r == RetryNever {
		return false
	}
	if r == RetryDefault {
		r = RetryConnectionError
	}

	code := getErrCode(err)

	// If retries are not Never, we always retry Busy and declined
	if code == ErrCodeBusy || code == ErrCodeDeclined {
		return true
	}

	switch r {
	case RetryConnectionError:
		return code == ErrCodeNetwork
	case RetryUnexpected:
		return code == ErrCodeUnexpected
	}

	return false
}

// RetryOptions are the retry options used to configure RunWithRetry.
type RetryOptions struct {
	// MaxAttempts is the maximum number of calls and retries that will be made.
	// If this is 0, the default number of attempts (5) is used.
	MaxAttempts int

	// RetryOn is the types of errors to retry on.
	RetryOn RetryOn

	// TODO: Add TimeoutPerAttempt
}

func getRetryOptions(ctx context.Context) *RetryOptions {
	opts := getTChannelParams(ctx).retryOptions
	if opts == nil {
		opts = &RetryOptions{}
	}

	if opts.MaxAttempts == 0 {
		opts.MaxAttempts = 5
	}
	return opts
}

// RunWithRetry will take a function that makes the TChannel call, and will
// rerun it as specifed in the RetryOptions in the Context.
func (ch *Channel) RunWithRetry(ctx context.Context, f func(context.Context) error) error {
	var err error
	opts := getRetryOptions(ctx)
	for i := 0; i < opts.MaxAttempts; i++ {
		// TODO: Create a sub-context with request state that is passed to the underlying function.
		if err = f(ctx); err == nil {
			return nil
		}
		if !opts.RetryOn.CanRetry(err) {
			return err
		}

		// TODO: log that a request failed with error, and it's being retried.
		ch.log.Infof("Retrying request")
	}

	// Too many retries, return the last error
	return err
}
