package lmac

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeSink struct {
	mu     sync.Mutex
	writes [][]byte
	acks   chan uint16
}

func (f *fakeSink) BulkOut(_ context.Context, b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]byte(nil), b...)
	f.writes = append(f.writes, cp)
	return nil
}

func (f *fakeSink) NextACK(ctx context.Context) (uint16, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case id := <-f.acks:
		return id, nil
	}
}

// TestSubmitterRoundTrip: Submit a MM_VERSION_REQ, then deliver a fake CFM
// (MM_VERSION_CFM). Expected: Submit returns within timeout, writes count = 1.
func TestSubmitterRoundTrip(t *testing.T) {
	f := &fakeSink{acks: make(chan uint16, 1)}
	s := NewSubmitter(f, f)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		f.acks <- MMVersionCfm
	}()
	if err := s.Submit(ctx, VersionReq{}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 1 {
		t.Fatalf("writes: %d", len(f.writes))
	}
	// Frames are wrapped for the command pipe; the lmac_msg id sits at offset 8.
	got := uint16(f.writes[0][8]) | uint16(f.writes[0][9])<<8
	if got != MMVersionReq {
		t.Fatalf("write id: 0x%04x", got)
	}
}

// TestSubmitterTimeout: Submit a MM_VERSION_REQ, never deliver a CFM.
// Expected: Submit returns *SubmitError{ErrTimeout}.
func TestSubmitterTimeout(t *testing.T) {
	f := &fakeSink{acks: make(chan uint16)}
	s := NewSubmitter(f, f)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := s.Submit(ctx, VersionReq{})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	se, ok := err.(*SubmitError)
	if !ok || se.Kind != ErrTimeout {
		t.Fatalf("expected *SubmitError{ErrTimeout}, got %T %v", err, err)
	}
}

// TestSubmitterIgnoresSpuriousAck: spurious ACK in a different task is
// ignored; the matching CFM satisfies the wait.
func TestSubmitterIgnoresSpuriousAck(t *testing.T) {
	f := &fakeSink{acks: make(chan uint16, 4)}
	s := NewSubmitter(f, f)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go func() {
		f.acks <- 0x1234 // spurious (TASK_LMAC_API-ish, not TASK_MM)
		time.Sleep(10 * time.Millisecond)
		f.acks <- MMVersionCfm
	}()
	if err := s.Submit(ctx, VersionReq{}); err != nil {
		t.Fatalf("submit: %v", err)
	}
}
