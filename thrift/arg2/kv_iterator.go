package arg2

import "encoding/binary"

// KeyValIterator is a iterator for reading tchannel-thrift Arg2 Scheme,
// which has key/value pairs (k~2 v~2).
// NOTE: to be optimized for performance, we try to limit the allocation
// done in the process of iteration.
type KeyValIterator struct {
	leftPairCount         int
	start                 []byte
	keyOffset, keyLen     int
	valueOffset, valueLen int
}

// InitKeyValIterator inits a KeyValIterator with the buffer pointing at
// start of Arg2.
// NOTE: tchannel-thrift Arg Scheme starts with number of key/value pair.
func InitKeyValIterator(arg2Start []byte) (KeyValIterator, bool) {
	nh := int(binary.BigEndian.Uint16(arg2Start[0:2]))
	if nh <= 0 {
		return KeyValIterator{}, false
	}

	return createKVIterator(nh, arg2Start[2:]), true
}

// Key Returns the key
func (i KeyValIterator) Key() []byte {
	return i.start[i.keyOffset : i.keyOffset+i.keyLen]
}

// Value returns value.
func (i KeyValIterator) Value() []byte {
	return i.start[i.valueOffset : i.valueOffset+i.valueLen]
}

// Next returns next iterator. Return nil if no more key/value pair available.
func (i KeyValIterator) Next() (KeyValIterator, bool) {
	if i.leftPairCount <= 0 {
		return KeyValIterator{}, false
	}

	return createKVIterator(
		i.leftPairCount,
		i.start[2 /*uint16 len*/ +i.keyLen+2 /*uint16 len*/ +i.valueLen:],
	), true
}

func createKVIterator(pairCount int, bufferStart []byte) KeyValIterator {
	var cur int
	keyLen := int(binary.BigEndian.Uint16(bufferStart[cur : cur+2]))
	cur += 2
	keyOffset := cur
	cur += keyLen

	valueLen := int(binary.BigEndian.Uint16(bufferStart[cur : cur+2]))
	cur += 2
	valueOffset := cur

	return KeyValIterator{
		start:         bufferStart,
		leftPairCount: pairCount - 1,
		keyOffset:     keyOffset,
		keyLen:        keyLen,
		valueOffset:   valueOffset,
		valueLen:      valueLen,
	}
}
