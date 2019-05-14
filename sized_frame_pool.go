package tchannel

import (
	"fmt"
	"math/bits"
)

type poolBySize struct {
	pools []FramePool
}

// Buckets are separated by powers of 2, but we don't need separate buckets
// for 1 byte vs 2 bytes etc. Use a single bucket up to 1kb.
var _shiftBottom = 10

func NewSizedPool(framePoolFactory func(frameSize int) FramePool) SizedFramePool {
	pools := make([]FramePool, toBucket(MaxFrameSize)+1)

	// Create separate pools for each size. We want:
	// 0: 0 -> 1kb
	// 1: 1kb -> 2kb
	// and so on.
	for i := range pools {
		pools[i] = framePoolFactory(1 << uint(i+_shiftBottom))
	}
	return &poolBySize{pools}
}

func (p poolBySize) Get() *Frame {
	return p.GetSized(MaxFramePayloadSize)
}

func (p poolBySize) GetSized(payloadSize int) *Frame {
	fmt.Println("sized", payloadSize)
	bucket := toBucket(payloadSize)
	f := p.pools[bucket].Get()
	return f
}

func (p poolBySize) Release(f *Frame) {
	bucket := toBucket(len(f.Payload))
	p.pools[bucket].Release(f)
}

func toBucket(size int) int {
	if size == 0 {
		return 0
	}
	bucket := bits.Len(uint(size-1)) - _shiftBottom
	if bucket < 0 {
		bucket = 0
	}
	return bucket
}
