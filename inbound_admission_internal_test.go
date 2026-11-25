package tchannel

import (
	"encoding/binary"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/uber/tchannel-go/typed"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeekMethod(t *testing.T) {
	withLength := func(n int, rest string) []byte {
		b := make([]byte, 2, 2+len(rest))
		binary.BigEndian.PutUint16(b, uint16(n))
		return append(b, rest...)
	}

	tests := []struct {
		msg      string
		contents []byte
		want     string
	}{
		{"no contents", nil, ""},
		{"a length that is cut", []byte{0}, ""},
		{"a method that is whole", withLength(4, "echo"), "echo"},
		{"a method that is followed by arg2 and arg3", withLength(4, "echo"+"\x00\x04arg2\x00\x04arg3"), "echo"},
		{"an empty method", withLength(0, "\x00\x04arg2"), ""},
		{"a method that is cut", withLength(8, "echo"), ""},
		{"a method that is longer than the fragment", withLength(16*1024, "echo"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			buf := typed.NewReadBuffer(tt.contents)
			assert.Equal(t, tt.want, peekMethod(buf))
			// The method is peeked, not consumed: the call reads arg1 itself afterwards.
			assert.Equal(t, len(tt.contents), len(buf.Remaining()))
		})
	}
}

// TestInboundAdmissionRejectedFragmentsDropped sends the fragments of a rejected call itself, so that
// they are known to reach the read loop: a client that goes through a Channel stops sending as soon as
// it gets the error for the first fragment.
func TestInboundAdmissionRejectedFragmentsDropped(t *testing.T) {
	var admitted atomic.Int32
	modifyOpts := func(opts *ChannelOptions) {
		opts.DefaultConnectionOptions.InboundAdmission = func(InboundAdmissionRequest) (func(), error) {
			admitted.Add(1)
			return nil, ErrServerBusy
		}
	}

	withCallReqEnv(t, false /* relayed */, modifyOpts, func(env *badCallReqEnv) {
		env.send(t, reqHasAll.frameWithParams(t, testCallReqParams{flags: hasMoreFragmentsFlag}))
		env.send(t, callReqContinueFrame(t, badCallReqID, true /* more */))
		env.send(t, callReqContinueFrame(t, badCallReqID, true /* more */))
		env.send(t, callReqContinueFrame(t, badCallReqID, false /* more */))

		replies := env.ping(t, 2)
		require.Len(t, replies, 1, "expected a single error for the call, not one per fragment")
		assert.Equal(t, messageTypeError, replies[0].Header.messageType)
		assert.EqualValues(t, badCallReqID, replies[0].Header.ID)

		var errMsg errorMessage
		require.NoError(t, replies[0].read(&errMsg))
		assert.Equal(t, ErrCodeBusy, errMsg.errCode)

		assert.EqualValues(t, 1, admitted.Load(), "the admission is asked once for the call, not for its fragments")

		logs := env.logs.String()
		assert.Equal(t, 3, strings.Count(logs, "[D] Dropping messageTypeCallReqContinue"), "each fragment is dropped with a debug log")
		assert.NotContains(t, logs, "unknown message exchange")
	})
}
