package tchannel

import (
	"io"
	"log"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getFrameOfSize(size int) *Frame {
	frame := NewFrame(size)
	frame.Header.messageType = 5
	for i := 0; i < size; i++ {
		frame.Payload[i] = byte(i % 256)
	}
	return frame
}

func verifyFramesSame(t *testing.T, f1 *Frame, f2 *Frame) bool {
	passed := true
	passed = passed && assert.Equal(t, f1.Header, f2.Header, "Headers")
	passed = passed && assert.Equal(t, f1.SizedPayload(), f2.SizedPayload(), "Payload")
	return passed
}

func testCircularBuffer(t *testing.T, size int) {
	pr, pw := io.Pipe()
	cb := NewCirularFrameBuffer(pr)

	var frames []*Frame
	for i := 0; i < 1000; i++ {
		f := getFrameOfSize(rand.Intn(MaxFramePayloadSize))
		frames = append(frames, f)
	}

	writeFrames := func(pw io.WriteCloser) {
		for _, f := range frames {
			require.NoError(t, f.WriteOut(pw), "WriteOut frame failed")
		}
		pw.Close()
	}
	go writeFrames(pw)

	// Start reading into our buffer
	for i, f := range frames {
		got, err := cb.GetFrame()
		assert.NoError(t, err, "GetFrame failed")
		passed := verifyFramesSame(t, f, got)
		log.Printf("Frame %v: %v", i, passed)
	}

	_, err := cb.GetFrame()
	assert.Error(t, err, "expected EOF")
}

func TestCircularBuffer(t *testing.T) {
	testCircularBuffer(t, MaxFrameSize)
}
