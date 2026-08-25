package wifi

import (
	"testing"
)

func TestGenerateSpectrumReport(t *testing.T) {
	mockHotspots := []*AccessPoint{
		{SSID: "Home_WiFi_6", BSSID: "aa:bb:cc:dd:ee:01", Channel: 6, RSSI: -45},
		{SSID: "Neighbor_2G", BSSID: "aa:bb:cc:dd:ee:02", Channel: 6, RSSI: -78},
		{SSID: "Office_5G", BSSID: "aa:bb:cc:dd:ee:03", Channel: 36, RSSI: -52},
		{SSID: "Starlink", BSSID: "aa:bb:cc:dd:ee:04", Channel: 1, RSSI: -40},
	}

	report := GenerateSpectrumReport(mockHotspots)

	if len(report.Channels24GHz) == 0 {
		t.Fatalf("Expected 2.4 GHz channel list, got 0")
	}

	if len(report.Channels5GHz) == 0 {
		t.Fatalf("Expected 5 GHz channel list, got 0")
	}

	if report.OptimalChannel24GHz <= 0 {
		t.Errorf("Expected valid optimal 2.4 GHz channel")
	}

	if report.OptimalChannel5GHz <= 0 {
		t.Errorf("Expected valid optimal 5 GHz channel")
	}

	if len(report.Recommendations) == 0 {
		t.Errorf("Expected spectrum recommendations")
	}

	// Verify channel 6 has 2 BSSIDs
	var ch6 *RFChannelInfo
	for i := range report.Channels24GHz {
		if report.Channels24GHz[i].Channel == 6 {
			ch6 = &report.Channels24GHz[i]
			break
		}
	}
	if ch6 == nil || ch6.BSSIDCount != 2 {
		t.Errorf("Expected channel 6 to have 2 BSSIDs, got %v", ch6)
	}
}
