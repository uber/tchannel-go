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

import (
	"log"

	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/hyperbahn"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/thrift"
	gen "github.com/uber/tchannel-go/thrift/gen-go/test"

	"golang.org/x/net/context"
)

// internalServer represents a benchmark server.
type internalServer struct {
	ch   *tchannel.Channel
	opts *options
}

// NewServer returns a new Server that can recieve Thrift calls or raw calls.
func NewServer(optFns ...Option) Server {
	opts := getOptions(optFns...)
	if opts.external {
		return newExternalServer(opts)
	}

	ch, err := tchannel.NewChannel(opts.svcName, nil)
	if err != nil {
		panic("failed to create channel: " + err.Error())
	}
	if err := ch.ListenAndServe("127.0.0.1:0"); err != nil {
		panic("failed to listen on port 0: " + err.Error())
	}

	tServer := thrift.NewServer(ch)
	tServer.Register(gen.NewTChanSecondServiceServer(handler{}))
	ch.Register(raw.Wrap(rawHandler{}), "echo")

	s := &internalServer{
		ch:   ch,
		opts: opts,
	}

	if len(opts.advertiseHosts) > 0 {
		if err := s.Advertise(opts.advertiseHosts); err != nil {
			panic("failed to advertise: " + err.Error())
		}
	}

	return s
}

// HostPort returns the host:port that the server is listening on.
func (s *internalServer) HostPort() string {
	return s.ch.PeerInfo().HostPort
}

// Advertise advertises with Hyperbahn.
func (s *internalServer) Advertise(hyperbahnHosts []string) error {
	config := hyperbahn.Configuration{InitialNodes: hyperbahnHosts}
	hc, err := hyperbahn.NewClient(s.ch, config, nil)
	if err != nil {
		panic("failed to setup Hyperbahn client: " + err.Error())
	}
	return hc.Advertise()
}

func (s *internalServer) Close() {
	s.ch.Close()
}

type rawHandler struct{}

func (rawHandler) OnError(ctx context.Context, err error) {
	log.Printf("Server error: %v", err)
}

func (rawHandler) Handle(ctx context.Context, args *raw.Args) (*raw.Res, error) {
	return &raw.Res{
		Arg2: args.Arg2,
		Arg3: args.Arg3,
	}, nil
}

type handler struct{}

func (handler) Echo(ctx thrift.Context, arg1 string) (string, error) {
	return arg1, nil
}
