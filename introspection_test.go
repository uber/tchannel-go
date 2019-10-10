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
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/json"
	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Purpose of this test is to ensure introspection doesn't cause any panics
// and we have coverage of the introspection code.
func TestIntrospection(t *testing.T) {
	opts := testutils.NewOpts().
		AddLogFilter("Couldn't find handler", 1). // call with service name fails
		NoRelay()                                 // "tchannel" service name is not forwarded.
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		client := testutils.NewClient(t, nil)
		defer client.Close()

		ctx, cancel := json.NewContext(time.Second)
		defer cancel()

		var resp map[string]interface{}
		peer := client.Peers().GetOrAdd(ts.HostPort())
		err := json.CallPeer(ctx, peer, "tchannel", "_gometa_introspect", map[string]interface{}{
			"includeExchanges":  true,
			"includeEmptyPeers": true,
			"includeTombstones": true,
		}, &resp)
		require.NoError(t, err, "Call _gometa_introspect failed")

		err = json.CallPeer(ctx, peer, ts.ServiceName(), "_gometa_introspect", nil /* arg */, &resp)
		require.NoError(t, err, "Call _gometa_introspect failed")

		// Try making the call on any other service name will fail.
		err = json.CallPeer(ctx, peer, "unknown-service", "_gometa_runtime", map[string]interface{}{
			"includeGoStacks": true,
		}, &resp)
		require.Error(t, err, "_gometa_introspect should only be registered under tchannel")
	})
}

func TestIntrospectByID(t *testing.T) {
	testutils.WithTestServer(t, nil, func(t testing.TB, ts *testutils.TestServer) {
		client := testutils.NewClient(t, nil)
		defer client.Close()

		ctx, cancel := json.NewContext(time.Second)
		defer cancel()

		clientID := client.IntrospectState(nil).ID

		var resp map[string]interface{}
		peer := client.Peers().GetOrAdd(ts.HostPort())
		err := json.CallPeer(ctx, peer, ts.ServiceName(), "_gometa_introspect", map[string]interface{}{
			"id": clientID,
		}, &resp)
		require.NoError(t, err, "Call _gometa_introspect failed")

		// Verify that the response matches the channel ID we expected.
		assert.EqualValues(t, clientID, resp["id"], "unexpected response channel ID")

		// If use an ID which doesn't exist, we get an error
		resp = nil
		err = json.CallPeer(ctx, peer, ts.ServiceName(), "_gometa_introspect", map[string]interface{}{
			"id": math.MaxUint32,
		}, &resp)
		require.NoError(t, err, "Call _gometa_introspect failed")
		assert.EqualValues(t, `failed to find channel with "id": `+strconv.Itoa(math.MaxUint32), resp["error"])
	})
}

func TestIntrospectClosedConn(t *testing.T) {
	// Disable the relay, since the relay does not maintain a 1:1 mapping betewen
	// incoming connections vs outgoing connections.
	opts := testutils.NewOpts().NoRelay()
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		blockEcho := make(chan struct{})
		gotEcho := make(chan struct{})
		testutils.RegisterEcho(ts.Server(), func() {
			close(gotEcho)
			<-blockEcho
		})

		ctx, cancel := NewContext(time.Second)
		defer cancel()

		assert.Equal(t, 0, ts.Server().IntrospectNumConnections(), "Expected no connection on new server")

		// Make sure that a closed connection will reduce NumConnections.
		client := ts.NewClient(nil)
		require.NoError(t, client.Ping(ctx, ts.HostPort()), "Ping from new client failed")
		assert.Equal(t, 1, ts.Server().IntrospectNumConnections(), "Number of connections expected to increase")

		go testutils.AssertEcho(t, client, ts.HostPort(), ts.ServiceName())

		// The state will change to "closeStarted", but be blocked due to the blocked
		// echo call.
		<-gotEcho
		client.Close()

		introspected := client.IntrospectState(nil)
		assert.Len(t, introspected.Connections, 1, "Expected single connection due to blocked call")
		assert.Len(t, introspected.InactiveConnections, 1, "Expected inactive connection due to blocked call")

		close(blockEcho)
		require.True(t, testutils.WaitFor(100*time.Millisecond, func() bool {
			return ts.Server().IntrospectNumConnections() == 0
		}), "Closed connection did not get removed, num connections is %v", ts.Server().IntrospectNumConnections())

		for i := 0; i < 10; i++ {
			client := ts.NewClient(nil)
			defer client.Close()

			require.NoError(t, client.Ping(ctx, ts.HostPort()), "Ping from new client failed")
			assert.Equal(t, 1, client.IntrospectNumConnections(), "Client should have single connection")
			assert.Equal(t, i+1, ts.Server().IntrospectNumConnections(), "Incorrect number of server connections")
		}
	})
}

func TestIntrospectionNotBlocked(t *testing.T) {
	testutils.WithTestServer(t, nil, func(t testing.TB, ts *testutils.TestServer) {
		subCh := ts.Server().GetSubChannel("tchannel")
		subCh.SetHandler(HandlerFunc(func(ctx context.Context, inbound *InboundCall) {
			panic("should not be called")
		}))

		// Ensure that tchannel is also relayed
		if ts.HasRelay() {
			ts.RelayHost().Add("tchannel", ts.Server().PeerInfo().HostPort)
		}

		ctx, cancel := NewContext(time.Second)
		defer cancel()

		client := ts.NewClient(nil)
		peer := client.Peers().GetOrAdd(ts.HostPort())

		// Ensure that SetHandler doesn't block introspection.
		var resp interface{}
		err := json.CallPeer(Wrap(ctx), peer, "tchannel", "_gometa_runtime", nil, &resp)
		require.NoError(t, err, "Call _gometa_runtime failed")
	})
}
