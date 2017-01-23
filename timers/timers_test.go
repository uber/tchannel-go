package timers

import (
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/andres-erbsen/clock"
	"github.com/stretchr/testify/assert"
)

// O(n), only for use in tests.
func (t *Timer) len() int {
	n := 0
	for el := t; el != nil; el = el.next {
		n++
	}
	return n
}

func assertListWellFormed(t testing.TB, list *Timer, expectedLen int) {
	if expectedLen == 0 {
		assert.Nil(t, list, "Expected zero-length list to be nil.")
		return
	}
	var (
		last   *Timer
		length int
	)
	head := list
	seen := make(map[*Timer]struct{})
	for el := list; el != nil; el = el.next {
		if _, ok := seen[el]; ok {
			t.Fatalf("Detected cycle in linked list with repeated element: %+v", *el)
		}
		seen[el] = struct{}{}
		if el != head {
			assert.Nil(t, el.tail, "Only head should have a tail set.")
		}
		last = el
		length++
	}
	assert.Equal(t, head.tail, last, "Expected list tail to point to last element.")
	assert.Equal(t, expectedLen, length, "Unexpected list length.")
}

func fakeWheel(tick time.Duration) (*Wheel, *clock.Mock) {
	clock := clock.NewMock()
	w := newWheel(tick, 2*time.Minute, clock)
	w.start()
	return w, clock
}

func newBucketList() *bucketList {
	return &bucketList{
		mask:    uint64(1<<3 - 1),
		buckets: make([]bucket, 1<<3),
	}
}

func assertQueuedTimers(t testing.TB, bs *bucketList, bucketIdx, expected int) {
	b := &bs.buckets[bucketIdx]
	b.Lock()
	defer b.Unlock()
	assert.Equal(
		t,
		expected,
		b.timers.len(),
		"Unexpected number of counters in bucket %v.", bucketIdx,
	)
}

func fakeWork() {} // non-nil func to schedule

func randomTimeouts(n int, max time.Duration) []time.Duration {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	ds := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		ds[i] = time.Duration(r.Int63n(int64(max)))
	}
	return ds
}

func TestTimersLinkedListPushOneNils(t *testing.T) {
	var root *Timer
	head := newTimer(0, nil)
	root = root.pushOne(head)
	assertListWellFormed(t, root, 1)
	assert.Equal(t, head, root, "Expected pushOne with nil receiver to return head.")
	assert.Panics(t, func() { root.pushOne(nil) }, "Expected pushOne'ing a nil to panic.")
}

func TestTimersLinkedListPushNils(t *testing.T) {
	// nil receiver & head
	var root *Timer
	assertListWellFormed(t, root.push(nil), 0)

	// nil receiver
	head := newTimer(0, nil)
	root = root.push(head)
	assertListWellFormed(t, root, 1)
	assert.Equal(t, head, root, "Expected push with nil receiver to return head list.")

	// nil head
	originalRoot := newTimer(0, nil)
	root = originalRoot
	root = root.push(nil)
	assertListWellFormed(t, root, 1)
	assert.Equal(t, originalRoot, root, "Expected pushing nil onto a list to return original list.")
}

func TestTimersLinkedList(t *testing.T) {
	els := []*Timer{
		newTimer(0, nil),
		newTimer(1, nil),
		newTimer(2, nil),
		newTimer(3, nil),
		newTimer(4, nil),
		newTimer(5, nil),
	}
	// Build a single-node list.
	var root *Timer
	root = root.pushOne(els[0])
	assert.Equal(t, root, els[0], "Unexpected first node.")
	assertListWellFormed(t, root, 1)

	// Add a second element.
	root = root.pushOne(els[1])
	assertListWellFormed(t, root, 2)

	// Push a list.
	root = root.push((*Timer)(nil).
		pushOne(els[2]).
		pushOne(els[3]).
		pushOne(els[4]).
		pushOne(els[5]))
	assertListWellFormed(t, root, 6)

	var deadlines []uint64
	for el := root; el != nil; el = el.next {
		deadlines = append(deadlines, el.deadline)
	}
	assert.Equal(t, []uint64{5, 4, 3, 2, 1, 0}, deadlines, "Unexpected list ordering.")
}

func TestBucketsAvoidFalseSharing(t *testing.T) {
	assert.Equal(t,
		int(cacheline),
		int(unsafe.Sizeof(bucket{})),
		"Expected buckets to exactly fill a CPU cache line.",
	)
}

