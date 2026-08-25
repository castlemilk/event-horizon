package ping

import (
	"testing"
	"time"
)

func TestSpeedTesterStatus(t *testing.T) {
	st := GetSpeedTester()
	status := st.GetStatus()

	if status.Server == "" {
		t.Errorf("Expected non-empty speedtest server target")
	}

	if status.Phase == "" {
		t.Errorf("Expected non-empty speedtest phase")
	}
}

func TestSpeedTesterSimulatedRun(t *testing.T) {
	st := GetSpeedTester()
	err := st.StartTest("lo0")
	if err != nil {
		t.Logf("Notice on speedtest start: %v", err)
	}

	time.Sleep(1 * time.Second)
	status := st.GetStatus()
	if status.ProgressPercent < 0.0 || status.ProgressPercent > 100.0 {
		t.Errorf("Invalid progress percent: %f", status.ProgressPercent)
	}
}
