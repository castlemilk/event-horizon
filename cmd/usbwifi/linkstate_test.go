package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempHome redirects linkStatePath() at ~/.event-horizon so tests
// never touch the operator's real state file.
func withTempHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestLinkStateWriteReadRoundTrip(t *testing.T) {
	withTempHome(t)
	st := LinkStateFile{
		SSID:      "Uncle Rad-Guest",
		Iface:     "utun11",
		IP:        "192.168.2.243",
		Gateway:   "192.168.2.1",
		PID:       os.Getpid(), // us — alive by definition
		StartedAt: time.Now(),
	}
	if err := WriteLinkState(st); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok := ReadLinkState(time.Now())
	if !ok {
		t.Fatal("just-written record reads as dead")
	}
	if got.SSID != st.SSID || got.Iface != st.Iface || got.IP != st.IP {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestLinkStateDeadPIDReadsAsGone(t *testing.T) {
	withTempHome(t)
	// PID 2^30 will never exist on a laptop; the entry must read dead
	// even though the JSON is well-formed and fresh.
	if err := WriteLinkState(LinkStateFile{
		SSID: "Ghost", PID: 1 << 30, StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, ok := ReadLinkState(time.Now()); ok {
		t.Error("record for a dead PID reads as live")
	}
}

func TestLinkStateRemoveClears(t *testing.T) {
	withTempHome(t)
	if err := WriteLinkState(LinkStateFile{SSID: "x", PID: os.Getpid(), StartedAt: time.Now()}); err != nil {
		t.Fatalf("write: %v", err)
	}
	RemoveLinkState()
	if _, ok := ReadLinkState(time.Now()); ok {
		t.Error("removed record still reads as live")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".event-horizon", "linkstate.json")); !os.IsNotExist(err) {
		t.Errorf("file survives RemoveLinkState: %v", err)
	}
}
