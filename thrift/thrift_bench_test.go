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

package thrift_test

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/testutils"
	"github.com/uber/tchannel-go/thrift"
	gen "github.com/uber/tchannel-go/thrift/gen-go/test"
)

func setupBenchServer() (string, error) {
	ch, err := testutils.NewServer(&testutils.ChannelOpts{
		ServiceName: "bench-server",
		DefaultConnectionOptions: tchannel.ConnectionOptions{
			FramePool: tchannel.NewSyncFramePool(),
		},
	})
	if err != nil {
		return "", err
	}

	server := thrift.NewServer(ch)
	server.Register(gen.NewTChanSecondServiceServer(benchSecondHandler{}))
	return ch.PeerInfo().HostPort, nil
}

func BenchmarkCallsSerial(b *testing.B) {
	b.SetParallelism(1)
	serverAddr, err := setupBenchServer()
	require.NoError(b, err, "setupBenchServer failed")

	// Start a client for each runner
	client, err := startClient(serverAddr)
	require.NoError(b, err, "startClient failed")
	defer client.Close()

	for i := 0; i < b.N; i++ {
		client.CallAndWait()
	}
}

func BenchmarkCallsParallel(b *testing.B) {
	var reqCount int32
	serverAddr, err := setupBenchServer()
	require.NoError(b, err, "setupBenchServer failed")

	started := time.Now()

	b.RunParallel(func(pb *testing.PB) {
		// Start a client for each runner
		client, err := startClient(serverAddr)
		require.NoError(b, err, "startClient failed")
		defer client.Close()

		for pb.Next() {
			client.CallAndWait()
			atomic.AddInt32(&reqCount, int32(1))
		}
		fmt.Println("Successful requests", client.numTimes, "Mean", client.mean)
	})

	duration := time.Since(started)
	fmt.Println("Requests", reqCount, "RPS: ", float64(reqCount)/duration.Seconds())
}

type benchSecondHandler struct{}

func (benchSecondHandler) Echo(ctx thrift.Context, s string) (string, error) {
	return s, nil
}

type benchClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	numTimes int
	mean     time.Duration
}

var (
	benchClientOnce sync.Once
	benchClientPath string
)

func getBenchClientPath() (path string, err error) {
	benchClientOnce.Do(func() {
		var tempFile *os.File
		tempFile, err = ioutil.TempFile("", "benchclient")
		if err != nil {
			return
		}
		if err = tempFile.Chmod(0755); err != nil {
			return
		}

		benchClientPath = tempFile.Name()
		cmd := exec.Command("go", "build", "-o", tempFile.Name(), ".")
		cmd.Dir = "./benchclient"
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
	})

	return benchClientPath, err
}

func startClient(serverAddr string) (*benchClient, error) {
	path, err := getBenchClientPath()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(path, serverAddr)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	bc := &benchClient{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	return bc, bc.waitForStart()
}

func (c *benchClient) waitForStart() error {
	line, err := c.stdout.ReadString('\n')
	if err != nil {
		return err
	}

	if line != "bench-client started\n" {
		return fmt.Errorf("unexpected line: %v", line)
	}
	return nil
}

func (c *benchClient) CallAndWait() error {
	fmt.Fprintln(c.stdin, "call")

	// Wait till we read a line with the result.
	line, err := c.stdout.ReadString('\n')
	if err != nil {
		return err
	}

	if strings.HasPrefix(line, "failed") {
		fmt.Println("client error:", line)
	} else if t, err := time.ParseDuration(strings.TrimSuffix(line, "\n")); err == nil {
		if c.numTimes > 0 {
			c.mean = time.Duration(float64(c.mean)*float64(c.numTimes)/float64(c.numTimes+1) + float64(t)/float64(c.numTimes+1))
		} else {
			c.mean = t
		}
		c.numTimes++
	} else {
		fmt.Println("unexpected line:", err)
	}

	return nil
}

func (c *benchClient) Close() {
	fmt.Fprintln(c.stdin, "quit")
	go func() {
		time.Sleep(time.Second)
		c.cmd.Process.Kill()
	}()
}
