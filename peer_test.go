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
	"sync/atomic"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go/testutils"
)

func TestGetPeerNoPeer(t *testing.T) {
	ch, err := testutils.NewClient(nil)
	require.NoError(t, err, "NewClient failed")

	peer, err := ch.Peers().Get()
	assert.Error(t, err, "Empty peer list should return error")
	assert.Nil(t, peer, "should not return peer")
}

func TestGetPeerSinglePeer(t *testing.T) {
	ch, err := testutils.NewClient(nil)
	require.NoError(t, err, "NewClient failed")

	ch.Peers().Add("1.1.1.1:1234")

	peer, err := ch.Peers().Get()
	assert.NoError(t, err, "peer list should return contained element")
	assert.Equal(t, "1.1.1.1:1234", peer.HostPort(), "returned peer mismatch")
}

func TestPeerSelection(t *testing.T) {
	var channels []*Channel
	defer func() {
		for _, ch := range channels {
			ch.Close()
		}
	}()

	newService := func(svcName string) (*Channel, string) {
		ch, err := testutils.NewServer(&testutils.ChannelOpts{ServiceName: svcName, Logger: NewLevelLogger(SimpleLogger, LogLevelWarn)})
		require.NoError(t, err, "NewServer failed")
		channels = append(channels, ch)
		return ch, ch.PeerInfo().HostPort
	}

	WithVerifiedServer(t, &testutils.ChannelOpts{ServiceName: "S1"}, func(ch *Channel, hostPort string) {
		doPing := func(ch *Channel) {
			ctx, cancel := NewContext(time.Second)
			defer cancel()
			assert.NoError(t, ch.Ping(ctx, hostPort), "Ping failed")
		}

		count := int32(0)
		testStrategy2 := ScoreCalculatorFunc(func(p *Peer) uint64 {
			atomic.AddInt32(&count, 1)
			return 0
		})

		s2, _ := newService("S2")
		s2.GetSubChannel("S1").Peers().SetStrategy(testStrategy2)
		doPing(s2)
		assert.EqualValues(t, 1+2, count, "Expect exchange update from init resp, ping, pong")
	})
}
