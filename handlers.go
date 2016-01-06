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
	"sync"

	"golang.org/x/net/context"
)

// A Handler is an object that can be registered with a Channel to process
// incoming calls for a given service and method
type Handler interface {
	// Handles an incoming call for service
	Handle(ctx context.Context, call *InboundCall)
}

// A HandlerFunc is an adapter to allow the use of ordering functions as
// Channel handlers.  If f is a function with the appropriate signature,
// HandlerFunc(f) is a Hander object that calls f
type HandlerFunc func(ctx context.Context, call *InboundCall)

// Handle calls f(ctx, call)
func (f HandlerFunc) Handle(ctx context.Context, call *InboundCall) { f(ctx, call) }

// Manages handlers
type handlerMap struct {
	sync.RWMutex

	handlers map[string]map[string]Handler
}

// Registers a handler
func (hmap *handlerMap) register(h Handler, serviceName, method string) {
	hmap.Lock()
	defer hmap.Unlock()

	if hmap.handlers == nil {
		hmap.handlers = make(map[string]map[string]Handler)
	}

	methods := hmap.handlers[serviceName]
	if methods == nil {
		methods = make(map[string]Handler)
		hmap.handlers[serviceName] = methods
	}

	methods[method] = h
}

// Finds the handler matching the given service and method.  See https://github.com/golang/go/issues/3512
// for the reason that method is []byte instead of a string
func (hmap *handlerMap) find(serviceName string, method []byte) Handler {
	hmap.RLock()
	handler := hmap.handlers[serviceName][string(method)]
	hmap.RUnlock()

	return handler
}
