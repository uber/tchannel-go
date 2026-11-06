package tchannel_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	. "github.com/uber/tchannel-go"
	"github.com/uber/tchannel-go/raw"
	"github.com/uber/tchannel-go/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

// admissionRecorder is an InboundAdmission that records what it is called with and how often its release is called.
type admissionRecorder struct {
	mu       sync.Mutex
	reqs     []InboundAdmissionRequest
	releases int
	// doubles counts the releases of a call that was already released.
	doubles int
	// releasedCh gets a value for every release, so the tests wait for a release instead of sleeping.
	releasedCh chan struct{}
	// decide returns the error for the n-th call, starting at 0, and whether a release is returned with it.
	decide func(n int) (err error, withRelease bool)
}

func (r *admissionRecorder) admission(req InboundAdmissionRequest) (func(), error) {
	r.mu.Lock()
	n := len(r.reqs)
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()

	err, withRelease := error(nil), true
	if r.decide != nil {
		err, withRelease = r.decide(n)
	}
	if !withRelease {
		return nil, err
	}
	var released bool
	return func() {
		r.mu.Lock()
		if released {
			r.doubles++
		}
		released = true
		r.releases++
		r.mu.Unlock()
		r.releasedCh <- struct{}{}
	}, err
}

// waitReleased blocks until n releases happened. The timeout only bounds a failing test, a passing one never waits for it.
func (r *admissionRecorder) waitReleased(t testing.TB, n int) {
	for i := 0; i < n; i++ {
		select {
		case <-r.releasedCh:
		case <-time.After(testutils.Timeout(5 * time.Second)):
			require.FailNow(t, "release not called", "%d of %d releases", i, n)
		}
	}
}

func (r *admissionRecorder) doubleReleases() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.doubles
}

func (r *admissionRecorder) released() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.releases
}

func (r *admissionRecorder) called() []InboundAdmissionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]InboundAdmissionRequest(nil), r.reqs...)
}

func newAdmissionRecorder(decide func(n int) (error, bool)) *admissionRecorder {
	return &admissionRecorder{decide: decide, releasedCh: make(chan struct{}, 1000)}
}

// countingHandler counts the calls that reach it and holds each one until it is released.
type countingHandler struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	proceed chan struct{}
}

func newCountingHandler() *countingHandler {
	return &countingHandler{started: make(chan struct{}, 100), proceed: make(chan struct{})}
}

func (h *countingHandler) Handle(ctx context.Context, args *raw.Args) (*raw.Res, error) {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	h.started <- struct{}{}
	<-h.proceed
	return &raw.Res{Arg2: args.Arg2, Arg3: args.Arg3}, nil
}

func (h *countingHandler) OnError(ctx context.Context, err error) {}

