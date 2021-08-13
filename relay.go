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

package tchannel

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/uber/tchannel-go/relay"
	"github.com/uber/tchannel-go/typed"
	"go.uber.org/atomic"
)

const (
	// _defaultRelayMaxTombs is the default maximum number of tombs we'll accumulate
	// in a single relayItems.
	_defaultRelayMaxTombs = 3e4
	// _relayTombTTL is the length of time we'll keep a tomb before GC'ing it.
	_relayTombTTL = 3 * time.Second
	// _defaultRelayMaxTimeout is the default max TTL for relayed calls.
	_defaultRelayMaxTimeout = 2 * time.Minute
)

// Error strings.
const (
	_relayErrorNotFound       = "relay-not-found"
	_relayErrorDestConnSlow   = "relay-dest-conn-slow"
	_relayErrorSourceConnSlow = "relay-source-conn-slow"
	_relayArg2ModifyFailed    = "relay-arg2-modify-failed"

	// _relayNoRelease indicates that the relayed frame should not be released immediately, since
	// relayed frames normally end up in a send queue where it is released afterward. However in some
	// cases, such as frames that are fragmented due to being mutated, we need to release the original
	// frame as it won't be relayed.
	_relayNoRelease     = false
	_relayShouldRelease = true
)

// TODO: Replace errFrameNotSent with more specific errors from Receive.
var (
	errRelayMethodFragmented    = NewSystemError(ErrCodeBadRequest, "relay handler cannot receive fragmented calls")
	errFrameNotSent             = NewSystemError(ErrCodeNetwork, "frame was not sent to remote side")
	errBadRelayHost             = NewSystemError(ErrCodeDeclined, "bad relay host implementation")
	errUnknownID                = errors.New("non-callReq for inactive ID")
	errNoNHInArg2               = errors.New("no nh in arg2")
	errFragmentedArg2WithAppend = errors.New("fragmented arg2 not supported for appends")
	errArg2ThriftOnly           = errors.New("cannot inspect or modify arg2 for non-Thrift calls")
)

type relayItem struct {
	remapID         uint32
	tomb            bool
	isOriginator    bool
	call            RelayCall
	destination     *Relayer
	span            Span
	timeout         *relayTimer
	mutatedChecksum Checksum
}

type relayItems struct {
	sync.RWMutex

	logger   Logger
	timeouts *relayTimerPool
	maxTombs uint64
	tombs    uint64
	items    map[uint32]relayItem
}

type frameReceiver interface {
	Receive(f *Frame, fType frameType) (sent bool, failureReason string)
}

func newRelayItems(logger Logger, maxTombs uint64) *relayItems {
	if maxTombs == 0 {
		maxTombs = _defaultRelayMaxTombs
	}
	return &relayItems{
		items:    make(map[uint32]relayItem),
		logger:   logger,
		maxTombs: maxTombs,
	}
}

func (ri *relayItem) reportRelayBytes(fType frameType, frameSize uint16) {
	if fType == requestFrame {
		ri.call.SentBytes(frameSize)
	} else {
		ri.call.ReceivedBytes(frameSize)
	}
}

// Count returns the number of non-tombstone items in the relay.
func (r *relayItems) Count() int {
	r.RLock()
	n := len(r.items) - int(r.tombs)
	r.RUnlock()
	return n
}

// Get checks for a relay item by ID, and will stop the timeout with the
// read lock held (to avoid a race between timeout stop and deletion).
// It returns whether a timeout was stopped, and if the item was found.
func (r *relayItems) Get(id uint32, stopTimeout bool) (_ relayItem, stopped bool, found bool) {
	r.RLock()
	defer r.RUnlock()

	item, ok := r.items[id]
	if !ok {
		return relayItem{}, false /* stopped */, false /* found */
	}

	if !stopTimeout {
		return item, false /* stopped */, true /* found */
	}

	return item, item.timeout.Stop(), true /* found */
}

// Add adds a relay item.
func (r *relayItems) Add(id uint32, item relayItem) {
	r.Lock()
	r.items[id] = item
	r.Unlock()
}

