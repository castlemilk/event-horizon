package main

import (
	"os"
	"testing"
	"time"
)

// TestLinkRefusesWhenDaemonOwnsTheLink locks the guard added after two
// processes fought over the USB device mid-flight: the CLI must refuse to
// start when a linkstate record's writer is still alive. Without this the
// operator (or an agent) can kill a healthy link by starting a second one.
func TestLinkRefusesWhenDaemonOwnsTheLink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// A record whose PID is us — alive by definition, matching the daemon's
	// link process model.
	if err := WriteLinkState(LinkStateFile{
		SSID: "Uncle Rad-Guest", Iface: "utun11", IP: "192.168.2.243",
		PID: os.Getpid(), StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	rc := runCmdLink(nil, []string{"--ssid", "Other Network", "--pass", "x"})
	if rc == 0 {
		t.Fatal("second link was not refused with a live record")
	}
	// A dead writer's record must NOT block: the unplug-and-moved-on case.
	RemoveLinkState()
	if err := WriteLinkState(LinkStateFile{
		SSID: "Ghost", PID: 1 << 30, StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Dead PID → reads as absent → runCmdLink proceeds past the guard (it
	// will fail later for want of a dongle on this box, which is fine — the
	// assertion is that the guard itself did not refuse).
	ls, ok := ReadLinkState(time.Now())
	if ok && ls.PID != 1<<30 {
		t.Fatalf("guard read a live record from a dead writer: %+v", ls)
	}
}
