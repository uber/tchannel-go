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
//
// These interfaces are currently unstable, and aren't covered by the API
// backwards-compatibility guarantee.
package relay

// Conn is an interface that exposes a bit of information about the underlying
// connection.
type Conn interface {
	// RemoteProcessPrefixMatches checks whether the remote peer's process name
	// matches a preconfigured list of prefixes specified in the connection
	// options. It's the caller's responsibility to match indices between the two
	// slices. Callers shouldn't mutate the returned slice.
	RemoteProcessPrefixMatches() []bool

	// RemoteHostPort returns the host:port of the remote peer.
	RemoteHostPort() string
}

// CallFrame is an interface that abstracts access to the call req frame.
type CallFrame interface {
	// Caller is the name of the originating service.
	Caller() []byte
	// Service is the name of the destination service.
	Service() []byte
	// Method is the name of the method being called.
	Method() []byte
	// RoutingDelegate is the name of the routing delegate, if any.
	RoutingDelegate() []byte
}

// Hosts allows external wrappers to inject peer selection logic for
// relaying.
type Hosts interface {
	// Get returns the peer to forward the given call to.
	// If a SystemError is returned, the error is forwarded. Otherwise
	// a Declined error is returned to the caller.
	Get(CallFrame, Conn) (Peer, error)
}

// CallStats is a reporter for per-request stats.
//
// Because call res frames don't include the OK bit, we can't wait until the
// last frame of a relayed RPC to decide whether or not the RPC succeeded.
// Instead, we mark the call successful or failed as we see the relevant frame,
// but we wait to end any timers until the last frame of the response.
type CallStats interface {
	// SetPeer is called once a peer has been selected for this call.
	// Note: This may not be called if a call fails before peer selection.
	SetPeer(Peer)

	// The call succeeded (possibly after retrying).
	Succeeded()
	// The RPC failed.
	Failed(reason string)
	// End stats collection for this RPC. Will be called exactly once.
	End()
}

// Stats is a CallStats factory.
type Stats interface {
	Begin(CallFrame) CallStats
}

// Peer represents the destination selected for a call.
type Peer struct {
	// HostPort of the peer that was selected.
	HostPort string

	// Pool allows the peer selection to specify a pool that this Peer belongs
	// to, which may be useful when reporting stats.
	Pool string

	// Zone allows the peer selection to specify the zone that this Peer belongs
	// to, which is also useful for stats.
	Zone string
}
