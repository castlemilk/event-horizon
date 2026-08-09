package uptime

import (
	"testing"
	"time"
)

func TestUptimeTracker(t *testing.T) {
	tracker := NewTracker()
	time.Sleep(10 * time.Millisecond)

	stats := tracker.GetStats()
	if stats.UptimeSeconds < 0 {
		t.Errorf("Expected non-negative uptime seconds")
	}

	if stats.StabilityScore != 100.0 {
		t.Errorf("Expected default stability score 100.0, got %f", stats.StabilityScore)
	}

	if stats.CurrentStatus != "CONNECTED" {
		t.Errorf("Expected current status CONNECTED, got %s", stats.CurrentStatus)
	}
}
