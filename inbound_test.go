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

	"github.com/uber/jaeger-client-go"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func TestSpanReportingForErrors(t *testing.T) {
	injectedSystemError := ErrTimeout
	tests := []struct {
		name           string
		method         string
		systemErr      bool
		applicationErr bool
	}{
		{
			name:           "System Error",
			method:         "system-error",
			systemErr:      true,
			applicationErr: false,
		},
		{
			name:           "Application Error",
			method:         "app-error",
			systemErr:      false,
			applicationErr: true,
		},
		{
			name:           "No Error",
			method:         "no-error",
			systemErr:      false,
			applicationErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We use a jaeger tracer here and not Mocktracer: because jaeger supports
			// zipkin format which is essential for inbound span extraction
			jaegerReporter := jaeger.NewInMemoryReporter()
			jaegerTracer, jaegerCloser := jaeger.NewTracer(testutils.DefaultServerName,
				jaeger.NewConstSampler(true),
				jaegerReporter)
			defer jaegerCloser.Close()

			opts := &testutils.ChannelOpts{
				ChannelOptions: ChannelOptions{Tracer: jaegerTracer},
			}

			testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
				// Register handler that returns app error
				ts.RegisterFunc("app-error", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
					return &raw.Res{
						IsErr: true,
					}, nil
				})
				// Register handler that returns system error
				ts.RegisterFunc("system-error", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
					return &raw.Res{
						SystemErr: injectedSystemError,
					}, nil
				})
				// Register handler that returns no error
				ts.RegisterFunc("no-error", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
					return &raw.Res{}, nil
				})

				ctx, cancel := NewContext(20 * time.Second)
				defer cancel()

				clientCh := ts.NewClient(opts)
				defer clientCh.Close()

				// Make a new call, which should fail
				_, _, resp, err := raw.Call(ctx, clientCh, ts.HostPort(), ts.ServiceName(), tt.method, []byte("Arg2"), []byte("Arg3"))

				if tt.systemErr {
					// Providing 'got: %q' is necessary since SystemErrCode is a type alias of byte; testify's
					// failed test ouput would otherwise print out hex codes.
					assert.Equal(t, injectedSystemError, err, "expected cancelled error code, got: %q", err)
				} else {
					assert.Nil(t, err, "expected no system error code")
				}

				if tt.applicationErr {
					assert.True(t, resp.ApplicationError(), "Call(%v) check application error")
				} else if !tt.systemErr {
					assert.False(t, resp.ApplicationError(), "Call(%v) check application error")
				}
			})

			// We should have 4 spans, 2 for client and 2 for server
			assert.Equal(t, len(jaegerReporter.GetSpans()), 4)
			for _, span := range jaegerReporter.GetSpans() {
				if span.(*jaeger.Span).Tags()["span.kind"] == "server" {
					assert.Equal(t, span.(*jaeger.Span).Tags()["error"], true)
					if tt.applicationErr {
						assert.Equal(t, span.(*jaeger.Span).Tags()["rpc.tchannel.error_type"], "application")
						assert.Nil(t, span.(*jaeger.Span).Tags()["rpc.tchannel.system_error_code"])
					} else if tt.systemErr {
						assert.Equal(t, span.(*jaeger.Span).Tags()["rpc.tchannel.error_type"], "system")
						assert.Equal(t, span.(*jaeger.Span).Tags()["rpc.tchannel.system_error_code"], GetSystemErrorCode(injectedSystemError).MetricsKey())
					} else {
						assert.Nil(t, span.(*jaeger.Span).Tags()["rpc.tchannel.error_type"])
						assert.Nil(t, span.(*jaeger.Span).Tags()["rpc.tchannel.system_error_code"])
					}
				}
			}
			jaegerReporter.Reset()
		})
	}
}

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

	// Disable relay since relays hide host:port on outbound calls.
	opts := testutils.NewOpts().NoRelay()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		s2 := ts.NewServer(nil)

		ts.RegisterFunc("test", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			c, rawConn := InboundConnection(CurrentCall(ctx))
			assert.Equal(t, s2.PeerInfo().HostPort, c.RemotePeerInfo().HostPort, "Unexpected host port")
			assert.NotNil(t, rawConn, "unexpected connection")
			return &raw.Res{}, nil
		})

		_, _, _, err := raw.Call(ctx, s2, ts.HostPort(), ts.ServiceName(), "test", nil, nil)
		require.NoError(t, err, "Call failed")
	})
}

