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
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/jessevdk/go-flags"
	tchannel "github.com/uber/tchannel-go"
)

var options = struct {
	ServiceName string `short:"s" long:"service" default:"raw_keyvalue"`

	HostPort string `long:"hostPort" required:"true" positional-arg-name:"hostPort"`
}{}

func parseArgs() {
	var err error
	if _, err = flags.Parse(&options); err != nil {
		os.Exit(-1)
	}

	// Convert host port to a real host port.
	host, port, err := net.SplitHostPort(options.HostPort)
	if err != nil {
		port = options.HostPort
	}
	if host == "" {
		hostIP, err := tchannel.ListenIP()
		if err != nil {
			log.Printf("could not get ListenIP: %v, defaulting to 127.0.0.1", err)
			host = "127.0.0.1"
		} else {
			host = hostIP.String()
		}
	}
	options.HostPort = host + ":" + port
}

type keyValClient struct {
	tchan       *tchannel.Channel
	peer        string
	serviceName string
}

func NewKeyValClient(serviceName string, peer string) (*keyValClient, error) {
	// &tchannel.ChannelOptions{ Logger: tchannel.SimpleLogger, }
	ch, err := tchannel.NewChannel(serviceName, nil)
	if err != nil {
		return nil, err
	}
	client := &keyValClient{
		tchan:       ch,
		peer:        peer,
		serviceName: serviceName,
	}
	return client, nil
}

func (client *keyValClient) runRepl(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		line := scanner.Text()
		parts := strings.Split(line, " ")
		cmd := strings.ToLower(strings.TrimSpace(parts[0]))
		var cmdFunc func([]string) error
		switch cmd {
		case "get":
			cmdFunc = client.replDoGet

		case "set":
			cmdFunc = client.replDoSet

		case "multi_set":
			cmdFunc = client.replDoMultiSet

		case "watch":
			cmdFunc = client.replDoWatch

		case "":
			continue

		default:
			fmt.Printf("ERROR: unsupported command %v\n", cmd)
			continue
		}

		args := parts[1:]
		if err := cmdFunc(args); err != nil {
			fmt.Printf("ERROR: %v%#v failed: %v\n", cmd, args, err)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("scan failed: %v", err)
	}
}

func (client *keyValClient) replDoSet(parts []string) error {
	if len(parts) < 1 {
		return fmt.Errorf("missing key argument to set")
	}

	// TODO: scan line value instead
	if len(parts) < 2 {
		return fmt.Errorf("missing value argument to set")
	}

	if len(parts) > 2 {
		return client.replDoMultiSet(parts)
	}

	keyString := parts[0]
	valString := parts[1]
	fmt.Printf("setting %#v...", keyString)
	err := client.SetKey(keyString, valString)
	if err != nil {
		return err
	}
	fmt.Printf("done.\n")

	return nil
}

func (client *keyValClient) replDoMultiSet(parts []string) error {
	if len(parts) < 1 {
		return fmt.Errorf("missing key argument to set")
	}

	keyString := parts[0]
	valStrings := parts[1:]
	fmt.Printf("multi setting %#v...", keyString)
	err := client.MultiSetKey(keyString, valStrings)
	if err != nil {
		return err
	}
	fmt.Printf("done.\n")

	return nil
}

func (client *keyValClient) replDoGet(parts []string) error {
	// TODO: bulk get and/or parallelize
	for _, keyString := range parts {
		fmt.Printf("getting %#v...", keyString)
		valString, err := client.GetKey(keyString)
		if err != nil {
			return err
		}
		fmt.Printf("got %#v.\n", valString)
	}
	return nil
}

func (client *keyValClient) replDoWatch(parts []string) error {
	// TODO: create and register a replWatcher object so that they can be
	// listed and canceled

	for _, keyString := range parts {
		fmt.Printf("watching %#v...", keyString)
		updates, err := client.WatchKey(keyString)
		if err != nil {
			return err
		}
		fmt.Printf("ok.\n")
		go func(keyString string, updates <-chan string) {
			for update := range updates {
				fmt.Printf("WATCH[%#v] => %#v\n", keyString, update)
			}
		}(keyString, updates)
	}

	return nil
}

func (client *keyValClient) SetKey(keyString string, valString string) error {
	ctx, cancelFunc := tchannel.NewContext(time.Second)
	defer cancelFunc()

	call, err := client.beginKeyValCall(ctx, "set", keyString, valString)
	if err != nil {
		return err
	}

	return tchannel.WithArg23(call.Response(), func(_, _ []byte) error {
		// TODO: use response arg2/arg3?
		return nil
	})
}