func (h *countingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

func admissionOpts(r *admissionRecorder) *testutils.ChannelOpts {
	// A relay forwards the frames of a call without handling the call request itself, so the tests use direct calls.
	opts := testutils.NewOpts().NoRelay()
	opts.DefaultConnectionOptions.InboundAdmission = r.admission
	return opts
}

func callEcho(t testing.TB, ts *testutils.TestServer, method string) error {
	return callEchoFrom(ts, ts.NewClient(nil), method)
}

// callEchoFrom calls the server from the given client. Unlike ts.NewClient it is safe to call concurrently.
func callEchoFrom(ts *testutils.TestServer, client *Channel, method string) error {
	ctx, cancel := NewContext(testutils.Timeout(time.Second))
	defer cancel()
	_, _, _, err := raw.Call(ctx, client, ts.HostPort(), ts.ServiceName(), method, []byte("arg2"), []byte("arg3"))
	return err
}

func TestInboundAdmissionAccept(t *testing.T) {
	rec := newAdmissionRecorder(nil)
	testutils.WithTestServer(t, admissionOpts(rec), func(t testing.TB, ts *testutils.TestServer) {
		h := newCountingHandler()
		ts.Register(raw.Wrap(h), "echo")

		client := ts.NewClient(nil)
		done := make(chan error, 1)
		go func() { done <- callEchoFrom(ts, client, "echo") }()
		<-h.started

		// The admission is called with the service, the method and the caller of the call, and is not released while the handler runs.
		assert.Equal(t, []InboundAdmissionRequest{{
			Service: ts.ServiceName(),
			Method:  "echo",
			Caller:  client.ServiceName(),
		}}, rec.called())
		assert.Equal(t, 0, rec.released(), "an accepted call is not released while its handler runs")

		close(h.proceed)
		require.NoError(t, <-done)
		rec.waitReleased(t, 1)
	})
	// The channel is closed and drained by now, so a second release would have been recorded.
	assert.Equal(t, 1, rec.released(), "released once the handler has returned, and only once")
	assert.Zero(t, rec.doubleReleases())
}

func TestInboundAdmissionRejectWithBusy(t *testing.T) {
	rec := newAdmissionRecorder(func(int) (error, bool) { return ErrServerBusy, true })
	testutils.WithTestServer(t, admissionOpts(rec), func(t testing.TB, ts *testutils.TestServer) {
		h := newCountingHandler()
		ts.Register(raw.Wrap(h), "echo")

		err := callEcho(t, ts, "echo")
		require.Error(t, err)
		sysErr, ok := err.(SystemError)
		require.True(t, ok, "the caller gets a system error: %v", err)
		assert.Equal(t, ErrCodeBusy, sysErr.Code())

		assert.Zero(t, h.count(), "a rejected call does not reach the handler")
		rec.waitReleased(t, 1)
	})
}

func TestInboundAdmissionRejectWithOtherError(t *testing.T) {
	rec := newAdmissionRecorder(func(int) (error, bool) { return errors.New("not today"), true })
	testutils.WithTestServer(t, admissionOpts(rec), func(t testing.TB, ts *testutils.TestServer) {
		h := newCountingHandler()
		ts.Register(raw.Wrap(h), "echo")

		err := callEcho(t, ts, "echo")
		require.Error(t, err)
		sysErr, ok := err.(SystemError)
		require.True(t, ok, "an error that is not a system error is wrapped: %v", err)
		assert.NotEqual(t, ErrCodeBusy, sysErr.Code())
		assert.Zero(t, h.count())
		rec.waitReleased(t, 1)
	})
}

func TestInboundAdmissionWithoutRelease(t *testing.T) {
	// A nil release is allowed, for an accepted call and for a rejected one.
	rec := newAdmissionRecorder(func(n int) (error, bool) {
		if n == 0 {
			return nil, false
		}
		return ErrServerBusy, false
	})
	testutils.WithTestServer(t, admissionOpts(rec), func(t testing.TB, ts *testutils.TestServer) {
		h := newCountingHandler()
		close(h.proceed)
		ts.Register(raw.Wrap(h), "echo")

		require.NoError(t, callEcho(t, ts, "echo"))
		assert.Equal(t, 1, h.count())

		err := callEcho(t, ts, "echo")
		require.Error(t, err)
		assert.Equal(t, 1, h.count(), "the second call is rejected")
		assert.Zero(t, rec.released())
	})
}

func TestInboundAdmissionMixedCalls(t *testing.T) {
	const calls = 20
	// Every other call is rejected: every call is released exactly once, whether it is accepted or rejected.
	rec := newAdmissionRecorder(func(n int) (error, bool) {
		if n%2 == 1 {
			return ErrServerBusy, true
		}
		return nil, true
	})
	testutils.WithTestServer(t, admissionOpts(rec), func(t testing.TB, ts *testutils.TestServer) {
		h := newCountingHandler()
		close(h.proceed)
		ts.Register(raw.Wrap(h), "echo")

		// ts.NewClient is not safe for concurrent use, so the clients are created before the calls start.
		clients := make([]*Channel, calls)
		for i := range clients {
			clients[i] = ts.NewClient(nil)
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		var failed int
		for _, client := range clients {
			wg.Add(1)
			go func(client *Channel) {
				defer wg.Done()
				if err := callEchoFrom(ts, client, "echo"); err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
				}
			}(client)
		}
		wg.Wait()

		rec.waitReleased(t, calls)
		assert.Equal(t, calls/2, failed)
		assert.Equal(t, calls/2, h.count())
	})
	assert.Equal(t, calls, rec.released(), "every call is released")
	assert.Zero(t, rec.doubleReleases(), "no call is released twice")
}

func TestInboundAdmissionNotSet(t *testing.T) {
	testutils.WithTestServer(t, testutils.NewOpts().NoRelay(), func(t testing.TB, ts *testutils.TestServer) {
		h := newCountingHandler()
		close(h.proceed)
		ts.Register(raw.Wrap(h), "echo")
		require.NoError(t, callEcho(t, ts, "echo"))
		assert.Equal(t, 1, h.count())
	})
}

func TestInboundAdmissionClosedConnection(t *testing.T) {
	// A call on a connection that is closing is answered before the admission is asked.
	rec := newAdmissionRecorder(nil)
	testutils.WithTestServer(t, admissionOpts(rec), func(t testing.TB, ts *testutils.TestServer) {
		h := newCountingHandler()
		ts.Register(raw.Wrap(h), "echo")
		client := ts.NewClient(nil)

		// Hold a call in flight, so the connection stays open while the server closes.
		done := make(chan error, 1)
		go func() { done <- callEchoFrom(ts, client, "echo") }()
		<-h.started

		ts.Server().Close()

		// The call goes over the connection that is closing, as the client still has it.
		err := callEchoFrom(ts, client, "echo")
		assert.Equal(t, ErrChannelClosed, err)
		require.Len(t, rec.called(), 1, "the admission is only asked for the first call")

		close(h.proceed)
		require.NoError(t, <-done)
		rec.waitReleased(t, 1)
	})
}

func TestInboundAdmissionRejectedMultiFragmentCall(t *testing.T) {
	// The follow-up fragments of a rejected call have no exchange. They are dropped without the info log of an unknown exchange.
	logs := &syncBuffer{}
	rec := newAdmissionRecorder(func(int) (error, bool) { return ErrServerBusy, true })
	opts := admissionOpts(rec)
	opts.Logger = NewLogger(logs)
	testutils.WithTestServer(t, opts, func(t testing.TB, ts *testutils.TestServer) {
		h := newCountingHandler()
		ts.Register(raw.Wrap(h), "echo")

		ctx, cancel := NewContext(testutils.Timeout(time.Second))
		defer cancel()
		arg3 := testutils.RandBytes(10 * MaxFramePayloadSize)
		_, _, _, err := raw.Call(ctx, ts.NewClient(nil), ts.HostPort(), ts.ServiceName(), "echo", []byte("arg2"), arg3)
		require.Error(t, err)
		sysErr, ok := err.(SystemError)
		require.True(t, ok, "the caller gets a system error: %v", err)
		assert.Equal(t, ErrCodeBusy, sysErr.Code())

		assert.Zero(t, h.count())
		assert.Len(t, rec.called(), 1, "the admission is asked once for the call, not for its fragments")
		rec.waitReleased(t, 1)
	})
	// The fragments reached the read loop and were dropped with a debug log only.
	assert.Contains(t, logs.String(), "[D] Dropping messageTypeCallReqContinue")
	assert.NotContains(t, logs.String(), "unknown message exchange")
}

// syncBuffer is a buffer that is safe for concurrent use, for the logger of a channel.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
