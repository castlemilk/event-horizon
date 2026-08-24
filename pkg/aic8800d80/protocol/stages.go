package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Stage represents the USB enumeration state of the AIC8800D80 dongle.
type Stage int

const (
	// StageUnknown means the loader hasn't yet determined the chip stage.
	StageUnknown Stage = iota
	// StageZeroCD — fake CD-ROM at VID:PID 1111:1111. Requires a vendor
	// SCSI mode-switch to exit.
	StageZeroCD
	// StageBootROM — boot ROM at VID:PID a69c:8d80. Accepts firmware
	// upload via DBG_MEM_BLOCK_WRITE_REQ.
	StageBootROM
	// StageOperational — operational firmware running at VID:PID
	// a69c:8d81 (WiFi+BT) or a69c:8d83 (WiFi only). Requires a kernel
	// driver to expose as enX.
	StageOperational
)

// String renders the stage enum as a human-readable label.
func (s Stage) String() string {
	switch s {
	case StageZeroCD:
		return "ZeroCD (Stage 0)"
	case StageBootROM:
		return "BootROM (Stage 1)"
	case StageOperational:
		return "Operational (Stage 2)"
	default:
		return "Unknown"
	}
}

// Canonical VID/PID for each stage.
const (
	VID_ZeroCD_Device       uint16 = 0x1111
	PID_ZeroCD_Device       uint16 = 0x1111
	VID_AIC8800D80_Storage  uint16 = 0xa69c
	PID_AIC8800D80_Storage  uint16 = 0x5723
	VID_AIC8800D80_BootROM  uint16 = 0xa69c
	PID_AIC8800D80_BootROM  uint16 = 0x8d80
	VID_AIC8800D80_OpWiFiBT uint16 = 0xa69c
	PID_AIC8800D80_OpWiFiBT uint16 = 0x8d81
	VID_AIC8800D80_OpWiFi   uint16 = 0xa69c
	PID_AIC8800D80_OpWiFi   uint16 = 0x8d83
)

// DetectAICStage checks for the presence of the AIC8800D80 in any of its
// three stages. Returns the stage of the first matching device, with
// priority: Operational > BootROM > ZeroCD.
func DetectAICStage(ctx context.Context) (Stage, error) {
	c, err := Init()
	if err != nil {
		return StageUnknown, err
	}
	defer Deinit(c)

	// Operational takes priority — the device is already usable (kernel
	// driver permitting).
	if hasPID(c, VID_AIC8800D80_OpWiFiBT, PID_AIC8800D80_OpWiFiBT) {
		return StageOperational, nil
	}
	if hasPID(c, VID_AIC8800D80_OpWiFi, PID_AIC8800D80_OpWiFi) {
		return StageOperational, nil
	}
	if hasPID(c, VID_AIC8800D80_BootROM, PID_AIC8800D80_BootROM) {
		return StageBootROM, nil
	}
	if hasPID(c, VID_ZeroCD_Device, PID_ZeroCD_Device) ||
		hasPID(c, VID_AIC8800D80_Storage, PID_AIC8800D80_Storage) {
		return StageZeroCD, nil
	}
	return StageUnknown, errors.New("AIC8800D80 not found on USB bus")
}

// hasPID returns true if at least one device matching vid:pid is on
// the bus.
func hasPID(c *libusbContext, vid, pid uint16) bool {
	locs, err := ListByVIDPID(c, vid, pid)
	return err == nil && len(locs) > 0
}

// WaitForReenumeration polls the USB bus for a device matching the
// target VID/PID. Returns the DeviceLocation of the first match.
func WaitForReenumeration(ctx context.Context, vid, pid uint16, timeout time.Duration, pollInterval time.Duration) (DeviceLocation, error) {
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	c, err := Init()
	if err != nil {
		return DeviceLocation{}, err
	}
	defer Deinit(c)

	for {
		if ctx != nil && ctx.Err() != nil {
			return DeviceLocation{}, ctx.Err()
		}
		locs, err := ListByVIDPID(c, vid, pid)
		if err == nil && len(locs) > 0 {
			return locs[0], nil
		}
		if time.Now().After(deadline) {
			return DeviceLocation{}, fmt.Errorf("device %04x:%04x not seen within %s", vid, pid, timeout)
		}
		select {
		case <-ctx.Done():
			return DeviceLocation{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// FirmwareBundle is the on-disk firmware bundle as expected by the
// loader. Each chip revision has its own set of files.
type FirmwareBundle struct {
	// Chip revision this bundle is for. 1 = U01, 2 = U02, 3 = U03.
	ChipRev uint8
	// Path to the bundle directory.
	Dir string
	// Map of file-name -> file contents.
	files map[string][]byte
}

// LoadFirmwareBundle reads the firmware files for the given chip
// revision from disk and returns them as an in-memory bundle.
func LoadFirmwareBundle(dir string, chipRev uint8) (*FirmwareBundle, error) {
	var names []string
	switch chipRev {
	case ChipRevU01:
		names = []string{
			FWBaseName8800D80,
			FWPatchBaseName8800D80,
			FWAdidBaseName8800D80,
			FWPatchTableName8800D80,
		}
	case ChipRevU02, ChipRevU03, ChipRevU04, ChipRevU05:
		names = []string{
			FWBaseName8800D80U02,
			FWPatchBaseName8800D80U02,
			FWAdidBaseName8800D80U02,
			FWPatchTableName8800D80U02,
		}
	default:
		return nil, fmt.Errorf("unknown chip revision: 0x%02x", chipRev)
	}

	bundle := &FirmwareBundle{
		ChipRev: chipRev,
		Dir:     dir,
		files:   make(map[string][]byte, len(names)),
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		bundle.files[name] = data
	}
	// Also pick up any other .bin blobs present on disk (ext patches, RF fw, etc.)
	matches, _ := filepath.Glob(filepath.Join(dir, "*.bin"))
	for _, path := range matches {
		if _, ok := bundle.files[filepath.Base(path)]; ok {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		bundle.files[filepath.Base(path)] = data
	}
	return bundle, nil
}

// Get returns the contents of a named firmware blob.
func (b *FirmwareBundle) Get(name string) ([]byte, bool) {
	if b == nil {
		return nil, false
	}
	d, ok := b.files[name]
	return d, ok
}
