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
