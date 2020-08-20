package tchannel

import (
	"errors"
	"testing"

	"github.com/uber/tchannel-go/testutils/thriftarg2test"

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
		frame                          *Frame
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
			msg:                            "send falure",
			frame:                          f,
			sent:                           false,
			failure:                        "something bad happened",
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
		appends         []keyVal
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
			appends: []keyVal{
				{[]byte("foo"), []byte("bar")},
			},
		},
		{
			msg: "no nh in data",
			writer: &dummyArgWriter{
				writeError: []string{
					"something went wrong", // nh
				},
			},
			overrideArg2Buf: []byte{0},
			wantError:       "no nh in arg2",
		},
		{
			msg: "write nh fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"something went wrong", // nh
				},
			},
			arg2Map:   exampleArg2Map,
			wantError: "something went wrong",
		},
		{
			msg: "write arg2 fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"",                     // write nh
					"something went wrong", // write arg2
				},
			},
			arg2Map:   exampleArg2Map,
			wantError: "something went wrong",
		},
		{
			msg: "write append key length fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"",                     // write nh
					"",                     // write arg2
					"something went wrong", // write key length
				},
			},
			arg2Map: exampleArg2Map,
			appends: []keyVal{
				{[]byte("foo"), []byte("bar")},
			},
			wantError: "something went wrong",
		},
		{
			msg: "write append key fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"",                     // write nh
					"",                     // write arg2
					"",                     // write key length
					"something went wrong", // write key
				},
			},
			arg2Map: exampleArg2Map,
			appends: []keyVal{
				{[]byte("foo"), []byte("bar")},
			},
			wantError: "something went wrong",
		},
		{
			msg: "write append val length fails",
			writer: &dummyArgWriter{
				writeError: []string{
					"",                     // write nh
					"",                     // write arg2
					"",                     // write key length
					"",                     // write key
					"something went wrong", // write val length
				},
			},
			arg2Map: exampleArg2Map,
			appends: []keyVal{
				{[]byte("foo"), []byte("bar")},
			},
			wantError: "something went wrong",
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
					"something went wrong", // write val
				},
			},
			arg2Map: exampleArg2Map,
			appends: []keyVal{
				{[]byte("foo"), []byte("bar")},
			},
			wantError: "something went wrong",
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

			finalMap := make(map[string]string)
			for k, v := range tt.arg2Map {
				finalMap[k] = v
			}
			for _, kv := range tt.appends {
				finalMap[string(kv.key)] = string(kv.val)
			}
			require.NoError(t, err)
			assert.Equal(t, finalMap, thriftarg2test.ReadKVBuffer(tt.writer.bytesWritten))
		})
	}
}
