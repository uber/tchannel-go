package arg2

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go/testutils/thriftarg2test"
)

func TestKeyValIterator(t *testing.T) {
	const (
		testBufSize = 100
		nh          = 5
	)

	kv := make(map[string]string, nh)
	for i := 0; i < nh; i++ {
		kv[fmt.Sprintf("key%v", i)] = fmt.Sprintf("value%v", i)
	}
	buf := thriftarg2test.BuildKVBuffer(kv)

	iter, err := NewKeyValIterator(buf)
	gotKV := make(map[string]string)
	for i := 0; i < nh; i++ {
		assert.NoError(t, err)
		gotKV[fmt.Sprintf("key%v", i)] = fmt.Sprintf("value%v", i)
		iter, err = iter.Next()
	}
	assert.Equal(t, io.EOF, err)
	assert.Equal(t, kv, gotKV)

	t.Run("init iterator w/o Arg2", func(t *testing.T) {
		_, err := NewKeyValIterator(nil)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("init iterator w/o pairs", func(t *testing.T) {
		buf := thriftarg2test.BuildKVBuffer(nil /*kv*/)
		_, err := NewKeyValIterator(buf)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("bad key value length", func(t *testing.T) {
		buf := thriftarg2test.BuildKVBuffer(map[string]string{
			"key": "value",
		})
		tests := []struct {
			msg     string
			arg2Len int
			wantErr string
		}{
			{
				msg:     "ok",
				arg2Len: len(buf),
			},
			{
				msg:     "not enough to read key len",
				arg2Len: 3, // nh (2) + 1
				wantErr: "buffer is too small",
			},
			{
				msg:     "not enough to hold key value",
				arg2Len: 6, // nh (2) + 2 + len(key) - 1
				wantErr: "buffer is too small",
			},
			{
				msg:     "not enough to read value len",
				arg2Len: 8, // nh (2) + 2 + len(key) + 1
				wantErr: "buffer is too small",
			},
			{
				msg:     "not enough to iterate value",
				arg2Len: 13, // nh (2) + 2 + len(key) + 2 + len(value) = 14
				wantErr: "buffer is too small",
			},
		}

		for _, tt := range tests {
			t.Run(tt.msg, func(t *testing.T) {
				iter, err := NewKeyValIterator(buf[:tt.arg2Len])
				if tt.wantErr == "" {
					assert.NoError(t, err)
					assert.Equal(t, "key", string(iter.Key()), "unexpected key")
					assert.Equal(t, "value", string(iter.Value()), "unexpected value")
					return
				}

				require.Error(t, err, "should not create iterator")
				assert.Contains(t, err.Error(), tt.wantErr)
			})
		}
	})
}

func BenchmarkKeyValIterator(b *testing.B) {
	kvBuffer := thriftarg2test.BuildKVBuffer(map[string]string{
		"foo":  "bar",
		"baz":  "qux",
		"quux": "corge",
	})

	for i := 0; i < b.N; i++ {
		iter, err := NewKeyValIterator(kvBuffer)
		for err == nil {
			iter, err = iter.Next()
		}
	}
}
