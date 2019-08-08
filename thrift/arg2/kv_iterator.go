package arg2

import "encoding/binary"

// KeyValIterator is a iterator for reading tchannel-thrift Arg2 Scheme,
// which has key/value pairs (k~2 v~2).
// NOTE: to be optimized for performance, we try to limit the allocation
// done in the process of iteration.
type KeyValIterator struct {
	arg2Len               int
	leftPairCount         int
	keyOffset, keyLen     int
	valueOffset, valueLen int
	arg2Payload           []byte
}

// InitKeyValIterator inits a KeyValIterator with the buffer pointing at
// start of Arg2.
// NOTE: tchannel-thrift Arg Scheme starts with number of key/value pair.
func InitKeyValIterator(arg2Payload []byte, arg2Len int) (KeyValIterator, bool) {
	nh := int(binary.BigEndian.Uint16(arg2Payload[0:2]))
	if nh <= 0 {
		return KeyValIterator{}, false
	}

	return KeyValIterator{
		leftPairCount: nh,
		arg2Len:       arg2Len,
		arg2Payload:   arg2Payload,
	}.next(2 /*nh*/)
}

// Key Returns the key
func (i KeyValIterator) Key() []byte {
	return i.arg2Payload[i.keyOffset : i.keyOffset+i.keyLen]
}

// Value returns value.
func (i KeyValIterator) Value() []byte {
	return i.arg2Payload[i.valueOffset : i.valueOffset+i.valueLen]
}

// Next returns next iterator. Return nil if no more key/value pair available.
func (i KeyValIterator) Next() (KeyValIterator, bool) {
	if i.leftPairCount <= 0 {
		return KeyValIterator{}, false
	}

	return i.next(i.valueOffset + i.valueLen)
}

// cur is the offset from the start of Arg2 payload to next key/value pair
// we want to iterate to.
func (i KeyValIterator) next(cur int) (KeyValIterator, bool) {
	if cur+2 > i.arg2Len {
		return KeyValIterator{}, false
	}
	keyLen := int(binary.BigEndian.Uint16(i.arg2Payload[cur : cur+2]))
	cur += 2
	keyOffset := cur
	cur += keyLen

	if cur+2 > i.arg2Len {
		return KeyValIterator{}, false
	}
	valueLen := int(binary.BigEndian.Uint16(i.arg2Payload[cur : cur+2]))
	cur += 2
	valueOffset := cur

	if valueOffset+valueLen > i.arg2Len {
		return KeyValIterator{}, false
	}

	return KeyValIterator{
		arg2Len:       i.arg2Len,
		arg2Payload:   i.arg2Payload,
		leftPairCount: i.leftPairCount - 1,
		keyOffset:     keyOffset,
		keyLen:        keyLen,
		valueOffset:   valueOffset,
		valueLen:      valueLen,
	}, true
}
