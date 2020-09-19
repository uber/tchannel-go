package typed

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dummyWriter struct {
	calls        int
	bytesWritten []byte
	// retError is a map of call ids to error strings
	retError map[int]string
}

func (w *dummyWriter) Write(b []byte) (int, error) {
	defer func() { w.calls++ }()

	if w.retError[w.calls] != "" {
		return 0, errors.New(w.retError[w.calls])
	}
	w.bytesWritten = append(w.bytesWritten, b...)
	return len(b), nil
}

func TestWriter(t *testing.T) {
	tests := []struct {
		msg              string
		w                *dummyWriter
		previousError    error
		wantError        string
		wantBytesWritten []byte
	}{
		{
			msg: "successful write",
			w: &dummyWriter{
				retError: map[int]string{},
			},
			wantBytesWritten: []byte{0, 1, 2, 0, 3, 4, 5, 6},
		},
		{
			msg:           "return error due to previous error",
			previousError: errors.New("something went wrong previously"),
			w:             &dummyWriter{},
			wantError:     "something went wrong previously",
		},
		{
			msg: "error writing length",
			w: &dummyWriter{
				retError: map[int]string{0: "something went wrong"},
			},
			wantError: "something went wrong",
		},
		{
			msg: "error writing data",
			w: &dummyWriter{
				retError: map[int]string{1: "something went wrong"},
			},
			wantError: "something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			writes := func(w *Writer) {
				w.WriteUint16(1)
				w.WriteBytes([]byte{2})
				w.WriteLen16Bytes([]byte{4, 5, 6})
			}

			w := NewWriter(tt.w)
			w.err = tt.previousError
			writes(w)

			if tt.wantError != "" {
				require.EqualError(t, w.Err(), tt.wantError, "Got unexpected error")
				return
			}
			require.NoError(t, w.Err(), "Got unexpected error")
			assert.Equal(t, tt.wantBytesWritten, tt.w.bytesWritten)
		})
	}
}