// Delete removes a relayItem completely (without leaving a tombstone). It
// returns the deleted item, along with a bool indicating whether we completed a
// relayed call.
func (r *relayItems) Delete(id uint32) (relayItem, bool) {
	r.Lock()
	item, ok := r.items[id]
	if !ok {
		r.Unlock()
		r.logger.WithFields(LogField{"id", id}).Warn("Attempted to delete non-existent relay item.")
		return item, false
	}
	delete(r.items, id)
	if item.tomb {
		r.tombs--
	}
	r.Unlock()

	item.timeout.Release()
	return item, !item.tomb
}

// Entomb sets the tomb bit on a relayItem and schedules a garbage collection. It
// returns the entombed item, along with a bool indicating whether we completed
// a relayed call.
func (r *relayItems) Entomb(id uint32, deleteAfter time.Duration) (relayItem, bool) {
	r.Lock()
	if r.tombs > r.maxTombs {
		r.Unlock()
		r.logger.WithFields(LogField{"id", id}).Warn("Too many tombstones, deleting relay item immediately.")
		return r.Delete(id)
	}
	item, ok := r.items[id]
	if !ok {
		r.Unlock()
		r.logger.WithFields(LogField{"id", id}).Warn("Can't find relay item to entomb.")
		return item, false
	}
	if item.tomb {
		r.Unlock()
		r.logger.WithFields(LogField{"id", id}).Warn("Re-entombing a tombstone.")
		return item, false
	}
	r.tombs++
	item.tomb = true
	r.items[id] = item
	r.Unlock()

	// TODO: We should be clearing these out in batches, rather than creating
	// individual timers for each item.
	time.AfterFunc(deleteAfter, func() { r.Delete(id) })
	return item, true
}

type frameType int

const (
	requestFrame  frameType = 0
	responseFrame frameType = 1
)

// A Relayer forwards frames.
type Relayer struct {
	relayHost      RelayHost
	maxTimeout     time.Duration
	maxConnTimeout time.Duration

	// localHandlers is the set of service names that are handled by the local
	// channel.
	localHandler map[string]struct{}

	// outbound is the remapping for requests that originated on this
	// connection, and are outbound towards some other connection.
	// It stores remappings for all request frames read on this connection.
	outbound *relayItems

	// inbound is the remapping for requests that originated on some other
	// connection which was directed to this connection.
	// It stores remappings for all response frames read on this connection.
	inbound *relayItems

	// timeouts is the pool of timers used to track call timeouts.
	// It allows timer re-use, while allowing timers to be created and started separately.
	timeouts *relayTimerPool

	peers     *RootPeerList
	conn      *Connection
	relayConn *relay.Conn
	logger    Logger
	pending   atomic.Uint32
}

// NewRelayer constructs a Relayer.
func NewRelayer(ch *Channel, conn *Connection) *Relayer {
	r := &Relayer{
		relayHost:      ch.RelayHost(),
		maxTimeout:     ch.relayMaxTimeout,
		maxConnTimeout: ch.relayMaxConnTimeout,
		localHandler:   ch.relayLocal,
		outbound:       newRelayItems(conn.log.WithFields(LogField{"relayItems", "outbound"}), ch.relayMaxTombs),
		inbound:        newRelayItems(conn.log.WithFields(LogField{"relayItems", "inbound"}), ch.relayMaxTombs),
		peers:          ch.RootPeers(),
		conn:           conn,
		relayConn: &relay.Conn{
			RemoteAddr:        conn.conn.RemoteAddr().String(),
			RemoteProcessName: conn.RemotePeerInfo().ProcessName,
			IsOutbound:        conn.connDirection == outbound,
			Context:           conn.baseContext,
		},
		logger: conn.log,
	}
	r.timeouts = newRelayTimerPool(r.timeoutRelayItem, ch.relayTimerVerify)
	return r
}

