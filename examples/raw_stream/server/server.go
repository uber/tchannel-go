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

package main

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"golang.org/x/net/context"

	tchannel "github.com/uber/tchannel-go"
)

type kvHandler struct {
	sync.RWMutex

	vals    map[string]string
	watches map[string][]chan string
}

func (kvh *kvHandler) handleGet(ctx context.Context, call *tchannel.InboundCall) error {
	return tchannel.WithArg23(call, func(key, _ []byte) error {
		resp := call.Response()
		if err := tchannel.NewArgWriter(resp.Arg2Writer()).Write(key); err != nil {
			return err
		}

		keyString := string(key)
		kvh.RLock()
		valString := kvh.vals[keyString] // XXX exists
		kvh.RUnlock()
		val := []byte(valString)
		fmt.Printf("GET(%#v) => %#v\n", keyString, valString)

		if err := tchannel.NewArgWriter(resp.Arg3Writer()).Write(val); err != nil {
			return err
		}

		return nil
	})
}

func (kvh *kvHandler) handleSet(ctx context.Context, call *tchannel.InboundCall) error {
	return tchannel.WithArg23(call, func(key, val []byte) error {
		resp := call.Response()
		if err := tchannel.NewArgWriter(resp.Arg2Writer()).Write(key); err != nil {
			return err
		}

		keyString := string(key)
		valString := string(val)

		kvh.Lock()
		kvh.vals[keyString] = valString
		watches, hasWatches := kvh.watches[keyString]
		kvh.Unlock()
		if hasWatches {
			for _, watch := range watches {
				watch <- valString
			}
		}
		fmt.Printf("SET(%#v, %#v)\n", keyString, valString)

		if err := tchannel.NewArgWriter(resp.Arg3Writer()).Write(val); err != nil {
			return err
		}

		return nil
	})
}

func (kvh *kvHandler) handleMultiSet(ctx context.Context, call *tchannel.InboundCall) error {
	return tchannel.WithArg23(call, func(key, val []byte) error {
		resp := call.Response()
		if err := tchannel.NewArgWriter(resp.Arg2Writer()).Write(key); err != nil {
			return err
		}

		keyString := string(key)
		valStrings := strings.Split(string(val), "\n")

		for _, valString := range valStrings {
			kvh.Lock()
			kvh.vals[keyString] = valString
			watches, hasWatches := kvh.watches[keyString]
			kvh.Unlock()
			if hasWatches {
				for _, watch := range watches {
					watch <- valString
				}
			}
		}

		fmt.Printf("MULTI_SET(%#v, %#v)\n", keyString, valStrings)

		if err := tchannel.NewArgWriter(resp.Arg3Writer()).Write(val); err != nil {
			return err
		}

		return nil
	})
}

func (kvh *kvHandler) handleWatch(ctx context.Context, call *tchannel.InboundCall) error {
	return tchannel.WithArg23(call, func(key, _ []byte) error {
		resp := call.Response()
		if err := tchannel.NewArgWriter(resp.Arg2Writer()).Write(key); err != nil {
			return err
		}
		arg3Writer, err := resp.Arg3Writer()
		if err != nil {
			return err
		}

		keyString := string(key)

		kvh.Lock()
		watch := make(chan string, 2)
		watches := kvh.watches[keyString]
		kvh.watches[keyString] = append(watches, watch)
		valString, hasValue := kvh.vals[keyString] // XXX exists
		kvh.Unlock()
		fmt.Printf("WATCH(%#v)\n", keyString)

		if hasValue {
			fmt.Printf("WATCH(%#v): put current value %#v\n", keyString, valString)
			watch <- valString
		}

		// TODO: set streaming response header (XXX should have that been set as a
		// request header, and if not set then this endpoint should have returned a
		// BadRequest or simply as much data as it currently had and then closed?)

		// TODO: how handle connection close (ctx.Done() ?)

		// TODO: worth it to spin out a tail goroutine (instead of just letting the
		// handler goroutine be long-lived)?

		sendVal := func(valString string) error {
			val := []byte(valString)
			fmt.Printf("WATCH(%#v): sending value %#v\n", keyString, valString)
			_, err := fmt.Fprintf(arg3Writer, "%v\n%s", len(val), val)
			return err
		}

		drain := func() error {
			for {
				select {
				case valString, ok := <-watch:
					if ok {
						if err := sendVal(valString); err != nil {
							return err
						}
						continue
					}
				default:
				}
				return arg3Writer.Flush()
			}
		}

		// call drain once before we start the blocking loop so that we
		// immediately flush a first response frame (with a maybe empty arg3 if
		// the key is not yet set)
		if err := drain(); err != nil {
			return err
		}

		for valString := range watch {
			if err := sendVal(valString); err != nil {
				return err
			}
			if err := drain(); err != nil {
				return err
			}
		}
		if err := arg3Writer.Close(); err != nil {
			return err
		}

		// TODO: remove watch from kvh.watches, maybe delet key

		return nil
	})
}

func main() {
	ch, err := tchannel.NewChannel("raw_keyvalue", nil)
	if err != nil {
		log.Fatalf("Failed to create tchannel: %v", err)
	}

	kvh := &kvHandler{
		vals:    make(map[string]string),
		watches: make(map[string][]chan string),
	}

	ch.Register(tchannel.ErrorHandlerFunc(kvh.handleGet), "get")
	ch.Register(tchannel.ErrorHandlerFunc(kvh.handleSet), "set")
	ch.Register(tchannel.ErrorHandlerFunc(kvh.handleMultiSet), "multi_set")
	ch.Register(tchannel.ErrorHandlerFunc(kvh.handleWatch), "watch")

	ip, err := tchannel.ListenIP()
	if err != nil {
		log.Fatalf("Failed to find IP to Listen on: %v", err)
	}
	ch.ListenAndServe(fmt.Sprintf("%v:%v", ip, 4040))

	log.Printf("test service has started on %v", ch.PeerInfo().HostPort)
	select {}
}
