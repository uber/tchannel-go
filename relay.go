package tchannel

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// RelayHosts allows external wrappers to inject peer selection logic for
// relaying.
type RelayHosts interface {
	// Get returns the host:port of the best peer for the group.
	Get(service string) string
	// TODO: add MarkFailed and MarkOK to for feedback loop into peer selection.
}

type relayItem struct {
	remapID     uint32
	destination *Relayer
}

type frameType int

const (
	requestFrame  frameType = 0
	responseFrame frameType = 1
)

// A Relayer forwards frames.
type Relayer struct {
	sync.RWMutex

	metrics StatsReporter
	hosts   RelayHosts

	// outbound is the remapping for requests that originated on this
	// connection, and are outbound towards some other connection.
	// It stores remappings for all request frames read on this connection.
	outbound map[uint32]relayItem

	// inbound is the remapping for requests that originated on some other
	// connection which was directed to this connection.
	// It stores remappings for all response frames read on this connection.
	inbound map[uint32]relayItem

	peers   *PeerList
	conn    *Connection
	logger  Logger
	pending uint32
}

// NewRelayer constructs a Relayer.
func NewRelayer(ch *Channel, conn *Connection) *Relayer {
	return &Relayer{
		metrics:  conn.statsReporter,
		hosts:    ch.relayHosts,
		outbound: make(map[uint32]relayItem),
		inbound:  make(map[uint32]relayItem),
		peers:    ch.Peers(),
		conn:     conn,
		logger:   ch.Logger(),
	}
}

// Hosts returns the RelayHosts guiding peer selection.
func (r *Relayer) Hosts() RelayHosts {
	return r.hosts
}

// Relay is called for each frame that is read on the connection.
func (r *Relayer) Relay(f *Frame) error {
	if f.messageType() != messageTypeCallReq {
		return r.handleNonCallReq(f)
	}
	return r.handleCallReq(f)
}

// Receive receives frames intended for this connection.
func (r *Relayer) Receive(f *Frame, fType frameType) {
	{
		// TODO: Since this block is only checking for safety, we should not
		// enable this in production builds.

		// If we receive a response frame, we expect to find that ID in our outbound.
		// If we receive a request frame, we expect to find that ID in our inbound.
		items := r.receiverItems(fType)

		r.RLock()
		_, ok := items[f.Header.ID]
		r.RUnlock()
		if !ok {
			r.logger.WithFields(
				LogField{"ID", f.Header.ID},
			).Warn("Received a frame without a RelayItem.")
		}
	}

	r.conn.sendCh <- f
	if finishesCall(f) {
		items := r.receiverItems(fType)
		r.removeRelayItem(items, f.Header.ID)
	}
}

func (r *Relayer) handleCallReq(f *Frame) error {
	r.RLock()
	_, ok := r.outbound[f.Header.ID]
	r.RUnlock()

	if ok {
		r.logger.WithFields(LogField{"id", f.Header.ID}).Warn("received duplicate callReq")
		return errors.New("callReq with already active ID")
	}

	// Get the destination
	svc := f.Service()
	hostPort := r.hosts.Get(svc)
	if hostPort == "" {
		return errors.New("no available peer for group")
	}
	peer := r.peers.GetOrAdd(hostPort)

	ctx, cancel := NewContext(time.Second)
	defer cancel()

	remoteConn, err := peer.GetConnection(ctx)
	if err != nil {
		r.logger.WithFields(
			ErrField(err),
			LogField{"hostPort", hostPort},
		).Warn("Failed to connect to relay host.")
		// TODO : return an error frame.
		return nil
	}

	// TODO: Is there a race for adding the same ID twice?
	destinationID := remoteConn.NextMessageID()
	remoteConn.relay.addRelayItem(false /* isOriginator */, destinationID, f.Header.ID, r)
	r.metrics.IncCounter("relay", nil, 1)
	relayToDest := r.addRelayItem(true /* isOriginator */, f.Header.ID, destinationID, remoteConn.relay)

	f.Header.ID = destinationID
	relayToDest.destination.Receive(f, requestFrame)
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

	r.RLock()
	item, ok := items[f.Header.ID]
	r.RUnlock()
	if !ok {
		return errors.New("non-callReq for inactive ID")
	}
	originalID := f.Header.ID
	f.Header.ID = item.remapID
	item.destination.Receive(f, frameType)

	if finishesCall(f) {
		r.removeRelayItem(items, originalID)
	}
	return nil
}

// addRelayItem adds a relay item to either outbound or inbound.
func (r *Relayer) addRelayItem(isOriginator bool, id, remapID uint32, destination *Relayer) relayItem {
	item := relayItem{
		remapID:     remapID,
		destination: destination,
	}
	r.incPending()

	items := r.inbound
	if isOriginator {
		items = r.outbound
	}

	r.Lock()
	items[id] = item
	r.Unlock()
	return item
}

func (r *Relayer) removeRelayItem(items map[uint32]relayItem, id uint32) {
	r.Lock()
	delete(items, id)
	r.Unlock()
	r.decPending()
	r.conn.checkExchanges()
}

func (r *Relayer) canClose() bool {
	if r == nil {
		return true
	}
	return r.countPending() == 0
}

func (r *Relayer) incPending() {
	atomic.AddUint32(&r.pending, 1)
}

func (r *Relayer) decPending() {
	atomic.AddUint32(&r.pending, ^uint32(0))
}

func (r *Relayer) countPending() uint32 {
	return atomic.LoadUint32(&r.pending)
}

func (r *Relayer) receiverItems(fType frameType) map[uint32]relayItem {
	if fType == requestFrame {
		return r.inbound
	}
	return r.outbound
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

// finishesCall checks whether this frame is the last one we should expect for
// this RPC req-res.
func finishesCall(f *Frame) bool {
	switch f.messageType() {
	case messageTypeCallRes, messageTypeCallResContinue:
		flags := f.Payload[_flagsIndex]
		return flags&hasMoreFragmentsFlag == 0
	// TODO: errors should also terminate an RPC.
	default:
		return false
	}
}
