package ping

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingInterfaceNeverUsesDefaultRoute(t *testing.T) {
	// If ping is ever launched, it would report success over the wrong route.
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "ping"), []byte("#!/bin/sh\nprintf '64 bytes from 127.0.0.1: time=1.2 ms\\n2 packets transmitted, 2 packets received, 0.0%% packet loss\\nround-trip min/avg/max/stddev = 1.0/1.2/1.4/0.2 ms\\n'\n"), 0755)
	if err != nil { t.Fatal(err) }
	t.Setenv("PATH", dir)
	result := NewTester().PingTargetOnInterface("missing-dongle", "127.0.0.1", 53)
	if result.IsReachable { t.Fatalf("missing interface was reported reachable: %+v", result) }
}

func TestUnknownRTTIsNotInvented(t *testing.T) {
	if got := parseRTT("no response statistics"); got != -1 { t.Fatalf("unknown RTT = %d, want -1", got) }
}
