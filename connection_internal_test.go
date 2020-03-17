// Copyright (c) 2020 Uber Technologies, Inc.

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
	"bytes"
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errSyscallConn struct {
	net.Conn
}

func (c errSyscallConn) SyscallConn() (syscall.RawConn, error) {
	return nil, assert.AnError
}

func TestGetSysConn(t *testing.T) {
	t.Run("no SyscallConn", func(t *testing.T) {
		loggerBuf := &bytes.Buffer{}
		logger := NewLogger(loggerBuf)

		type dummyConn struct {
			net.Conn
		}

		syscallConn := getSysConn(dummyConn{}, logger)
		require.Nil(t, syscallConn, "expected no syscall.RawConn to be returned")
		assert.Contains(t, loggerBuf.String(), "Connection does not implement SyscallConn", "missing log")
		assert.Contains(t, loggerBuf.String(), "dummyConn", "missing type in log")
	})

	t.Run("SyscallConn returns error", func(t *testing.T) {
		loggerBuf := &bytes.Buffer{}
		logger := NewLogger(loggerBuf)

		syscallConn := getSysConn(errSyscallConn{}, logger)
		require.Nil(t, syscallConn, "expected no syscall.RawConn to be returned")
		assert.Contains(t, loggerBuf.String(), "Could not get SyscallConn", "missing log")
		assert.Contains(t, loggerBuf.String(), assert.AnError.Error(), "missing error in log")
	})

	t.Run("SyscallConn is successful", func(t *testing.T) {
		loggerBuf := &bytes.Buffer{}
		logger := NewLogger(loggerBuf)

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err, "Failed to listen")
		defer ln.Close()

		conn, err := net.Dial("tcp", ln.Addr().String())
		require.NoError(t, err, "failed to dial")
		defer conn.Close()

		sysConn := getSysConn(conn, logger)
		require.NotNil(t, sysConn)
		assert.Empty(t, loggerBuf.String(), "expected no logs on success")
	})
}
