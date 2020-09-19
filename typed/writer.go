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

package typed

import (
	"encoding/binary"
	"io"
	"sync"
)

type intBuffer [8]byte

var intBufferPool = sync.Pool{New: func() interface{} {
	return new(intBuffer)
}}

// Writer is a writer that writes typed values to an io.Writer
type Writer struct {
	writer io.Writer
	err    error
}

// NewWriter creates a writer that writes typed value to a reader
func NewWriter(w io.Writer) *Writer {
	return &Writer{
		writer: w,
	}
}

// WriteBytes writes a slice of bytes to the io.Writer
func (w *Writer) WriteBytes(b []byte) {
	if w.err != nil {
		return
	}

	if _, err := w.writer.Write(b); err != nil {
		w.err = err
	}
}

// WriteUint16 writes a uint16 to the io.Writer
func (w *Writer) WriteUint16(n uint16) {
	if w.err != nil {
		return
	}

	sizeBuf := intBufferPool.Get().(*intBuffer)
	defer intBufferPool.Put(sizeBuf)

	binary.BigEndian.PutUint16(sizeBuf[:2], n)
	if _, err := w.writer.Write(sizeBuf[:2]); err != nil {
		w.err = err
	}
}

// WriteLen16Bytes writes a slice of bytes to the io.Writer preceded with
// the length of the slice
func (w *Writer) WriteLen16Bytes(b []byte) {
	if w.err != nil {
		return
	}

	w.WriteUint16(uint16(len(b)))
	w.WriteBytes(b)
}

// Err returns the error state of the writer
func (w *Writer) Err() error {
	return w.err
}
