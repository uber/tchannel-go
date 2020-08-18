package tchannel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go/typed"
)

type dummyFrameReceiver struct {
	retSent          bool
	retFailureReason string
}

func (d *dummyFrameReceiver) Receive(f *Frame, fType frameType) (sent bool, failureReason string) {
	return d.retSent, d.retFailureReason
}

func TestRelayFragmentSender(t *testing.T) {
	f := NewFrame(MaxFramePayloadSize)

	wbuf := typed.NewWriteBuffer(f.Payload)

	wbuf.WriteBytes([]byte("foo"))

	tests := []struct {
		msg                            string
		frame                          interface{}
		wantError                      string
		sent                           bool
		failure                        string
		wantFailureRelayItemFuncCalled bool
	}{
		{
			msg:   "successful send",
			frame: f,
			sent:  true,
		},
		{
			msg:       "unexpected frame type",
			frame:     "some other object",
			wantError: "got unexpected frame type: %!t(string=some other object)",
		},
		{
			msg:     "send falure",
			frame:   f,
			sent:    false,
			failure: "something bad happened",
			wantFailureRelayItemFuncCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			wf := &writableFragment{
				frame:    tt.frame,
				contents: wbuf,
			}

			var failRelayItemFuncCalled bool

			rfs := relayFragmentSender{
				framePool: DefaultFramePool,
				frameReceiver: &dummyFrameReceiver{
					retSent:          tt.sent,
					retFailureReason: tt.failure,
				},
				failRelayItemFunc: func(items *relayItems, id uint32, failure string) {
					failRelayItemFuncCalled = true
					assert.Equal(t, uint32(123), id, "got unexpected id")
					assert.Equal(t, tt.failure, failure, "got unexpected failure string")
				},
				origID: 123,
			}

			err := rfs.flushFragment(wf)

			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantFailureRelayItemFuncCalled, failRelayItemFuncCalled, "unexpected failRelayItemFunc called state")
		})
	}
}
