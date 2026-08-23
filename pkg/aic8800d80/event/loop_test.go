package event

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

type fakeSource struct {
	frames chan protocol.RxFrame
	done   chan struct{}
}

func (s *fakeSource) Next(ctx context.Context) (protocol.RxFrame, error) {
	// Drain buffered frames first so closing done doesn't race with
	// already-queued frames.
	select {
	case f, ok := <-s.frames:
		if !ok {
			return protocol.RxFrame{}, io.EOF
		}
		return f, nil
	default:
	}
	select {
	case <-ctx.Done():
		return protocol.RxFrame{}, ctx.Err()
	case f, ok := <-s.frames:
		if !ok {
			return protocol.RxFrame{}, io.EOF
		}
		return f, nil
	case <-s.done:
		return protocol.RxFrame{}, io.EOF
	}
}

type captureSink struct {
	mu       sync.Mutex
	calls    []uint16
	fatalOn  uint16
}

func (c *captureSink) Handle(_ context.Context, id uint16, _ []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, id)
	if c.fatalOn != 0 && id == c.fatalOn {
		return &Fatal{Err: errors.New("test fatal")}
	}
	return nil
}

func TestLoopDispatchesFrames(t *testing.T) {
	src := &fakeSource{frames: make(chan protocol.RxFrame, 4), done: make(chan struct{})}
	sink := &captureSink{}
	loop := NewLoop(src, sink)
	src.frames <- protocol.RxFrame{Type: protocol.USBTypeCfg, Payload: makeMsgIDPayload(0x1004)}
	src.frames <- protocol.RxFrame{Type: protocol.USBTypeCfg, Payload: makeMsgIDPayload(0x0005)}
	close(src.done)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.calls) != 2 || sink.calls[0] != 0x1004 || sink.calls[1] != 0x0005 {
		t.Fatalf("calls: %v", sink.calls)
	}
}

func TestLoopStopsOnFatal(t *testing.T) {
	src := &fakeSource{frames: make(chan protocol.RxFrame, 4), done: make(chan struct{})}
	sink := &captureSink{fatalOn: 0x0005}
	loop := NewLoop(src, sink)
	src.frames <- protocol.RxFrame{Type: protocol.USBTypeCfg, Payload: makeMsgIDPayload(0x0005)}
	close(src.done)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := loop.Run(ctx)
	if err == nil {
		t.Fatal("expected fatal error")
	}
	if _, ok := err.(*Fatal); !ok {
		// Run wraps in fmt.Errorf; unwrap to find Fatal.
		var f *Fatal
		if !errors.As(err, &f) {
			t.Fatalf("expected *Fatal in chain, got %T: %v", err, err)
		}
	}
}

func TestLoopSkipsDataFrames(t *testing.T) {
	src := &fakeSource{frames: make(chan protocol.RxFrame, 4), done: make(chan struct{})}
	sink := &captureSink{}
	loop := NewLoop(src, sink)
	src.frames <- protocol.RxFrame{Type: 0x02, Payload: []byte{0x01, 0x02}} // data frame (no 0x10 bit)
	close(src.done)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.calls) != 0 {
		t.Fatalf("expected no calls (data frame dropped), got %v", sink.calls)
	}
}

func makeMsgIDPayload(id uint16) []byte {
	b := make([]byte, lmac.HeaderSize)
	binary.LittleEndian.PutUint16(b[0:2], id)
	return b
}
