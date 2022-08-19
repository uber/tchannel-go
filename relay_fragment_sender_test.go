package tchannel

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go/relay"
	"github.com/uber/tchannel-go/testutils/thriftarg2test"
	"github.com/uber/tchannel-go/typed"
)

var _ frameReceiver = (*dummyFrameReceiver)(nil)

type dummyFrameReceiver struct {
	retSent          bool
	retFailureReason string
	pool             FramePool

	// mutable
	gotPayload []byte
}

func newDummyFrameReceiver(retSent bool, retFailureReason string, pool FramePool) *dummyFrameReceiver {
	return &dummyFrameReceiver{
		retSent:          retSent,
		retFailureReason: retFailureReason,
		pool:             pool,
	}
}

func (d *dummyFrameReceiver) Receive(f *Frame, fType frameType) (sent bool, failureReason string) {
	if d.retSent {
		// Keep a record of the received payload for verification
		d.gotPayload = make([]byte, len(f.SizedPayload()))
		copy(d.gotPayload, f.SizedPayload())

		// Frames should be released after transmission
		d.pool.Release(f)
	}
	return d.retSent, d.retFailureReason
}

type noopSentReporter struct{}

func (r *noopSentReporter) SentBytes(_ uint16) {}

func TestRelayFragmentSender(t *testing.T) {
	f := NewFrame(MaxFramePayloadSize)
	wbuf := typed.NewWriteBuffer(f.Payload)
	wbuf.WriteBytes([]byte("hello, world"))

	tests := []struct {
		msg                            string
		frame                          *Frame
		wantError                      string
		sent                           bool
		failure                        string
		wantFailureRelayItemFuncCalled bool
		wantPayload                    []byte
	}{
		{
			msg:         "successful send",
			frame:       f,
			sent:        true,
			wantPayload: []byte("hello, world"),
		},
		{
			msg:                            "send failure",
			frame:                          f,
			sent:                           false,
			failure:                        "something bad happened",
			wantFailureRelayItemFuncCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			var failRelayItemFuncCalled bool

			pool := NewCheckedFramePoolForTest()
			defer func() {
				CheckFramePoolIsEmpty(t, pool)
			}()

			cr := reqHasAll.req(t)
			receiver := newDummyFrameReceiver(tt.sent, tt.failure, pool)
			rfs := relayFragmentSender{
				callReq:       &cr,
				framePool:     pool,
				frameReceiver: receiver,
				failRelayItemFunc: func(items *relayItems, id uint32, failure string, err error) {
					failRelayItemFuncCalled = true
					assert.Equal(t, uint32(123), id, "got unexpected id")
					assert.Equal(t, tt.failure, failure, "got unexpected failure string")
					assert.Error(t, err, "missing err")
				},
				origID:       123,
				sentReporter: &noopSentReporter{},
			}

			wf, err := rfs.newFragment(true, nullChecksum{})
			require.NoError(t, err)

			wantPayload := make([]byte, wf.contents.BytesWritten())
			copy(wantPayload, wf.frame.Payload[:wf.contents.BytesWritten()])

			err = rfs.flushFragment(wf)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, wantPayload, receiver.gotPayload)
			assert.Equal(t, tt.wantFailureRelayItemFuncCalled, failRelayItemFuncCalled, "unexpected failRelayItemFunc called state")
		})
	}
}

type dummyArgWriter struct {
	numCall      int
	writeError   []string
	closeError   string
	bytesWritten []byte
}

func (w *dummyArgWriter) Write(b []byte) (int, error) {
	retErr := w.writeError[w.numCall]
	w.bytesWritten = append(w.bytesWritten, b...)
	w.numCall++
	if retErr != "" {
		return 0, errors.New(retErr)
	}
	return len(b), nil
}

func (w *dummyArgWriter) Close() error {
	if w.closeError != "" {
		return errors.New(w.closeError)
	}
	return nil
}

func TestWriteArg2WithAppends(t *testing.T) {
	tests := []struct {
		msg             string
		writer          *dummyArgWriter
		arg2Map         map[string]string
		overrideArg2Buf []byte
		appends         []relay.KeyVal
		wantError       string
	}{
		{
			msg: "write success without appends",
			writer: &dummyArgWriter{
				writeError: []string{
					"", // nh
					"", // arg2
				},
			},
			arg2Map: exampleArg2Map,
		},
		{
			msg: "write success with appends",
			writer: &dummyArgWriter{
				writeError: []string{
					"", // nh
					"", // arg2
					"", // key length
					"", // key
					"", // val length
					"", // val
				},
			},
			arg2Map: exampleArg2Map,
			appends: []relay.KeyVal{
				{Key: []byte("foo"), Val: []byte("bar")},
			},
		},
		{
			msg: "no nh in data",
			writer: &dummyArgWriter{
				writeError: []string{
					assert.AnError.Error(), // nh
				},
			},
			overrideArg2Buf: []byte{0},
			wantError:       "no nh in arg2",
		},
		{
			msg: "write nh fails",
			writer: &dummyArgWriter{
				writeError: []string{
					assert.AnError.Error(), // nh
				},
			},
			arg2Map:   exampleArg2Map,
			wantError: assert.AnError.Error(),
		},
		{
			msg: "write arg2 fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"",                     // write nh
					assert.AnError.Error(), // write arg2
				},
			},
			arg2Map:   exampleArg2Map,
			wantError: assert.AnError.Error(),
		},
		{
			msg: "write append key length fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"",                     // write nh
					"",                     // write arg2
					assert.AnError.Error(), // write key length
				},
			},
			arg2Map: exampleArg2Map,
			appends: []relay.KeyVal{
				{Key: []byte("foo"), Val: []byte("bar")},
			},
			wantError: assert.AnError.Error(),
		},
		{
			msg: "write append key fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"",                     // write nh
					"",                     // write arg2
					"",                     // write key length
					assert.AnError.Error(), // write key
				},
			},
			arg2Map: exampleArg2Map,
			appends: []relay.KeyVal{
				{Key: []byte("foo"), Val: []byte("bar")},
			},
			wantError: assert.AnError.Error(),
		},
		{
			msg: "write append val length fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"",                     // write nh
					"",                     // write arg2
					"",                     // write key length
					"",                     // write key
					assert.AnError.Error(), // write val length
				},
			},
			arg2Map: exampleArg2Map,
			appends: []relay.KeyVal{
				{Key: []byte("foo"), Val: []byte("bar")},
			},
			wantError: assert.AnError.Error(),
		},
		{
			msg: "write append val fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"",                     // write nh
					"",                     // write arg2
					"",                     // write key length
					"",                     // write key
					"",                     // write val length
					assert.AnError.Error(), // write val
				},
			},
			arg2Map: exampleArg2Map,
			appends: []relay.KeyVal{
				{Key: []byte("foo"), Val: []byte("bar")},
			},
			wantError: assert.AnError.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			var arg2buf []byte
			if tt.overrideArg2Buf != nil {
				arg2buf = tt.overrideArg2Buf
			} else if len(tt.arg2Map) > 0 {
				arg2buf = thriftarg2test.BuildKVBuffer(tt.arg2Map)
			}
			err := writeArg2WithAppends(tt.writer, arg2buf, tt.appends)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, tt.writer.Close())

			finalMap := make(map[string]string)
			for k, v := range tt.arg2Map {
				finalMap[k] = v
			}
			for _, kv := range tt.appends {
				finalMap[string(kv.Key)] = string(kv.Val)
			}
			require.NoError(t, err)
			assert.Equal(t, finalMap, thriftarg2test.MustReadKVBuffer(t, tt.writer.bytesWritten))
		})
	}
}