func TestBucketListSchedule(t *testing.T) {
	bs := newBucketList()
	assertQueuedTimers(t, bs, 0, 0)

	bs.Schedule(uint64(0), fakeWork)
	assertQueuedTimers(t, bs, 0, 1)

	bs.Schedule(uint64(1), fakeWork)
	assertQueuedTimers(t, bs, 1, 1)

	bs.Schedule(uint64(7), fakeWork)
	assertQueuedTimers(t, bs, 7, 1)

	bs.Schedule(uint64(8), fakeWork)
	assertQueuedTimers(t, bs, 0, 2)
}

func TestBucketListGatherExpired(t *testing.T) {
	bs := newBucketList()
	for i := range bs.buckets {
		bs.Schedule(uint64(i), fakeWork)
	}
	assert.Equal(t, 0, bs.GatherExpired(0, 0, 0).len(), "Unexpected no. expired timers in range [0,0).")
	assert.Equal(t, 3, bs.GatherExpired(0, 3, 3).len(), "Unexpected no. expired timers in range [0,3).")
	assert.Equal(t, 0, bs.GatherExpired(0, 3, 3).len(), "Unexpected no. expired timers in range [0,3) on re-gather.")
	assert.Equal(t, 5, bs.GatherExpired(3, 100, 100).len(), "Unexpected no. expired timers in range [3,100).")

	// We should be leaving unexpired timers in place.
	bs.Schedule(8, fakeWork)
	bs.Schedule(8, fakeWork)
	assert.Equal(t, 0, bs.GatherExpired(0, 1, 1).len(), "Unexpected no. expired timers in range [0,1).")
	assertQueuedTimers(t, bs, 0, 2)
}

func TestBucketListClear(t *testing.T) {
	bs := newBucketList()
	for i := range bs.buckets {
		bs.Schedule(uint64(i), fakeWork)
	}
	bs.Schedule(uint64(1<<20), fakeWork)
	bs.Clear()
	all := bs.GatherExpired(0, math.MaxUint64, math.MaxUint64)
	assert.Equal(t, 0, all.len(), "Unexpected number of timers after clearing bucketList.")
}

func TestTimerBucketCalculations(t *testing.T) {
	tests := []struct {
		tick, max time.Duration
		buckets   int
	}{
		// If everything is a power of two, buckets = max / tick.
		{2, 8, 4},
		// Tick gets dropped to 1<<2, so we need 5 buckets to support 20ns
		// max without overlap. We then bump that to 1<<3.
		{6, 20, 8},
	}

	for _, tt := range tests {
		w := NewWheel(tt.tick, tt.max)
		defer w.Stop()
		assert.Equal(t, tt.buckets, len(w.buckets.buckets), "Unexpected number of buckets.")
	}
}

func TestTimersScheduleAndCancel(t *testing.T) {
	const (
		numTimers    = 20
		tickDuration = 1 << 22
		maxDelay     = tickDuration * numTimers
	)
	w, c := fakeWheel(tickDuration)
	defer w.Stop()

	var wg sync.WaitGroup
	scheduled := make([]*Timer, 0, numTimers)
	for i := time.Duration(0); i < numTimers; i++ {
		scheduled = append(scheduled, w.AfterFunc(i*tickDuration, wg.Done))
		// wg.Done() panics if the counter is less than zero, but these extra
		// calls should never fire.
		canceled := w.AfterFunc(i*tickDuration, wg.Done)
		assert.True(t, canceled.Stop())
	}

	for i := 0; i < numTimers; i++ {
		wg.Add(1)
		c.Add(tickDuration)
		wg.Wait()
	}

	for _, timer := range scheduled {
		assert.False(t, timer.Stop(), "Shouldn't be able to cancel after expiry.")
	}
}

func TestTimerDroppingTicks(t *testing.T) {
	const (
		numTimers    = 100
		tickDuration = 1 << 22
		maxDelay     = numTimers * tickDuration
	)
	now := make(chan time.Time)
	defer close(now)

	c := clock.NewMock()
	w := newWheel(5*time.Millisecond, 2*time.Minute, c)
	go w.tick(time.Unix(0, 0), now)
	defer w.Stop()

	var wg sync.WaitGroup
	wg.Add(numTimers)
	timeouts := randomTimeouts(numTimers, maxDelay)
	for _, d := range timeouts {
		w.AfterFunc(d, func() { wg.Done() })
	}
	now <- time.Unix(0, int64(2*maxDelay))
	wg.Wait()
}

