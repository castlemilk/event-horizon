package event

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

type fakeBulk struct {
	chunks [][]byte
	errAfter int // return error once after this many reads (-1 = never)
	reads  int
}

func (f *fakeBulk) BulkIn(buf []byte, _ int) (int, error) {
	f.reads++
	if f.errAfter >= 0 && f.reads > f.errAfter {
		return 0, errors.New("timeout")
	}
	if len(f.chunks) == 0 {
		return 0, errors.New("timeout")
	}
	c := f.chunks[0]
	f.chunks = f.chunks[1:]
	copy(buf, c)
	return len(c), nil
}

func cfgFrame(id uint16) []byte {
	b := make([]byte, 4+4)
	b[0] = 4
	b[2] = protocol.USBTypeCfg
	b[4] = byte(id)
	b[5] = byte(id >> 8)
	return b
}

func TestBulkFrameSourceYieldsFrames(t *testing.T) {
	fb := &fakeBulk{errAfter: -1, chunks: [][]byte{
		cfgFrame(0x0005),
		cfgFrame(0x1004),
	}}
	src := NewBulkFrameSource(fb, 50)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	f1, err := src.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !f1.IsConfig() || f1.MsgID() != 0x0005 {
		t.Fatalf("frame1: %+v", f1)
	}
	f2, err := src.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if f2.MsgID() != 0x1004 {
		t.Fatalf("frame2 id: 0x%04x", f2.MsgID())
	}
}

func TestBulkFrameSourceSplitsAcrossChunks(t *testing.T) {
	full := cfgFrame(0x0401)
	fb := &fakeBulk{errAfter: -1, chunks: [][]byte{
		full[:3], // split mid-record-header
		full[3:],
	}}
	src := NewBulkFrameSource(fb, 50)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f, err := src.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if f.MsgID() != 0x0401 {
		t.Fatalf("id: 0x%04x", f.MsgID())
	}
}

func TestBulkFrameSourceRespectsCtx(t *testing.T) {
	fb := &fakeBulk{errAfter: -1} // always times out, never delivers
	src := NewBulkFrameSource(fb, 20)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := src.Next(ctx); err == nil {
		t.Fatal("expected ctx error")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation took too long")
	}
}
