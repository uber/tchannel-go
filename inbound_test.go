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
	"strings"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func TestActiveCallReq(t *testing.T) {
	t.Skip("Test skipped due to unreliable way to test for protocol errors")

	ctx, cancel := NewContext(time.Second)
	defer cancel()

	// Note: This test cannot use log verification as the duplicate ID causes a log.
	// It does not use a verified server, as it leaks a message exchange due to the
	// modification of IDs in the relay.
	opts := testutils.NewOpts().DisableLogVerification()
	testutils.WithServer(t, opts, func(ch *Channel, hostPort string) {
		gotCall := make(chan struct{})
		unblock := make(chan struct{})

		testutils.RegisterFunc(ch, "blocked", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			gotCall <- struct{}{}
			<-unblock
			return &raw.Res{}, nil
		})

		relayFunc := func(outgoing bool, frame *Frame) *Frame {
			if outgoing && frame.Header.ID == 3 {
				frame.Header.ID = 2
			}
			return frame
		}

		relayHostPort, closeRelay := testutils.FrameRelay(t, hostPort, relayFunc)
		defer closeRelay()

		firstComplete := make(chan struct{})
		go func() {
			// This call will block until we close unblock.
			raw.Call(ctx, ch, relayHostPort, ch.PeerInfo().ServiceName, "blocked", nil, nil)
			close(firstComplete)
		}()

		// Wait for the first call to be received by the server
		<-gotCall

		// Make a new call, which should fail
		_, _, _, err := raw.Call(ctx, ch, relayHostPort, ch.PeerInfo().ServiceName, "blocked", nil, nil)
		assert.Error(t, err, "Expect error")
		assert.True(t, strings.Contains(err.Error(), "already active"),
			"expected already active error, got %v", err)

		close(unblock)
		<-firstComplete
	})
}

func TestInboundConnection(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	WithVerifiedServer(t, nil, func(ch *Channel, hostPort string) {
		testutils.RegisterFunc(ch, "test", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			c, _ := InboundConnection(CurrentCall(ctx))
			assert.Equal(t, hostPort, c.RemotePeerInfo().HostPort, "Unexpected host port")
			return &raw.Res{}, nil
		})

		_, _, _, err := raw.Call(ctx, ch, hostPort, ch.PeerInfo().ServiceName, "test", nil, nil)
		require.NoError(t, err, "Call failed")
	})
}

func TestInboundConnection_CallOptions(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	WithVerifiedServer(t, nil, func(serverCh *Channel, serverHostPort string) {
		testutils.RegisterFunc(serverCh, "test", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			assert.Equal(t, "client", CurrentCall(ctx).CallerName(), "Expected caller name to be passed through")
			return &raw.Res{}, nil
		})

		backendName := serverCh.PeerInfo().ServiceName

		proxyCh, err := testutils.NewServerChannel(&testutils.ChannelOpts{ServiceName: "proxy"})
		require.NoError(t, err, "Proxy channel creation failed")
		defer proxyCh.Close()

		subCh := proxyCh.GetSubChannel(backendName)
		subCh.SetHandler(HandlerFunc(func(ctx context.Context, inbound *InboundCall) {
			outbound, err := proxyCh.BeginCall(ctx, serverHostPort, backendName, inbound.MethodString(), inbound.CallOptions())
			require.NoError(t, err, "Create outbound call failed")

			require.NoError(t, NewArgWriter(outbound.Arg2Writer()).Write([]byte("hi")), "Write arg2 failed")
			require.NoError(t, NewArgWriter(outbound.Arg3Writer()).Write([]byte("body")), "Write arg3 failed")
			var arg2, arg3 []byte
			require.NoError(t, NewArgReader(outbound.Response().Arg2Reader()).Read(&arg2), "Read arg2 failed")
			require.NoError(t, NewArgReader(outbound.Response().Arg3Reader()).Read(&arg3), "Read arg3 failed")
			require.NoError(t, NewArgWriter(inbound.Response().Arg2Writer()).Write(arg2), "Write arg2 failed")
			require.NoError(t, NewArgWriter(inbound.Response().Arg3Writer()).Write(arg3), "Write arg3 failed")
		}))

		clientCh, err := NewChannel("client", nil)
		require.NoError(t, err, "Create client channel failed")
		defer clientCh.Close()

		clientCh.Peers().Add(proxyCh.PeerInfo().HostPort)

		_, _, _, err = raw.Call(ctx, clientCh, proxyCh.PeerInfo().HostPort, backendName, "test", nil, nil)
		require.NoError(t, err, "Call through proxy failed")
	})
}
