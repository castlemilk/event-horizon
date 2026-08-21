package event

import "errors"

// Fatal wraps an error to request loop termination (Handle returns Fatal(err)).
type Fatal struct{ Err error }

func (f *Fatal) Error() string { return "event loop fatal: " + f.Err.Error() }
func (f *Fatal) Unwrap() error { return f.Err }

// ErrUnknownMsgID is returned by Sink.Handle when the dispatch table does not
// contain a handler for msgID. The loop logs and continues.
var ErrUnknownMsgID = errors.New("event: unknown message id")