// Relay is called for each frame that is read on the connection.
func (r *Relayer) Relay(f *Frame) (shouldRelease bool, _ error) {
	if f.messageType() != messageTypeCallReq {
		err := r.handleNonCallReq(f)
		if err == errUnknownID {
			// This ID may be owned by an outgoing call, so check the outbound
			// message exchange, and if it succeeds, then the frame has been
			// handled successfully.
			if err := r.conn.outbound.forwardPeerFrame(f); err == nil {
				return _relayNoRelease, nil
			}
		}
		return _relayNoRelease, err
	}

	cr, err := newLazyCallReq(f)
	if err != nil {
		return _relayNoRelease, err
	}

	return r.handleCallReq(cr)
}

// Receive receives frames intended for this connection.
// It returns whether the frame was sent and a reason for failure if it failed.
func (r *Relayer) Receive(f *Frame, fType frameType) (sent bool, failureReason string) {
	id := f.Header.ID

	// If we receive a response frame, we expect to find that ID in our outbound.
	// If we receive a request frame, we expect to find that ID in our inbound.
	items := r.receiverItems(fType)
	finished := finishesCall(f)

	// Stop the timeout if the call if finished.
	item, stopped, ok := items.Get(id, finished /* stopTimeout */)
	if !ok {
		r.logger.WithFields(
			LogField{"id", id},
		).Warn("Received a frame without a RelayItem.")
		return false, _relayErrorNotFound
	}
	if item.tomb || (finished && !stopped) {
		// Item has previously timed out, or is in the process of timing out.
		// TODO: metrics for late-arriving frames.
		return true, ""
	}

	// call res frames don't include the OK bit, so we can't wait until the last
	// frame of a relayed RPC to determine if the call succeeded.
	if fType == responseFrame {
		// If we've gotten a response frame, we're the originating relayer and
		// should handle stats.
		if succeeded, failMsg := determinesCallSuccess(f); succeeded {
			item.call.Succeeded()
		} else if len(failMsg) > 0 {
			item.call.Failed(failMsg)
		}
	}
	select {
	case r.conn.sendCh <- f:
	default:
		// Buffer is full, so drop this frame and cancel the call.

		// Since this is typically due to the send buffer being full, get send buffer
		// usage + limit and add that to the log.
		sendBuf, sendBufLimit, sendBufErr := r.conn.sendBufSize()
		now := r.conn.timeNow().UnixNano()
		logFields := []LogField{
			{"id", id},
			{"destConnSendBufferCurrent", sendBuf},
			{"destConnSendBufferLimit", sendBufLimit},
			{"sendChQueued", len(r.conn.sendCh)},
			{"sendChCapacity", cap(r.conn.sendCh)},
			{"lastActivityRead", r.conn.lastActivityRead.Load()},
			{"lastActivityWrite", r.conn.lastActivityRead.Load()},
			{"sinceLastActivityRead", time.Duration(now - r.conn.lastActivityRead.Load()).String()},
			{"sinceLastActivityWrite", time.Duration(now - r.conn.lastActivityWrite.Load()).String()},
		}
		if sendBufErr != nil {
			logFields = append(logFields, LogField{"destConnSendBufferError", sendBufErr.Error()})
		}

		r.logger.WithFields(logFields...).Warn("Dropping call due to slow connection.")

		items := r.receiverItems(fType)

		err := _relayErrorDestConnSlow
		// If we're dealing with a response frame, then the client is slow.
		if fType == responseFrame {
			err = _relayErrorSourceConnSlow
		}

		r.failRelayItem(items, id, err, errFrameNotSent)
		return false, err
	}

	if finished {
		r.finishRelayItem(items, id)
	}

	return true, ""
}

func (r *Relayer) canHandleNewCall() (bool, connectionState) {
	var (
		canHandle bool
		curState  connectionState
	)

	r.conn.withStateRLock(func() error {
		curState = r.conn.state
		canHandle = curState == connectionActive
		if canHandle {
			r.pending.Inc()
		}
		return nil
	})
	return canHandle, curState
}

