package testutils

import "github.com/uber/tchannel-go/typed"

// BuildKVBuffer builds an thrift Arg2 KV buffer.
func BuildKVBuffer(kv map[string]string) []byte {
	// Scan once to know size of buffer
	var bufSize int
	for k, v := range kv {
		// k~2 v~2
		bufSize += 2 + len(k) + 2 + len(v)
	}
	bufSize += 2 // nh:2

	buf := make([]byte, bufSize)
	wb := typed.NewWriteBuffer(buf)
	wb.WriteUint16(uint16(len(kv)))
	for k, v := range kv {
		wb.WriteLen16String(k)
		wb.WriteLen16String(v)
	}
	return buf[:wb.BytesWritten()]
}
