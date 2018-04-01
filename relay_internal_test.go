package tchannel

import (
	"sync"
	"testing"
	"time"

	"github.com/uber/tchannel-go/typed"

	"github.com/stretchr/testify/assert"
)

func TestFinishesCallResponses(t *testing.T) {
	tests := []struct {
		msgType      messageType
		flags        byte
		finishesCall bool
	}{
		{messageTypeCallRes, 0x00, true},
		{messageTypeCallRes, 0x01, false},
		{messageTypeCallRes, 0x02, true},
		{messageTypeCallRes, 0x03, false},
		{messageTypeCallRes, 0x04, true},
		{messageTypeCallResContinue, 0x00, true},
		{messageTypeCallResContinue, 0x01, false},
		{messageTypeCallResContinue, 0x02, true},
		{messageTypeCallResContinue, 0x03, false},
		{messageTypeCallResContinue, 0x04, true},
		// By definition, callreq should never terminate an RPC.
		{messageTypeCallReq, 0x00, false},
		{messageTypeCallReq, 0x01, false},
		{messageTypeCallReq, 0x02, false},
		{messageTypeCallReq, 0x03, false},
		{messageTypeCallReq, 0x04, false},
	}
	for _, tt := range tests {
		f := NewFrame(100)
		fh := FrameHeader{
			size:        uint16(0xFF34),
			messageType: tt.msgType,
			ID:          0xDEADBEEF,
		}
		f.Header = fh
		fh.write(typed.NewWriteBuffer(f.headerBuffer))

		payload := typed.NewWriteBuffer(f.Payload)
		payload.WriteSingleByte(tt.flags)
		assert.Equal(t, tt.finishesCall, finishesCall(f), "Wrong isLast for flags %v and message type %v", tt.flags, tt.msgType)
	}
}

func TestRelayTimerPoolMisuse(t *testing.T) {
	tests := []struct {
		msg string
		f   func(*relayTimer)
	}{
		{
			msg: "release without stop",
			f: func(rt *relayTimer) {
				rt.Start(time.Hour, &relayItems{}, 0, false /* isOriginator */)
				rt.Release()
			},
		},
		{
			msg: "start twice",
			f: func(rt *relayTimer) {
				rt.Start(time.Hour, &relayItems{}, 0, false /* isOriginator */)
				rt.Start(time.Hour, &relayItems{}, 0, false /* isOriginator */)
			},
		},
		{
			msg: "underlying timer is already active",
			f: func(rt *relayTimer) {
				rt.timer.Reset(time.Hour)
				rt.Start(time.Hour, &relayItems{}, 0, false /* isOriginator */)
			},
		},
	}

	for _, tt := range tests {
		trigger := func(*relayItems, uint32, bool) {}
		rtp := newRelayTimerPool(trigger)

		rt := rtp.Get()
		assert.Panics(t, func() {
			tt.f(rt)
		}, tt.msg)
	}
}

func TestRelayTimerStopConcurrently(t *testing.T) {
	trigger := func(*relayItems, uint32, bool) {}
	rtp := newRelayTimerPool(trigger)
	timer := rtp.Get()
	timer.Start(time.Nanosecond, nil, 0 /* items */, false /* isOriginator */)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timer.Stop()
		}()
	}

	wg.Wait()
}
