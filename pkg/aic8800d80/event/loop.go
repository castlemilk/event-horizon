package event

import (
	"context"
	"fmt"
	"log"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// FrameSource pumps LMAC frames from the device's bulk IN endpoint. The Loop
// calls Next repeatedly until it returns a non-nil error.
type FrameSource interface {
	Next(ctx context.Context) (protocol.RxFrame, error)
}

// Loop reads frames from a FrameSource and dispatches to a Sink.
type Loop struct {
	src  FrameSource
	sink Sink
}

// NewLoop creates a Loop.
func NewLoop(src FrameSource, sink Sink) *Loop {
	return &Loop{src: src, sink: sink}
}

// Run drives the loop until ctx is cancelled, the source is exhausted, or a
// sink handler returns *Fatal.
func (l *Loop) Run(ctx context.Context) error {
	for {
		f, err := l.src.Next(ctx)
		if err != nil {
			// EOF / closed source / ctx cancellation = clean exit.
			return nil
		}
		if !f.IsConfig() {
			if l.sink != nil && len(f.Payload) > 60 {
				_ = l.sink.Handle(ctx, 0xFFFF, f.Payload)
			}
			continue
		}
		if len(f.Payload) < lmac.HeaderSize {
			log.Printf("[event] short config frame (%d bytes), dropping", len(f.Payload))
			continue
		}
		msgID := f.MsgID()
		param := f.Payload[lmac.HeaderSize:]
		if err := l.sink.Handle(ctx, msgID, param); err != nil {
			if _, ok := err.(*Fatal); ok {
				return fmt.Errorf("event loop fatal: %w", err)
			}
			log.Printf("[event] handler for msg 0x%04x returned: %v", msgID, err)
		}
	}
}
