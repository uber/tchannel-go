package tchannel

import (
	"io"

	"github.com/uber/tchannel/golang/typed"
)

type circularBuffer struct {
	readInto int
	readFrom int

	buffer []byte
	reader io.Reader
}

func NewCirularFrameBuffer(r io.Reader) *circularBuffer {
	return &circularBuffer{
		buffer: make([]byte, 1024*1024),
		reader: r,
	}
}

func (b *circularBuffer) fill() error {
	if b.readInto > b.readFrom {
		n, err := b.reader.Read(b.buffer[b.readInto:])
		b.readInto += n
		return err
	}

	n, err := b.reader.Read(b.buffer[b.readInto:b.readFrom])
	b.readInto += n
	return err
}

func (b *circularBuffer) readEnd() int {
	if b.readInto > b.readFrom {
		return b.readInto
	}
	return len(b.buffer)
}

func (b *circularBuffer) HasNBytes(n int) bool {
	return n > b.readEnd()-b.readFrom
}

func (b *circularBuffer) GetFrame() (*Frame, error) {
	if b.readFrom == b.readInto {
		if err := b.fill(); err != nil {
			return nil, err
		}
	}

	// TODO what if the header is split as well?
	f := b.Get()
	f.headerBuffer = b.buffer[b.readFrom : b.readFrom+FrameHeaderSize]
	var rbuf typed.ReadBuffer
	rbuf.Wrap(f.headerBuffer)
	f.Header.read(&rbuf)

	// If the whole frame is readable read it, otherwise do copy magic?
	if b.HasNBytes(int(f.Header.PayloadSize())) {
		f.buffer = b.buffer[b.readFrom : b.readFrom+int(f.Header.FrameSize())]
		return f, nil
	}

	// Copy bytes in.
	f.buffer = make([]byte, f.Header.FrameSize())
	_ = copy(f.buffer, b.buffer[b.readFrom:b.readEnd()])

	// Now we need to read n more bytes into the frame.

	return f, nil
}

func (c *circularBuffer) Get() *Frame {
	return nil
}

func (c *circularBuffer) Release(f *Frame) {

}
