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

	iter, ok := InitKeyValIterator(buf)
	for i := 0; i < nh; i++ {
		assert.True(t, ok)
		assert.Equal(t, fmt.Sprintf("key%v", i), string(iter.Key()))
		assert.Equal(t, fmt.Sprintf("value%v", i), string(iter.Value()))
		iter, ok = iter.Next()
	}
	assert.False(t, ok)

	t.Run("init iterator w/o pairs", func(t *testing.T) {
		buf := make([]byte, 2)
		wb := typed.NewWriteBuffer(buf)
		wb.WriteUint16(0)
		_, ok := InitKeyValIterator(buf)
		assert.False(t, ok)
	})
}
