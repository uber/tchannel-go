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

package tchannel

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uber/tchannel-go/relay"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// badCallReqID is the ID of the frames built by testCallReq.frameWithParams.
const badCallReqID = 0xDEADBEEF

// noopRelayHost is a RelayHost for tests where no call is expected to reach it.
type noopRelayHost struct{}

func (noopRelayHost) SetChannel(*Channel) {}

func (noopRelayHost) Start(relay.CallFrame, *relay.Conn) (RelayCall, error) {
	return nil, errors.New("unexpected call to the relay host")
}

// rawLogBuffer is a buffer that is safe for concurrent use, for the logger of a channel.
type rawLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *rawLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *rawLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// countingFramePool is a checked frame pool that also counts the frames that are not released yet.
// CheckEmpty can't be polled while frames are still being released, so wait for the count instead.
type countingFramePool struct {
	*CheckedFramePoolForTest
	outstanding atomic.Int64
}

func (p *countingFramePool) Get() *Frame {
	p.outstanding.Add(1)
	return p.CheckedFramePoolForTest.Get()
}

func (p *countingFramePool) Release(f *Frame) {
	p.CheckedFramePoolForTest.Release(f)
	p.outstanding.Add(-1)
}

// badCallReqEnv is a raw connection, past the init handshake, to a channel that is
// either a plain server or a relay.
type badCallReqEnv struct {
	conn net.Conn
	logs *rawLogBuffer
}

// withBadCallReqEnv runs f against a channel with a checked frame pool, and verifies that
// every frame was released once the channel is closed.
func withBadCallReqEnv(t *testing.T, relayed bool, f func(env *badCallReqEnv)) {
	logs := &rawLogBuffer{}
	pool := &countingFramePool{CheckedFramePoolForTest: NewCheckedFramePoolForTest()}
	opts := &ChannelOptions{
		Logger:                   NewLogger(logs),
		DefaultConnectionOptions: ConnectionOptions{FramePool: pool},
	}
	if relayed {
		opts.RelayHost = noopRelayHost{}
	}

	ch, err := NewChannel("test", opts)
	require.NoError(t, err)
	require.NoError(t, ch.ListenAndServe("127.0.0.1:0"))

	conn, err := net.Dial("tcp", ch.PeerInfo().HostPort)
	require.NoError(t, err)
	require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)))

	require.NoError(t, writeMessage(conn, &initReq{initMessage{id: 1, Version: CurrentProtocolVersion, initParams: initParams{
		InitParamHostPort:    "0.0.0.0:0",
		InitParamProcessName: "test",
	}}}))
	initRes, err := readFrame(conn)
	require.NoError(t, err, "read init res")
	require.Equal(t, messageTypeInitRes, initRes.Header.messageType)

	f(&badCallReqEnv{conn: conn, logs: logs})

	require.NoError(t, conn.Close())
	ch.Close()
	waitForCondition(t, func() bool { return ch.State() == ChannelClosed }, "channel to close")

	// Frames are released asynchronously as the connections shut down.
	for deadline := time.Now().Add(5 * time.Second); pool.outstanding.Load() != 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	result := pool.CheckEmpty()
	assert.Empty(t, result.Unreleased, "frames were not released")
	assert.Empty(t, result.BadReleases)
}

func waitForCondition(t *testing.T, cond func() bool, what string) {
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %v", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (e *badCallReqEnv) send(t *testing.T, f *Frame) {
	require.NoError(t, f.WriteOut(e.conn))
}

// ping sends a ping, and returns the frames that were received before its response. The
// connection handles frames in order, so these are the replies to everything sent before.
func (e *badCallReqEnv) ping(t *testing.T, id uint32) []*Frame {
	require.NoError(t, writeMessage(e.conn, &pingReq{id: id}))

	var before []*Frame
	for {
		f, err := readFrame(e.conn)
		require.NoError(t, err, "read frame")
		if f.Header.messageType == messageTypePingRes && f.Header.ID == id {
			return before
		}
		before = append(before, f)
	}
}

