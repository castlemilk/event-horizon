package lmac

import "fmt"

// SubmitErrorKind categorises submission failures.
type SubmitErrorKind uint8

const (
	ErrTimeout SubmitErrorKind = iota + 1
	ErrSeqMismatch
	ErrChannelClosed
	ErrShortFrame
)

// SubmitError is a structured submission failure.
type SubmitError struct {
	Kind  SubmitErrorKind
	MsgID uint16
	Cause error
}

func (e *SubmitError) Error() string {
	switch e.Kind {
	case ErrTimeout:
		return fmt.Sprintf("lmac submit: timeout waiting for ACK on msg 0x%04x", e.MsgID)
	case ErrSeqMismatch:
		return fmt.Sprintf("lmac submit: sequence mismatch on msg 0x%04x", e.MsgID)
	case ErrChannelClosed:
		return "lmac submit: channel closed"
	case ErrShortFrame:
		return fmt.Sprintf("lmac submit: short frame for msg 0x%04x", e.MsgID)
	default:
		return fmt.Sprintf("lmac submit: unknown error on msg 0x%04x", e.MsgID)
	}
}

func (e *SubmitError) Unwrap() error { return e.Cause }

// ErrUnknownTLV is returned when a payload contains a tag we don't know.
var ErrUnknownTLV = fmt.Errorf("lmac: unknown TLV tag")
