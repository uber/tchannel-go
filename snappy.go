package tchannel

import (
	"errors"
	"net"
	"syscall"
	"time"

	"github.com/golang/snappy"
	"go.uber.org/multierr"
)

// SnappyConn wraps net.Conn with Snappy compression
type SnappyConn struct {
	conn   net.Conn
	reader *snappy.Reader
	writer *snappy.Writer
}

// NewSnappyConnection creates a new Snappy compressed connection.
//
// The snappy writer is not buffered, to honor potential deadlines on the underlying net.Conn
func NewSnappyConnection(conn net.Conn) net.Conn {
	w := snappy.NewWriter(conn)
	r := snappy.NewReader(conn)
	return &SnappyConn{conn: conn, writer: w, reader: r}
}

// Read reads data from the connection.
// Read can be made to time out and return an Error with Timeout() == true
// after a fixed time limit; see SetDeadline and SetReadDeadline.
func (sc *SnappyConn) Read(b []byte) (n int, err error) {
	return sc.reader.Read(b)
}

// Write writes data to the connection.
// Write can be made to time out and return an Error with Timeout() == true
// after a fixed time limit; see SetDeadline and SetWriteDeadline.
func (sc *SnappyConn) Write(b []byte) (n int, err error) {
	return sc.writer.Write(b)
}

// Close closes the connection.
// Any blocked Read or Write operations will be unblocked and return errors.
func (sc *SnappyConn) Close() (err error) {
	return multierr.Combine(
		sc.writer.Close(),
		sc.conn.Close(),
	)
}

// LocalAddr returns the local network address.
func (sc *SnappyConn) LocalAddr() net.Addr {
	return sc.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (sc *SnappyConn) RemoteAddr() net.Addr {
	return sc.conn.RemoteAddr()
}

// SetDeadline sets the read and write deadlines associated
// with the connection. It is equivalent to calling both
// SetReadDeadline and SetWriteDeadline.
//
// A deadline is an absolute time after which I/O operations
// fail with a timeout (see type Error) instead of
// blocking. The deadline applies to all future and pending
// I/O, not just the immediately following call to Read or
// Write. After a deadline has been exceeded, the connection
// can be refreshed by setting a deadline in the future.
//
// An idle timeout can be implemented by repeatedly extending
// the deadline after successful Read or Write calls.
//
// A zero value for t means I/O operations will not time out.
//
// Note that if a TCP connection has keep-alive turned on,
// which is the default unless overridden by Dialer.KeepAlive
// or ListenConfig.KeepAlive, then a keep-alive failure may
// also return a timeout error. On Unix systems a keep-alive
// failure on I/O can be detected using
// errors.Is(err, syscall.ETIMEDOUT).
func (sc *SnappyConn) SetDeadline(t time.Time) error {
	return sc.conn.SetDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls
// and any currently-blocked Read call.
// A zero value for t means Read will not time out.
func (sc *SnappyConn) SetReadDeadline(t time.Time) error {
	return sc.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls
// and any currently-blocked Write call.
// Even if write times out, it may return n > 0, indicating that
// some of the data was successfully written.
// A zero value for t means Write will not time out.
func (sc *SnappyConn) SetWriteDeadline(t time.Time) error {
	return sc.conn.SetWriteDeadline(t)
}

// SyscallConn from the underlying connection
func (sc *SnappyConn) SyscallConn() (syscall.RawConn, error) {
	if sysc, ok := sc.conn.(syscall.Conn); ok {
		return sysc.SyscallConn()
	}
	return nil, errors.New("the underlying connection does not implement syscall.Conn")
}
