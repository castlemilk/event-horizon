package usb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The daemon cannot import cmd/usbwifi, so this test replays the CLI's
// wire format by hand — the JSON the writer emits. If the two ever drift,
// this test (not production) is where it shows up, which is exactly why
// both sides use flat lowercase keys with no nesting.
func writeCLIState(t *testing.T, ssid string, pid int, startedAt time.Time) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	rec := map[string]any{
		"ssid": ssid, "iface": "utun11", "ip": "192.168.2.243",
		"pid": pid, "startedAt": startedAt.Format(time.RFC3339Nano),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".event-horizon"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".event-horizon", "linkstate.json"), b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	InvalidateLinkStateCache()
}

func TestLinkStateLiveRecordSurfacesSSID(t *testing.T) {
	writeCLIState(t, "Uncle Rad-Guest", os.Getpid(), time.Now())
	got := ReadDongleLinkState(time.Now())
	if !got.Alive {
		t.Fatal("live CLI record reads as dead")
	}
	if got.Record.SSID != "Uncle Rad-Guest" {
		t.Errorf("SSID = %q, want the recorded network", got.Record.SSID)
	}
}

func TestLinkStateDeadProcessIsNotAlive(t *testing.T) {
	writeCLIState(t, "Ghost", 1<<30, time.Now())
	if got := ReadDongleLinkState(time.Now()); got.Alive {
		t.Error("dead-PID record reports alive")
	}
}

func TestLinkStateMissingFileIsNotAlive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	InvalidateLinkStateCache()
	if got := ReadDongleLinkState(time.Now()); got.Alive {
		t.Error("missing file reports alive")
	}
}
