// @generated Code generated by thrift-gen. Do not modify.

// Package hyperbahn is generated code used to make or handle TChannel calls using Thrift.
package hyperbahn

import (
	"fmt"

	athrift "github.com/apache/thrift/lib/go/thrift"
	"github.com/uber/tchannel-go/thrift"
)

// Interfaces for the service and client for the services defined in the IDL.

// TChanHyperbahn is the interface that defines the server handler and client interface.
type TChanHyperbahn interface {
	Discover(ctx thrift.Context, query *DiscoveryQuery) (*DiscoveryResult_, error)
}

// Implementation of a client and service handler.

type tchanHyperbahnClient struct {
	thriftService string
	client        thrift.TChanClient
}

func NewTChanHyperbahnInheritedClient(thriftService string, client thrift.TChanClient) *tchanHyperbahnClient {
	return &tchanHyperbahnClient{
		thriftService,
		client,
	}
}

// NewTChanHyperbahnClient creates a client that can be used to make remote calls.
func NewTChanHyperbahnClient(client thrift.TChanClient) TChanHyperbahn {
	return NewTChanHyperbahnInheritedClient("Hyperbahn", client)
}

func (c *tchanHyperbahnClient) Discover(ctx thrift.Context, query *DiscoveryQuery) (*DiscoveryResult_, error) {
	var resp HyperbahnDiscoverResult
	args := HyperbahnDiscoverArgs{
		Query: query,
	}
	success, err := c.client.Call(ctx, c.thriftService, "discover", &args, &resp)
	if err == nil && !success {
		switch {
		case resp.NoPeersAvailable != nil:
			err = resp.NoPeersAvailable
		case resp.InvalidServiceName != nil:
			err = resp.InvalidServiceName
		default:
			err = fmt.Errorf("received no result or unknown exception for discover")
		}
	}

	return resp.GetSuccess(), err
}

type tchanHyperbahnServer struct {
	handler TChanHyperbahn
}

// NewTChanHyperbahnServer wraps a handler for TChanHyperbahn so it can be
// registered with a thrift.Server.
func NewTChanHyperbahnServer(handler TChanHyperbahn) thrift.TChanServer {
	return &tchanHyperbahnServer{
		handler,
	}
}

func (s *tchanHyperbahnServer) Service() string {
	return "Hyperbahn"
}

func (s *tchanHyperbahnServer) Methods() []string {
	return []string{
		"discover",
	}
}

func (s *tchanHyperbahnServer) Handle(ctx thrift.Context, methodName string, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	switch methodName {
	case "discover":
		return s.handleDiscover(ctx, protocol)

	default:
		return false, nil, fmt.Errorf("method %v not found in service %v", methodName, s.Service())
	}
}

func (s *tchanHyperbahnServer) handleDiscover(ctx thrift.Context, protocol athrift.TProtocol) (bool, athrift.TStruct, error) {
	var req HyperbahnDiscoverArgs
	var res HyperbahnDiscoverResult

	if err := req.Read(protocol); err != nil {
		return false, nil, err
	}

	r, err :=
		s.handler.Discover(ctx, req.Query)

	if err != nil {
		switch v := err.(type) {
		case *NoPeersAvailable:
			if v == nil {
				return false, nil, fmt.Errorf("Handler for noPeersAvailable returned non-nil error type *NoPeersAvailable but nil value")
			}
			res.NoPeersAvailable = v
		case *InvalidServiceName:
			if v == nil {
				return false, nil, fmt.Errorf("Handler for invalidServiceName returned non-nil error type *InvalidServiceName but nil value")
			}
			res.InvalidServiceName = v
		default:
			return false, nil, err
		}
	} else {
		res.Success = r
	}

	return err == nil, &res, nil
}
