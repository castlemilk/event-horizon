package usb

import (
	"testing"
)

func TestGetHardwareTopology(t *testing.T) {
	nodes := GetHardwareTopology()
	if len(nodes) == 0 {
		t.Fatalf("Expected hardware topology nodes, got 0")
	}

	foundBuiltIn := false
	foundDongle := false

	for _, n := range nodes {
		if n.BSDInterface == "en0" {
			foundBuiltIn = true
		}
		if n.VendorID == "0xA69C" || n.VendorID == "0xa69c" {
			foundDongle = true
		}
	}

	if !foundBuiltIn {
		t.Errorf("Expected built-in en0 interface in hardware topology")
	}
	if !foundDongle {
		t.Errorf("Expected USB Wi-Fi dongle in hardware topology")
	}
}

func TestCheckAndSwitchDevices(t *testing.T) {
	dev, err := CheckAndSwitchDevices()
	if err != nil {
		t.Logf("CheckAndSwitchDevices notice: %v", err)
	} else if dev.Name == "" {
		t.Errorf("Expected non-empty device name")
	}
}
