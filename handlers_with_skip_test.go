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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uber/tchannel-go"
	. "github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"
	"golang.org/x/net/context"
)

func procedure(svc, method string) string {
	return fmt.Sprintf("%s::%s", svc, method)
}

func TestUserHandlerWithSkip(t *testing.T) {
	const (
		svc                  = "svc"
		userHandleMethod     = "method"
		userHandleSkipMethod = "skipMethod"
		handleRuns           = 3
		handleSkipRuns       = 4
	)

	userCounter, channelCounter := map[string]int{}, map[string]int{}

	opts := testutils.NewOpts().NoRelay()
	opts.ServiceName = svc
	opts.ChannelOptions = ChannelOptions{
		Handler:            recorderHandler{m: userCounter},
		SkipHandlerMethods: []string{procedure(svc, userHandleSkipMethod)},
	}

	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		// channel should be able to handle user ignored methods
		ts.Register(recorderHandler{m: channelCounter}, userHandleSkipMethod)

		client := ts.NewClient(nil)

		for i := 0; i < handleRuns; i++ {
			ctx, cancel := tchannel.NewContext(testutils.Timeout(300 * time.Millisecond))
			defer cancel()
			raw.Call(ctx, client, ts.HostPort(), svc, userHandleMethod, nil, nil)
		}
		assert.Equal(t, map[string]int{procedure(svc, userHandleMethod): handleRuns}, userCounter, "user provided handler not invoked correct amount of times")

		for i := 0; i < handleSkipRuns; i++ {
			ctx, cancel := tchannel.NewContext(testutils.Timeout(300 * time.Millisecond))
			defer cancel()
			raw.Call(ctx, client, ts.HostPort(), svc, userHandleSkipMethod, nil, nil)
		}
		assert.Equal(t, map[string]int{procedure(svc, userHandleSkipMethod): handleSkipRuns}, channelCounter, "channel handler not invoked correct amount of times after ignoring user provided handler")
	})
}

type recorderHandler struct {
	sync.Mutex
	m map[string]int
}

func (r recorderHandler) Handle(ctx context.Context, call *InboundCall) {
	r.Lock()
	defer r.Unlock()
	r.m[procedure(call.ServiceName(), call.MethodString())]++
}
