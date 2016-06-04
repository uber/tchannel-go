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

// Package relay contains relaying interfaces for external use.
package relay

// CallFrame is an interface that abstracts access to the call req frame.
type CallFrame interface {
	// Caller is the name of the originating service.
	Caller() string
	// Service is the name of the destination service.
	Service() string
	// Method is the name of the method being called.
	Method() string
}

// Hosts allows external wrappers to inject peer selection logic for
// relaying.
type Hosts interface {
	// Get returns the host:port of the best peer for the given call.
	Get(CallFrame) string
}

// CallStats is a reporter for per-request stats.
type CallStats interface {
	// The call succeeded (possibly after retrying).
	Succeeded()
	// The RPC failed.
	Failed(reason string)
	// End stats collection for this RPC. Must be called exactly once.
	End()
}

// Stats is a CallStats factory.
type Stats interface {
	Begin(CallFrame) CallStats
}
