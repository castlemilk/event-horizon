package wifi

import (
	"testing"
)

func TestScannerMock(t *testing.T) {
	scanner := NewScanner()
	scanner.StartMockScanner()

	hotspots := scanner.ListHotspots()
	if len(hotspots) == 0 {
		t.Fatalf("Expected hotspots in scanner, got 0")
	}

	foundSFH := false
	for _, ap := range hotspots {
		if ap.SSID == "SFH" || ap.SSID == "aliens exist" {
			foundSFH = true
			break
		}
	}

	if !foundSFH {
		t.Errorf("Expected to find default SSID in hotspots list")
	}

	ap, err := scanner.SelectHotspot("SFH")
	if err != nil {
		t.Fatalf("Failed to select hotspot SFH: %v", err)
	}

	if !ap.IsSelected {
		t.Errorf("Expected selected hotspot IsSelected to be true")
	}
}

func TestWPAConnection(t *testing.T) {
	conn := NewWPAConnection("SFH", "cnh12345", "00:13:02:8f:9a:33")
	err := conn.Connect()
	if err != nil {
		t.Fatalf("WPAConnection Connect returned error: %v", err)
	}

	if conn.State != "CONNECTED" {
		t.Errorf("Expected WPAConnection state to be CONNECTED, got %s", conn.State)
	}
}
