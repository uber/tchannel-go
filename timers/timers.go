// Package timers provides a timer facility similar to the standard library's.
// It's designed to be faster, but less accurate.
package timers

import (
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/andres-erbsen/clock"
	"github.com/uber-go/atomic"
)

const cacheline = 64

const (
	// State machine for timers.
	stateScheduled = iota
	stateExpired
	stateCanceled
)

// A Timer is a handle to a deferred operation.
type Timer struct {
	f        func()
	deadline uint64 // in ticks
	state    atomic.Int32
	next     *Timer
	tail     *Timer // only set on head (TODO: move to bucket)
}

func newTimer(deadline uint64, f func()) *Timer {
	t := &Timer{
		f:        f,
		deadline: deadline,
	}
	t.tail = t
	return t
}

// Stop cancels the deferred operation. It returns whether or not the
// cancellation succeeded.
func (t *Timer) Stop() bool {
	return t.state.CAS(stateScheduled, stateCanceled)
}

func (t *Timer) expire() bool {
	return t.state.CAS(stateScheduled, stateExpired)
}

func (t *Timer) pushOne(head *Timer) *Timer {
	head.next = t
	if t == nil {
		head.tail = head
	} else {
		head.tail = t.tail
		t.tail = nil
	}
	return head
}

func (t *Timer) push(head *Timer) *Timer {
	if head == nil {
		return t
	}
	if t == nil {
		return head
	}
	head.tail.next = t
	head.tail = t.tail
	t.tail = nil
	return head
}

type bucket struct {
	sync.Mutex
	timers *Timer
	// Avoid false sharing:
	// http://mechanical-sympathy.blogspot.com/2011/07/false-sharing.html
	_ [cacheline - unsafe.Sizeof(sync.Mutex{}) - unsafe.Sizeof(&Timer{})]byte
}

type bucketList struct {
	mask    uint64
	buckets []bucket
}

func (bs *bucketList) Schedule(deadline uint64, f func()) *Timer {
	t := newTimer(deadline, f)
	b := &bs.buckets[deadline&bs.mask]
	b.Lock()
	b.timers = b.timers.pushOne(t)
	b.Unlock()
	return t
}

func (bs *bucketList) Clear() {
	for i := range bs.buckets {
		b := &bs.buckets[i]
		b.Lock()
		b.timers = nil
		b.Unlock()
	}
}

func (bs *bucketList) GatherExpired(start, end, now uint64) *Timer {
	// If we're more than a full rotation behind, only inspect each bucket
	// once.
	if (end-start == math.MaxUint64) || (int(end-start+1) > len(bs.buckets)) {
		start, end = 0, uint64(len(bs.buckets))
	}

	var todo *Timer
	for tick := start; tick < end; tick++ {
		todo = bs.gatherBucket(tick, now, todo)
	}
	return todo
}

func (bs *bucketList) gatherBucket(tick, now uint64, todo *Timer) *Timer {
	b := &bs.buckets[tick&bs.mask]

	b.Lock()
	batch := b.timers
	b.timers = nil
	b.Unlock()

	var unexpired *Timer
	for batch != nil {
		next := batch.next
		if batch.deadline > now {
			unexpired = unexpired.pushOne(batch)
		} else if batch.expire() {
			todo = todo.pushOne(batch)
		}
		batch = next
	}
	if unexpired != nil {
		// We should only hit this case if we're a full wheel rotation
		// behind.
		b.Lock()
		b.timers = b.timers.push(unexpired)
		b.Unlock()
	}
	return todo
}

// A Wheel schedules and executes deferred operations.
type Wheel struct {
	clock     clock.Clock
	periodExp uint64
	ticker    *clock.Ticker
	buckets   bucketList
	stopOnce  sync.Once
	stop      chan struct{}
	stopped   chan struct{}
}

// NewWheel creates and starts a new Wheel.
func NewWheel(period, maxTimeout time.Duration) *Wheel {
	w := newWheel(period, maxTimeout, clock.New())
	w.start()
	return w
}

func newWheel(period, maxTimeout time.Duration, clock clock.Clock) *Wheel {
	tickNanos, power := nextPowerOfTwo(int64(period)/2 + 1)
	numBuckets, _ := nextPowerOfTwo(int64(maxTimeout) / tickNanos)
	w := &Wheel{
		clock:     clock,
		periodExp: power,
		ticker:    clock.Ticker(time.Duration(tickNanos)),
		buckets: bucketList{
			mask:    uint64(numBuckets - 1),
			buckets: make([]bucket, numBuckets),
		},
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	return w
}

// Stop shuts down the wheel, blocking until all the background goroutines
// complete. It then clears all remaining timers, without firing any of their
// associated callbacks.
//
// Stop is safe to call multiple times; calls after the first are no-ops.
func (w *Wheel) Stop() {
	// TChannel Channels are safe to close more than once, so wheels should be
	// too.
	w.stopOnce.Do(func() {
		w.ticker.Stop()
		close(w.stop)
		<-w.stopped
		w.buckets.Clear()
	})
}

// AfterFunc schedules a deferred operation and returns a Timer.
func (w *Wheel) AfterFunc(d time.Duration, f func()) *Timer {
	deadline := w.asTick(w.clock.Now().Add(d))
	return w.buckets.Schedule(deadline, f)
}

func (w *Wheel) start() {
	go w.tick(w.clock.Now(), w.ticker.C)
}

func (w *Wheel) tick(now time.Time, nowCh <-chan time.Time) {
	watermark := w.asTick(now)
	for {
		select {
		case now := <-nowCh:
			nowTick := w.asTick(now)
			todo := w.buckets.GatherExpired(watermark, nowTick, nowTick)
			w.fire(todo)
			watermark = nowTick
		case <-w.stop:
			close(w.stopped)
			return
		}
	}
}

func (w *Wheel) fire(batch *Timer) {
	for t := batch; t != nil; t = t.next {
		t.f()
	}
}

func (w *Wheel) asTick(t time.Time) uint64 {
	return uint64(t.UnixNano() >> w.periodExp)
}

func nextPowerOfTwo(n int64) (num int64, exponent uint64) {
	pow := uint64(0)
	for (1 << pow) < n {
		pow++
	}
	return 1 << pow, pow
}
