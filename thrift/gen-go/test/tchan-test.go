// @generated Code generated by thrift-gen. Do not modify.

// Package test is generated code used to make or handle TChannel calls using Thrift.
package test

import (
	"fmt"

	athrift "github.com/uber/tchannel-go/thirdparty/github.com/apache/thrift/lib/go/thrift"
	"github.com/uber/tchannel-go/thrift"
)

// Interfaces for the service and client for the services defined in the IDL.

// TChanMeta is the interface that defines the server handler and client interface.
type TChanMeta interface {
	Health(ctx thrift.Context) (*HealthStatus, error)
}

// TChanSecondService is the interface that defines the server handler and client interface.
type TChanSecondService interface {
	Echo(ctx thrift.Context, arg string) (string, error)
}

// TChanSimpleService is the interface that defines the server handler and client interface.
type TChanSimpleService interface {
	Call(ctx thrift.Context, arg *Data) (*Data, error)
	Simple(ctx thrift.Context) error
	SimpleFuture(ctx thrift.Context) error
}

// Implementation of a client and service handler.

type tchanMetaClient struct {
	thriftService string
	client        thrift.TChanClient
}

func NewTChanMetaInheritedClient(thriftService string, client thrift.TChanClient) *tchanMetaClient {
	return &tchanMetaClient{
		thriftService,
		client,
	}
}

// NewTChanMetaClient creates a client that can be used to make remote calls.
func NewTChanMetaClient(client thrift.TChanClient) TChanMeta {
	return NewTChanMetaInheritedClient("Meta", client)
}

func (c *tchanMetaClient) Health(ctx thrift.Context) (*HealthStatus, error) {
	var resp MetaHealthResult
	args := MetaHealthArgs{}
	success, err := c.client.Call(ctx, c.thriftService, "health", &args, &resp)
	if err == nil && !success {
		switch {
		default:
			err = fmt.Errorf("received no result or unknown exception for health")
		}
	}

	return resp.GetSuccess(), err
}

type tchanMetaServer struct {
	handler TChanMeta
}

// NewTChanMetaServer wraps a handler for TChanMeta so it can be
// registered with a thrift.Server.
func NewTChanMetaServer(handler TChanMeta) thrift.TChanServer {
	return &tchanMetaServer{
		handler,
	}
}

func (s *tchanMetaServer) Service() string {
	return "Meta"
}

func (s *tchanMetaServer) Methods() []string {
	return []string{
		"health",
	}
}

func (s *tchanMetaServer) Handle(ctx thrift.Context, methodName string, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	switch methodName {
	case "health":
		return s.handleHealth(ctx, protocol)

	default:
		return false, nil, fmt.Errorf("method %v not found in service %v", methodName, s.Service())
	}
}

func (s *tchanMetaServer) handleHealth(ctx thrift.Context, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	var req MetaHealthArgs
	var res MetaHealthResult

	if err := req.Read(protocol); err != nil {
		return false, nil, err
	}

	r, err :=
		s.handler.Health(ctx)

	if err != nil {
		return false, nil, err
	} else {
		res.Success = r
	}

	return err == nil, &res, nil
}

type tchanSecondServiceClient struct {
	thriftService string
	client        thrift.TChanClient
}

func NewTChanSecondServiceInheritedClient(thriftService string, client thrift.TChanClient) *tchanSecondServiceClient {
	return &tchanSecondServiceClient{
		thriftService,
		client,
	}
}

// NewTChanSecondServiceClient creates a client that can be used to make remote calls.
func NewTChanSecondServiceClient(client thrift.TChanClient) TChanSecondService {
	return NewTChanSecondServiceInheritedClient("SecondService", client)
}

func (c *tchanSecondServiceClient) Echo(ctx thrift.Context, arg string) (string, error) {
	var resp SecondServiceEchoResult
	args := SecondServiceEchoArgs{
		Arg: arg,
	}
	success, err := c.client.Call(ctx, c.thriftService, "Echo", &args, &resp)
	if err == nil && !success {
		switch {
		default:
			err = fmt.Errorf("received no result or unknown exception for Echo")
		}
	}

	return resp.GetSuccess(), err
}

type tchanSecondServiceServer struct {
	handler TChanSecondService
}

// NewTChanSecondServiceServer wraps a handler for TChanSecondService so it can be
// registered with a thrift.Server.
func NewTChanSecondServiceServer(handler TChanSecondService) thrift.TChanServer {
	return &tchanSecondServiceServer{
		handler,
	}
}

func (s *tchanSecondServiceServer) Service() string {
	return "SecondService"
}

func (s *tchanSecondServiceServer) Methods() []string {
	return []string{
		"Echo",
	}
}

func (s *tchanSecondServiceServer) Handle(ctx thrift.Context, methodName string, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	switch methodName {
	case "Echo":
		return s.handleEcho(ctx, protocol)

	default:
		return false, nil, fmt.Errorf("method %v not found in service %v", methodName, s.Service())
	}
}

func (s *tchanSecondServiceServer) handleEcho(ctx thrift.Context, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	var req SecondServiceEchoArgs
	var res SecondServiceEchoResult

	if err := req.Read(protocol); err != nil {
		return false, nil, err
	}

	r, err :=
		s.handler.Echo(ctx, req.Arg)

	if err != nil {
		return false, nil, err
	} else {
		res.Success = &r
	}

	return err == nil, &res, nil
}

