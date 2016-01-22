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
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/apache/thrift/lib/go/thrift"
	tchannel "github.com/uber/tchannel-go"
	"golang.org/x/net/context"
)

type handler struct {
	standard       TChanServer
	streaming      TChanStreamingServer
	postResponseCB PostResponseCB
}

// Server handles incoming TChannel calls and forwards them to the matching TChanServer.
type Server struct {
	sync.RWMutex

	ch          tchannel.Registrar
	channel     *tchannel.Channel
	log         tchannel.Logger
	handlers    map[string]handler
	metaHandler *metaHandler
	ctxFn       func(ctx context.Context, method string, headers map[string]string) Context
}

// NewServer returns a server that can serve thrift services over TChannel.
func NewServer(registrar tchannel.Registrar) *Server {
	metaHandler := newMetaHandler()
	server := &Server{
		ch:          registrar,
		log:         registrar.Logger(),
		handlers:    make(map[string]handler),
		metaHandler: metaHandler,
		ctxFn:       defaultContextFn,
	}
	server.Register(newTChanMetaServer(metaHandler))
	if ch, ok := registrar.(*tchannel.Channel); ok {
		// Register the meta endpoints on the "tchannel" service name.
		NewServer(ch.GetSubChannel("tchannel"))
		server.channel = ch
	}
	return server
}

func (s *Server) registerHandler(ts TChanServer, h handler, opts ...RegisterOption) {
	serviceName := ts.Service()
	for _, opt := range opts {
		opt.Apply(&h)
	}

	s.Lock()
	s.handlers[serviceName] = h
	s.Unlock()

	for _, m := range ts.Methods() {
		s.ch.Register(s, serviceName+"::"+m)
	}

	if sts, ok := ts.(TChanStreamingServer); ok {
		if len(sts.StreamingMethods()) > 0 && h.streaming == nil {
			panic("Using Register when you should use RegisterStreaming")
		}
		for _, m := range sts.StreamingMethods() {
			s.ch.Register(s, serviceName+"::"+m)
		}
	}
}

// Register registers the given TChanServer to be called on any incoming call for its' services.
func (s *Server) Register(svr TChanServer, opts ...RegisterOption) {
	s.registerHandler(svr, handler{standard: svr}, opts...)
}

// RegisterStreaming registers the given TChanStreamingServer to be called on any incoming call
// for its' services.
func (s *Server) RegisterStreaming(svr TChanStreamingServer, opts ...RegisterOption) {
	s.registerHandler(svr, handler{standard: svr, streaming: svr}, opts...)
}

// RegisterHealthHandler uses the user-specified function f for the Health endpoint.
func (s *Server) RegisterHealthHandler(f HealthFunc) {
	s.metaHandler.setHandler(f)
}

// SetContextFn sets the function used to convert a context.Context to a thrift.Context.
// Note: This API may change and is only intended to bridge different contexts.
func (s *Server) SetContextFn(f func(ctx context.Context, method string, headers map[string]string) Context) {
	s.ctxFn = f
}

func (s *Server) onError(err error) {
	// TODO(prashant): Expose incoming call errors through options for NewServer.
	// Timeouts should not be reported as errors.
	if tchannel.GetSystemErrorCode(err) == tchannel.ErrCodeTimeout {
		s.log.Debugf("thrift Server timeout: %v", err)
	} else {
		s.log.WithFields(tchannel.ErrField(err)).Error("Thrift server error.")
	}
}

func defaultContextFn(ctx context.Context, method string, headers map[string]string) Context {
	return WithHeaders(ctx, headers)
}

func (s *Server) handleStandard(origCtx context.Context, handler handler, method string, call *tchannel.InboundCall) error {
	headers, err := readHeadersFromCall(call)
	if err != nil {
		return err
	}
	ctx := s.ctxFn(origCtx, method, headers)

	reader, err := call.Arg3Reader()
	if err != nil {
		return err
	}

	wp := getProtocolReader(reader)
	success, resp, err := handler.standard.Handle(ctx, method, wp.protocol)
	thriftProtocolPool.Put(wp)

	if err != nil {
		if _, ok := err.(thrift.TProtocolException); ok {
			// We failed to parse the Thrift generated code, so convert the error to bad request.
			err = tchannel.NewSystemError(tchannel.ErrCodeBadRequest, err.Error())
		}

		reader.Close()
		call.Response().SendSystemError(err)
		return nil
	}
	if err := reader.Close(); err != nil {
		return err
	}

	if !success {
		call.Response().SetApplicationError()
	}

	writer, err := call.Response().Arg2Writer()
	if err != nil {
		return err
	}

	if err := WriteHeaders(writer, ctx.ResponseHeaders()); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	writer, err = call.Response().Arg3Writer()
	wp = getProtocolWriter(writer)
	resp.Write(wp.protocol)
	thriftProtocolPool.Put(wp)
	err = writer.Close()

	if handler.postResponseCB != nil {
		handler.postResponseCB(method, resp)
	}

	return err
}