func TestInboundConnection_CallOptions(t *testing.T) {
	ctx, cancel := NewContext(time.Second)
	defer cancel()

	testutils.WithTestServer(t, nil, func(t testing.TB, server *testutils.TestServer) {
		server.RegisterFunc("test", func(ctx context.Context, args *raw.Args) (*raw.Res, error) {
			assert.Equal(t, "client", CurrentCall(ctx).CallerName(), "Expected caller name to be passed through")
			return &raw.Res{}, nil
		})

		backendName := server.ServiceName()

		proxyCh := server.NewServer(&testutils.ChannelOpts{ServiceName: "proxy"})
		defer proxyCh.Close()

		subCh := proxyCh.GetSubChannel(backendName)
		subCh.SetHandler(HandlerFunc(func(ctx context.Context, inbound *InboundCall) {
			outbound, err := proxyCh.BeginCall(ctx, server.HostPort(), backendName, inbound.MethodString(), inbound.CallOptions())
			require.NoError(t, err, "Create outbound call failed")
			arg2, arg3, _, err := raw.WriteArgs(outbound, []byte("hello"), []byte("world"))
			require.NoError(t, err, "Write outbound call failed")
			require.NoError(t, raw.WriteResponse(inbound.Response(), &raw.Res{
				Arg2: arg2,
				Arg3: arg3,
			}), "Write response failed")
		}))

		clientCh := server.NewClient(&testutils.ChannelOpts{
			ServiceName: "client",
		})
		defer clientCh.Close()

		_, _, _, err := raw.Call(ctx, clientCh, proxyCh.PeerInfo().HostPort, backendName, "test", nil, nil)
		require.NoError(t, err, "Call through proxy failed")
	})
}

func TestCallOptionsPropogated(t *testing.T) {
	const handler = "handler"

	giveCallOpts := CallOptions{
		Format:          JSON,
		CallerName:      "test-caller-name",
		ShardKey:        "test-shard-key",
		RoutingKey:      "test-routing-key",
		RoutingDelegate: "test-routing-delegate",
	}

	var gotCallOpts *CallOptions

	testutils.WithTestServer(t, nil, func(t testing.TB, ts *testutils.TestServer) {
		ts.Register(HandlerFunc(func(ctx context.Context, inbound *InboundCall) {
			gotCallOpts = inbound.CallOptions()

			err := raw.WriteResponse(inbound.Response(), &raw.Res{})
			assert.NoError(t, err, "write response failed")
		}), handler)

		ctx, cancel := NewContext(testutils.Timeout(time.Second))
		defer cancel()

		call, err := ts.Server().BeginCall(ctx, ts.HostPort(), ts.ServiceName(), handler, &giveCallOpts)
		require.NoError(t, err, "could not call test server")

		_, _, _, err = raw.WriteArgs(call, nil, nil)
		require.NoError(t, err, "could not write args")

		assert.Equal(t, &giveCallOpts, gotCallOpts)
	})
}

func TestBlackhole(t *testing.T) {
	ctx, cancel := NewContext(testutils.Timeout(time.Hour))

	testutils.WithTestServer(t, nil, func(t testing.TB, server *testutils.TestServer) {
		serviceName := server.ServiceName()
		handlerName := "test-handler"

		server.Register(HandlerFunc(func(ctx context.Context, inbound *InboundCall) {
			// cancel client context in handler so the client can return after being blackholed
			defer cancel()

			c, _ := InboundConnection(inbound)
			require.NotNil(t, c)

			state := c.IntrospectState(&IntrospectionOptions{})
			require.Equal(t, 1, state.InboundExchange.Count, "expected exactly one inbound exchange")

			// blackhole request
			inbound.Response().Blackhole()

			// give time for exchange to cleanup
			require.True(t, testutils.WaitFor(10*time.Millisecond, func() bool {
				state = c.IntrospectState(&IntrospectionOptions{})
				return state.InboundExchange.Count == 0
			}),
				"expected no inbound exchanges",
			)

		}), handlerName)

		clientCh := server.NewClient(nil)
		defer clientCh.Close()

		_, _, _, err := raw.Call(ctx, clientCh, server.HostPort(), serviceName, handlerName, nil, nil)
		require.Error(t, err, "expected call error")

		errCode := GetSystemErrorCode(err)
		// Providing 'got: %q' is necessary since SystemErrCode is a type alias of byte; testify's
		// failed test output would otherwise print out hex codes.
		assert.Equal(t, ErrCodeCancelled, errCode, "expected cancelled error code, got: %q", errCode)
	})
}