func (r *Relayer) getDestination(f *lazyCallReq, call RelayCall) (*Connection, bool, error) {
	if _, _, ok := r.outbound.Get(f.Header.ID, false /* stopTimeout */); ok {
		r.logger.WithFields(
			LogField{"id", f.Header.ID},
			LogField{"source", string(f.Caller())},
			LogField{"dest", string(f.Service())},
			LogField{"method", string(f.Method())},
		).Warn("Received duplicate callReq.")
		call.Failed(ErrCodeProtocol.relayMetricsKey())
		// TODO: this is a protocol error, kill the connection.
		return nil, false, errors.New("callReq with already active ID")
	}

	// Get the destination
	peer, ok := call.Destination()
	if !ok {
		call.Failed("relay-bad-relay-host")
		r.conn.SendSystemError(f.Header.ID, f.Span(), errBadRelayHost)
		return nil, false, errBadRelayHost
	}

	remoteConn, err := peer.getConnectionRelay(f.TTL(), r.maxConnTimeout)
	if err != nil {
		r.logger.WithFields(
			ErrField(err),
			LogField{"source", string(f.Caller())},
			LogField{"dest", string(f.Service())},
			LogField{"method", string(f.Method())},
			LogField{"selectedPeer", peer},
		).Warn("Failed to connect to relay host.")
		call.Failed("relay-connection-failed")
		r.conn.SendSystemError(f.Header.ID, f.Span(), NewWrappedSystemError(ErrCodeNetwork, err))
		return nil, false, nil
	}

	return remoteConn, true, nil
}

func (r *Relayer) handleCallReq(f *lazyCallReq) (shouldRelease bool, _ error) {
	if handled := r.handleLocalCallReq(f); handled {
		return _relayNoRelease, nil
	}

	call, err := r.relayHost.Start(f, r.relayConn)
	if err != nil {
		// If we have a RateLimitDropError we record the statistic, but
		// we *don't* send an error frame back to the client.
		if _, silentlyDrop := err.(relay.RateLimitDropError); silentlyDrop {
			if call != nil {
				call.Failed("relay-dropped")
				call.End()
			}
			return _relayNoRelease, nil
		}
		if _, ok := err.(SystemError); !ok {
			err = NewSystemError(ErrCodeDeclined, err.Error())
		}
		if call != nil {
			call.Failed(GetSystemErrorCode(err).relayMetricsKey())
			call.End()
		}
		r.conn.SendSystemError(f.Header.ID, f.Span(), err)

		// If the RelayHost returns a protocol error, close the connection.
		if GetSystemErrorCode(err) == ErrCodeProtocol {
			return _relayNoRelease, r.conn.close(LogField{"reason", "RelayHost returned protocol error"})
		}
		return _relayNoRelease, nil
	}

	// Check that the current connection is in a valid state to handle a new call.
	if canHandle, state := r.canHandleNewCall(); !canHandle {
		call.Failed("relay-client-conn-inactive")
		call.End()
		err := errConnNotActive{"incoming", state}
		r.conn.SendSystemError(f.Header.ID, f.Span(), NewWrappedSystemError(ErrCodeDeclined, err))
		return _relayNoRelease, err
	}

	// Get a remote connection and check whether it can handle this call.
	remoteConn, ok, err := r.getDestination(f, call)
	if err == nil && ok {
		if canHandle, state := remoteConn.relay.canHandleNewCall(); !canHandle {
			err = NewWrappedSystemError(ErrCodeNetwork, errConnNotActive{"selected remote", state})
			call.Failed("relay-remote-inactive")
			r.conn.SendSystemError(f.Header.ID, f.Span(), NewWrappedSystemError(ErrCodeDeclined, err))
		}
	}
	if err != nil || !ok {
		// Failed to get a remote connection, or the connection is not in the right
		// state to handle this call. Since we already incremented pending on
		// the current relay, we need to decrement it.
		r.decrementPending()
		call.End()
		return _relayNoRelease, err
	}

	origID := f.Header.ID
	destinationID := remoteConn.NextMessageID()
	ttl := f.TTL()
	if ttl > r.maxTimeout {
		ttl = r.maxTimeout
		f.SetTTL(r.maxTimeout)
	}
	span := f.Span()

	var mutatedChecksum Checksum
	if len(f.arg2Appends) > 0 {
		mutatedChecksum = f.checksumType.New()
	}

	// The remote side of the relay doesn't need to track stats or call state.
	remoteConn.relay.addRelayItem(false /* isOriginator */, destinationID, f.Header.ID, r, ttl, span, call, nil /* mutatedChecksum */)
	relayToDest := r.addRelayItem(true /* isOriginator */, f.Header.ID, destinationID, remoteConn.relay, ttl, span, call, mutatedChecksum)

	f.Header.ID = destinationID

	// If we have appends, the size of the frame to be relayed will change, potentially going
	// over the max frame size. Do a fragmenting send which is slightly more expensive but
	// will handle fragmenting if it is needed.
	if len(f.arg2Appends) > 0 {
		if err := r.fragmentingSend(call, f, relayToDest, origID); err != nil && true {
			r.failRelayItem(r.outbound, origID, _relayArg2ModifyFailed, err)
			r.logger.WithFields(
				LogField{"id", origID},
				LogField{"err", err.Error()},
				LogField{"caller", string(f.Caller())},
				LogField{"dest", string(f.Service())},
				LogField{"method", string(f.Method())},
			).Warn("Failed to send call with modified arg2.")
		}

		// fragmentingSend always sends new frames in place of the old frame so we must
		// release it separately
		return _relayShouldRelease, nil
	}

	call.SentBytes(f.Frame.Header.FrameSize())
	sent, failure := relayToDest.destination.Receive(f.Frame, requestFrame)
	if !sent {
		r.failRelayItem(r.outbound, origID, failure, errFrameNotSent)
		return _relayNoRelease, nil
	}
	return _relayNoRelease, nil
}