func TestTimerStopClearsWheel(t *testing.T) {
	const numTimers = 100
	w, _ := fakeWheel(1 * time.Millisecond)

	timeouts := randomTimeouts(numTimers, 100*time.Millisecond)
	for _, d := range timeouts {
		w.AfterFunc(d, fakeWork)
	}

	w.Stop()
	all := w.buckets.GatherExpired(0, math.MaxUint64, math.MaxUint64)
	assert.Equal(t, 0, all.len(), "Expected wheel.Stop to clear all timers.")
}

func TestPowersOfTwo(t *testing.T) {
	tests := []struct {
		in       int
		out      int
		exponent int
	}{
		{-42, 1, 0},
		{0, 1, 0},
		{1, 1, 0},
		{5, 8, 3},
		{1000, 1024, 10},
		{1 << 22, 1 << 22, 22},
	}
	for _, tt := range tests {
		n, exp := nextPowerOfTwo(int64(tt.in))
		assert.Equal(t, tt.out, int(n), "Unexpected next power of two for input %v", tt.in)
		assert.Equal(t, tt.exponent, int(exp), "Unexpected exponent for input %v", tt.in)
	}
}

func TestWheelsDontConsumeInsaneAmountsOfMemory(t *testing.T) {
	w := NewWheel(5*time.Millisecond, 2*time.Minute)
	defer w.Stop()

	assert.Equal(
		t,
		32768,
		len(w.buckets.buckets),
		"Agh, too much memory",
	)
}

func scheduleAndCancel(b *testing.B, w *Wheel) {
	ds := randomTimeouts(1024, time.Minute)
	for i := range ds {
		ds[i] = ds[i] + 24*time.Hour
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			w.AfterFunc(ds[i&10], fakeWork).Stop()
			i++
		}
	})
}

func scheduleAndCancelStd(b *testing.B) {
	ds := randomTimeouts(1024, time.Minute)
	for i := range ds {
		ds[i] = ds[i] + 24*time.Hour
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			time.AfterFunc(ds[i&10], fakeWork).Stop()
			i++
		}
	})
}

func BenchmarkScheduleAndCancelWheelNoHeap(b *testing.B) {
	w := NewWheel(5*time.Millisecond, 2*time.Minute)
	defer w.Stop()

	scheduleAndCancel(b, w)
	b.StopTimer()
}

func BenchmarkScheduleAndCancelWheelWithHeap(b *testing.B) {
	w := NewWheel(5*time.Millisecond, 2*time.Minute)
	defer w.Stop()

	ds := randomTimeouts(10000, time.Minute)
	for i := range ds {
		ds[i] = ds[i] + 24*time.Hour
	}
	for i := range ds {
		w.AfterFunc(ds[i], fakeWork)
	}

	scheduleAndCancel(b, w)
	b.StopTimer()
}

func BenchmarkScheduleAndCancelStandardLibraryNoHeap(b *testing.B) {
	scheduleAndCancelStd(b)
}

func BenchmarkScheduleAndCancelStandardLibraryWithHeap(b *testing.B) {
	ds := randomTimeouts(10000, time.Minute)
	timers := make([]*time.Timer, 10000)
	for i := range ds {
		timers[i] = time.AfterFunc(ds[i]+24*time.Hour, fakeWork)
	}

	scheduleAndCancelStd(b)
	b.StopTimer()
	for _, t := range timers {
		t.Stop()
	}
}

func BenchmarkWheelWorkThread(b *testing.B) {
	// Note that this benchmark includes 1ms of wait time; attempting to reduce
	// this by ticking faster is counterproductive, since we start missing
	// ticks and then need to lock more buckets to catch up.
	w := NewWheel(time.Millisecond, time.Second)
	defer w.Stop()
	var wg sync.WaitGroup
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		w.AfterFunc(0, func() { wg.Done() })
		wg.Wait()
	}
}

func BenchmarkWheelTimerExpiry(b *testing.B) {
	w, c := fakeWheel(time.Millisecond)
	defer w.Stop()
	var wg sync.WaitGroup
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		w.AfterFunc(time.Millisecond, func() { wg.Done() })
		c.Add(10 * time.Millisecond)
		wg.Wait()
	}
}

func BenchmarkStandardLibraryTimerExpiry(b *testing.B) {
	// To compare accurately against our timer wheel, we need to create a mock
	// clock, listen on it, and advance it. (This actually accounts for the
	// vast majority of time spent and memory allocated in this benchmark.)
	clock := clock.NewMock()
	go func() {
		<-clock.Ticker(time.Millisecond).C
	}()

	var wg sync.WaitGroup
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		time.AfterFunc(0, func() { wg.Done() })
		clock.Add(10 * time.Millisecond)
		wg.Wait()
	}
}
