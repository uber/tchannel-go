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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/uber/tchannel-go/relay"

	"github.com/uber-go/atomic"
)

// _maxRelayTombs is the maximum number of tombs we'll accumulate in a single
// relayItems.
const _maxRelayTombs = 1e4

// _relayTombTTL is the length of time we'll keep a tomb before GC'ing it.
const _relayTombTTL = time.Second

var (
	errRelayMethodFragmented = NewSystemError(ErrCodeBadRequest, "relay handler cannot receive fragmented calls")
	errUnknownID             = errors.New("non-callReq for inactive ID")
)

// relayConn implements the relay.Connection interface.
type relayConn Connection

type relayItem struct {
	*time.Timer

	stats       relay.CallStats
	remapID     uint32
	destination *Relayer
	tomb        bool
	local       bool
	span        Span
}

type relayItems struct {
	sync.RWMutex

	logger Logger
	tombs  uint64
	items  map[uint32]relayItem
}

func newRelayItems(logger Logger) *relayItems {
	return &relayItems{
		items:  make(map[uint32]relayItem),
		logger: logger,
	}
}

// Count returns the number of non-tombstone items in the relay.
func (r *relayItems) Count() int {
	r.RLock()
	n := len(r.items) - int(r.tombs)
	r.RUnlock()
	return n
}

// Get checks for a relay item by ID, returning the item and a bool indicating
// whether the item was found.
func (r *relayItems) Get(id uint32) (relayItem, bool) {
	r.RLock()
	item, ok := r.items[id]
	r.RUnlock()

	return item, ok
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

	item.Stop()
	return item, !item.tomb
}

