package tchannel

import (
	"encoding/binary"
	"testing"

	"github.com/uber/tchannel-go/typed"

	"github.com/stretchr/testify/assert"
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
