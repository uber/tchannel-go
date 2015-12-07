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

package hyperbahn

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/hyperbahn/gen-go/hyperbahn"
	"github.com/uber/tchannel-go/hyperbahn/mocks"
)

// testServicePeerStrings is a static list of peers used in the tests. These
// match what are returned by createTestServicePeers
var testServicePeerStrings []string = []string{
	"127.0.0.1:8000",
	"127.0.0.2:8001",
	"1.2.4.8:21150",
	"10.254.254.1:21151",
}

// testServicePeers is a static list of hyperbahn.ServicePeers used in the
// tests. They match the testServicePeerStrings.
var testServicePeers []*hyperbahn.ServicePeer = func() []*hyperbahn.ServicePeer {
	ips := []int32{
		0x7f000001, // 127.0.0.1
		0x7f000002, // 127.0.0.2
		0x01020408, // 1.2.4.8
		0x0afefe01, // 10.254.254.1
	}
	peers := []hyperbahn.ServicePeer{
		{&hyperbahn.IpAddress{&ips[0]}, 8000},
		{&hyperbahn.IpAddress{&ips[1]}, 8001},
		{&hyperbahn.IpAddress{&ips[2]}, 21150},
		{&hyperbahn.IpAddress{&ips[3]}, 21151},
	}
	peerList := []*hyperbahn.ServicePeer{
		&peers[0],
		&peers[1],
		&peers[2],
		&peers[3],
	}
	return peerList
}()

type DiscoverTests struct {
	suite.Suite
	channel *tchannel.Channel
	client  *Client
	mock    *mocks.TChanHyperbahn
}

func (s *DiscoverTests) SetupTest() {
	var err error

	s.channel, err = tchannel.NewChannel("test", nil)
	s.NoError(err)

	err = s.channel.ListenAndServe("127.0.0.1:0")
	s.NoError(err)

	s.client, err = NewClient(s.channel, Configuration{
		InitialNodes: []string{s.channel.PeerInfo().HostPort},
	}, nil)
	s.NoError(err)

	s.mock = &mocks.TChanHyperbahn{}
	s.client.hyperbahnClient = s.mock
}

func (s *DiscoverTests) TearDownTest() {
	s.channel.Close()
	s.client.Close()
}

func (s *DiscoverTests) TestDiscoverSuccess() {
	s.mock.On("Discover", mock.Anything, mock.Anything).Return(&hyperbahn.DiscoveryResult_{Peers: testServicePeers}, nil)

	peers, err := s.client.Discover("test")
	s.NoError(err)
	s.Equal(testServicePeerStrings, peers)
}

func (s *DiscoverTests) TestDiscoverFails() {
	s.mock.On("Discover", mock.Anything, mock.Anything).Return(nil, errors.New("testing error"))

	peers, err := s.client.Discover("test")
	s.Error(err)
	s.Nil(peers)
}

func TestDiscoverTests(t *testing.T) {
	suite.Run(t, new(DiscoverTests))
}
