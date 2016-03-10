package tchannel

import (
	"errors"
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

// A Relayer forwards frames.
type Relayer struct {
	sync.RWMutex

	metrics StatsReporter
	hosts   RelayHosts
	items   map[uint32]relayItem
	peers   *PeerList
	conn    *Connection
	logger  Logger
	pending uint32
}

// NewRelayer constructs a Relayer.
func NewRelayer(ch *Channel, conn *Connection) *Relayer {
	return &Relayer{
		metrics: conn.statsReporter,
		hosts:   ch.relayHosts,
		items:   make(map[uint32]relayItem),
		peers:   ch.Peers(),
		conn:    conn,
		logger:  ch.Logger(),
	}
}

// Hosts returns the RelayHosts guiding peer selection.
func (r *Relayer) Hosts() RelayHosts {
	return r.hosts
}

// Relay forwards a frame.
func (r *Relayer) Relay(f *Frame) error {
	if f.messageType() != messageTypeCallReq {
		return r.handleNonCallReq(f)
	}
	return r.handleCallReq(f)
}

// Receive accepts a relayed frame.
func (r *Relayer) Receive(f *Frame) {
	r.RLock()
	_, ok := r.items[f.Header.ID]
	r.RUnlock()

	r.conn.sendCh <- f

	if ok && finishesCall(f) {
		r.removeRelayItem(f.Header.ID)
	}
}

func (r *Relayer) handleCallReq(f *Frame) error {
	r.RLock()
	if _, ok := r.items[f.Header.ID]; ok {
		r.RUnlock()
		return errors.New("callReq with already active ID")
	}
	r.RUnlock()

	// Get the destination
	svc := f.Service()
	hostPort := r.hosts.Get(svc)
	if hostPort == "" {
		return errors.New("no available peer for group")
	}
	peer := r.peers.GetOrAdd(hostPort)

	ctx, cancel := NewContext(time.Second)
	defer cancel()

	c, err := peer.GetConnection(ctx)
	if err != nil {
		r.logger.WithFields(
			ErrField(err),
			LogField{"hostPort", hostPort},
		).Warn("Failed to connect to relay host.")
		// TODO : return an error frame.
		return nil
	}

	destinationID := c.NextMessageID()
	c.relay.addRelayItem(destinationID, f.Header.ID, r)
	r.metrics.IncCounter("relay", nil, 1)
	relayToDest := r.addRelayItem(f.Header.ID, destinationID, c.relay)

	f.Header.ID = destinationID
	relayToDest.destination.Receive(f)
	return nil
}

// Handle all frames except messageTypeCallReq.
func (r *Relayer) handleNonCallReq(f *Frame) error {
	r.RLock()
	item, ok := r.items[f.Header.ID]
	r.RUnlock()
	if !ok {
		return errors.New("non-callReq for inactive ID")
	}
	f.Header.ID = item.remapID
	item.destination.Receive(f)

	if finishesCall(f) {
		r.removeRelayItem(f.Header.ID)
	}
	return nil
}

func (r *Relayer) addRelayItem(id, remapID uint32, destination *Relayer) relayItem {
	item := relayItem{
		remapID:     remapID,
		destination: destination,
	}
	r.incPending()
	r.Lock()
	r.items[id] = item
	r.Unlock()
	return item
}

func (r *Relayer) removeRelayItem(id uint32) {
	r.Lock()
	delete(r.items, id)
	r.Unlock()
	r.decPending()
	r.conn.checkExchanges()
}

func (r *Relayer) canClose() bool {
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
