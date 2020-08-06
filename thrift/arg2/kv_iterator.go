// Package arg2 contains tchannel thrift Arg2 interfaces for external use.
//
// These interfaces are currently unstable, and aren't covered by the API
// backwards-compatibility guarantee.
package arg2

import (
	"io"

	"github.com/uber/tchannel-go/typed"
)

// KeyValIterator is a iterator for reading tchannel-thrift Arg2 Scheme,
// which has key/value pairs (k~2 v~2).
// NOTE: to be optimized for performance, we try to limit the allocation
// done in the process of iteration.
type KeyValIterator struct {
	arg2Len       int
	remaining     []byte
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

	// We don't hold on to rbuf to avoid the associated alloc
	rbuf := typed.NewReadBuffer(arg2Payload)
	leftPairCount := rbuf.ReadUint16()

	return KeyValIterator{
		arg2Len:       len(arg2Payload),
		leftPairCount: int(leftPairCount),
		remaining:     arg2Payload[rbuf.BytesRead():],
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

	rbuf := typed.NewReadBuffer(i.remaining)
	keyLen := int(rbuf.ReadUint16())
	key := rbuf.ReadBytes(keyLen)
	valLen := int(rbuf.ReadUint16())
	val := rbuf.ReadBytes(valLen)
	if rbuf.Err() != nil {
		return KeyValIterator{}, rbuf.Err()
	}

	leftPairCount := i.leftPairCount - 1

	return KeyValIterator{
		arg2Len:       i.arg2Len,
		remaining:     i.remaining[rbuf.BytesRead():],
		leftPairCount: leftPairCount,
		key:           key,
		val:           val,
	}, nil
}
