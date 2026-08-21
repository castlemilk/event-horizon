package lmac

import (
	"testing"
)

func TestSubmitErrorIs(t *testing.T) {
	err := &SubmitError{Kind: ErrSeqMismatch, MsgID: 0x1000}
	if err.Kind != ErrSeqMismatch {
		t.Fatalf("kind: got %d want %d", err.Kind, ErrSeqMismatch)
	}
}

func TestSubmitErrorMessage(t *testing.T) {
	err := &SubmitError{Kind: ErrTimeout, MsgID: 0x0004}
	if err.Error() == "" {
		t.Fatal("Error() returned empty string")
	}
}

func TestSubmitErrorUnwrap(t *testing.T) {
	inner := errSentinel("boom")
	err := &SubmitError{Kind: ErrTimeout, MsgID: 0x0004, Cause: inner}
	if got := err.Unwrap(); got != inner {
		t.Fatalf("Unwrap: got %v want %v", got, inner)
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
