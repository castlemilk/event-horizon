package supervisor

import (
	"testing"
)

func TestWatchdogEvents(t *testing.T) {
	wd := GetWatchdog()

	wd.LogEvent(SeverityInfo, "TEST", "Testing supervisor audit logging", "Payload details")

	status := wd.GetStatus()
	if status["events"] == nil {
		t.Fatalf("Expected events array in status")
	}

	events, ok := status["events"].([]SupervisorEvent)
	if !ok {
		t.Fatalf("Failed to cast events")
	}

	if len(events) == 0 {
		t.Errorf("Expected at least one event logged")
	}
}
