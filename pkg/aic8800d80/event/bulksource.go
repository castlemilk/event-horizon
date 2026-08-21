package event

import (
	"context"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// BulkDevice is the minimal USB surface the bulkFrameSource needs.
type BulkDevice interface {
	BulkIn(buf []byte, timeoutMs int) (int, error)
}

// NewBulkFrameSource returns a FrameSource that pumps the bulk IN endpoint
// through an RxStream, yielding one frame per Next call. Read timeouts are
// normal (no data available); they translate to ctx-aware retries so
// cancellation stays responsive within ~readTimeoutMs.
func NewBulkFrameSource(dev BulkDevice, readTimeoutMs int) FrameSource {
	return &bulkFrameSource{dev: dev, timeoutMs: readTimeoutMs}
}

type bulkFrameSource struct {
	dev       BulkDevice
	stream    protocol.RxStream
	timeoutMs int
}

func (s *bulkFrameSource) Next(ctx context.Context) (protocol.RxFrame, error) {
	for {
		if f, ok, err := s.stream.Next(); ok || err != nil {
			return f, err
		}
		if ctx.Err() != nil {
			return protocol.RxFrame{}, ctx.Err()
		}
		buf := make([]byte, 512)
		n, rerr := s.dev.BulkIn(buf, s.timeoutMs)
		if rerr != nil || n <= 0 {
			continue // timeout / transient — loop re-checks ctx
		}
		s.stream.Feed(buf[:n])
	}
}
