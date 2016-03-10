package tchannel

import (
	"errors"
	"sync"
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
	// TODO: remove relay items on timeout.
	if f.messageType() != messageTypeCallReq {
		r.RLock()
		item, ok := r.items[f.Header.ID]
		r.RUnlock()
		if !ok {
			return errors.New("non-callReq for inactive ID")
		}
		f.Header.ID = item.remapID
		item.destination.Receive(f)
		if f.isLast() {
			r.removeRelayItem(f.Header.ID)
		}
		return nil
	}

	// Handle messageTypeCallReq
	if _, ok := r.items[f.Header.ID]; ok {
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
	if f.isLast() {
		r.removeRelayItem(f.Header.ID)
	}
	return nil
}

// Receive accepts a relayed frame.
func (r *Relayer) Receive(f *Frame) {
	r.conn.sendCh <- f
}

func (r *Relayer) addRelayItem(id, remapID uint32, destination *Relayer) relayItem {
	item := relayItem{
		remapID:     remapID,
		destination: destination,
	}
	r.Lock()
	r.items[id] = item
	r.Unlock()
	return item
}

func (r *Relayer) removeRelayItem(id uint32) {
	r.Lock()
	delete(r.items, id)
	r.Unlock()
}
