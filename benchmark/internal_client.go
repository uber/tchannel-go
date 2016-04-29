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
	"bytes"
	"time"

	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/thrift"
	gen "github.com/uber/tchannel-go/thrift/gen-go/test"
)

// internalClient represents a benchmark client.
type internalClient struct {
	ch       *tchannel.Channel
	sc       *tchannel.SubChannel
	tClient  gen.TChanSecondService
	argStr   string
	argBytes []byte
	opts     *options
}

// NewClient returns a new Client that can make calls to a benchmark server.
func NewClient(hosts []string, optFns ...Option) Client {
	opts := getOptions(optFns...)
	if opts.external {
		return newExternalClient(hosts, opts)
	}

	ch, err := tchannel.NewChannel(opts.svcName, nil)
	if err != nil {
		panic("failed to create channel: " + err.Error())
	}
	for _, host := range hosts {
		ch.Peers().Add(host)
	}
	thriftClient := thrift.NewClient(ch, opts.svcName, nil)
	client := gen.NewTChanSecondServiceClient(thriftClient)

	argBytes := randomBytes(opts.reqSize)
	return &internalClient{
		ch:       ch,
		sc:       ch.GetSubChannel(opts.svcName),
		tClient:  client,
		argBytes: argBytes,
		argStr:   string(argBytes),
		opts:     opts,
	}
}

func (c *internalClient) Warmup() error {
	for _, peer := range c.ch.Peers().Copy() {
		ctx, cancel := tchannel.NewContext(c.opts.timeout)
		_, err := peer.GetConnection(ctx)
		cancel()

		if err != nil {
			return err
		}
	}

	return nil
}

func (c *internalClient) RawCall() (time.Duration, error) {
	ctx, cancel := tchannel.NewContext(c.opts.timeout)
	defer cancel()

	started := time.Now()
	rArg2, rArg3, _, err := raw.CallSC(ctx, c.sc, "echo", c.argBytes, c.argBytes)
	duration := time.Since(started)

	if err != nil {
		return 0, err
	}
	if !bytes.Equal(rArg2, c.argBytes) || !bytes.Equal(rArg3, c.argBytes) {
		panic("echo call returned wrong results")
	}
	return duration, nil
}

func (c *internalClient) ThriftCall() (time.Duration, error) {
	ctx, cancel := thrift.NewContext(c.opts.timeout)
	defer cancel()

	started := time.Now()
	res, err := c.tClient.Echo(ctx, c.argStr)
	duration := time.Since(started)

	if err != nil {
		return 0, err
	}
	if res != c.argStr {
		panic("thrift Echo returned wrong result")
	}
	return duration, nil
}

func (c *internalClient) Close() {
	c.ch.Close()
}