type tchanSimpleServiceClient struct {
	thriftService string
	client        thrift.TChanClient
}

func NewTChanSimpleServiceInheritedClient(thriftService string, client thrift.TChanClient) *tchanSimpleServiceClient {
	return &tchanSimpleServiceClient{
		thriftService,
		client,
	}
}

// NewTChanSimpleServiceClient creates a client that can be used to make remote calls.
func NewTChanSimpleServiceClient(client thrift.TChanClient) TChanSimpleService {
	return NewTChanSimpleServiceInheritedClient("SimpleService", client)
}

func (c *tchanSimpleServiceClient) Call(ctx thrift.Context, arg *Data) (*Data, error) {
	var resp SimpleServiceCallResult
	args := SimpleServiceCallArgs{
		Arg: arg,
	}
	success, err := c.client.Call(ctx, c.thriftService, "Call", &args, &resp)
	if err == nil && !success {
		switch {
		default:
			err = fmt.Errorf("received no result or unknown exception for Call")
		}
	}

	return resp.GetSuccess(), err
}

func (c *tchanSimpleServiceClient) Simple(ctx thrift.Context) error {
	var resp SimpleServiceSimpleResult
	args := SimpleServiceSimpleArgs{}
	success, err := c.client.Call(ctx, c.thriftService, "Simple", &args, &resp)
	if err == nil && !success {
		switch {
		case resp.SimpleErr != nil:
			err = resp.SimpleErr
		default:
			err = fmt.Errorf("received no result or unknown exception for Simple")
		}
	}

	return err
}

func (c *tchanSimpleServiceClient) SimpleFuture(ctx thrift.Context) error {
	var resp SimpleServiceSimpleFutureResult
	args := SimpleServiceSimpleFutureArgs{}
	success, err := c.client.Call(ctx, c.thriftService, "SimpleFuture", &args, &resp)
	if err == nil && !success {
		switch {
		case resp.SimpleErr != nil:
			err = resp.SimpleErr
		case resp.NewErr_ != nil:
			err = resp.NewErr_
		default:
			err = fmt.Errorf("received no result or unknown exception for SimpleFuture")
		}
	}

	return err
}

type tchanSimpleServiceServer struct {
	handler TChanSimpleService
}

// NewTChanSimpleServiceServer wraps a handler for TChanSimpleService so it can be
// registered with a thrift.Server.
func NewTChanSimpleServiceServer(handler TChanSimpleService) thrift.TChanServer {
	return &tchanSimpleServiceServer{
		handler,
	}
}

func (s *tchanSimpleServiceServer) Service() string {
	return "SimpleService"
}

func (s *tchanSimpleServiceServer) Methods() []string {
	return []string{
		"Call",
		"Simple",
		"SimpleFuture",
	}
}

func (s *tchanSimpleServiceServer) Handle(ctx thrift.Context, methodName string, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	switch methodName {
	case "Call":
		return s.handleCall(ctx, protocol)
	case "Simple":
		return s.handleSimple(ctx, protocol)
	case "SimpleFuture":
		return s.handleSimpleFuture(ctx, protocol)

	default:
		return false, nil, fmt.Errorf("method %v not found in service %v", methodName, s.Service())
	}
}

func (s *tchanSimpleServiceServer) handleCall(ctx thrift.Context, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	var req SimpleServiceCallArgs
	var res SimpleServiceCallResult

	if err := req.Read(protocol); err != nil {
		return false, nil, err
	}

	r, err :=
		s.handler.Call(ctx, req.Arg)

	if err != nil {
		return false, nil, err
	} else {
		res.Success = r
	}

	return err == nil, &res, nil
}

func (s *tchanSimpleServiceServer) handleSimple(ctx thrift.Context, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	var req SimpleServiceSimpleArgs
	var res SimpleServiceSimpleResult

	if err := req.Read(protocol); err != nil {
		return false, nil, err
	}

	err :=
		s.handler.Simple(ctx)

	if err != nil {
		switch v := err.(type) {
		case *SimpleErr:
			if v == nil {
				return false, nil, fmt.Errorf("Handler for simpleErr returned non-nil error type *SimpleErr but nil value")
			}
			res.SimpleErr = v
		default:
			return false, nil, err
		}
	} else {
	}

	return err == nil, &res, nil
}

func (s *tchanSimpleServiceServer) handleSimpleFuture(ctx thrift.Context, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	var req SimpleServiceSimpleFutureArgs
	var res SimpleServiceSimpleFutureResult

	if err := req.Read(protocol); err != nil {
		return false, nil, err
	}

	err :=
		s.handler.SimpleFuture(ctx)

	if err != nil {
		switch v := err.(type) {
		case *SimpleErr:
			if v == nil {
				return false, nil, fmt.Errorf("Handler for simpleErr returned non-nil error type *SimpleErr but nil value")
			}
			res.SimpleErr = v
		case *NewErr_:
			if v == nil {
				return false, nil, fmt.Errorf("Handler for newErr returned non-nil error type *NewErr_ but nil value")
			}
			res.NewErr_ = v
		default:
			return false, nil, err
		}
	} else {
	}

	return err == nil, &res, nil
}
