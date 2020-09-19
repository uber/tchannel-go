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

func TestReadKVBuffer(t *testing.T) {
	kvMap := map[string]string{
		"key":  "valval",
		"key2": "val",
	}

	var buffer [128]byte
	wbuf := typed.NewWriteBuffer(buffer[:])
	wbuf.WriteUint16(uint16(len(kvMap))) // nh

	// the order doesn't matter here since we're comparing maps
	for k, v := range kvMap {
		wbuf.WriteLen16String(k)
		wbuf.WriteLen16String(v)
	}
	assert.Equal(t, kvMap, MustReadKVBuffer(t, buffer[:wbuf.BytesWritten()]))
}
