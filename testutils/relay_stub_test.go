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

package testutils

import (
	"errors"
	"fmt"
	"testing"

	"github.com/uber/tchannel-go/relay"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimpleRelayHosts(t *testing.T) {
	hosts := map[string][]string{
		"foo":        {"1.1.1.1:1234", "1.1.1.1:1235", "1.1.1.1:1236"},
		"foo-added":  {},
		"foo-canary": {},
	}
	rh := NewSimpleRelayHosts(t, hosts)
	rh.DisableConnVerification()
	rh.Add("foo-added", "1.1.1.1:1234")

	tests := []struct {
		call      relay.CallFrame
		wantOneOf []string
	}{
		{
			call:      FakeCallFrame{ServiceF: "foo-canary"},
			wantOneOf: nil,
		},
		{
			call:      FakeCallFrame{ServiceF: "foo"},
			wantOneOf: []string{"1.1.1.1:1234", "1.1.1.1:1235", "1.1.1.1:1236"},
		},
		{
			call:      FakeCallFrame{ServiceF: "foo-added"},
			wantOneOf: []string{"1.1.1.1:1234"},
		},
	}

	for _, tt := range tests {
		// Since we use random, run the test a few times.
		for i := 0; i < 5; i++ {
			got, err := rh.Get(tt.call, nil)
			require.NoError(t, err, "Get failed")
			if tt.wantOneOf == nil {
				assert.Equal(t, "", got.HostPort, "Expected %v to find no hosts", tt.call)
				continue
			}

			wantOneOf := StrMap(tt.wantOneOf...)
			_, found := wantOneOf[got.HostPort]
			assert.True(t, found, "Got unexpected hostPort %q, want one of: %v", got, tt.wantOneOf)
		}
	}
}

func TestSimpleRelayHostsPeer(t *testing.T) {
	hosts := NewSimpleRelayHosts(t, nil)
	hosts.DisableConnVerification()
	hosts.AddPeer("svc", "1.1.1.1:1", "a1", "sjc1")
	peer, err := hosts.Get(FakeCallFrame{ServiceF: "svc"}, nil)
	require.NoError(t, err, "Get failed")
	assert.Equal(t, relay.Peer{HostPort: "1.1.1.1:1", Pool: "a1", Zone: "sjc1"}, peer, "Unexpected peer")
}

func TestSimpleRelayHostsPeerError(t *testing.T) {
	wantErr := errors.New("test error")
	hosts := NewSimpleRelayHosts(t, nil)
	hosts.DisableConnVerification()
	hosts.AddError("svc", wantErr)
	peer, err := hosts.Get(FakeCallFrame{ServiceF: "svc"}, nil)
	assert.Equal(t, relay.Peer{}, peer, "Unexpected peer")
	assert.Equal(t, wantErr, err, "Unexpected error")
}

type fakeTB struct {
	testing.TB
	errMsg string
}

func (tb *fakeTB) Error(args ...interface{}) {
	tb.errMsg = fmt.Sprint(args...)
}

func TestSimpleRelayHostsConnVerification(t *testing.T) {
	spy := &fakeTB{TB: nil}

	hosts := NewSimpleRelayHosts(spy, nil)
	hosts.Get(FakeCallFrame{ServiceF: "svc"}, nil)
	assert.Equal(
		t,
		_noConnMsg,
		spy.errMsg,
		"Expected test failure when passing a nil relay.Conn.",
	)
}
