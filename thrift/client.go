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
	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/internal/argreader"

	"github.com/apache/thrift/lib/go/thrift"
	"golang.org/x/net/context"
)

const _serviceHeaderKey = "$rpc$-service"

// client implements TChanClient and makes outgoing Thrift calls.
type client struct {
	ch          *tchannel.Channel
	sc          *tchannel.SubChannel
	serviceName string
	opts        ClientOptions
}

// ClientOptions are options to customize the client.
type ClientOptions struct {
	// HostPort specifies a specific server to hit.
	HostPort string
}

// NewClient returns a Client that makes calls over the given tchannel to the given Hyperbahn service.
func NewClient(ch *tchannel.Channel, serviceName string, opts *ClientOptions) TChanClient {
	client := &client{
		ch:          ch,
		sc:          ch.GetSubChannel(serviceName),
		serviceName: serviceName,
	}
	if opts != nil {
		client.opts = *opts
	}
	return client
}

func (c *client) startCall(ctx context.Context, method string, callOptions *tchannel.CallOptions) (*tchannel.OutboundCall, error) {
	if c.opts.HostPort != "" {
		return c.ch.BeginCall(ctx, c.opts.HostPort, c.serviceName, method, callOptions)
	}
	return c.sc.BeginCall(ctx, method, callOptions)
}

func writeArgs(call *tchannel.OutboundCall, headers map[string]string, req thrift.TStruct) error {
	writer, err := call.Arg2Writer()
	if err != nil {
		return err
	}
	headers = tchannel.InjectOutboundSpan(call.Response(), headers)
	if err := WriteHeaders(writer, headers); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	writer, err = call.Arg3Writer()
	if err != nil {
		return err
	}

	if err := WriteStruct(writer, req); err != nil {
		return err
	}

	return writer.Close()
}

// readResponse reads the response struct into resp, and returns:
// (response headers, whether there was an application error, unexpected error).
func readResponse(response *tchannel.OutboundCallResponse, resp thrift.TStruct) (map[string]string, bool, error) {
	reader, err := response.Arg2Reader()
	if err != nil {
		return nil, false, err
	}

	headers, err := ReadHeaders(reader)
	if err != nil {
		return nil, false, err
	}

	if err := argreader.EnsureEmpty(reader, "reading response headers"); err != nil {
		return nil, false, err
	}

	if err := reader.Close(); err != nil {
		return nil, false, err
	}

	success := !response.ApplicationError()
	reader, err = response.Arg3Reader()
	if err != nil {
		return headers, success, err
	}

	if err := ReadStruct(reader, resp); err != nil {
		return headers, success, err
	}

	if err := argreader.EnsureEmpty(reader, "reading response body"); err != nil {
		return nil, false, err
	}

	return headers, success, reader.Close()
}

func (c *client) Call(ctx Context, thriftService, methodName string, req, resp thrift.TStruct) (bool, error) {
	var (
		headers = ctx.Headers()

		respHeaders map[string]string
		isOK        bool
	)

	err := c.ch.RunWithRetry(ctx, func(ctx context.Context, rs *tchannel.RequestState) error {
		respHeaders, isOK = nil, false

		call, err := c.startCall(ctx, thriftService+"::"+methodName, &tchannel.CallOptions{
			Format:       tchannel.Thrift,
			RequestState: rs,
		})
		if err != nil {
			return err
		}

		if err := writeArgs(call, headers, req); err != nil {
			return err
		}

		respHeaders, isOK, err = readResponse(call.Response(), resp)
		return err
	})
	if err != nil {
		return false, err
	}

	ctx.SetResponseHeaders(removeRPCServiceHeader(respHeaders))
	return isOK, nil
}

// removeRPCServiceHeader removes the $rpc$-service from headers map when present
// tchannel inbound does not add a $rpc$-header. tchannel outbound sets the response headers
// that are received from the outbound call on to the context and they get sent back to the
// caller of this inbound. This becomes problematic when a tchannel inbound server is an
// intermediate part of a call graph where the initial callee verifies $rpc$-service header
// Eg. A -> B -> C
//           |-> D
// In this call graph, let us say B is a tchannel inbound server and makes two tchannel outbound
// calls to C and D. If C and D both reply with response headers, the headers get set in B's
// context, one overwriting the other, and they get sent back to A. If either one of C or D or both
// send a $rpc$-header, A will start receiving one of C's or D's $rpc$-service response header and
// can perform validation on the header value and can inadvertently believe that some other service
// other B handled the request that it sent to B and can mark the rpc as failure. This is the behavior
// that is observed with yarpc/yarpc-go framework when one of the intermediaries in the call graph
// is using a tchannel inbound server using a tchannel outbound.
//
// We have observed this issue while migrating to yarpc from tchannel inbound. For example, let us
// assume that both C & D were tchannel inbounds earlier and one of them wants to migrate to yarpc
// and at which point the $rpc$-service header flows upstream and assuming A is already using yarpc outbound
// the call will fail. Although it is a backward incompatible change, by removing the $rpc$-service header
// prevents A from seeing the $rpc$-header and marking the rpc as a failure. The alternative is to make
// B use Child() context while making downstream calls and the net effect is the same and while migrating to yarpc,
// it is unreasonable to expect the callee service owners to perform changes into the upstream callers
// to create Child() context especially when the call graph is complex are multiple levels deep. This is
// a reasonbable backward incompatible change to make as we do not expect any caller to actually depend on
// or find the $rpc$-service header from a descendant service to be actually useful.
func removeRPCServiceHeader(headers map[string]string) map[string]string {
	delete(headers, _serviceHeaderKey)
	return headers
}
