package event

import (
	"context"
	"sync"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// BulkDevice is the minimal USB surface the bulkFrameSource needs. MsgIn
// drains the second (command) bulk-IN endpoint; HasMsgIn reports whether the
// device exposes one. The firmware routes some responses there — notably
// after MM_SET_STACK_START — so both endpoints must be pumped.
type BulkDevice interface {
	BulkIn(buf []byte, timeoutMs int) (int, error)
	MsgIn(buf []byte, timeoutMs int) (int, error)
	HasMsgIn() bool
}

// NewBulkFrameSource returns a FrameSource that pumps the bulk IN endpoint
// (and, when present, the dedicated command IN endpoint) through per-endpoint
// RxStreams, yielding one frame per Next call. Read timeouts are normal (no
// data available); they translate to ctx-aware retries so cancellation stays
// responsive within ~readTimeoutMs.
func NewBulkFrameSource(dev BulkDevice, readTimeoutMs int) FrameSource {
	return &bulkFrameSource{dev: dev, timeoutMs: readTimeoutMs, hasMsg: dev.HasMsgIn()}
}

type bulkFrameSource struct {
	dev       BulkDevice
	stream    protocol.RxStream // frames from the main bulk-IN endpoint
	msgStream protocol.RxStream // frames from the command IN endpoint
	timeoutMs int
	hasMsg    bool

	// A dedicated reader keeps a bulk-IN read permanently outstanding. Reading
	// only between dispatches (the previous design) dropped every frame that
	// arrived while a frame was being processed, which is why a scan reported
	// only 1-3 of the BSSes actually on air.
	once     sync.Once
	bulkCh   chan []byte
	msgCh    chan []byte
	readerWG sync.Once
}

// startReaders launches the background pump exactly once.
func (s *bulkFrameSource) startReaders() {
	s.bulkCh = make(chan []byte, 256)
	pump := func(read func([]byte, int) (int, error), out chan []byte) {
		for {
			buf := make([]byte, 4096)
			n, err := read(buf, s.timeoutMs)
			if err == nil && n > 0 {
				select {
				case out <- buf[:n]:
				default: // consumer behind; drop oldest-style backpressure
				}
			}
		}
	}
	go pump(s.dev.BulkIn, s.bulkCh)
	if s.hasMsg {
		s.msgCh = make(chan []byte, 256)
		go pump(s.dev.MsgIn, s.msgCh)
	}
}

func (s *bulkFrameSource) Next(ctx context.Context) (protocol.RxFrame, error) {
	s.once.Do(s.startReaders)
	for {
		if f, ok, err := s.stream.Next(); ok || err != nil {
			return f, err
		}
		if s.hasMsg {
			if f, ok, err := s.msgStream.Next(); ok || err != nil {
				return f, err
			}
		}
		if ctx.Err() != nil {
			return protocol.RxFrame{}, ctx.Err()
		}
		select {
		case chunk := <-s.bulkCh:
			s.stream.Feed(chunk)
		case chunk := <-msgChan(s):
			s.msgStream.Feed(chunk)
		case <-ctx.Done():
			return protocol.RxFrame{}, ctx.Err()
		}
	}
}

// msgChan returns the command-endpoint channel, or nil (blocks forever, which
// select handles) when the device has no second IN endpoint.
func msgChan(s *bulkFrameSource) chan []byte {
	if !s.hasMsg {
		return nil
	}
	return s.msgCh
}
