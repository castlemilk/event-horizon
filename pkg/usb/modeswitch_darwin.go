//go:build darwin

package usb

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// ZeroCD mode-switching on macOS.
//
// The libusb path in modeswitch.go cannot work here. macOS binds its own
// IOUSBMassStorageDriver to the dongle's storage interface as soon as it
// enumerates, so libusb_claim_interface(0) fails and the SCSI EJECT that
// follows never reaches the device — the observed symptom is
// "mode-switch eject failed for VID 0xa69c PID 0x5723" while the dongle sits
// happily in ZeroCD.
//
// What does work is asking macOS's own storage stack to eject the volume,
// exactly as `diskutil eject disk4` does by hand. That is what this file
// automates.
//
// Picking the right disk is the whole risk. This machine has a 2 TB external
// NVMe that is also USB and also "ejectable", and ejecting a user's drive
// because a Wi-Fi dongle was expected would be an unforgivable way to fail. So
// the match is deliberately narrow and every condition must hold:
//
//   - the device is on USB,
//   - macOS says it is ejectable,
//   - it is smaller than maxZeroCDBytes — a ZeroCD image is a few megabytes and
//     no real storage device is, so this alone separates it from the NVMe by
//     five orders of magnitude,
//   - and a storage-mode dongle is actually present on the USB bus by VID/PID.
//
// If more than one disk matches, it ejects none of them and says so. Guessing
// between candidate disks is not a thing this should do.

// maxZeroCDBytes bounds what can plausibly be a ZeroCD firmware image. The
// observed UGREEN AX900 image is 3.7 MB; 16 MiB leaves room for larger vendors
// while staying far below any real drive.
const maxZeroCDBytes = 16 * 1024 * 1024

type diskutilList struct {
	WholeDisks []string `json:"WholeDisks"`
}

type diskutilInfo struct {
	BusProtocol    string `json:"BusProtocol"`
	Ejectable      bool   `json:"Ejectable"`
	TotalSize      int64  `json:"TotalSize"`
	MediaName      string `json:"MediaName"`
	VolumeName     string `json:"VolumeName"`
	DeviceNode     string `json:"DeviceNode"`
	DeviceIdentifi string `json:"DeviceIdentifier"`
	Internal       bool   `json:"Internal"`
}

// ejectZeroCDVolume finds the dongle's ZeroCD volume and ejects it, which is
// what makes the chip re-enumerate in boot-ROM mode.
//
// Returns the disk identifier it ejected, so the caller can log precisely what
// it acted on rather than "a disk".
func ejectZeroCDVolume() (string, error) {
	disks, err := externalWholeDisks()
	if err != nil {
		return "", err
	}

	var matches []diskutilInfo
	for _, id := range disks {
		info, err := diskInfo(id)
		if err != nil {
			// One unreadable disk should not stop the search, but it should be
			// visible: a disk we could not classify is a disk we did not rule out.
			log.Printf("[USB] diskutil info %s: %v (skipping)", id, err)
			continue
		}
		if info.Internal || !info.Ejectable || info.BusProtocol != "USB" {
			continue
		}
		if info.TotalSize <= 0 || info.TotalSize > maxZeroCDBytes {
			continue
		}
		matches = append(matches, info)
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no ZeroCD-sized USB volume found (checked %d external disks)", len(disks))
	case 1:
		// fall through
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, fmt.Sprintf("%s (%s, %d bytes)", m.DeviceIdentifi, m.MediaName, m.TotalSize))
		}
		return "", fmt.Errorf("refusing to guess: %d disks look like a ZeroCD volume [%s]; "+
			"eject the dongle's one by hand with `diskutil eject <disk>`",
			len(matches), strings.Join(names, ", "))
	}

	m := matches[0]
	id := m.DeviceIdentifi
	if id == "" {
		id = strings.TrimPrefix(m.DeviceNode, "/dev/")
	}
	log.Printf("[USB] ejecting ZeroCD volume %s (%q, %d bytes) to mode-switch", id, m.MediaName, m.TotalSize)
	out, err := exec.Command("diskutil", "eject", id).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("diskutil eject %s: %w: %s", id, err, strings.TrimSpace(string(out)))
	}
	return id, nil
}

func externalWholeDisks() ([]string, error) {
	out, err := exec.Command("diskutil", "list", "-plist", "external", "physical").Output()
	if err != nil {
		return nil, fmt.Errorf("diskutil list: %w", err)
	}
	var l diskutilList
	if err := plistToJSON(out, &l); err != nil {
		return nil, fmt.Errorf("parse diskutil list: %w", err)
	}
	return l.WholeDisks, nil
}

func diskInfo(id string) (diskutilInfo, error) {
	var info diskutilInfo
	out, err := exec.Command("diskutil", "info", "-plist", id).Output()
	if err != nil {
		return info, err
	}
	return info, plistToJSON(out, &info)
}

// plistToJSON converts a diskutil plist to JSON via plutil and decodes it.
//
// diskutil has no JSON output and the repo has no plist dependency; plutil ships
// with macOS and converting is one pipe, which beats adding a parser or writing
// a fragile one.
func plistToJSON(plist []byte, v any) error {
	cmd := exec.Command("plutil", "-convert", "json", "-o", "-", "-")
	cmd.Stdin = strings.NewReader(string(plist))
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("plutil: %w", err)
	}
	return json.Unmarshal(out, v)
}

// switchStorageDongleModeDarwin is the macOS mode-switch.
//
// It still requires a storage-mode dongle to be present by VID/PID before
// touching any disk, so an unrelated small USB volume cannot be ejected just
// because it happened to be plugged in.
func switchStorageDongleModeDarwin() (*DeviceInfo, error) {
	var target *DeviceInfo
	for _, d := range ListWiFiDongles() {
		if isStorageModeVIDPID(d.VendorID, d.ProductID) {
			dd := d
			target = &dd
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("no storage-mode USB Wi-Fi dongle found to mode-switch")
	}

	id, err := ejectZeroCDVolume()
	if err != nil {
		return nil, fmt.Errorf("mode-switch for VID 0x%04x PID 0x%04x: %w",
			target.VendorID, target.ProductID, err)
	}

	log.Printf("[USB] ejected %s; waiting for re-enumeration in boot-ROM mode...", id)
	time.Sleep(2 * time.Second)
	return &DeviceInfo{
		VendorID:  target.VendorID,
		ProductID: target.ProductID,
		Name:      "Wi-Fi Dongle (Post-Modeswitch)",
		IsStorage: false,
		IsWlan:    true,
	}, nil
}
