package tchannel

import (
	"fmt"
	"runtime"
	"sync"
)

// CheckedFramePoolForTest tracks gets and releases of frames, verifies that
// frames aren't double released, and can be used to check for frame leaks.
// As such, it is not performant, nor is it even a proper frame pool.
//
// It is intended to be used ONLY in tests.
type CheckedFramePoolForTest struct {
	mu sync.Mutex

	allocations map[*Frame]string
	badRelease  []string
}

// NewCheckedFramePoolForTest initializes a new CheckedFramePoolForTest.
func NewCheckedFramePoolForTest() *CheckedFramePoolForTest {
	return &CheckedFramePoolForTest{
		allocations: make(map[*Frame]string),
	}
}

// Get implements FramePool
func (p *CheckedFramePoolForTest) Get() *Frame {
	p.mu.Lock()
	defer p.mu.Unlock()

	frame := NewFrame(MaxFramePayloadSize)
	p.allocations[frame] = recordStack()
	return frame
}

// Release implements FramePool
func (p *CheckedFramePoolForTest) Release(f *Frame) {
	// Make sure the payload is not used after this point by clearing the frame.
	zeroOut(f.Payload)
	f.Payload = nil
	zeroOut(f.buffer)
	f.buffer = nil
	zeroOut(f.headerBuffer)
	f.headerBuffer = nil
	f.Header = FrameHeader{}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.allocations[f]; !ok {
		p.badRelease = append(p.badRelease, "bad Release at "+recordStack())
		return
	}

	delete(p.allocations, f)
}

// CheckedFramePoolForTestResult contains info on mismatched gets/releases
type CheckedFramePoolForTestResult struct {
	BadReleases []string
	Unreleased  []string
}

// HasIssues indicates whether there were any issues with gets/releases
func (r CheckedFramePoolForTestResult) HasIssues() bool {
	return len(r.BadReleases)+len(r.Unreleased) > 0
}

// CheckEmpty returns the number of unreleased frames in the pool
func (p *CheckedFramePoolForTest) CheckEmpty() CheckedFramePoolForTestResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	var badCalls []string
	for f, s := range p.allocations {
		badCalls = append(badCalls, fmt.Sprintf("frame %p: %v not released, get from: %v", f, f.Header, s))
	}

	return CheckedFramePoolForTestResult{
		Unreleased:  badCalls,
		BadReleases: p.badRelease,
	}
}

func recordStack() string {
	buf := make([]byte, 4096)
	runtime.Stack(buf, false /* all */)
	return string(buf)
}

func zeroOut(bs []byte) {
	for i := range bs {
		bs[i] = 0
	}
}
