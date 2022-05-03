package thrift

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/testutils"
	athrift "github.com/uber/tchannel-go/thirdparty/github.com/apache/thrift/lib/go/thrift"
)

var errIO = errors.New("IO Error")

// badTStruct implements TStruct that always fails with the provided error.
type badTStruct struct {
	// If specified, runs the specified function before failing the Write.
	PreWrite func(athrift.TProtocol)

	Err error
}

func (t *badTStruct) Write(p athrift.TProtocol) error {
	if t.PreWrite != nil {
		t.PreWrite(p)
	}
	return t.Err
}

func (t *badTStruct) Read(p athrift.TProtocol) error {
	return t.Err
}

// nullTStruct implements TStruct that does nothing at all with no errors.
type nullTStruct struct{}

func (*nullTStruct) Write(p athrift.TProtocol) error {
	return nil
}

func (*nullTStruct) Read(p athrift.TProtocol) error {
	return nil
}

// thriftStruction is a TChannel service that implements the following
// methods:
//
//   destruct
//     Returns a TStruct that fails without writing anything.
//   partialDestruct
//     Returns a TStruct that fails after writing partial output.
type thriftStruction struct{}

func (ts *thriftStruction) Handle(
	ctx Context,
	methodName string,
	protocol athrift.TProtocol,
) (success bool, resp athrift.TStruct, err error) {
	var preWrite func(athrift.TProtocol)
	if methodName == "partialDestruct" {
		preWrite = func(p athrift.TProtocol) {
			p.WriteStructBegin("foo")
			p.WriteFieldBegin("bar", athrift.STRING, 42)
			p.WriteString("baz")
		}
	}

	// successful call with a TStruct that fails while writing.
	return true, &badTStruct{Err: errIO, PreWrite: preWrite}, nil
}

func (ts *thriftStruction) Service() string { return "destruct" }

func (ts *thriftStruction) Methods() []string {
	return []string{"destruct", "partialDestruct"}
}

func TestHandleTStructError(t *testing.T) {
	serverOpts := testutils.NewOpts().
		AddLogFilter(
			"Thrift server error.", 1,
			"error", "IO Error",
			"method", "destruct::destruct").
		AddLogFilter(
			"Thrift server error.", 1,
			"error", "IO Error",
			"method", "destruct::partialDestruct")
	server := testutils.NewTestServer(t, serverOpts)
	defer server.CloseAndVerify()

	// Create a thrift server with a handler that returns success with
	// TStructs that refuse to do I/O.
	tchan := server.Server()
	NewServer(tchan).Register(&thriftStruction{})

	client := NewClient(
		server.NewClient(testutils.NewOpts()),
		tchan.ServiceName(),
		&ClientOptions{HostPort: server.HostPort()},
	)

	t.Run("failing response", func(t *testing.T) {
		ctx, cancel := NewContext(time.Second)
		defer cancel()

		_, err := client.Call(ctx, "destruct", "destruct", &nullTStruct{}, &nullTStruct{})
		assert.Error(t, err)
		assert.IsType(t, tchannel.SystemError{}, err)
		assert.Equal(t, tchannel.ErrCodeUnexpected, tchannel.GetSystemErrorCode(err))
		assert.Equal(t, "IO Error", tchannel.GetSystemErrorMessage(err))
	})

	t.Run("failing response with partial write", func(t *testing.T) {
		ctx, cancel := NewContext(time.Second)
		defer cancel()

		_, err := client.Call(ctx, "destruct", "partialDestruct", &nullTStruct{}, &nullTStruct{})
		assert.Error(t, err)
		assert.IsType(t, tchannel.SystemError{}, err)
		assert.Equal(t, tchannel.ErrCodeUnexpected, tchannel.GetSystemErrorCode(err))
		assert.Equal(t, "IO Error", tchannel.GetSystemErrorMessage(err))
	})
}
