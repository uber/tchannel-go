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
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func toMap(fields LogFields) map[string]interface{} {
	m := make(map[string]interface{})
	for _, f := range fields {
		m[f.Key] = f.Value
	}
	return m
}

func TestLoggers(t *testing.T) {
	ch, err := NewChannel("svc", &ChannelOptions{
		Logger: NewLogger(ioutil.Discard),
	})
	require.NoError(t, err, "NewChannel failed")

	peerInfo := ch.PeerInfo()
	fields := toMap(ch.Logger().Fields())
	assert.Equal(t, peerInfo.ServiceName, fields["service"])

	sc := ch.GetSubChannel("subch")
	fields = toMap(sc.Logger().Fields())
	assert.Equal(t, peerInfo.ServiceName, fields["service"])
	assert.Equal(t, "subch", fields["subchannel"])
}

func TestStats(t *testing.T) {
	ch, err := NewChannel("svc", &ChannelOptions{
		Logger: NewLogger(ioutil.Discard),
	})
	require.NoError(t, err, "NewChannel failed")

	hostname, err := os.Hostname()
	require.NoError(t, err, "Hostname failed")

	peerInfo := ch.PeerInfo()
	tags := ch.StatsTags()
	assert.NotNil(t, ch.StatsReporter(), "StatsReporter missing")
	assert.NotNil(t, ch.TraceReporter(), "TraceReporter missing")
	assert.Equal(t, peerInfo.ProcessName, tags["app"], "app tag")
	assert.Equal(t, peerInfo.ServiceName, tags["service"], "service tag")
	assert.Equal(t, hostname, tags["host"], "hostname tag")

	sc := ch.GetSubChannel("subch")
	subTags := sc.StatsTags()
	assert.NotNil(t, sc.StatsReporter(), "StatsReporter missing")
	for k, v := range tags {
		assert.Equal(t, v, subTags[k], "subchannel missing tag %v", k)
	}
	assert.Equal(t, "subch", subTags["subchannel"], "subchannel tag missing")
}

func TestIsolatedSubChannelsDontSharePeers(t *testing.T) {
	ch, err := NewChannel("svc", &ChannelOptions{
		Logger: NewLogger(ioutil.Discard),
	})
	require.NoError(t, err, "NewChannel failed")

	sub := ch.GetSubChannel("svc-ringpop")
	if ch.peers != sub.peers {
		t.Log("Channel and subchannel don't share the same peer list.")
		t.Fail()
	}

	isolatedSub := ch.GetSubChannel("svc-shy-ringpop", Isolated)
	if ch.peers == isolatedSub.peers {
		t.Log("Channel and isolated subchannel share the same peer list.")
		t.Fail()
	}

	// Nobody knows about the peer.
	assert.Nil(t, ch.peers.peersByHostPort["127.0.0.1:3000"])
	assert.Nil(t, sub.peers.peersByHostPort["127.0.0.1:3000"])
	assert.Nil(t, isolatedSub.peers.peersByHostPort["127.0.0.1:3000"])

	// Uses of the parent channel should be reflected in the subchannel, but
	// not the isolated subchannel.
	ch.Peers().Add("127.0.0.1:3000")
	assert.NotNil(t, ch.peers.peersByHostPort["127.0.0.1:3000"])
	assert.NotNil(t, sub.peers.peersByHostPort["127.0.0.1:3000"])
	assert.Nil(t, isolatedSub.peers.peersByHostPort["127.0.0.1:3000"])
}

func TestSetHandler(t *testing.T) {
	// Generate a Handler that expects only the given methods to be called.
	genHandler := func(methods ...string) Handler {
		allowedMethods := make(map[string]struct{}, len(methods))
		for _, m := range methods {
			allowedMethods[m] = struct{}{}
		}

		return HandlerFunc(func(ctx context.Context, call *InboundCall) {
			method := call.MethodString()
			assert.Contains(t, allowedMethods, method, "unexpected call to %q", method)

			resp := call.Response()
			require.NoError(t, NewArgWriter(resp.Arg2Writer()).Write(nil))
			require.NoError(t, NewArgWriter(resp.Arg3Writer()).Write([]byte(method)))
		})
	}

	ch, err := NewChannel("svc1", &ChannelOptions{
		Logger: NewLogger(ioutil.Discard),
	})
	require.NoError(t, err, "NewChannel failed")

	// Catch-all handler for the main channel that accepts foo, bar, and baz,
	// and a single registered handler for a different subchannel.
	ch.GetSubChannel("svc1").SetHandler(genHandler("foo", "bar", "baz"))
	ch.GetSubChannel("svc2").Register(genHandler("foo"), "foo")
	require.NoError(t, ch.ListenAndServe("127.0.0.1:0"), "ListenAndServe failed")
	defer ch.Close()

	client, err := NewChannel("client", &ChannelOptions{
		Logger: NewLogger(ioutil.Discard),
	})
	require.NoError(t, err, "NewChannel failed")
	client.Peers().Add(ch.PeerInfo().HostPort)

	tests := []struct {
		Service    string
		Method     string
		ShouldFail bool
	}{
		{"svc1", "foo", false},
		{"svc1", "bar", false},
		{"svc1", "baz", false},

		{"svc2", "foo", false},
		{"svc2", "bar", true},
	}

	for _, tt := range tests {
		c := client.GetSubChannel(tt.Service)

		ctx, _ := NewContext(1 * time.Second)
		call, err := c.BeginCall(ctx, tt.Method, nil)
		require.NoError(t, err, "BeginCall failed")
		require.NoError(t, NewArgWriter(call.Arg2Writer()).Write(nil))
		require.NoError(t, NewArgWriter(call.Arg3Writer()).Write([]byte("irrelevant")))

		var data []byte
		resp := call.Response()
		if tt.ShouldFail {
			require.Error(t, NewArgReader(resp.Arg2Reader()).Read(&data))
			require.Error(t, NewArgReader(resp.Arg3Reader()).Read(&data))
		} else {
			require.NoError(t, NewArgReader(resp.Arg2Reader()).Read(&data))
			require.NoError(t, NewArgReader(resp.Arg3Reader()).Read(&data))
			assert.Equal(t, tt.Method, string(data))
		}
	}
}

func TestCannotRegisterAfterSetHandler(t *testing.T) {
	ch, err := NewChannel("svc", &ChannelOptions{
		Logger: NewLogger(ioutil.Discard),
	})
	require.NoError(t, err, "NewChannel failed")

	someHandler := HandlerFunc(func(ctx context.Context, call *InboundCall) {
		panic("unexpected call")
	})

	anotherHandler := HandlerFunc(func(ctx context.Context, call *InboundCall) {
		panic("unexpected call")
	})

	ch.GetSubChannel("foo").SetHandler(someHandler)

	// Registering against the original service should not panic but
	// registering against the "foo" service should panic.
	assert.NotPanics(t, func() { ch.Register(anotherHandler, "bar") })
	assert.Panics(t, func() { ch.GetSubChannel("foo").Register(anotherHandler, "bar") })
}
