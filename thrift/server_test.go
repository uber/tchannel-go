package thrift

import (
	"testing"
	"time"

	athrift "github.com/apache/thrift/lib/go/thrift"
	"github.com/stretchr/testify/assert"
	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/testutils"
)

type ioerror struct{}

func (_ ioerror) Error() string {
	return "IO Error"
}

// ioless implements TStruct that always fails with ioerror.
type ioless struct{}

func (i *ioless) Write(p athrift.TProtocol) error {
	return ioerror{}
}

func (i *ioless) Read(p athrift.TProtocol) error {
	return ioerror{}
}

// null implements TStruct that does nothing at all with no errors.
type null struct{}

func (i *null) Write(p athrift.TProtocol) error {
	return nil
}

func (i *null) Read(p athrift.TProtocol) error {
	return nil
}

// thriftStruction is a TChannel service that implements a method that returns
// success with a TStruct that will always error.
type thriftStruction struct{}

func (ts *thriftStruction) Handle(
	ctx Context,
	methodName string,
	protocol athrift.TProtocol,
) (success bool, resp athrift.TStruct, err error) {
	// successful call with a TStruct that won't IO
	return true, &ioless{}, nil
}

func (ts *thriftStruction) Service() string {
	return "destruct"
}

func (ts *thriftStruction) Methods() []string {
	return []string{"destruct"}
}

func TestHandleTStructError(t *testing.T) {
	// We should see an ioerror on the server side.
	expectedLog := testutils.LogVerification{
		Filters: []testutils.LogFilter{{
			Filter: "Thrift server error.",
			Count:  1,
			FieldFilters: map[string]string{
				"error": "IO Error",
			},
		}}}
	opts := testutils.ChannelOpts{LogVerification: expectedLog}

	server := testutils.NewTestServer(t, &opts)
	defer server.CloseAndVerify()

	// Create a thrift server with a handler that returns success with
	// TStructs that refuse to do I/O.
	tchan := server.Server()
	thriftServer := NewServer(tchan)
	thriftServer.Register(&thriftStruction{})

	// A client should observe a server error.
	client := NewClient(
		server.NewClient(nil),
		tchan.ServiceName(),
		&ClientOptions{
			HostPort: server.HostPort(),
		})
	ctx, cancel := NewContext(time.Second)
	defer cancel()
	_, err := client.Call(ctx, "destruct", "destruct", &null{}, &null{})
	assert.Error(t, err)
	assert.IsType(t, tchannel.SystemError{}, err)
}
