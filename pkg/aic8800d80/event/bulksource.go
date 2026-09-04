package event

import (
	"context"

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
}

func (s *bulkFrameSource) Next(ctx context.Context) (protocol.RxFrame, error) {
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
		// Split the read timeout across the two endpoints so neither starves
		// the other; a blocking read on one must not stall the other.
		to := s.timeoutMs
		if s.hasMsg {
			to = s.timeoutMs / 2
			if to < 20 {
				to = 20
			}
		}
		buf := make([]byte, 4096)
		if n, rerr := s.dev.BulkIn(buf, to); rerr == nil && n > 0 {
			s.stream.Feed(buf[:n])
		}
		if s.hasMsg {
			mbuf := make([]byte, 4096)
			if n, rerr := s.dev.MsgIn(mbuf, to); rerr == nil && n > 0 {
				s.msgStream.Feed(mbuf[:n])
			}
		}
	}
}