// Entomb sets the tomb bit on a relayItem and schedules a garbage collection. It
// returns the entombed item, along with a bool indicating whether we completed
// a relayed call.
func (r *relayItems) Entomb(id uint32, deleteAfter time.Duration) (relayItem, bool) {
	r.Lock()
	if r.tombs > _maxRelayTombs {
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
	stats relay.Stats
	hosts relay.Hosts

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

	peers   *PeerList
	conn    *Connection
	logger  Logger
	pending atomic.Uint32
}

// NewRelayer constructs a Relayer.
func NewRelayer(ch *Channel, conn *Connection) *Relayer {
	return &Relayer{
		stats:        ch.relayStats,
		hosts:        ch.RelayHosts(),
		localHandler: ch.relayLocal,
		outbound:     newRelayItems(ch.Logger().WithFields(LogField{"relay", "outbound"})),
		inbound:      newRelayItems(ch.Logger().WithFields(LogField{"relay", "inbound"})),
		peers:        ch.Peers(),
		conn:         conn,
		logger:       conn.log,
	}
}

// Hosts returns the RelayHosts guiding peer selection.
func (r *Relayer) Hosts() relay.Hosts {
	return r.hosts
}

// Relay is called for each frame that is read on the connection.
func (r *Relayer) Relay(f *Frame) error {
	if f.messageType() != messageTypeCallReq {
		err := r.handleNonCallReq(f)
		if err == errUnknownID {
			// This ID may be owned by an outgoing call, so check the outbound
			// message exchange, and if it succeeds, then the frame has been
			// handled successfully.
			if err := r.conn.outbound.forwardPeerFrame(f); err == nil {
				return nil
			}
		}
		return err
	}
	return r.handleCallReq(newLazyCallReq(f))
}

// Receive receives frames intended for this connection.
func (r *Relayer) Receive(f *Frame, fType frameType) {
	id := f.Header.ID

	// If we receive a response frame, we expect to find that ID in our outbound.
	// If we receive a request frame, we expect to find that ID in our inbound.
	items := r.receiverItems(fType)

	item, ok := items.Get(id)
	if !ok {
		r.logger.WithFields(
			LogField{"id", id},
		).Warn("Received a frame without a RelayItem.")
		return
	}

	// call res frames don't include the OK bit, so we can't wait until the last
	// frame of a relayed RPC to determine if the call succeeded.
	if fType == responseFrame {
		// If we've gotten a response frame, we're the originating relayer and
		// should handle stats.
		if succeeded, failMsg := determinesCallSuccess(f); succeeded {
			item.stats.Succeeded()
		} else if len(failMsg) > 0 {
			item.stats.Failed(failMsg)
		}
	}

	// When we write the frame to sendCh, we lose ownership of the frame, and it
	// may be released to the frame pool at any point.
	finished := finishesCall(f)

	// TODO: Add some sort of timeout here to avoid blocking forever on a
	// stalled connection.
	r.conn.sendCh <- f

	if finished {
		items := r.receiverItems(fType)
		r.finishRelayItem(items, id)
	}
}

func (r *Relayer) canHandleNewCall() bool {
	var canHandle bool
	r.conn.withStateRLock(func() error {
		canHandle = r.conn.state == connectionActive
		if canHandle {
			r.pending.Inc()
		}
		return nil
	})
	return canHandle
}

func (r *Relayer) getDestination(f lazyCallReq, cs relay.CallStats) (*Connection, bool, error) {
	if _, ok := r.outbound.Get(f.Header.ID); ok {
		r.logger.WithFields(LogField{"id", f.Header.ID}).Warn("received duplicate callReq")
		cs.Failed("relay-" + ErrCodeProtocol.MetricsKey())
		// TODO: this is a protocol error, kill the connection.
		return nil, false, errors.New("callReq with already active ID")
	}

	// Get the destination
	selectedPeer, err := r.hosts.Get(f, (*relayConn)(r.conn))
	cs.SetPeer(selectedPeer)
	if err == nil && selectedPeer.HostPort == "" {
		err = errInvalidPeerForGroup(f.Service())
	}
	if err != nil {
		if _, ok := err.(SystemError); !ok {
			err = NewSystemError(ErrCodeDeclined, err.Error())
		}
		cs.Failed("relay-" + GetSystemErrorCode(err).MetricsKey())
		r.conn.SendSystemError(f.Header.ID, f.Span(), err)
		return nil, false, nil
	}

	peer := r.peers.GetOrAdd(selectedPeer.HostPort)

	// TODO: Should connections use the call timeout? Or a separate timeout?
	remoteConn, err := peer.getConnectionRelay(f.TTL())
	if err != nil {
		r.logger.WithFields(
			ErrField(err),
			LogField{"selectedPeer", selectedPeer},
		).Warn("Failed to connect to relay host.")
		cs.Failed("relay-connection-failed")
		r.conn.SendSystemError(f.Header.ID, f.Span(), NewWrappedSystemError(ErrCodeNetwork, err))
		return nil, false, nil
	}

	return remoteConn, true, nil
}

func (r *Relayer) handleCallReq(f lazyCallReq) error {
	if handled := r.handleLocalCallReq(f); handled {
		return nil
	}

	callStats := r.stats.Begin(f)
	if !r.canHandleNewCall() {
		callStats.Failed("relay-channel-closed")
		callStats.End()
		return ErrChannelClosed
	}

	// Get a remote connection and check whether it can handle this call.
	remoteConn, ok, err := r.getDestination(f, callStats)
	if err == nil && ok {
		if !remoteConn.relay.canHandleNewCall() {
			err = NewSystemError(ErrCodeNetwork, "selected closed connection, retry")
			callStats.Failed("relay-connection-closed")
		}
	}
	if err != nil || !ok {
		// Failed to get a remote connection, or the connection is not in the right
		// state to handle this call. Since we already incremented pending on
		// the current relay, we need to decrement it.
		r.decrementPending()
		callStats.End()
		return err
	}

	destinationID := remoteConn.NextMessageID()
	ttl := f.TTL()
	span := f.Span()
	// The remote side of the relay doesn't need to track stats.
	remoteConn.relay.addRelayItem(false /* isOriginator */, destinationID, f.Header.ID, r, ttl, span, nil)
	relayToDest := r.addRelayItem(true /* isOriginator */, f.Header.ID, destinationID, remoteConn.relay, ttl, span, callStats)

	f.Header.ID = destinationID
	relayToDest.destination.Receive(f.Frame, requestFrame)
	return nil
}

// Handle all frames except messageTypeCallReq.
func (r *Relayer) handleNonCallReq(f *Frame) error {
	frameType := frameTypeFor(f)

	// If we read a request frame, we need to use the outbound map to decide
	// the destination. Otherwise, we use the inbound map.
	items := r.outbound
	if frameType == responseFrame {
		items = r.inbound
	}

	item, ok := items.Get(f.Header.ID)
	if !ok {
		return errUnknownID
	}
	if item.tomb {
		// Call timed out, ignore this frame. (We've already handled stats.)
		// TODO: metrics for late-arriving frames.
		return nil
	}
	originalID := f.Header.ID
	f.Header.ID = item.remapID

	// Once we call Receive on the frame, we lose ownership of the frame.
	finished := finishesCall(f)

	item.destination.Receive(f, frameType)

	if finished {
		r.finishRelayItem(items, originalID)
	}
	return nil
}

// addRelayItem adds a relay item to either outbound or inbound.
func (r *Relayer) addRelayItem(isOriginator bool, id, remapID uint32, destination *Relayer, ttl time.Duration, span Span, cs relay.CallStats) relayItem {
	item := relayItem{
		stats:       cs,
		remapID:     remapID,
		destination: destination,
		span:        span,
	}

	items := r.inbound
	if isOriginator {
		items = r.outbound
	}
	item.Timer = time.AfterFunc(ttl, func() { r.timeoutRelayItem(isOriginator, items, id) })
	items.Add(id, item)
	return item
}

func (r *Relayer) timeoutRelayItem(isOriginator bool, items *relayItems, id uint32) {
	item, ok := items.Entomb(id, _relayTombTTL)
	if !ok {
		return
	}
	if isOriginator {
		r.conn.SendSystemError(id, item.span, ErrTimeout)
		item.stats.Failed("timeout")
		item.stats.End()
	}

	r.decrementPending()
}

func (r *Relayer) finishRelayItem(items *relayItems, id uint32) {
	item, ok := items.Delete(id)
	if !ok {
		return
	}
	if item.stats != nil {
		item.stats.End()
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

func (r *Relayer) handleLocalCallReq(cr lazyCallReq) bool {
	// Check whether this is a service we want to handle locally.
	if _, ok := r.localHandler[string(cr.Service())]; !ok {
		return false
	}

	f := cr.Frame

	// We can only handle non-fragmented calls in the relay channel.
	// This is a simplification to avoid back references from a mex to a
	// relayItem so that the relayItem is cleared when the call completes.
	if cr.HasMoreFragments() {
		r.logger.WithFields(
			LogField{"id", cr.Header.ID},
			LogField{"service", string(cr.Service())},
			LogField{"method", string(cr.Method())},
		).Error("Received fragmented callReq intended for local relay channel, can only handle unfragmented calls.")
		r.conn.SendSystemError(f.Header.ID, cr.Span(), errRelayMethodFragmented)
		return true
	}

	if release := r.conn.handleFrameNoRelay(f); release {
		r.conn.framePool.Release(f)
	}
	return true
}

func (r *relayConn) RemoteProcessPrefixMatches() []bool {
	return (*Connection)(r).remoteProcessPrefixMatches
}

func (r *relayConn) RemoteHostPort() string {
	return (*Connection)(r).RemotePeerInfo().HostPort
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

func errInvalidPeerForGroup(group []byte) error {
	return NewSystemError(ErrCodeDeclined, "invalid peer for %q", group)
}

func determinesCallSuccess(f *Frame) (succeeded bool, failMsg string) {
	switch f.messageType() {
	case messageTypeError:
		msg := newLazyError(f).Code().MetricsKey()
		return false, msg
	case messageTypeCallRes:
		if newLazyCallRes(f).OK() {
			return true, ""
		}
		return false, "application-error"
	default:
		return false, ""
	}
}
