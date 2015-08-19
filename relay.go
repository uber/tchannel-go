package tchannel

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type relayItem struct {
	remapID     uint32
	destination *Relay
}

type ServiceHosts struct {
	sync.RWMutex

	r        *rand.Rand
	randLock sync.Mutex
	peers    map[string][]string
}

func NewServiceHosts() *ServiceHosts {
	return &ServiceHosts{
		r:     rand.New(rand.NewSource(rand.Int63())),
		peers: make(map[string][]string),
	}
}

func (h *ServiceHosts) Register(service, hostPort string) {
	h.Lock()
	h.peers[service] = append(h.peers[service], hostPort)
	h.Unlock()
}

// GetHostPort returns a random host:port to use for the given service
func (h *ServiceHosts) GetHostPort(service string) string {
	h.RLock()
	hostPorts := h.peers[service]
	h.RUnlock()
	if len(hostPorts) == 0 {
		return ""
	}

	h.randLock.Lock()
	randHost := h.r.Intn(len(hostPorts))
	h.randLock.Unlock()

	return hostPorts[randHost]
}

// Relay contains all relay specific information.
type Relay struct {
	sync.RWMutex
	connections  map[uint32]relayItem
	serviceHosts *ServiceHosts

	// Immutable
	ch   *Channel
	conn *Connection
}

// NewRelay creates a relay.
func NewRelay(ch *Channel, conn *Connection) *Relay {
	return &Relay{
		ch:           ch,
		serviceHosts: ch.serviceHosts,
		conn:         conn,
		connections:  make(map[uint32]relayItem),
	}
}

// Receive receives a frame from another relay, and sends it to the underlying connection.
func (r *Relay) Receive(frame *Frame) {
	r.conn.log.Debugf("Relay received frame %v", frame.Header)
	r.conn.sendCh <- frame
}

// AddRelay adds a relay that will remap IDs from id to remapID
// and then send the frame to the given destination relay.
func (r *Relay) AddRelay(id, remapID uint32, destination *Relay) relayItem {
	newRelay := relayItem{
		remapID:     remapID,
		destination: destination,
	}

	r.Lock()
	r.connections[id] = newRelay
	r.Unlock()
	return newRelay
}

// RelayFrame relays the given frame.
// TODO(prashant): Remove the id from the map once that sequence is complete.
func (r *Relay) RelayFrame(frame *Frame) {
	r.conn.log.Debugf("RelayFrame %v", frame.Header)
	if frame.MessageType() != messageTypeCallReq {
		r.RLock()
		relay, ok := r.connections[frame.Header.ID]
		r.RUnlock()
		if !ok {
			panic(fmt.Sprintf("got non-call req for inactive ID: %v", frame.Header.ID))
		}
		frame.Header.ID = relay.remapID
		relay.destination.Receive(frame)
		return
	}

	if _, ok := r.connections[frame.Header.ID]; ok {
		panic(fmt.Sprintf("callReq with already active ID: %b", frame.Header.ID))
	}

	// Get the destination
	svc := string(frame.Service())

	// Get a host port for it
	hostPort := r.serviceHosts.GetHostPort(svc)

	// Get a connection to that host port
	peer := r.ch.Peers().GetOrAdd(hostPort)

	// What context do we want?
	ctx, _ := NewContext(5 * time.Second)
	c, err := peer.GetConnection(ctx)
	if err != nil {
		panic(err)
	}

	destinationID := c.NextMessageID()
	c.relay.AddRelay(destinationID, frame.Header.ID, r)
	relayToDest := r.AddRelay(frame.Header.ID, destinationID, c.relay)

	frame.Header.ID = destinationID
	relayToDest.destination.Receive(frame)
}
