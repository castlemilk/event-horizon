// Package event runs the firmware event loop: drains the bulk IN endpoint,
// reassembles length-prefixed config frames, and dispatches each frame to a
// registered Sink by message id.
package event

import "context"

// Sink receives decoded LMAC event payloads from the Loop.
//
// Implementations must NOT block longer than necessary; the Loop is single-
// threaded. Return a non-fatal error to log + continue. Wrap fatal errors in
// &Fatal{Err: ...} to stop the loop.
type Sink interface {
	Handle(ctx context.Context, msgID uint16, payload []byte) error
}

// SinkFunc adapts a function to the Sink interface.
type SinkFunc func(ctx context.Context, msgID uint16, payload []byte) error

func (f SinkFunc) Handle(ctx context.Context, msgID uint16, payload []byte) error {
	return f(ctx, msgID, payload)
}
