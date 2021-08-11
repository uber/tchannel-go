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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type dummyHandler struct{}

func (dummyHandler) Handle(ctx context.Context, call *InboundCall) {}

func TestHandlers(t *testing.T) {
	const (
		m1 = "m1"
		m2 = "m2"
	)
	var (
		hmap = &handlerMap{}

		h1 = &dummyHandler{}
		h2 = &dummyHandler{}

		m1b = []byte(m1)
		m2b = []byte(m2)
	)

	assert.Nil(t, hmap.find(m1b))
	assert.Nil(t, hmap.find(m2b))

	hmap.Register(h1, m1)
	assert.Equal(t, h1, hmap.find(m1b))
	assert.Nil(t, hmap.find(m2b))

	hmap.Register(h2, m2)
	assert.Equal(t, h1, hmap.find(m1b))
	assert.Equal(t, h2, hmap.find(m2b))
}

func procedure(svc, method string) string {
	return fmt.Sprintf("%s::%s", svc, method)
}

func makeInboundCall(svc, method string, logger Logger) *InboundCall {
	// need to populate connection.log to avoid nil pointer
	conn := &Connection{}
	// can't inline due to embeds
	conn.log = logger
	return &InboundCall{
		serviceName:  svc,
		method:       []byte(method),
		methodString: method,
		conn:         conn,
	}
}

func TestUserHandlerWithIgnore(t *testing.T) {
	const (
		svc          = "svc"
		method       = "method"
		ignoreMethod = "ignoreMethod"
		runs         = 3
	)

	userCounter, channelCounter := map[string]int{}, map[string]int{}

	opts := &ChannelOptions{
		Handler:       recorderHandler(userCounter),
		IgnoreMethods: []string{procedure(svc, ignoreMethod)},
	}
	ch, err := NewChannel(svc, opts)
	require.NoError(t, err, "error creating a TChannel channel")

	// channel should be able to handle user ignored methods
	ch.Register(recorderHandler(channelCounter), ignoreMethod)

	// need to populate connection.log to avoid nil pointer
	conn := &Connection{}
	conn.log = NullLogger

	call, ignoreCall := makeInboundCall(svc, method, NullLogger), makeInboundCall(svc, ignoreMethod, NullLogger)

	h := channelHandler{ch}

	for i := 0; i < runs; i++ {
		h.Handle(context.Background(), ignoreCall)
		h.Handle(context.Background(), call)
	}
	assert.Equal(t, map[string]int{procedure(svc, method): runs}, userCounter, "user provided handler not invoked correct amount of times")
	assert.Equal(t, map[string]int{procedure(svc, ignoreMethod): runs}, channelCounter, "channel handler not invoked correct amount of times after ignoring user provided handler")
}

type recorderHandler map[string]int

func (r recorderHandler) Handle(ctx context.Context, call *InboundCall) {
	r[procedure(call.ServiceName(), call.MethodString())]++
}