// Handle all frames except messageTypeCallReq.
func (r *Relayer) handleNonCallReq(f *Frame) error {
	frameType := frameTypeFor(f)
	finished := finishesCall(f)

	// If we read a request frame, we need to use the outbound map to decide
	// the destination. Otherwise, we use the inbound map.
	items := r.outbound
	if frameType == responseFrame {
		items = r.inbound
	}

	// Stop the timeout if the call if finished.
	item, stopped, ok := items.Get(f.Header.ID, finished /* stopTimeout */)
	if !ok {
		return errUnknownID
	}
	if item.tomb || (finished && !stopped) {
		// Item has previously timed out, or is in the process of timing out.
		// TODO: metrics for late-arriving frames.
		return nil
	}

	switch f.messageType() {
	case messageTypeCallRes:
		// Invoke call.CallResponse() if we get a valid call response frame.
		cr, err := newLazyCallRes(f)
		if err == nil {
			item.call.CallResponse(cr)
		} else {
			r.logger.WithFields(
				ErrField(err),
				LogField{"id", f.Header.ID},
			).Error("Malformed callRes frame.")
		}
	case messageTypeCallReqContinue:
		// Recalculate and update the checksum for this frame if it has non-nil item.mutatedChecksum
		// (meaning the call was mutated) and it is a callReqContinue frame.
		if item.mutatedChecksum != nil {
			r.updateMutatedCallReqContinueChecksum(f, item.mutatedChecksum)
		}
	}

	// Track sent/received bytes. We don't do this before we check
	// for timeouts, since this should only be called before call.End().
	item.reportRelayBytes(frameType, f.Header.FrameSize())

	originalID := f.Header.ID
	f.Header.ID = item.remapID

	sent, failure := item.destination.Receive(f, frameType)
	if !sent {
		r.failRelayItem(items, originalID, failure, errFrameNotSent)
		return nil
	}

	if finished {
		r.finishRelayItem(items, originalID)
	}
	return nil
}

