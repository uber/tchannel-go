// Copyright (c) 2015 Uber Technologies, Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package thrift

import (
	"encoding/binary"
	"io"

	"github.com/apache/thrift/lib/go/thrift"
)

// WriteStreamStruct writes a length-prefixed struct.
func WriteStreamStruct(writer io.Writer, s thrift.TStruct) error {
	transport := thrift.NewTMemoryBuffer()
	transport.Buffer.Reset()

	protocol := thrift.NewTBinaryProtocol(transport, false, false)
	if err := s.Write(protocol); err != nil {
		return err
	}

	// First write out the length prefix.
	numBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(numBuf, uint32(transport.Len()))

	if _, err := writer.Write(numBuf); err != nil {
		return err
	}

	if _, err := io.Copy(writer, transport); err != nil {
		return err
	}

	return nil
}

// ReadStreamStruct reads a length-prefixed struct.
func ReadStreamStruct(reader io.Reader, f func(protocol thrift.TProtocol) error) error {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return err
	}

	length := binary.BigEndian.Uint32(buf)
	l := io.LimitReader(reader, int64(length))
	protocol := thrift.NewTBinaryProtocol(thrift.NewStreamTransportR(l), false, false)
	return f(protocol)
}
