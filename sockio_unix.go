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

// Match the golang/sys unix file, https://github.com/golang/sys/blob/master/unix/syscall_unix.go#L5
// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris

package tchannel

import (
	"go.uber.org/multierr"
	"golang.org/x/sys/unix"
)

func (c *Connection) sendBufSize() (sendBufUsage int, sendBufSize int, _ error) {
	sendBufSize = -1
	sendBufUsage = -1

	if c.sysConn == nil {
		return sendBufUsage, sendBufSize, errNoSyscallConn
	}

	var sendBufLenErr, sendBufLimErr error
	errs := c.sysConn.Control(func(fd uintptr) {
		sendBufUsage, sendBufLenErr = getSendQueueLen(fd)
		sendBufSize, sendBufLimErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUF)
	})

	errs = multierr.Append(errs, sendBufLimErr)
	errs = multierr.Append(errs, sendBufLenErr)
	return sendBufUsage, sendBufSize, errs
}