// addRelayItem adds a relay item to either outbound or inbound.
func (r *Relayer) addRelayItem(isOriginator bool, id, remapID uint32, destination *Relayer, ttl time.Duration, span Span, call RelayCall, mutatedChecksum Checksum) relayItem {
	item := relayItem{
		isOriginator:    isOriginator,
		call:            call,
		remapID:         remapID,
		destination:     destination,
		span:            span,
		mutatedChecksum: mutatedChecksum,
	}

	items := r.inbound
	if isOriginator {
		items = r.outbound
	}
	item.timeout = r.timeouts.Get()
	items.Add(id, item)
	item.timeout.Start(ttl, items, id, isOriginator)
	return item
}

func (r *Relayer) timeoutRelayItem(items *relayItems, id uint32, isOriginator bool) {
	item, ok := items.Entomb(id, _relayTombTTL)
	if !ok {
		return
	}
	if isOriginator {
		r.conn.SendSystemError(id, item.span, ErrTimeout)
		item.call.Failed("timeout")
		item.call.End()
	}

	r.decrementPending()
}

// failRelayItem tombs the relay item so that future frames for this call are not
// forwarded. We keep the relay item tombed, rather than delete it to ensure that
// future frames do not cause error logs.
func (r *Relayer) failRelayItem(items *relayItems, id uint32, reason string, err error) {
	// Stop the timeout, so we either fail it here, or in the timeout goroutine but not both.
	item, stopped, found := items.Get(id, true /* stopTimeout */)
	if !found {
		items.logger.WithFields(LogField{"id", id}).Warn("Attempted to fail non-existent relay item.")
		return
	}
	if !stopped {
		return
	}

	// Entomb it so that we don't get unknown exchange errors on further frames
	// for this call.
	item, ok := items.Entomb(id, _relayTombTTL)
	if !ok {
		return
	}
	if item.isOriginator {
		// If the client is too slow, then there's no point sending an error frame.
		if reason != _relayErrorSourceConnSlow {
			r.conn.SendSystemError(id, item.span, fmt.Errorf("%v: %v", reason, err))
		}
		item.call.Failed(reason)
		item.call.End()
	}

	r.decrementPending()
}

func (r *Relayer) finishRelayItem(items *relayItems, id uint32) {
	item, ok := items.Delete(id)
	if !ok {
		return
	}
	if item.isOriginator {
		item.call.End()
		if item.mutatedChecksum != nil {
			item.mutatedChecksum.Release()
		}
	}
	r.decrementPending()
}

func (r *Relayer) decrementPending() {
	r.pending.Dec()
	r.conn.checkExchanges()
}

func (r *Relayer) canClose() bool {
	if r == nil {
		return true
	}
	return r.countPending() == 0
}

func (r *Relayer) countPending() uint32 {
	return r.pending.Load()
}

func (r *Relayer) receiverItems(fType frameType) *relayItems {
	if fType == requestFrame {
		return r.inbound
	}
	return r.outbound
}

func (r *Relayer) handleLocalCallReq(cr *lazyCallReq) (shouldRelease bool) {
	// Check whether this is a service we want to handle locally.
	if _, ok := r.localHandler[string(cr.Service())]; !ok {
		return _relayNoRelease
	}

	f := cr.Frame

	// We can only handle non-fragmented calls in the relay channel.
	// This is a simplification to avoid back references from a mex to a
	// relayItem so that the relayItem is cleared when the call completes.
	if cr.HasMoreFragments() {
		r.logger.WithFields(
			LogField{"id", cr.Header.ID},
			LogField{"source", string(cr.Caller())},
			LogField{"dest", string(cr.Service())},
			LogField{"method", string(cr.Method())},
		).Error("Received fragmented callReq intended for local relay channel, can only handle unfragmented calls.")
		r.conn.SendSystemError(f.Header.ID, cr.Span(), errRelayMethodFragmented)
		return _relayShouldRelease
	}

	if release := r.conn.handleFrameNoRelay(f); release {
		r.conn.opts.FramePool.Release(f)
	}
	return _relayShouldRelease
}

