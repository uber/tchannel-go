package arg2

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uber/tchannel-go/typed"
)

func TestKeyValIterator(t *testing.T) {
	const (
		testBufSize = 100
		nh          = 5
	)

	buf := make([]byte, testBufSize)
	wb := typed.NewWriteBuffer(buf)
	wb.WriteUint16(nh)
	for i := 0; i < nh; i++ {
		wb.WriteLen16String(fmt.Sprintf("key%v", i))
		wb.WriteLen16String(fmt.Sprintf("value%v", i))
	}

	iter, ok := InitKeyValIterator(buf, wb.BytesWritten())
	for i := 0; i < nh; i++ {
		assert.True(t, ok)
		assert.Equal(t, fmt.Sprintf("key%v", i), string(iter.Key()))
		assert.Equal(t, fmt.Sprintf("value%v", i), string(iter.Value()))
		iter, ok = iter.Next()
	}
	assert.False(t, ok)

	t.Run("init iterator w/o Arg2", func(t *testing.T) {
		var buf []byte
		_, ok := InitKeyValIterator(buf, 0)
		assert.False(t, ok)
	})

	t.Run("init iterator w/o pairs", func(t *testing.T) {
		buf := make([]byte, 2)
		wb := typed.NewWriteBuffer(buf)
		wb.WriteUint16(0)
		_, ok := InitKeyValIterator(buf, wb.BytesWritten())
		assert.False(t, ok)
	})

	t.Run("bad key value length", func(t *testing.T) {
		buf := make([]byte, testBufSize)
		wb := typed.NewWriteBuffer(buf)
		wb.WriteUint16(1)
		wb.WriteLen16String("key")
		wb.WriteLen16String("value")
		tests := []struct {
			msg          string
			arg2Len      int
			wantIterator bool
		}{
			{
				msg:          "ok",
				arg2Len:      wb.BytesWritten(),
				wantIterator: true,
			},
			{
				msg:          "not enough to read key len",
				arg2Len:      3, // nh (2) + 1
				wantIterator: false,
			},
			{
				msg:          "not enough to read value len",
				arg2Len:      8, // nh (2) + 2 + len(key) + 1
				wantIterator: false,
			},
			{
				msg:          "not enough to iterate key",
				arg2Len:      13, // nh (2) + 2 + len(key) + 2 + len(value) = 14
				wantIterator: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.msg, func(t *testing.T) {
				iter, ok := InitKeyValIterator(buf, tt.arg2Len)
				assert.Equal(t, tt.wantIterator, ok)

				if tt.wantIterator {
					assert.Equal(t, "key", string(iter.Key()))
					assert.Equal(t, "value", string(iter.Value()))
				}
			})
		}
	})
}
