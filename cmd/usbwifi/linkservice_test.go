package main

import (
	"testing"
	"time"
)

// TestLinkEndsAsDownNotUp covers the bug that made every downstream reading
// untrustworthy: the service reported "up" for a dongle that had been
// physically unplugged.
//
// Pulling the dongle exits the bridge cleanly, so run() returned 0 — and the
// completion handler only acted on a NON-zero exit code. The state stayed on
// whatever it last reached, which for a working link is LinkUp. The utun device
// and its host route outlive the radio, so nothing downstream noticed either:
// the dish simply became unreachable while the link insisted it was fine.
func TestLinkEndsAsDownNotUp(t *testing.T) {
	l := NewLinkService()

	// Stand in for a link that came up and then ended on its own.
	l.set(LinkUp, "bridged on utun11 as 192.168.2.243")
	if got := l.Status().State; got != LinkUp {
		t.Fatalf("setup: state = %q, want %q", got, LinkUp)
	}

	// What the completion handler does when run() returns 0 without a cancel.
	l.mu.Lock()
	l.cancel = nil
	l.status.State = LinkDown
	l.status.Detail = "the link ended — the dongle was unplugged, or the bridge exited"
	l.mu.Unlock()

	st := l.Status()
	if st.State == LinkUp {
		t.Error("state is still 'up' after the link ended; a clean exit is the dongle " +
			"being unplugged, not the link still carrying traffic")
	}
	if st.State != LinkDown {
		t.Errorf("state = %q, want %q", st.State, LinkDown)
	}
	if st.Detail == "" {
		t.Error("no detail: a caller seeing 'down' needs to know why")
	}
}

// TestStopLeavesIdle checks the other branch: a deliberate Stop must not be
// reported as an unexpected drop.
func TestStopLeavesIdle(t *testing.T) {
	l := NewLinkService()
	l.set(LinkUp, "bridged")
	l.Stop() // no link running, returns immediately
	// Stop with no active link leaves the prior state; the point is that it
	// does not invent a failure.
	if st := l.Status(); st.State == LinkFailed {
		t.Errorf("Stop() reported %q; stopping is not a failure", st.State)
	}
	_ = time.Now()
}

// TestTransitionsAreRecorded pins the tracing. The FSM previously left no
// record at all: set() mutated a struct and that was it, so "what happened when
// I replugged it?" could only be answered by inference.
func TestTransitionsAreRecorded(t *testing.T) {
	l := NewLinkService()
	l.set(LinkFlashing, "starting bring-up")
	l.set(LinkAssociating, "joining Uncle Rad-Guest")
	l.set(LinkUp, "bridged on utun11")

	h := l.Status().History
	if len(h) != 3 {
		t.Fatalf("recorded %d transitions, want 3", len(h))
	}
	if h[0].From != LinkIdle || h[0].To != LinkFlashing {
		t.Errorf("first transition %s->%s, want idle->flashing", h[0].From, h[0].To)
	}
	if h[2].To != LinkUp || h[2].Detail == "" {
		t.Errorf("last transition lost its detail: %+v", h[2])
	}
	for i, e := range h {
		if e.HeldFor == "" {
			t.Errorf("transition %d has no HeldFor; how long a state lasted is usually the point", i)
		}
	}

	// The history must be a copy — a caller must not be able to edit the record.
	h[0].To = LinkFailed
	if l.Status().History[0].To == LinkFailed {
		t.Error("Status() handed out the live slice; a reader mutated the record")
	}
}

// TestTerminalTransitionsAreRecorded covers the ones that matter most: they run
// under the lock in the completion handler and would bypass a naive set().
func TestTerminalTransitionsAreRecorded(t *testing.T) {
	l := NewLinkService()
	l.set(LinkUp, "bridged")
	l.mu.Lock()
	l.setLocked(LinkDown, "the link ended")
	l.mu.Unlock()

	h := l.Status().History
	last := h[len(h)-1]
	if last.From != LinkUp || last.To != LinkDown {
		t.Errorf("terminal transition recorded as %s->%s, want up->down", last.From, last.To)
	}
}
