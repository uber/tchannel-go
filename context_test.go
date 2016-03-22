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

package tchannel_test

import (
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/stretchr/testify/assert"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"
	"golang.org/x/net/context"
)

var cn = "hello"

func TestWrapContextForTest(t *testing.T) {
	call := testutils.NewIncomingCall(cn)
	ctx, cancel := NewContext(time.Second)
	defer cancel()
	actual := WrapContextForTest(ctx, call)
	assert.Equal(t, call, CurrentCall(actual), "Incorrect call object returned.")
}

func TestNewContextBuilderHasSpan(t *testing.T) {
	ctx, cancel := NewContextBuilder(time.Second).Build()
	defer cancel()

	assert.NotNil(t, CurrentSpan(ctx), "NewContext should contain span")
	assert.True(t, CurrentSpan(ctx).TracingEnabled(), "Tracing should be enabled")
}

func TestNewContextBuilderDisableTracing(t *testing.T) {
	ctx, cancel := NewContextBuilder(time.Second).
		DisableTracing().Build()
	defer cancel()

	assert.False(t, CurrentSpan(ctx).TracingEnabled(), "Tracing should be disabled")
}

func TestNewContextTimeoutZero(t *testing.T) {
	ctx, cancel := NewContextBuilder(0).Build()
	defer cancel()

	deadline, ok := ctx.Deadline()
	assert.True(t, ok, "Context missing deadline")
	assert.True(t, deadline.Sub(time.Now()) <= 0, "Deadline should be Now or earlier")
}

func TestRoutingDelegatePropagates(t *testing.T) {
	WithVerifiedServer(t, nil, func(ch *Channel, hostPort string) {
		peerInfo := ch.PeerInfo()
		testutils.RegisterFunc(ch, "test", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			return &raw.Res{
				Arg3: []byte(CurrentCall(ctx).RoutingDelegate()),
			}, nil
		})

		ctx, cancel := NewContextBuilder(time.Second).Build()
		defer cancel()
		_, arg3, _, err := raw.Call(ctx, ch, peerInfo.HostPort, peerInfo.ServiceName, "test", nil, nil)
		assert.NoError(t, err, "Call failed")
		assert.Equal(t, "", string(arg3), "Expected no routing delegate header")

		ctx, cancel = NewContextBuilder(time.Second).SetRoutingDelegate("xpr").Build()
		defer cancel()
		_, arg3, _, err = raw.Call(ctx, ch, peerInfo.HostPort, peerInfo.ServiceName, "test", nil, nil)
		assert.NoError(t, err, "Call failed")
		assert.Equal(t, "xpr", string(arg3), "Expected routing delegate header to be set")
	})
}

func TestShardKeyPropagates(t *testing.T) {
	WithVerifiedServer(t, nil, func(ch *Channel, hostPort string) {
		peerInfo := ch.PeerInfo()
		testutils.RegisterFunc(ch, "test", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			return &raw.Res{
				Arg3: []byte(CurrentCall(ctx).ShardKey()),
			}, nil
		})

		ctx, cancel := NewContextBuilder(time.Second).Build()
		defer cancel()
		_, arg3, _, err := raw.Call(ctx, ch, peerInfo.HostPort, peerInfo.ServiceName, "test", nil, nil)
		assert.NoError(t, err, "Call failed")
		assert.Equal(t, arg3, []byte(""))

		ctx, cancel = NewContextBuilder(time.Second).
			SetShardKey("shard").Build()
		defer cancel()
		_, arg3, _, err = raw.Call(ctx, ch, peerInfo.HostPort, peerInfo.ServiceName, "test", nil, nil)
		assert.NoError(t, err, "Call failed")
		assert.Equal(t, string(arg3), "shard")
	})
}

func TestCurrentCallWithNilResult(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()
	call := CurrentCall(ctx)
	assert.Nil(t, call, "Should return nil.")
}

func TestContextWithHeaders(t *testing.T) {
	ctx := context.WithValue(context.Background(), "some key", "some value")

	t_ctx1, _ := NewContextBuilder(time.Second).
		AddHeader("header key", "header value").
		Build()
	assert.EqualValues(t, "header value", t_ctx1.Headers()["header key"])
	assert.Nil(t, t_ctx1.Value("some key"), "no inheritance of parent context")

	t_ctx2, _ := NewContextBuilder(time.Second).
		SetParentContext(ctx).
		AddHeader("header key", "header value").
		Build()
	assert.EqualValues(t, "header value", t_ctx2.Headers()["header key"])
	assert.EqualValues(t, "some value", t_ctx2.Value("some key"), "inherited from parent ctx")

	t_ctx2.SetResponseHeaders(map[string]string{"resp key": "resp value"})
	assert.EqualValues(t, "resp value", t_ctx2.ResponseHeaders()["resp key"])

	ctx = t_ctx2 // test as regular context
	assert.EqualValues(t, "some value", ctx.Value("some key"))
}
