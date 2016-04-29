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

package benchmark

import "time"

type options struct {
	external bool
	svcName  string

	// Following options only make sense for clients.
	timeout time.Duration
	reqSize int

	// Following options only make sense for servers.
	advertiseHosts []string
}

// Option represents a Benchmark option.
type Option func(*options)

// WithTimeout sets the timeout to use for each call.
func WithTimeout(timeout time.Duration) Option {
	return func(opts *options) {
		opts.timeout = timeout
	}
}

// WithRequestSize sets the request size for each call.
func WithRequestSize(reqSize int) Option {
	return func(opts *options) {
		opts.reqSize = reqSize
	}
}

// WithServiceName sets the service name of the benchmark server.
func WithServiceName(svcName string) Option {
	return func(opts *options) {
		opts.svcName = svcName
	}
}

// WithExternalProcess creates a separate process to host the server/client.
func WithExternalProcess() Option {
	return func(opts *options) {
		opts.external = true
	}
}

// WithAdvertiseHosts sets the hosts to advertise with on startup.
func WithAdvertiseHosts(hosts []string) Option {
	return func(opts *options) {
		opts.advertiseHosts = hosts
	}
}

func getOptions(optFns ...Option) *options {
	opts := &options{
		timeout: time.Second,
		svcName: "bench-server",
	}
	for _, opt := range optFns {
		opt(opts)
	}
	return opts
}