func (client *keyValClient) MultiSetKey(keyString string, valStrings []string) error {
	ctx, cancelFunc := tchannel.NewContext(time.Second)
	defer cancelFunc()

	valsString := strings.Join(valStrings, "\n")
	call, err := client.beginKeyValCall(ctx, "multi_set", keyString, valsString)
	if err != nil {
		return err
	}

	return tchannel.WithArg23(call.Response(), func(_, _ []byte) error {
		// TODO: use response arg2/arg3?
		return nil
	})
}

func (client *keyValClient) GetKey(keyString string) (string, error) {
	ctx, cancelFunc := tchannel.NewContext(time.Second)
	defer cancelFunc()

	call, err := client.beginKeyValCall(ctx, "get", keyString, "")
	if err != nil {
		return "", err
	}

	var valString string
	if err := tchannel.WithArg23(call.Response(), func(_, val []byte) error {
		valString = string(val)
		return nil
	}); err != nil {
		return "", err
	}
	return valString, nil
}

func (client *keyValClient) WatchKey(keyString string) (<-chan string, error) {
	// TODO: rectify the streaming timeout scene:
	// - set total stream timeout header for response stream...
	// - lower context timeout, which should then apply only to the first frame
	ctx, cancelFunc := tchannel.NewContext(time.Hour)

	call, err := client.beginKeyValCall(ctx, "watch", keyString, "")
	if err != nil {
		return nil, err
	}

	var updates chan string

	if err := tchannel.WithArg2(call.Response(), func(_ []byte, arg3Reader tchannel.ArgReader) error {
		updates = make(chan string, 2)

		go func() {
			defer cancelFunc() // XXX unclear if doing this right
			defer func() {
				close(updates)
			}()

			bufReader := bufio.NewReader(arg3Reader)

			for {
				valLengthString, err := bufReader.ReadString('\n')
				if err != nil {
					fmt.Printf("ERROR: watch(%#v) read error: %v\n", keyString, err)
					return
				}
				valLengthString = valLengthString[:len(valLengthString)-1]

				valLength, err := strconv.ParseUint(valLengthString, 10, 64)
				if err != nil {
					fmt.Printf("ERROR: watch(%#v) read error: %v\n", keyString, err)
					return
				}

				valBytes := make([]byte, valLength)
				if _, err := io.ReadFull(bufReader, valBytes); err != nil {
					fmt.Printf("ERROR: watch(%#v) read error: %v\n", keyString, err)
					return
				}

				valString := string(valBytes)
				updates <- valString
			}

		}()

		return nil
	}); err != nil {
		return nil, err
	}

	return updates, nil
}

func (client *keyValClient) beginCall(ctx context.Context, methodName string) (*tchannel.OutboundCall, error) {
	return client.tchan.BeginCall(ctx, client.peer, client.serviceName, methodName, nil)
}

func (client *keyValClient) beginKeyCall(ctx context.Context, methodName string, keyString string) (*tchannel.OutboundCall, error) {
	call, err := client.beginCall(ctx, methodName)
	if err != nil {
		return nil, err
	}
	arg2Writer, err := call.Arg2Writer()
	if err != nil {
		return nil, err
	}
	key := []byte(keyString)
	if _, err := arg2Writer.Write(key); err != nil {
		return nil, err
	}
	if err := arg2Writer.Close(); err != nil {
		return nil, err
	}
	return call, nil
}

func (client *keyValClient) beginKeyValCall(ctx context.Context, methodName string, keyString string, valString string) (*tchannel.OutboundCall, error) {
	call, err := client.beginKeyCall(ctx, methodName, keyString)
	if err != nil {
		return nil, err
	}
	arg3Writer, err := call.Arg3Writer()
	if err != nil {
		return nil, err
	}
	if len(valString) > 0 {
		val := []byte(valString)
		if _, err := arg3Writer.Write(val); err != nil {
			return nil, err
		}
	}
	if err := arg3Writer.Close(); err != nil {
		return nil, err
	}
	return call, nil
}

func main() {
	parseArgs()

	client, err := NewKeyValClient(options.ServiceName, options.HostPort)
	if err != nil {
		log.Fatalf("NewKeyValClient failed: %v", err)
	}

	client.runRepl(os.Stdin)
}