func (r *Relayer) fragmentingSend(call RelayCall, f *lazyCallReq, relayToDest relayItem, origID uint32) error {
	if f.isArg2Fragmented {
		return errFragmentedArg2WithAppend
	}
	if !bytes.Equal(f.as, _tchanThriftValueBytes) {
		return fmt.Errorf("%v: got %s", errArg2ThriftOnly, f.as)
	}

	cs := relayToDest.mutatedChecksum

	// TODO(echung): should we pool the writers?
	fragWriter := newFragmentingWriter(
		r.logger, r.newFragmentSender(relayToDest.destination, f, origID, call),
		cs,
	)

	arg2Writer, err := fragWriter.ArgWriter(false /* last */)
	if err != nil {
		return fmt.Errorf("get arg2 writer: %v", err)
	}

	if err := writeArg2WithAppends(arg2Writer, f.arg2(), f.arg2Appends); err != nil {
		return fmt.Errorf("write arg2: %v", err)
	}
	if err := arg2Writer.Close(); err != nil {
		return fmt.Errorf("close arg2 writer: %v", err)
	}

	if err := NewArgWriter(fragWriter.ArgWriter(true /* last */)).Write(f.arg3()); err != nil {
		return errors.New("arg3 write failed")
	}

	return nil
}

func (r *Relayer) updateMutatedCallReqContinueChecksum(f *Frame, cs Checksum) {
	rbuf := typed.NewReadBuffer(f.SizedPayload())
	rbuf.SkipBytes(1) // flags
	rbuf.SkipBytes(1) // checksum type: this should match the checksum type of the callReq frame

	checksumRef := typed.BytesRef(rbuf.ReadBytes(cs.Size()))

	// We only support non-fragmented arg2 for mutated calls, so by the time we hit callReqContinue both
	// arg1 and arg2 must already have been read. As the call would be finished when we've read all of
	// arg3, it isn't necessary to separately track its completion.
	//
	// In theory we could have a frame with 0-length arg3, which can happen if a manual flush occurred
	// after writing 0 bytes for arg3. This is handled correctly by
	// 1) reading n=0 (nArg3)
	// 2) reading 0 bytes from the rbuf
	// 3) updating the checksum with the current running checksum
	//
	// Additionally, if the checksum type results in a 0-length checksum, the .Update() would
	// become a copy between empty slices, which correctly becomes a noop.
	// TODO(cinchurge): include a test for len(arg3)==0 in the unit tests
	n := rbuf.ReadUint16()
	cs.Add(rbuf.ReadBytes(int(n)))
	checksumRef.Update(cs.Sum())
}

func writeArg2WithAppends(w io.WriteCloser, arg2 []byte, appends []relay.KeyVal) (err error) {
	if len(arg2) < 2 {
		return errNoNHInArg2
	}

	writer := typed.NewWriter(w)

	// nh:2 is the first two bytes of arg2, which should always be present
	nh := binary.BigEndian.Uint16(arg2[:2]) + uint16(len(appends))
	writer.WriteUint16(nh)

	// arg2[2:] is the existing sequence of key/val pairs, which we can just copy
	// over verbatim
	if len(arg2) > 2 {
		writer.WriteBytes(arg2[2:])
	}

	// append new key/val pairs to end of arg2
	for _, kv := range appends {
		writer.WriteLen16Bytes(kv.Key)
		writer.WriteLen16Bytes(kv.Val)
	}

	return writer.Err()
}

func frameTypeFor(f *Frame) frameType {
	switch t := f.Header.messageType; t {
	case messageTypeCallRes, messageTypeCallResContinue, messageTypeError, messageTypePingRes:
		return responseFrame
	case messageTypeCallReq, messageTypeCallReqContinue, messageTypePingReq:
		return requestFrame
	default:
		panic(fmt.Sprintf("unsupported frame type: %v", t))
	}
}

func determinesCallSuccess(f *Frame) (succeeded bool, failMsg string) {
	switch f.messageType() {
	case messageTypeError:
		msg := newLazyError(f).Code().MetricsKey()
		return false, msg
	case messageTypeCallRes:
		if isCallResOK(f) {
			return true, ""
		}
		return false, "application-error"
	default:
		return false, ""
	}
}

