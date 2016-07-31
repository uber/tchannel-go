// Copyright (c) 2016 Uber Technologies, Inc.
//
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

package trace

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/uber/tchannel-go"

	"github.com/crossdock/crossdock-go"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/utils"
	"golang.org/x/net/context"
)

// Different parameter keys and values used by the system
const (
	BehaviorName = "trace"
)

// Behavior is an implementation of "trace behavior", that verifies
// that a tracing context and baggage are properly propagated through
// two level of servers.
type Behavior struct {
	ServerPort    string
	Tracer        opentracing.Tracer
	ServiceToHost func(string) string
	ch            *tchannel.Channel
	thriftCall    DownstreamCall
	jsonCall      DownstreamCall
}

// DownstreamCall is used in a few other structs here
type DownstreamCall func(ctx context.Context, target *Downstream) (*Response, error)

// Register function adds JSON and Thrift handlers to the server channel ch
func (b *Behavior) Register(ch *tchannel.Channel) {
	b.registerThriftServer(ch)
	b.registerJSONServer(ch)
}

// Run executes the trace behavior
func (b *Behavior) Run(t crossdock.T) {
	sampled, err := strconv.ParseBool(t.Param(sampledParam))
	if err != nil {
		t.Fatalf("Malformed param %s: %s", sampledParam, err)
		return
	}
	baggage := randomBaggage()

	level1 := &Request{
		ServerRole: RoleS1,
	}
	server1 := t.Param(server1NameParam)

	level2 := &Downstream{
		ServiceName: t.Param(server2NameParam),
		ServerRole:  RoleS2,
		HostPort: fmt.Sprintf("%s:%s",
			b.serviceToHost(t.Param(server2NameParam)),
			b.ServerPort,
		),
		Encoding: t.Param(server2EncodingParam),
	}
	level1.Downstream = level2

	level3 := &Downstream{
		ServiceName: t.Param(server3NameParam),
		ServerRole:  RoleS3,
		HostPort: fmt.Sprintf("%s:%s",
			b.serviceToHost(t.Param(server3NameParam)),
			b.ServerPort,
		),
		Encoding: t.Param(server3EncodingParam),
	}
	level2.Downstream = level3

	resp, err := b.startTrace(level1, sampled, baggage)
	if err != nil {
		t.Errorf(err.Error())
		return
	}

	traceID := resp.Span.TraceID
	if traceID == "" {
		t.Errorf("Trace ID is empty in S1(%s)", server1)
		return
	}

	success := validateTrace(t, level1.Downstream, resp, server1, 1, traceID, sampled, baggage)
	if success {
		t.Successf("trace checks out")
		log.Println("PASS")
	}
}

func (b *Behavior) serviceToHost(service string) string {
	if b.ServiceToHost != nil {
		return b.ServiceToHost(service)
	}
	return service
}

func (b *Behavior) startTrace(req *Request, sampled bool, baggage string) (*Response, error) {
	span := b.Tracer.StartSpan(req.ServerRole)
	if sampled {
		ext.SamplingPriority.Set(span, 1)
	}
	span.Context().SetBaggageItem(BaggageKey, baggage)
	defer span.Finish()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	ctx = opentracing.ContextWithSpan(ctx, span)

	return b.prepareResponse(ctx, req.Downstream)
}

func validateTrace(
	t crossdock.T,
	target *Downstream,
	resp *Response,
	service string,
	level int,
	traceID string,
	sampled bool,
	baggage string,
) bool {
	success := true
	if traceID != resp.Span.TraceID {
		t.Errorf("Trace ID mismatch in S%d(%s): expected %s, received %s",
			level, service, traceID, resp.Span.TraceID)
		success = false
	}
	if baggage != resp.Span.Baggage {
		t.Errorf("Baggage mismatch in S%d(%s): expected %s, received %s",
			level, service, baggage, resp.Span.Baggage)
		success = false
	}
	if sampled != resp.Span.Sampled {
		t.Errorf("Sampled mismatch in S%d(%s): expected %t, received %t",
			level, service, sampled, resp.Span.Sampled)
		success = false
	}
	if target != nil {
		if resp.Downstream == nil {
			t.Errorf("Missing downstream in S%d(%s)", level, service)
			success = false
		} else {
			success = validateTrace(t, target.Downstream, resp.Downstream,
				target.HostPort, level+1, traceID, sampled, baggage) && success
		}
	} else if resp.Downstream != nil {
		t.Errorf("Unexpected downstream in S%d(%s)", level, service)
		success = false
	}
	return success
}

func randomBaggage() string {
	r := utils.NewRand(time.Now().UnixNano())
	n := uint64(r.Int63())
	return fmt.Sprintf("%x", n)
}

func (b *Behavior) prepareResponse(ctx context.Context, reqDwn *Downstream) (*Response, error) {
	observedSpan, err := observeSpan(ctx)
	if err != nil {
		return nil, err
	}

	resp := &Response{
		Span: observedSpan,
	}

	if reqDwn != nil {
		downstreamResp, err := b.callDownstream(ctx, reqDwn)
		if err != nil {
			return nil, err
		}
		resp.Downstream = downstreamResp
	}

	return resp, nil
}

func (b *Behavior) callDownstream(ctx context.Context, downstream *Downstream) (*Response, error) {
	switch tchannel.Format(downstream.Encoding) {
	case tchannel.JSON:
		return b.jsonCall(ctx, downstream)
	case tchannel.Thrift:
		return b.thriftCall(ctx, downstream)
	default:
		return nil, errUnsupportedEncoding
	}
}

func observeSpan(ctx context.Context) (*ObservedSpan, error) {
	span := opentracing.SpanFromContext(ctx)
	if span == nil {
		return nil, errNoSpanObserved
	}
	sc, ok := span.Context().(*jaeger.SpanContext)
	if !ok {
		return &ObservedSpan{}, nil
	}
	observedSpan := &ObservedSpan{
		TraceID: fmt.Sprintf("%x", sc.TraceID()),
		Sampled: sc.IsSampled(),
		Baggage: sc.BaggageItem(BaggageKey),
	}
	return observedSpan, nil
}
