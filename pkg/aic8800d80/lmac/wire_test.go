package lmac

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"
)

func TestWrapCommandFraming(t *testing.T) {
	// A minimal MM_VERSION_REQ lmac_msg: id=0x0004 dst=Last src=DRV len=0.
	msg := make([]byte, HeaderSize)
	Header{ID: MMVersionReq, DestID: uint16(TaskLast), SrcID: DRVTaskID}.Encode(msg)

	got := WrapCommand(msg)
	wantRecLen := len(msg) + 4
	if got[0] != byte(wantRecLen&0xff) || got[1] != byte((wantRecLen>>8)&0x0f) {
		t.Errorf("record length: got %02x %02x want %02x %02x", got[0], got[1], byte(wantRecLen&0xff), byte((wantRecLen>>8)&0x0f))
	}
	if got[2] != 0x11 {
		t.Errorf("type: got 0x%02x want 0x11", got[2])
	}
	if got[3] != 0x00 {
		t.Errorf("pad: got 0x%02x", got[3])
	}
	if !bytes.Equal(got[4:8], []byte{0, 0, 0, 0}) {
		t.Errorf("dummy word: % x", got[4:8])
	}
	if !bytes.Equal(got[8:], msg) {
		t.Errorf("lmac_msg mismatch: % x", got[8:])
	}
	if len(got) != 8+len(msg) {
		t.Errorf("total length: %d want %d", len(got), 8+len(msg))
	}
}

func TestSubmitterWritesWrappedFrame(t *testing.T) {
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
	w := f.writes[0]
	if w[2] != 0x11 {
		t.Fatalf("written frame missing 0x11 type byte: % x", w[:4])
	}
	// Inner lmac_msg id must be intact at offset 8.
	if id := binary.LittleEndian.Uint16(w[8:10]); id != MMVersionReq {
		t.Fatalf("inner id: 0x%04x", id)
	}
}
