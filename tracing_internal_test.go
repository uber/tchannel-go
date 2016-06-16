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

package tchannel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uber/tchannel-go/typed"
)

func TestTracingSpanEncoding(t *testing.T) {
	s1 := Span{
		traceID:  1,
		parentID: 2,
		spanID:   3,
		flags:    4,
	}
	// Encoding is: spanid:8 parentid:8 traceid:8 traceflags:1
	// http://tchannel.readthedocs.io/en/latest/protocol/#tracing
	encoded := []byte{
		0, 0, 0, 0, 0, 0, 0, 3, /* spanID */
		0, 0, 0, 0, 0, 0, 0, 2, /* parentID */
		0, 0, 0, 0, 0, 0, 0, 1, /* traceID */
		4, /* flags */
	}

	buf := make([]byte, len(encoded))
	writer := typed.NewWriteBuffer(buf)
	require.NoError(t, s1.write(writer), "Failed to encode span")

	assert.Equal(t, encoded, buf, "Encoded span mismatch")

	var s2 Span
	reader := typed.NewReadBuffer(buf)
	require.NoError(t, s2.read(reader), "Failed to decode span")

	assert.Equal(t, s1, s2, "Roundtrip of span failed")
}
