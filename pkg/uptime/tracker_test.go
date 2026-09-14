package uptime

import "testing"

func TestNewTrackerDoesNotInventConnection(t *testing.T) {
	stats := NewTracker().GetStats()
	if stats.CurrentStatus != "UNKNOWN" || stats.UptimeSeconds != 0 || !stats.ConnectedAt.IsZero() || stats.StabilityScore != 0 {
		t.Fatalf("unobserved link reports a connection: %+v", stats)
	}
}

func TestTrackerCountsTransitionsOnce(t *testing.T) {
	tracker := NewTracker()
	tracker.RecordReconnect()
	first := tracker.GetStats()
	tracker.RecordReconnect()
	stats := tracker.GetStats()
	if stats.ReconnectCount != 0 || stats.ConnectedAt != first.ConnectedAt {
		t.Fatalf("first observation/repeated connected poll counted as reconnect: %+v", stats)
	}
	tracker.RecordDisconnect()
	tracker.RecordDisconnect()
	if got := tracker.GetStats(); got.DisconnectCount != 1 || got.UptimeSeconds != 0 {
		t.Fatalf("duplicate disconnects: %+v", got)
	}
	tracker.RecordReconnect()
	tracker.RecordReconnect()
	if got := tracker.GetStats(); got.ReconnectCount != 1 {
		t.Fatalf("duplicate reconnects: %+v", got)
	}
}
