package ping

import (
	"testing"
)

func TestPingDiagnostics(t *testing.T) {
	tester := NewTester()
	results := tester.RunDiagnosticsOnInterface("en0")

	if len(results) == 0 {
		t.Fatalf("Expected ping diagnostics results, got 0")
	}

	foundTarget := false
	for _, res := range results {
		if res.Target == "1.1.1.1" || res.Target == "8.8.8.8" {
			foundTarget = true
			if !res.IsReachable {
				t.Logf("Notice: Target %s is reachable: %v", res.Target, res.IsReachable)
			}
		}
	}

	if !foundTarget {
		t.Errorf("Expected 1.1.1.1 or 8.8.8.8 target in ping diagnostic test results")
	}
}

func TestSpeedTestOnInterface(t *testing.T) {
	result := RunSpeedTestOnInterface("en0")
	if result.Interface != "en0" {
		t.Errorf("Expected interface 'en0', got '%s'", result.Interface)
	}
	if result.DownloadMbps < 0 || result.UploadMbps < 0 {
		t.Errorf("Expected positive download/upload throughput values")
	}
}
