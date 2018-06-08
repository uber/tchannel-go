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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/testutils"
	"github.com/uber/tchannel-go/thrift/gen-go/meta"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThriftIDL(t *testing.T) {
	withMetaSetup(t, func(ctx Context, c tchanMeta, server *Server) {
		_, err := c.ThriftIDL(ctx)
		assert.Error(t, err, "Health endpoint failed")
		assert.Contains(t, err.Error(), "unimplemented")
	})
}

func TestVersionInfo(t *testing.T) {
	withMetaSetup(t, func(ctx Context, c tchanMeta, server *Server) {
		ret, err := c.VersionInfo(ctx)
		if assert.NoError(t, err, "VersionInfo endpoint failed") {
			expected := &meta.VersionInfo{
				Language:        "go",
				LanguageVersion: strings.TrimPrefix(runtime.Version(), "go"),
				Version:         tchannel.VersionInfo,
			}
			assert.Equal(t, expected, ret, "Unexpected version info")
		}
	})
}

func TestHealth(t *testing.T) {
	tests := []struct {
		msg           string
		healthFunc    HealthFunc
		healthReqFunc HealthRequestFunc
		req           *meta.HealthRequest
		wantOK        bool
		wantMessage   *string
	}{
		{
			msg:    "default health func",
			wantOK: true,
		},
		{
			msg: "healthFunc returning unhealthy, no message",
			healthFunc: func(Context) (bool, string) {
				return false, ""
			},
			wantOK: false,
		},
		{
			msg: "healthFunc returning healthy, with message",
			healthFunc: func(Context) (bool, string) {
				return true, "ok"
			},
			wantOK:      true,
			wantMessage: stringPtr("ok"),
		},
		{
			msg: "healthReqFunc returning unhealthy for traffic, default check",
			healthReqFunc: func(_ Context, r HealthRequest) (bool, string) {
				return r.Type != Traffic, ""
			},
			wantOK: true,
		},
		{
			msg: "healthReqFunc returning unhealthy for traffic, traffic check",
			healthReqFunc: func(_ Context, r HealthRequest) (bool, string) {
				return r.Type != Traffic, ""
			},
			req:    &meta.HealthRequest{Type: meta.HealthRequestTypePtr(meta.HealthRequestType_TRAFFIC)},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			withMetaSetup(t, func(ctx Context, c tchanMeta, server *Server) {
				if tt.healthFunc != nil {
					server.RegisterHealthHandler(tt.healthFunc)
				}
				if tt.healthReqFunc != nil {
					server.RegisterHealthRequestHandler(tt.healthReqFunc)
				}

				req := tt.req
				if req == nil {
					req = &meta.HealthRequest{}
				}

				ret, err := c.Health(ctx, req)
				require.NoError(t, err, "Health endpoint failed")

				assert.Equal(t, tt.wantOK, ret.Ok, "Health status mismatch")
				assert.Equal(t, tt.wantMessage, ret.Message, "Health message mismatch")
			})
		})
	}
}

func TestMetaReqToReq(t *testing.T) {
	tests := []struct {
		msg  string
		r    *meta.HealthRequest
		want HealthRequest
	}{
		{
			msg:  "nil",
			r:    nil,
			want: HealthRequest{},
		},
		{
			msg:  "default",
			r:    &meta.HealthRequest{},
			want: HealthRequest{},
		},
		{
			msg: "explcit process check",
			r: &meta.HealthRequest{
				Type: meta.HealthRequestTypePtr(meta.HealthRequestType_PROCESS),
			},
			want: HealthRequest{
				Type: Process,
			},
		},
		{
			msg: "explcit traffic check",
			r: &meta.HealthRequest{
				Type: meta.HealthRequestTypePtr(meta.HealthRequestType_TRAFFIC),
			},
			want: HealthRequest{
				Type: Traffic,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			assert.Equal(t, tt.want, metaReqToReq(tt.r))
		})
	}
}

func withMetaSetup(t *testing.T, f func(ctx Context, c tchanMeta, server *Server)) {
	ctx, cancel := NewContext(time.Second * 10)
	defer cancel()

	// Start server
	tchan, server := setupMetaServer(t)
	defer tchan.Close()

	// Get client1
	c := getMetaClient(t, tchan.PeerInfo().HostPort)
	f(ctx, c, server)
}

func setupMetaServer(t *testing.T) (*tchannel.Channel, *Server) {
	tchan := testutils.NewServer(t, testutils.NewOpts().SetServiceName("meta"))
	server := NewServer(tchan)
	return tchan, server
}

func getMetaClient(t *testing.T, dst string) tchanMeta {
	tchan := testutils.NewClient(t, nil)
	tchan.Peers().Add(dst)
	thriftClient := NewClient(tchan, "meta", nil)
	return newTChanMetaClient(thriftClient)
}

func stringPtr(s string) *string {
	return &s
}
