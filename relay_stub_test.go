package tchannel_test

import (
	"math/rand"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

type SimpleRelayHosts struct {
	sync.RWMutex
	r     *rand.Rand
	peers map[string][]string
}

func NewSimpleRelayHosts(peers map[string][]string) *SimpleRelayHosts {
	// Use a known seed for repeatable tests.
	return &SimpleRelayHosts{
		r:     rand.New(rand.NewSource(1)),
		peers: peers,
	}
}

func (rh *SimpleRelayHosts) Get(group string) string {
	rh.RLock()
	defer rh.RUnlock()

	available, ok := rh.peers[group]
	if !ok || len(available) == 0 {
		return ""
	}
	i := rh.r.Intn(len(available))
	return available[i]
}

func (rh *SimpleRelayHosts) Add(group, hostPort string) {
	rh.Lock()
	rh.peers[group] = append(rh.peers[group], hostPort)
	rh.Unlock()
}

func TestSimpleRelayHosts(t *testing.T) {
	hosts := map[string][]string{
		"foo":        {"1.1.1.1:1234", "1.1.1.1:1235"},
		"foo-canary": {},
	}
	rh := NewSimpleRelayHosts(hosts)
	assert.Equal(t, "", rh.Get("foo-canary"), "Expected no canary hosts.")
	assert.Equal(t, "1.1.1.1:1235", rh.Get("foo"), "Unexpected peer chosen.")
}
