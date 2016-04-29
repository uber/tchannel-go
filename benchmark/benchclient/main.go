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

// benchclient is used to make requests to a specific server.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/benchmark"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"
	"github.com/uber/tchannel-go/thrift"
	gen "github.com/uber/tchannel-go/thrift/gen-go/test"
)

var (
	serviceName = flag.String("service", "bench-server", "The benchmark server's service name")
	timeout     = flag.Duration("timeout", time.Second, "Timeout for each request")
	requestSize = flag.Int("request-size", 10000, "The number of bytes of each request")
)

func main() {
	flag.Parse()

	client := benchmark.NewClient(flag.Args(),
		benchmark.WithServiceName(*serviceName),
		benchmark.WithTimeout(*timeout),
		benchmark.WithRequestSize(*requestSize))
	fmt.Println("bench-client started")

	rdr := bufio.NewScanner(os.Stdin)
	for rdr.Scan() {
		var (
			d   time.Duration
			err error
		)

		switch line := rdr.Text(); line {
		case "warmup":
			if err := client.Warmup(); err != nil {
				log.Fatalf("warmup failed: %v", err)
			}
			fmt.Println("success")
			continue
		case "rcall":
			d, err = client.RawCall()
		case "tcall":
			d, err = client.ThriftCall()
		case "quit":
			return
		default:
			log.Fatalf("unrecognized command: %v", line)
		}

		if err != nil {
			log.Printf("Call failed: %v", err)
			continue
		}
		fmt.Println(d)
	}

	if err := rdr.Err(); err != nil {
		log.Fatalf("Reader failed: %v", err)
	}
}

func makeRawCall(ch *tchannel.Channel) {
	ctx, cancel := tchannel.NewContext(*timeout)
	defer cancel()

	arg := testutils.RandBytes(*requestSize)
	started := time.Now()

	sc := ch.GetSubChannel(*serviceName)
	rArg2, rArg3, _, err := raw.CallSC(ctx, sc, "echo", arg, arg)
	if err != nil {
		fmt.Println("failed:", err)
		return
	}
	duration := time.Since(started)
	if !bytes.Equal(rArg2, arg) || !bytes.Equal(rArg3, arg) {
		log.Fatalf("Echo gave different string!")
	}
	fmt.Println(duration)
}

func makeCall(client gen.TChanSecondService) {
	ctx, cancel := thrift.NewContext(*timeout)
	defer cancel()

	arg := testutils.RandString(*requestSize)
	started := time.Now()
	res, err := client.Echo(ctx, arg)
	if err != nil {
		fmt.Println("failed:", err)
		return
	}
	duration := time.Since(started)
	if res != arg {
		log.Fatalf("Echo gave different string!")
	}
	fmt.Println(duration)
}