func getServiceMethod(method string) (string, string, bool) {
	s := string(method)
	sep := strings.Index(s, "::")
	if sep == -1 {
		return "", "", false
	}
	return s[:sep], s[sep+2:], true
}

// StreamWriter is the interface for writing multiple Thrift structs.
type StreamWriter interface {
	Flush() error
	Write(resp thrift.TStruct) error
	Close(err error) error
}

type streamWriter struct {
	call        *tchannel.InboundCall
	protocol    thrift.TProtocol
	arg2Written bool
}

func (w *streamWriter) sendArg2() error {

	w.arg2Written = true
	return nil
}

func (w *streamWriter) Flush() error {
	if !w.arg2Written {
		if err := w.sendArg2(); err != nil {
			return err
		}
	}

	return w.Flush()
}

func (w *streamWriter) Write(resp thrift.TStruct) error {
	if !w.arg2Written {
		if err := w.sendArg2(); err != nil {
			return err
		}
	}

	// Write out the struct
	if err := resp.Write(w.protocol); err != nil {
		return err
	}

	return nil
}

func (w *streamWriter) Close(err error) error {
	if err != nil && w.arg2Written {
		// All errors must be sent by now
		return fmt.Errorf("errors must be sent before any stream results")
	}

	if err != nil {
		w.call.Response().SetApplicationError()

		// NOT STREAMING?
	}

	if !w.arg2Written {
		if err := w.sendArg2(); err != nil {
			return err
		}

	}

	_, err = w.call.Response().Arg3Writer()
	if err != nil {
		return err
	}

	w.protocol.Flush()
	return nil
}

// Each language can then just translate directly?

// Handle handles an incoming TChannel call and forwards it to the correct handler.
func (s *Server) Handle(tctx context.Context, call *tchannel.InboundCall) {
	op := call.MethodString()
	service, method, ok := getServiceMethod(op)
	if !ok {
		log.Fatalf("Handle got call for %s which does not match the expected call format", op)
	}

	s.RLock()
	handler, ok := s.handlers[service]
	s.RUnlock()
	if !ok {
		log.Fatalf("Handle got call for service %v which is not registered", service)
	}

	// TODO(prashant): Make the is streaming method check more efficient.
	var isStreaming bool
	if handler.streaming != nil {
		for _, v := range handler.streaming.StreamingMethods() {
			if v == method {
				isStreaming = true
				break
			}
		}
	}

	// TODO(prashant): Logic for reading headers should not be duplicated.
	if !isStreaming {
		if err := s.handleStandard(tctx, handler, method, call); err != nil {
			s.onError(err)
		}
		return
	}

	headers, err := readHeadersFromCall(call)
	if err != nil {
		log.Fatal(err)
	}
	ctx := WithHeaders(tctx, headers)
	// TODO(prashant): Call post-response callback for each response write?
	handler.streaming.HandleStreaming(ctx, call)
}

//
// // HandleStreaming handles an incoming streaming TChannel call and forwards it to
// // the correct handler.
// func (s *Server) HandleStreaming(ctx context.Context, call *tchannel.InboundCall) {
// 	parts := strings.Split(string(call.MethodString()), "::")
// 	if len(parts) != 2 {
// 		log.Fatalf("Handle got call for %v which does not match the expected call format", parts)
// 	}
//
// 	service := parts[0]
// 	s.RLock()
// 	handler, ok := s.handlers[service]
// 	s.RUnlock()
// 	if !ok {
// 		log.Fatalf("Handle got call for service %v which is not registered", service)
// 	}
//
// 	headers, err := readHeadersFromCall(call)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	tctx := WithHeaders(ctx, headers)
// 	// TODO(prashant): Call post-response callback for each response write?
// 	handler.streaming.HandleStreaming(tctx, call)
// }

func readHeadersFromCall(call *tchannel.InboundCall) (map[string]string, error) {
	var headers map[string]string
	err := readHelper(call.Arg2Reader, func(r tchannel.ArgReader) (err error) {
		headers, err = ReadHeaders(r)
		return err
	})
	return headers, err
}