// callReqContinueFrame returns a follow-up fragment, with no content, for the call with the given ID.
func callReqContinueFrame(t *testing.T, id uint32, more bool) *Frame {
	f := NewFrame(MaxFramePayloadSize)
	require.NoError(t, f.write(&callReqContinue{id: id}))

	f.Payload[_flagsIndex] = 0
	if more {
		f.Payload[_flagsIndex] = hasMoreFragmentsFlag
	}
	f.Header.SetPayloadSize(1)
	return f
}

func withBadCallReqEnvs(t *testing.T, f func(t *testing.T, env *badCallReqEnv)) {
	for _, tt := range []struct {
		name    string
		relayed bool
	}{
		{"server", false},
		{"relay", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withBadCallReqEnv(t, tt.relayed, func(env *badCallReqEnv) { f(t, env) })
		})
	}
}

// TestBadCallReqGetsBadRequestError sends a call request with an unknown checksum type, which
// used to panic. The caller must get an error back instead of waiting for its deadline, and the
// connection must keep working.
func TestBadCallReqGetsBadRequestError(t *testing.T) {
	withBadCallReqEnvs(t, func(t *testing.T, env *badCallReqEnv) {
		env.send(t, reqHasAll.frameWithParams(t, testCallReqParams{checksumTypeOverride: 11}))

		replies := env.ping(t, 2)
		require.Len(t, replies, 1, "expected an error for the bad call")
		assert.Equal(t, messageTypeError, replies[0].Header.messageType)
		assert.EqualValues(t, badCallReqID, replies[0].Header.ID)

		var errMsg errorMessage
		require.NoError(t, replies[0].read(&errMsg))
		assert.Equal(t, ErrCodeBadRequest, errMsg.errCode)
		assert.Contains(t, errMsg.message, "unknown checksum type 11")

		// A bad caller can send these at will, so they must not be logged as errors.
		logs := env.logs.String()
		assert.NotContains(t, logs, "Couldn't decode initial fragment")
		assert.NotContains(t, logs, "Failed to relay frame")
		assert.Contains(t, logs, "Rejecting call request that couldn't be decoded")
	})
}

// TestBadMultiFragmentCallReq verifies that the fragments that follow a rejected call are
// dropped without logging for each of them. That the call is forgotten after its last
// fragment is covered by TestRejectedCalls.
func TestBadMultiFragmentCallReq(t *testing.T) {
	withBadCallReqEnvs(t, func(t *testing.T, env *badCallReqEnv) {
		env.send(t, reqHasAll.frameWithParams(t, testCallReqParams{
			checksumTypeOverride: 11,
			flags:                hasMoreFragmentsFlag,
		}))
		env.send(t, callReqContinueFrame(t, badCallReqID, true /* more */))
		env.send(t, callReqContinueFrame(t, badCallReqID, true /* more */))
		env.send(t, callReqContinueFrame(t, badCallReqID, false /* more */))

		replies := env.ping(t, 2)
		require.Len(t, replies, 1, "expected a single error for the call, not one per fragment")
		assert.Equal(t, messageTypeError, replies[0].Header.messageType)

		logs := env.logs.String()
		assert.NotContains(t, logs, "unknown message exchange")
		assert.NotContains(t, logs, "Failed to relay frame")
		assert.Contains(t, logs, "of a rejected call")
	})
}

func TestRejectedCalls(t *testing.T) {
	var r rejectedCalls
	assert.False(t, r.isRejected(1, false), "unknown call")

	r.add(1)
	r.add(1)
	assert.Len(t, r.ids, 1, "an ID is only remembered once")
	assert.True(t, r.isRejected(1, false))
	assert.True(t, r.isRejected(1, false), "still rejected until the last fragment")
	assert.True(t, r.isRejected(1, true))
	assert.False(t, r.isRejected(1, false), "forgotten after the last fragment")

	// Only the most recent calls are remembered.
	for id := uint32(1); id <= maxRejectedCalls+1; id++ {
		r.add(id)
	}
	assert.Len(t, r.ids, maxRejectedCalls)
	assert.False(t, r.isRejected(1, false), "the oldest call is forgotten")
	assert.True(t, r.isRejected(2, false))
	assert.True(t, r.isRejected(maxRejectedCalls+1, false))
}
