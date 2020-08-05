// Package arg2 contains tchannel thrift Arg2 interfaces for external use.
//
// These interfaces are currently unstable, and aren't covered by the API
// backwards-compatibility guarantee.
package arg2

import (
	"fmt"
	"io"

	"github.com/uber/tchannel-go/typed"
)

// KeyValIterator is a iterator for reading tchannel-thrift Arg2 Scheme,
// which has key/value pairs (k~2 v~2).
// NOTE: to be optimized for performance, we try to limit the allocation
// done in the process of iteration.
type KeyValIterator struct {
	arg2Len       int
	rbuf          *typed.ReadBuffer
	leftPairCount int
	key           []byte
	val           []byte
}

// NewKeyValIterator inits a KeyValIterator with the buffer pointing at
// start of Arg2. Return io.EOF if no iterator is available.
// NOTE: tchannel-thrift Arg Scheme starts with number of key/value pair.
func NewKeyValIterator(arg2Payload []byte) (KeyValIterator, error) {
	if len(arg2Payload) < 2 {
		return KeyValIterator{}, io.EOF
	}

	rbuf := typed.NewReadBuffer(arg2Payload)
	leftPairCount := rbuf.ReadUint16()

	return KeyValIterator{
		arg2Len:       len(arg2Payload),
		leftPairCount: int(leftPairCount),
		rbuf:          rbuf,
	}.Next()
}

// Key Returns the key.
func (i KeyValIterator) Key() []byte {
	return i.key
}

// Value returns value.
func (i KeyValIterator) Value() []byte {
	return i.val
}

// Next returns next iterator. Return io.EOF if no more key/value pair is
// available.
func (i KeyValIterator) Next() (KeyValIterator, error) {
	if i.leftPairCount <= 0 {
		return KeyValIterator{}, io.EOF
	}

	if i.rbuf.BytesRemaining() < 2 {
		return KeyValIterator{}, fmt.Errorf("invalid key offset %v (arg2 len %v)", i.rbuf.BytesRead(), i.arg2Len)
	}
	keyLen := int(i.rbuf.ReadUint16())
	keyOffset := i.rbuf.BytesRead()
	if i.rbuf.BytesRemaining() < keyLen {
		return KeyValIterator{}, fmt.Errorf("key exceeds arg2 range (key offset %v, key len %v, arg2 len %v)", keyOffset, keyLen, i.arg2Len)
	}

	key := i.rbuf.ReadBytes(keyLen)

	if i.rbuf.BytesRemaining() < 2 {
		return KeyValIterator{}, fmt.Errorf("invalid value offset %v (key offset %v, key len %v, arg2 len %v)", i.rbuf.BytesRead(), keyOffset, keyLen, i.arg2Len)
	}
	valLen := int(i.rbuf.ReadUint16())
	valOffset := i.rbuf.BytesRead()
	if i.rbuf.BytesRemaining() < valLen {
		return KeyValIterator{}, fmt.Errorf("value exceeds arg2 range (offset %v, len %v, arg2 len %v)", valOffset, valLen, i.arg2Len)
	}

	val := i.rbuf.ReadBytes(valLen)
	leftPairCount := i.leftPairCount - 1

	return KeyValIterator{
		arg2Len:       i.arg2Len,
		rbuf:          i.rbuf,
		leftPairCount: leftPairCount,
		key:           key,
		val:           val,
	}, nil
}