func validateRelayMaxTimeout(d time.Duration, logger Logger) time.Duration {
	maxMillis := d / time.Millisecond
	if maxMillis > 0 && maxMillis <= math.MaxUint32 {
		return d
	}
	if d == 0 {
		return _defaultRelayMaxTimeout
	}
	logger.WithFields(
		LogField{"configuredMaxTimeout", d},
		LogField{"defaultMaxTimeout", _defaultRelayMaxTimeout},
	).Warn("Configured RelayMaxTimeout is invalid, using default instead.")
	return _defaultRelayMaxTimeout
}

type sentBytesReporter interface {
	SentBytes(size uint16)
}

type relayFragmentSender struct {
	callReq            *lazyCallReq
	framePool          FramePool
	frameReceiver      frameReceiver
	failRelayItemFunc  func(items *relayItems, id uint32, failure string, err error)
	outboundRelayItems *relayItems
	origID             uint32
	sentReporter       sentBytesReporter
}

func (r *Relayer) newFragmentSender(dstRelay frameReceiver, cr *lazyCallReq, origID uint32, sentReporter sentBytesReporter) *relayFragmentSender {
	// TODO(cinchurge): pool fragment senders
	return &relayFragmentSender{
		callReq:            cr,
		framePool:          r.conn.opts.FramePool,
		frameReceiver:      dstRelay,
		failRelayItemFunc:  r.failRelayItem,
		outboundRelayItems: r.outbound,
		origID:             origID,
		sentReporter:       sentReporter,
	}
}

func (rfs *relayFragmentSender) newFragment(initial bool, checksum Checksum) (*writableFragment, error) {
	frame := rfs.framePool.Get()
	frame.Header.ID = rfs.callReq.Header.ID
	if initial {
		frame.Header.messageType = messageTypeCallReq
	} else {
		frame.Header.messageType = messageTypeCallReqContinue
	}

	contents := typed.NewWriteBuffer(frame.Payload[:])

	// flags:1
	// Flags MUST be copied over from the callReq frame to all new fragments since if there are more
	// fragments to follow the callReq, the destination needs to know about this or those frames will
	// be dropped from the call
	flagsRef := contents.DeferByte()
	flagsRef.Update(rfs.callReq.Payload[_flagsIndex])

	if initial {
		// Copy all data before the checksum for the initial frame
		contents.WriteBytes(rfs.callReq.Payload[_flagsIndex+1 : rfs.callReq.checksumTypeOffset])
	}

	// checksumType:1
	contents.WriteSingleByte(byte(checksum.TypeCode()))

	// checksum: checksum.Size()
	checksumRef := contents.DeferBytes(checksum.Size())

	if initial {
		// arg1~1: write arg1 to the initial frame
		contents.WriteUint16(uint16(len(rfs.callReq.method)))
		contents.WriteBytes(rfs.callReq.method)
		checksum.Add(rfs.callReq.method)
	}

	// TODO(cinchurge): pool writableFragment
	return &writableFragment{
		flagsRef:    flagsRef,
		checksumRef: checksumRef,
		// checksum will be released by the relayer when the call is finished
		checksum: &noReleaseChecksum{Checksum: checksum},
		contents: contents,
		frame:    frame,
	}, contents.Err()
}

func (rfs *relayFragmentSender) flushFragment(wf *writableFragment) error {
	wf.frame.Header.SetPayloadSize(uint16(wf.contents.BytesWritten()))
	rfs.sentReporter.SentBytes(wf.frame.Header.FrameSize())

	sent, failure := rfs.frameReceiver.Receive(wf.frame, requestFrame)
	if !sent {
		rfs.failRelayItemFunc(rfs.outboundRelayItems, rfs.origID, failure, errFrameNotSent)
		return nil
	}
	return nil
}

func (rfs *relayFragmentSender) doneSending() {}
