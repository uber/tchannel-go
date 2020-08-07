// Package arg2 contains tchannel thrift Arg2 interfaces for external use.
//
// These interfaces are currently unstable, and aren't covered by the API
// backwards-compatibility guarantee.
package arg2

import (
	"encoding/binary"
	"io"

	"github.com/uber/tchannel-go/typed"
)

// KeyValIterator is a iterator for reading tchannel-thrift Arg2 Scheme,
// which has key/value pairs (k~2 v~2).
// NOTE: to be optimized for performance, we try to limit the allocation
// done in the process of iteration.
type KeyValIterator struct {
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

	leftPairCount := binary.BigEndian.Uint16(arg2Payload[0:2])
	return KeyValIterator{
		leftPairCount: int(leftPairCount),
		remaining:     arg2Payload[2:],
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

// Remaining returns whether there's any pairs left to consume.
func (i KeyValIterator) Remaining() bool {
	return i.leftPairCount > 0
}

// Next returns next iterator. Return io.EOF if no more key/value pair is
// available.
//
// Note: We used named returns because of an unexpected performance improvement
// See https://github.com/golang/go/issues/40638
func (i KeyValIterator) Next() (kv KeyValIterator, _ error) {
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

	kv = KeyValIterator{
		remaining:     rbuf.Remaining(),
		leftPairCount: leftPairCount,
		key:           key,
		val:           val,
	}
	return kv, nil
}
