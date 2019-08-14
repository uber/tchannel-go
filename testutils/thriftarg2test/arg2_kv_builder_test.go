package thriftarg2test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uber/tchannel-go/typed"
)

func TestBuildKVBuffer(t *testing.T) {
	kv := map[string]string{
		"key":  "valval",
		"key2": "val",
	}
	buf := BuildKVBuffer(kv)
	rb := typed.NewReadBuffer(buf)
	assert.EqualValues(t, len(kv), rb.ReadUint16())

	gotKV := make(map[string]string)
	for i := 0; i < len(kv); i++ {
		k := rb.ReadLen16String()
		v := rb.ReadLen16String()
		gotKV[k] = v
	}
	assert.Equal(t, kv, gotKV)
}
