package driver

import (
	"testing"
)

func TestDriverRegistryCoverage(t *testing.T) {
	reg := GetRegistry()
	chipsets := reg.ListAllChipsets()

	if len(chipsets) < 5 {
		t.Fatalf("Expected at least 5 registered chipset families, got %d", len(chipsets))
	}

	// Verify AIC8800 detection
	drv, devID, matched := reg.FindDriverForDevice(0xa69c, 0x8d81)
	if !matched {
		t.Errorf("Expected AIC8800D80 Operational device (0xa69c:0x8d81) to match")
	}
	if devID.Manufacturer != "UGREEN" {
		t.Errorf("Expected manufacturer UGREEN, got %s", devID.Manufacturer)
	}
	if drv.Info().Family != "AicSemi AIC8800" {
		t.Errorf("Expected family AicSemi AIC8800, got %s", drv.Info().Family)
	}

	// Verify Realtek AC detection
	_, rtlID, matchedRtl := reg.FindDriverForDevice(0x2357, 0x0120)
	if !matchedRtl {
		t.Errorf("Expected TP-Link Archer T4U (0x2357:0x0120) to match")
	}
	if rtlID.Manufacturer != "TP-Link" {
		t.Errorf("Expected manufacturer TP-Link, got %s", rtlID.Manufacturer)
	}

	// Verify Realtek AX Wi-Fi 6 detection
	_, axID, matchedAX := reg.FindDriverForDevice(0x2357, 0x0138)
	if !matchedAX {
		t.Errorf("Expected TP-Link TX20U (0x2357:0x0138) to match")
	}
	if axID.ProductName != "TP-Link Archer TX20U AX1800 Nano USB Adapter" {
		t.Errorf("Unexpected product name: %s", axID.ProductName)
	}

	// Verify MediaTek detection
	_, mtID, matchedMT := reg.FindDriverForDevice(0x0e8d, 0x7612)
	if !matchedMT {
		t.Errorf("Expected MT7612U to match")
	}
	if mtID.Manufacturer != "MediaTek" {
		t.Errorf("Expected manufacturer MediaTek, got %s", mtID.Manufacturer)
	}

	// Verify Qualcomm Atheros detection
	_, athID, matchedAth := reg.FindDriverForDevice(0x0cf3, 0x9271)
	if !matchedAth {
		t.Errorf("Expected AR9271 to match")
	}
	if athID.Manufacturer != "Qualcomm Atheros" {
		t.Errorf("Expected manufacturer Qualcomm Atheros, got %s", athID.Manufacturer)
	}
}

func TestDriverInstallerPipeline(t *testing.T) {
	installer := GetInstaller()

	req := InstallRequest{
		VID:          0xa69c,
		PID:          0x8d81,
		UseDriverKit: false,
	}

	err := installer.RunInstall(req)
	if err != nil {
		t.Fatalf("Failed to trigger installation: %v", err)
	}

	// Wait for background execution
	prog := installer.GetProgress()
	if !prog.IsActive && prog.Percent == 0 {
		t.Errorf("Expected active installation progress")
	}
}
