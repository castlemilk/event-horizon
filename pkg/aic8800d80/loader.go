// Package aic8800d80 implements the user-space firmware loader for the
// AICSEMI AIC8800D80 USB Wi-Fi 6 chipset.
//
// The loader drives the AIC8800D80 from the boot-ROM stage (USB
// VID:PID 0xa69c:0x8d80) into the operational stage (0xa69c:0x8d81 or
// 0xa69c:0x8d83) by uploading the firmware blobs over USB bulk
// transfers. It does not install a kernel driver — that is the
// responsibility of the DriverKit driver described in
// docs/aic8800d80-macos-driver-plan.md.
//
// The loader is the user-space component of the macOS driver stack. It
// follows the protocol reverse-engineered from the Linux driver at
// github.com/radxa-pkg/aic8800.
package aic8800d80

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// Loader is the user-space firmware loader. It is safe to call
// concurrently from multiple goroutines; the underlying libusb context
// is initialised once and reused.
type Loader struct {
	fwDir string
	debug bool

	mu sync.Mutex
}

// LoaderOption configures a Loader.
type LoaderOption func(*Loader)

// WithDebug enables verbose logging of every USB transfer.
func WithDebug() LoaderOption {
	return func(l *Loader) { l.debug = true }
}

// WithFirmwareDir sets the directory containing the firmware blobs.
// Default: ~/.event-horizon/firmware/aic8800D80
func WithFirmwareDir(dir string) LoaderOption {
	return func(l *Loader) { l.fwDir = dir }
}

// NewLoader creates a new loader.
func NewLoader(opts ...LoaderOption) *Loader {
	l := &Loader{
		fwDir: defaultFirmwareDir(),
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

func defaultFirmwareDir() string {
	if home, err := homeDir(); err == nil {
		return home + "/.event-horizon/firmware/aic8800D80"
	}
	return "./firmware/aic8800D80"
}

// LoadFirmwareResult summarises a successful upload.
type LoadFirmwareResult struct {
	FromStage     protocol.Stage
	ToStage       protocol.Stage
	BootROM       bool // true if the device was at BootROM (Stage 1)
	ChipRev       uint8
	ChipMCUID     uint8
	BootAddr      uint32
	BytesUploaded int
	Duration      time.Duration
}

// LoadFirmware drives the dongle from boot ROM to operational. It can
// be called from a device at either the ZeroCD stage (1111:1111) or the
// boot-ROM stage (a69c:8d80); from operational it is a no-op and
// returns immediately.
func (l *Loader) LoadFirmware(ctx context.Context) (*LoadFirmwareResult, error) {
	start := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	stage, err := protocol.DetectAICStage(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect stage: %w", err)
	}

	switch stage {
	case protocol.StageOperational:
		log.Printf("[AIC] device already operational; nothing to do")
		return &LoadFirmwareResult{
			FromStage: protocol.StageOperational,
			ToStage:   protocol.StageOperational,
			Duration:  time.Since(start),
		}, nil
	case protocol.StageZeroCD:
		log.Printf("[AIC] device at ZeroCD; sending mode-switch...")
		if err := l.modeSwitchZeroCD(ctx); err != nil {
			return nil, fmt.Errorf("zeroCD mode-switch: %w", err)
		}
		// After mode-switch, the device re-enumerates as BootROM.
		// Fall through to load firmware.
		stage = protocol.StageBootROM
	}

	res := &LoadFirmwareResult{FromStage: stage, BootROM: true}
	if err := l.uploadFirmware(ctx, res); err != nil {
		return nil, fmt.Errorf("upload firmware: %w", err)
	}
	res.Duration = time.Since(start)
	return res, nil
}

// SwitchToBootROM mode-switches a ZeroCD storage device (a69c:5723 or
// 1111:1111) to the boot-ROM stage (a69c:8d80). Public wrapper for
// tools like the probe.
func (l *Loader) SwitchToBootROM(ctx context.Context) error {
	return l.modeSwitchZeroCD(ctx)
}

// modeSwitchZeroCD sends the proprietary vendor-specific SCSI command
// that flips a ZeroCD storage-mode device (1111:1111 "Pandora" clone or
// a69c:5723 genuine AIC) into the boot-ROM stage (a69c:8d80).
//
// The command (from olamellberg/AIC8800D80 linux/usb_modeswitch/1111_1111)
// is a 16-byte CDB wrapped in a standard 31-byte USB Bulk-Only Mass
// Storage Command Block Wrapper:
//
//	MessageContent="555342431234567800000000000010fd0000000000000000000000000000f2"
//
// Decoded CBW: "USBC" | tag=0x12345678 | dDataLen=0 | flags=0 | LUN=0 |
// bCDBLen=0x10 | CDB[0]=0xFD | CDB[1..14]=0 | CDB[15]=0xF2.
//
// If the vendor command does not trigger re-enumeration, fall back to a
// SCSI START STOP UNIT eject (0x1B, LoEj=1) which many AIC firmware
// builds also honor.
func (l *Loader) modeSwitchZeroCD(ctx context.Context) error {
	c, err := protocol.Init()
	if err != nil {
		return err
	}
	defer protocol.Deinit(c)

	// The storage-mode device may be either the genuine AIC PID or the
	// Pandora clone placeholder. Try both.
	var dev *protocol.USBDevice
	for _, id := range [][2]uint16{
		{protocol.VID_AIC8800D80_Storage, protocol.PID_AIC8800D80_Storage},
		{protocol.VID_ZeroCD_Device, protocol.PID_ZeroCD_Device},
	} {
		if d, err := protocol.OpenByVIDPID(c, id[0], id[1]); err == nil {
			dev = d
			break
		}
	}
	if dev == nil {
		return fmt.Errorf("open ZeroCD storage device (a69c:5723 or 1111:1111): not found")
	}
	defer dev.Close()

	if err := dev.DetachKernelDriver(0); err != nil {
		log.Printf("[AIC] detach kernel driver: %v (continuing)", err)
	}
	if err := dev.ClaimInterface(0); err != nil {
		return fmt.Errorf("claim interface 0: %w", err)
	}
	defer dev.ReleaseInterface(0)

	ep := dev.BulkOutEndpoint()
	if ep == 0 {
		return fmt.Errorf("storage device has no bulk OUT endpoint")
	}

	// Candidate commands: vendor FD..F2 first, then SCSI eject.
	vendorCBW := make([]byte, 31)
	copy(vendorCBW[0:4], []byte("USBC"))
	vendorCBW[4], vendorCBW[5], vendorCBW[6], vendorCBW[7] = 0x78, 0x56, 0x34, 0x12 // tag
	// bytes 8..11 = dataTransferLength = 0
	vendorCBW[12] = 0x00 // flags: host-to-device
	vendorCBW[13] = 0x00 // LUN
	vendorCBW[14] = 0x10 // bCDBLength = 16
	vendorCBW[15] = 0xFD // vendor opcode
	vendorCBW[30] = 0xF2 // AIC magic (last CDB byte)

	ejectCBW := make([]byte, 31)
	copy(ejectCBW[0:4], []byte("USBC"))
	ejectCBW[4], ejectCBW[5], ejectCBW[6], ejectCBW[7] = 0x78, 0x56, 0x34, 0x12
	ejectCBW[12] = 0x00
	ejectCBW[13] = 0x00
	ejectCBW[14] = 0x06 // bCDBLength = 6
	ejectCBW[15] = 0x1B // START STOP UNIT
	ejectCBW[19] = 0x02 // LoEj=1 (load/eject the medium)

	for _, cmd := range []struct {
		name string
		cbw  []byte
	}{
		{"vendor FD..F2", vendorCBW},
		{"scsi eject", ejectCBW},
	} {
		log.Printf("[AIC] sending mode-switch (%s) to storage device", cmd.name)
		if _, err := dev.BulkSend(ep, cmd.cbw, 5000); err != nil {
			log.Printf("[AIC] %s send failed: %v (continuing)", cmd.name, err)
			continue
		}
		loc, err := protocol.WaitForReenumeration(ctx,
			protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM,
			6*time.Second, 200*time.Millisecond)
		if err == nil {
			log.Printf("[AIC] device re-enumerated as boot ROM a69c:8d80 at bus %d addr %d",
				loc.Bus, loc.Address)
			return nil
		}
		log.Printf("[AIC] %s did not trigger re-enumeration (%v)", cmd.name, err)
	}
	return fmt.Errorf("mode-switch failed: device did not re-enumerate as a69c:8d80")
}

// uploadFirmware opens the device at boot-ROM stage and uploads the
// required firmware blobs. The exact upload sequence matches the Linux
// driver (aic_compat_8800d80.c:aicfw_download_fw_8800d80).
func (l *Loader) uploadFirmware(ctx context.Context, res *LoadFirmwareResult) error {
	resRunStart := time.Now()
	c, err := protocol.Init()
	if err != nil {
		return err
	}
	defer protocol.Deinit(c)

	dev, err := protocol.OpenByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM)
	if err != nil {
		return fmt.Errorf("open boot-ROM device: %w", err)
	}
	// Closure defers reference the dev VARIABLE so a device swap (port
	// reset path below) releases the current device exactly once.
	devReleased := false
	defer func() {
		if !devReleased {
			dev.ReleaseInterface(0)
		}
		dev.Close()
	}()

	if err := dev.DetachKernelDriver(0); err != nil {
		log.Printf("[AIC] detach kernel driver: %v (continuing)", err)
	}
	if err := dev.ClaimInterface(0); err != nil {
		return fmt.Errorf("claim interface 0: %w", err)
	}

	// Give the freshly-enumerated boot ROM a moment to settle.
	time.Sleep(300 * time.Millisecond)

	// Calibrate the TX length-field convention with a recovery ladder.
	// Each rung re-attempts a DBG_MEM_READ of the system-config register:
	//
	//   1. drain stale CFMs, probe convention 0 (Linux default)
	//   2. USB port reset (revives a hung-but-alive ROM), probe conv 1
	//   3. chip reboot — fire-and-forget DBG_START_APP(HOST_START_APP_REBOOT),
	//      the same recovery Linux sends on probe for bad-state devices.
	//      Wait for re-enumeration (boot ROM or ZeroCD storage), mode-switch
	//      back if needed, reopen, probe conv 0 again.
	const memAddr = 0x40500000
	var chipID, chipMCUID uint8
	probe := func(conv protocol.TxLenConv) (uint32, error) {
		protocol.SetTxLenConv(conv)
		protocol.Drain(dev, 16)
		log.Printf("[AIC] probing TX length convention %v ...", conv)
		cfm, err := protocol.SendRequest(dev, protocol.DBGMemReadReq, protocol.TaskDBG, protocol.MemReadPayload(memAddr), 8000)
		if err != nil {
			return 0, err
		}
		_, val, perr := protocol.ParseMemReadCfm(cfm)
		return val, perr
	}
	accept := func(val uint32, conv protocol.TxLenConv) {
		log.Printf("[AIC] convention %v answered: 0x40500000 = 0x%08x", conv, val)
		chipID = uint8((val >> 16) & 0xFF)
		// Linux system_config_8800d80: chip_mcu_id = 1 when bit 25 is CLEAR.
		if (val>>25)&0x01 == 0 {
			chipMCUID = 1
		}
	}
	reopenBootROM := func() error {
		time.Sleep(1200 * time.Millisecond)
		nd, oerr := protocol.OpenByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM)
		if oerr != nil {
			return oerr
		}
		dev = nd
		devReleased = false
		if cerr := dev.ClaimInterface(0); cerr != nil {
			return cerr
		}
		return nil
	}
	closeCurrent := func() {
		if !devReleased {
			dev.ReleaseInterface(0)
			devReleased = true
		}
		dev.Close()
	}

	calibrated := false
	// Rung 1: as-is.
	if val, err := probe(protocol.ConvLinux); err == nil {
		accept(val, protocol.ConvLinux)
		calibrated = true
	} else {
		log.Printf("[AIC] convention 0 failed: %v", err)
	}

	// Rung 2: USB port reset.
	if !calibrated {
		log.Printf("[AIC] device silent — attempting USB port reset")
		closeCurrent()
		// Port reset needs an open handle; reopen just for the reset.
		if rd, oerr := protocol.OpenByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM); oerr == nil {
			if rerr := rd.ResetDevice(); rerr != nil {
				log.Printf("[AIC] port reset failed: %v", rerr)
			} else {
				log.Printf("[AIC] port reset OK")
			}
			rd.Close()
		}
		if err := reopenBootROM(); err != nil {
			return fmt.Errorf("reopen after port reset: %w (unplug/replug the dongle)", err)
		}
		if val, err := probe(protocol.ConvTotal); err == nil {
			accept(val, protocol.ConvTotal)
			calibrated = true
		} else {
			log.Printf("[AIC] convention 1 failed: %v", err)
		}
	}

	// Rung 3: chip reboot via DBG_START_APP(HOST_START_APP_REBOOT).
	if !calibrated {
		preAddr := dev.Location()
		log.Printf("[AIC] attempting chip reboot (START_APP REBOOT) — current addr %d-%d",
			preAddr.Bus, preAddr.Address)
		if err := protocol.StartApp(dev, 2000, protocol.HostStartAppReboot); err != nil {
			log.Printf("[AIC] reboot command send failed: %v", err)
		}
		closeCurrent()
		// The chip re-enumerates as boot ROM (8d80) or ZeroCD (5723).
		// A changed USB address proves the reboot actually happened —
		// matching the old address would just find the stale enumeration.
		var saw [2]uint16
		found := false
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && !found {
			// Prefer the storage PID: any 5723 sighting counts.
			if locs, lerr := protocol.ListByVIDPID(c, protocol.VID_AIC8800D80_Storage, protocol.PID_AIC8800D80_Storage); lerr == nil && len(locs) > 0 {
				saw = [2]uint16{protocol.VID_AIC8800D80_Storage, protocol.PID_AIC8800D80_Storage}
				found = true
				break
			}
			if locs, lerr := protocol.ListByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM); lerr == nil {
				for _, loc := range locs {
					if loc.Address != preAddr.Address || loc.Bus != preAddr.Bus {
						saw = [2]uint16{protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM}
						found = true
						break
					}
				}
			}
			if !found {
				time.Sleep(300 * time.Millisecond)
			}
		}
		if !found {
			return fmt.Errorf("chip reboot: device did not re-enumerate within 10s — power-cycle required: unplug the dongle (and its hub) for ~5s, then plug it directly into the Mac")
		}
		log.Printf("[AIC] chip rebooted — re-enumerated as %04x:%04x", saw[0], saw[1])
		if saw[1] == protocol.PID_AIC8800D80_Storage {
			if err := l.modeSwitchZeroCD(ctx); err != nil {
				return fmt.Errorf("post-reboot mode-switch: %w", err)
			}
		}
		if err := reopenBootROM(); err != nil {
			return fmt.Errorf("reopen after chip reboot: %w", err)
		}
		if val, err := probe(protocol.ConvLinux); err == nil {
			accept(val, protocol.ConvLinux)
			calibrated = true
		} else {
			log.Printf("[AIC] post-reboot probe failed: %v", err)
		}
	}

	if !calibrated {
		return fmt.Errorf("read system config: device did not answer DBG_MEM_READ even after port reset and chip reboot — power-cycle required: unplug the dongle (and the hub it sits on) for ~5s, then plug it directly into the Mac")
	}
	res.ChipRev = chipID
	res.ChipMCUID = chipMCUID
	log.Printf("[AIC] chip_id=0x%02x chip_mcu_id=%d", chipID, chipMCUID)

	// MCU1 cache fix (Radxa SDK V3 loader system_config_8800d80,
	// shenmintao PR #35 + issue #58): the older MCU (chip_mcu_id=1)
	// must have bit 0 of 0x40100020 set before firmware upload, else
	// DBG_MEM_WRITE misbehaves / returns bogus confirmations.
	if chipMCUID == 1 {
		cacheReg, err := protocol.MemRead(dev, 0x40100020)
		if err != nil {
			return fmt.Errorf("mcu1 cache fix: read 0x40100020: %w", err)
		}
		if err := protocol.MemWrite(dev, 0x40100020, cacheReg|0x01); err != nil {
			return fmt.Errorf("mcu1 cache fix: write 0x40100020: %w", err)
		}
		log.Printf("[AIC] mcu1 cache fix: 0x40100020 = 0x%08x | 0x01 = 0x%08x", cacheReg, cacheReg|0x01)
	}

	// Stop hardware watchdogs (syscfg_tbl_8800d80) so the chip doesn't reset mid-download
	_ = protocol.MemWrite(dev, 0x70001408, 0x00000000)
	_ = protocol.MemWrite(dev, 0x50017008, 0x00000000)

	// Load firmware blobs.
	bundle, err := protocol.LoadFirmwareBundle(l.fwDir, chipID)
	if err != nil {
		return fmt.Errorf("load firmware bundle: %w", err)
	}

	var ramFMACFW uint32
	switch chipID {
	case protocol.ChipRevU01:
		ramFMACFW = protocol.RAMFMACFWAddr8800D80
	case protocol.ChipRevU02, protocol.ChipRevU03, protocol.ChipRevU04, protocol.ChipRevU05:
		ramFMACFW = protocol.RAMFMACFWAddr8800D80U02
	default:
		return fmt.Errorf("unsupported chip revision 0x%02x", chipID)
	}

	// Parse the patch table FIRST — it carries the REAL RAM addresses
	// (Linux: aicbt_patch_table_alloc + aicbt_patch_info_unpack). The
	// hardcoded constants are only defaults.
	tableName, _ := bundleNameFor(chipID, "patch_table")
	tableBlob, ok := bundle.Get(tableName)
	if !ok {
		return fmt.Errorf("missing %s in firmware bundle", tableName)
	}
	tables, err := protocol.ParsePatchTable(tableBlob)
	if err != nil {
		return fmt.Errorf("parse %s: %w", tableName, err)
	}
	pi, err := protocol.UnpackPatchInfo(tables)
	if err != nil {
		return fmt.Errorf("unpack patch info: %w", err)
	}
	log.Printf("[AIC] patch info: addr_adid=0x%08x addr_patch=0x%08x reset@0x%08x=0x%08x adid_flag@0x%08x=0x%08x ext_nb=%d",
		pi.AddrAdid, pi.AddrPatch, pi.ResetAddr, pi.ResetVal, pi.AdidFlagAddr, pi.AdidFlag, pi.ExtPatchNb)

	// Drain any stale confirmations queued from previous sessions —
	// otherwise they desync pairing and can fill the device TX FIFO.
	protocol.Drain(dev, 16)

	// 1. Upload ADID blob to the address from the patch info.
	adidName, _ := bundleNameFor(chipID, "adid")
	adid, ok := bundle.Get(adidName)
	if !ok {
		return fmt.Errorf("missing %s in firmware bundle", adidName)
	}
	log.Printf("[AIC] uploading %s (%d bytes) to 0x%x", adidName, len(adid), pi.AddrAdid)
	if err := protocol.MemBlockWriteAll(dev, pi.AddrAdid, adid, func(written int) {
		res.BytesUploaded += written
	}); err != nil {
		return fmt.Errorf("upload adid: %w", err)
	}

	// 2. Upload PATCH blob to the address from the patch info.
	patchName, _ := bundleNameFor(chipID, "patch")
	patch, ok := bundle.Get(patchName)
	if !ok {
		return fmt.Errorf("missing %s in firmware bundle", patchName)
	}
	log.Printf("[AIC] uploading %s (%d bytes) to 0x%x", patchName, len(patch), pi.AddrPatch)
	if err := protocol.MemBlockWriteAll(dev, pi.AddrPatch, patch, func(written int) {
		res.BytesUploaded += written
	}); err != nil {
		return fmt.Errorf("upload patch: %w", err)
	}

	// 3. Upload supplementary ext patch blobs (aicbt_ext_patch_data_load).
	extPrefix := protocol.FWPatchBaseName8800D80U02Ext
	if chipID == protocol.ChipRevU01 {
		extPrefix = "fw_patch_8800d80_ext"
	}
	for i := 0; i < int(pi.ExtPatchNb); i++ {
		extName := fmt.Sprintf("%s%d.bin", extPrefix, pi.ExtPatchID[i])
		extBlob, ok := bundle.Get(extName)
		if !ok {
			log.Printf("[AIC] WARNING: ext patch %s missing — skipping (device may misbehave)", extName)
			continue
		}
		log.Printf("[AIC] uploading %s (%d bytes) to 0x%x", extName, len(extBlob), pi.ExtPatchAddr[i])
		if err := protocol.MemBlockWriteAll(dev, pi.ExtPatchAddr[i], extBlob, func(written int) {
			res.BytesUploaded += written
		}); err != nil {
			return fmt.Errorf("upload %s: %w", extName, err)
		}
	}

	// 4. Apply the patch table itself (aicbt_patch_table_load): every
	// (addr, value) pair via DBG_MEM_WRITE, with the BT-mode table
	// overridden by driver defaults, and a 100ms settle after PWRON.
	if err := applyPatchTables(dev, tables); err != nil {
		return fmt.Errorf("apply patch table: %w", err)
	}

	// 5. Upload fmacfw blob (the main Wi-Fi firmware).
	//
	// Clone-adaptive plan (all quirks hardware-verified 2026-08-16..18):
	//   - 1KB block writes work below 0x170000, wedge at/above it
	//   - 16B block writes work AND retain at/above 0x170000, EXCEPT
	//     within ~±0x30 of the register blocks at 0x2220 stride
	//     (0x1701e0, 0x172400, 0x174620, 0x176840) where the ROM wedges —
	//     6/6 runs died at zone-adjacent addresses, drifting per run
	// So: 1KB chunks to the wall, then 16B chunks past it, skipping the
	// register zones, with per-chunk readback verification past the wall.
	// AIC_SKIP_WINDOW=1 loads only the 1KB phase (boot-feasibility
	// experiment — zero wedge risk).
	fmacName, _ := bundleNameFor(chipID, "fmacfw")
	fmac, ok := bundle.Get(fmacName)
	if !ok {
		return fmt.Errorf("missing %s in firmware bundle", fmacName)
	}

	isU02Layout := ramFMACFW == protocol.RAMFMACFWAddr8800D80U02 && chipID != protocol.ChipRevU01
	// V3-generation image (Radxa SDK V3 / legacy-mcu1, issue #58): the
	// FMAC ends entirely below the 0x170000 wall — no window phase, no
	// write budget, and a 2-pair patch config with a fixed pair buffer.
	v3Profile := isU02Layout && len(fmac) < int(protocol.CloneWallAddr-ramFMACFW)
	if v3Profile {
		log.Printf("[AIC] V3-generation firmware (%d bytes): entirely below the 0x%x wall — no window phase", len(fmac), protocol.CloneWallAddr)
	}
	wall := uint32(0xFFFFFFFF)
	zones := []protocol.SkipZone{}
	chunk := protocol.CloneSmallChunk
	wordMode := false
	hybrid := false
	hybridWindowEnd := uint32(0x172390)
	if isU02Layout {
		wall = protocol.CloneWallAddr
		switch {
		case os.Getenv("AIC_HYBRID") != "":
			// RETENTION-MAPPED HYBRID (2026-08-20): write each 16B window
			// chunk via the path that retains (word or 16B), mapped by
			// --retention-map → /tmp/aic-retention.txt. Loads the trimmed
			// prefix 0x170000..0x172390 with FULL retention; 9,024 B +
			// 2,720 B (1KB phase) = 11,744 B, the 12:50-run geometry
			// proven alive through patch config + START_APP.
			zones = protocol.CloneRegZones()
			chunk = protocol.CloneSmallChunk
			wordMode = true
			hybrid = true
			log.Printf("[AIC] HYBRID RETENTION MODE: word/16B per retention map, prefix 0x%x..0x%x", wall, hybridWindowEnd)
		case os.Getenv("AIC_WINDOW_1KB") != "":
			// VERIFIED 2026-08-18: 1KB writes work above the wall (the
			// old "wall" was block1's halo). The ROM NAKs hard in the
			// register window (~460ms/op) and the verify read at
			// 0x172830 wedged it — this mode is a probe, not a keeper.
			zones = protocol.CloneRegZones()
			chunk = protocol.BlockWriteChunkBytes
			log.Printf("[AIC] REGISTER-WINDOW 1KB MODE: %dB chunks above 0x%x, halo zones %v", chunk, wall, zones)
		case os.Getenv("AIC_WINDOW_1KB") != "":
			zones = protocol.CloneRegZones()
			chunk = protocol.BlockWriteChunkBytes
			log.Printf("[AIC] REGISTER-WINDOW 1KB MODE: %dB chunks above 0x%x, halo zones %v", chunk, wall, zones)
		case os.Getenv("AIC_WINDOW_16B") != "":
			zones = protocol.CloneRegZones()
			chunk = protocol.CloneSmallChunk
			log.Printf("[AIC] REGISTER-WINDOW 16B MODE: %dB chunks above 0x%x", chunk, wall)
		case os.Getenv("AIC_SKIP_WINDOW") != "":
			zones = []protocol.SkipZone{{Start: wall, End: 0xFFFFFFFF}}
			log.Printf("[AIC] REGISTER-WINDOW SKIP MODE: loading only the %d bytes below 0x%x (boot feasibility experiment)",
				int(wall)-int(ramFMACFW), wall)
		default:
			zones = protocol.CloneRegZones()
			chunk = 256
			wordMode = false
			log.Printf("[AIC] DEFAULT MODE: 1KB block writes below 0x%x, 256B block writes above with widened USB descriptor zones skipped", wall)
		}
		// Probe-driven zone overrides (hex): the wedge-zone boundaries
		// past block2..4 are still being mapped. The ~9.1KB window-write
		// budget (2026-08-19 runs) means AIC_ZONE_START2/END2 trade
		// loaded window bytes against ROM survival; --probe-window and
		// the budget runs calibrate them.
		if len(zones) >= 4 {
			for i, name := range []string{"AIC_ZONE_START1", "AIC_ZONE_START2", "AIC_ZONE_START3", "AIC_ZONE_START4"} {
				if v := os.Getenv(name); v != "" {
					if start, err := strconv.ParseUint(v, 0, 32); err == nil {
						zones[i].Start = uint32(start)
					} else {
						return fmt.Errorf("bad %s %q: %w", name, v, err)
					}
				}
			}
			for i, name := range []string{"AIC_ZONE_END1", "AIC_ZONE_END2", "AIC_ZONE_END3", "AIC_ZONE_END4"} {
				if v := os.Getenv(name); v != "" {
					if end, err := strconv.ParseUint(v, 0, 32); err == nil {
						zones[i].End = uint32(end)
					} else {
						return fmt.Errorf("bad %s %q: %w", name, v, err)
					}
				}
			}
		}
		log.Printf("[AIC] uploading %s (%d bytes) to 0x%x — adaptive: 1KB below 0x%x, %dB above, register zones %v skipped",
			fmacName, len(fmac), ramFMACFW, wall, chunk, zones)
		zones = protocol.MergePoisonZones(zones)
		log.Printf("[AIC] zones after poison merge: %v", zones)
	} else {
		log.Printf("[AIC] uploading %s (%d bytes) to 0x%x (plain 1KB chunks)", fmacName, len(fmac), ramFMACFW)
	}

	ops := protocol.PlanAdaptiveUpload(ramFMACFW, fmac, wall, zones, chunk)
	if os.Getenv("AIC_SKIP_BELOW_WALL") != "" {
		// Accumulation cycles: the below-wall image persists across
		// START_APP(REBOOT)s, so later slices skip the 1KB phase and
		// spend the whole ~11.8KB budget on window bytes.
		var wops []protocol.ChunkOp
		for _, op := range ops {
			if op.Addr >= wall {
				wops = append(wops, op)
			}
		}
		log.Printf("[AIC] skipping below-wall phase: %d window ops (%d bytes)", len(wops), len(wops)*int(chunk))
		ops = wops
	}
	if hybrid {
		classes, err := loadRetentionClasses(os.Getenv("AIC_RETENTION_FILE"))
		if err != nil {
			return fmt.Errorf("hybrid mode: %w", err)
		}
		ops = buildHybridOps(fmac, ramFMACFW, wall, hybridWindowEnd, classes)
		log.Printf("[AIC] hybrid plan: %d ops (%d below-wall 1KB + %d window chunks), total %d bytes",
			len(ops), 320, len(ops)-320, 320*1024+(len(ops)-320)*16)
	}
	var holes []uint32 // addresses whose readback never matched
	writtenTotal := 0
	lastMark := 0

	for i, op := range ops {
		opStart := time.Now()
		if wordMode && len(op.Block) == 4 && op.Addr >= wall {
			if err := protocol.MemWrite(dev, op.Addr, binary.LittleEndian.Uint32(op.Block)); err != nil {
				return fmt.Errorf("upload fmacfw: op %d/%d (0x%08x, word write): %w", i+1, len(ops), op.Addr, err)
			}
		} else {
			if err := protocol.MemBlockWrite(dev, op.Addr, op.Block); err != nil {
				return fmt.Errorf("upload fmacfw: op %d/%d (0x%08x, %dB): %w", i+1, len(ops), op.Addr, len(op.Block), err)
			}
		}
		if op.Addr >= wall {
			time.Sleep(100 * time.Millisecond)
		}
		writtenTotal += len(op.Block)
		res.BytesUploaded += len(op.Block)
		if writtenTotal-lastMark >= 64*1024 {
			lastMark = writtenTotal
			log.Printf("[AIC] fmacfw progress: %d / %d bytes", writtenTotal, len(fmac))
		}
		// Match Linux driver: do not interleave verify reads during firmware upload (interleaved reads can wedge the BootROM)
		if os.Getenv("AIC_VERIFY_WRITES") != "" && op.Addr >= protocol.CloneVerifySafeAddr && len(op.Block) >= 4 {
			want := uint32(op.Block[0]) | uint32(op.Block[1])<<8 | uint32(op.Block[2])<<16 | uint32(op.Block[3])<<24
			got, rerr := protocol.MemRead(dev, op.Addr)
			if rerr != nil {
				return fmt.Errorf("upload fmacfw: verify read at 0x%08x: %w", op.Addr, rerr)
			}
			if got != want {
				_ = protocol.MemBlockWrite(dev, op.Addr, op.Block)
				if got2, rerr2 := protocol.MemRead(dev, op.Addr); rerr2 == nil && got2 != want {
					holes = append(holes, op.Addr)
				}
			}
		}
		// The clone's ROM can degrade gradually before wedging (ops slow
		// from ~0.4ms to NAK-storm territory — observed 45ms/op before a
		// final wedge). Log the curve so a failed run shows where it
		// started dying.
		if d := time.Since(opStart); d > time.Second {
			log.Printf("[AIC] op %d/%d (0x%08x) slow: %v", i+1, len(ops), op.Addr, d.Round(time.Millisecond))
		}
	}
	log.Printf("[AIC] fmacfw upload complete: %d/%d ops, %d bytes placed, %d verification hole(s)",
		len(ops), len(ops), writtenTotal, len(holes))
	if len(holes) > 0 {
		log.Printf("[AIC] WARNING: %d bytes did not retain: %v ... (firmware may misbehave)",
			len(holes)*protocol.CloneSmallChunk, holes[:min(len(holes), 8)])
	}

	// 6. Firmware patch config (aicwf_patch_config_8800d80): writes the
	// "PTCH/HCTP" magic descriptor and the config patch pairs.
	// AIC_SKIP_PATCH_CONFIG=1: the clone's ROM dies ~ms after the last
	// register-window write (the ~9.1KB budget), so the descriptor
	// write would time out — skip it and race START_APP in instead.
	if chipID != protocol.ChipRevU01 {
		if os.Getenv("AIC_SKIP_PATCH_CONFIG") != "" {
			log.Printf("[AIC] skipping patch config (AIC_SKIP_PATCH_CONFIG=1) — racing START_APP before the ROM dies")
		} else {
			if err := applyPatchConfig(dev, ramFMACFW, fmac, v3Profile); err != nil {
				return fmt.Errorf("patch config: %w", err)
			}
		}
	}

	// 7. Boot the firmware.
	if os.Getenv("AIC_END_RESET") != "" {
		// Accumulation via USB PORT RESET (2026-08-20): the ~11.8KB
		// write budget survives START_APP(REBOOT) (measured: slice 2 died
		// at op 390, cumulative ~13.3KB). A libusb port reset re-inits
		// the ROM's USB controller without touching the chip core — if
		// the budget lives in the controller, it clears here while the
		// loaded SRAM survives. Next cycle loads the next slice.
		log.Printf("[AIC] END-RESET: USB port reset — budget clear + SRAM survive is the question")
		if err := dev.ResetDevice(); err != nil {
			return fmt.Errorf("end-reset: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
		nd, oerr := protocol.OpenByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM)
		if oerr != nil {
			return fmt.Errorf("end-reset: reopen boot ROM: %w", oerr)
		}
		dev = nd
		devReleased = false
		if cerr := dev.ClaimInterface(0); cerr != nil {
			return fmt.Errorf("end-reset: claim interface: %w", cerr)
		}
		protocol.SetTxLenConv(protocol.ConvLinux)
		protocol.Drain(dev, 16)
		if v, perr := protocol.MemRead(dev, 0x40500000); perr != nil {
			log.Printf("[AIC] end-reset: DBG probe FAILED: %v — port-reset path dead", perr)
		} else {
			log.Printf("[AIC] end-reset: DBG alive (0x40500000 = 0x%08x) — port-reset path usable", v)
		}
		res.ToStage = protocol.StageBootROM
		res.BootAddr = 0
		res.Duration = time.Since(resRunStart)
		log.Printf("[AIC] end-reset done (%s, %d bytes uploaded)", res.Duration, res.BytesUploaded)
		log.Printf("[AIC] SRAM/budget check — run: sudo ./bin/usbwifi aicloader --kill-daemon --probe")
		return nil
	}
	if os.Getenv("AIC_END_REBOOT") != "" {
		// Accumulation cycle (2026-08-20): reboot the ROM instead of
		// running the app. Unlike a firmware crash-back (DBG fully dead),
		// the ROM's own START_APP(REBOOT) comes back with a LIVE DBG —
		// and possibly a PRESERVED SRAM (0x120000+). The next cycle
		// loads the next window slice (AIC_SKIP_BELOW_WALL + zone
		// overrides); the probe then verifies whether earlier slices
		// survived the reboot.
		preAddr := dev.Location()
		log.Printf("[AIC] END-REBOOT: sending START_APP(REBOOT) — will SRAM 0x%x+ survive?", ramFMACFW)
		if err := protocol.StartApp(dev, 2000, protocol.HostStartAppReboot); err != nil {
			return fmt.Errorf("end-reboot: %w", err)
		}
		res.ToStage = protocol.StageBootROM
		res.BootAddr = 0
		var saw [2]uint16
		found := false
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && !found {
			if locs, lerr := protocol.ListByVIDPID(c, protocol.VID_AIC8800D80_Storage, protocol.PID_AIC8800D80_Storage); lerr == nil && len(locs) > 0 {
				saw = [2]uint16{protocol.VID_AIC8800D80_Storage, protocol.PID_AIC8800D80_Storage}
				found = true
				break
			}
			if locs, lerr := protocol.ListByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM); lerr == nil {
				for _, loc := range locs {
					if loc.Address != preAddr.Address || loc.Bus != preAddr.Bus {
						saw = [2]uint16{protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM}
						found = true
						break
					}
				}
			}
			if !found {
				time.Sleep(300 * time.Millisecond)
			}
		}
		if !found {
			// A same-address re-enumeration is indistinguishable from
			// "no reboot" — the watch's address-change guard can't prove
			// movement. Don't hard-fail: the DBG may still be alive with
			// the SRAM intact (observed 16:49: probe opened the device
			// fine after this timeout). Reopen and check.
			log.Printf("[AIC] end-reboot: no re-enumeration observed in 10s (same-address reboot is indistinguishable from none)")
		} else {
			log.Printf("[AIC] end-reboot: re-enumerated as %04x:%04x", saw[0], saw[1])
			if saw[1] == protocol.PID_AIC8800D80_Storage {
				if err := l.modeSwitchZeroCD(ctx); err != nil {
					return fmt.Errorf("post-reboot mode-switch: %w", err)
				}
			}
		}
		nd, oerr := protocol.OpenByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM)
		if oerr != nil {
			return fmt.Errorf("end-reboot: reopen boot ROM: %w", oerr)
		}
		dev = nd
		devReleased = false
		if cerr := dev.ClaimInterface(0); cerr != nil {
			return fmt.Errorf("end-reboot: claim interface: %w", cerr)
		}
		protocol.SetTxLenConv(protocol.ConvLinux)
		protocol.Drain(dev, 16)
		if v, perr := protocol.MemRead(dev, 0x40500000); perr != nil {
			log.Printf("[AIC] end-reboot: DBG probe FAILED: %v — reboot path dead", perr)
		} else {
			log.Printf("[AIC] end-reboot: DBG alive (0x40500000 = 0x%08x) — reboot path usable for accumulation", v)
		}
		res.Duration = time.Since(resRunStart)
		log.Printf("[AIC] end-reboot done (%s, %d bytes uploaded)", res.Duration, res.BytesUploaded)
		log.Printf("[AIC] SRAM survival check — run: sudo ./bin/usbwifi aicloader --kill-daemon --probe")
		return nil
	}
	log.Printf("[AIC] starting firmware at 0x%x", ramFMACFW)
	if err := protocol.StartApp(dev, ramFMACFW, protocol.HostStartAppAuto); err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	res.ToStage = protocol.StageOperational
	res.BootAddr = ramFMACFW

	// 8. Wait for re-enumeration as operational. The 1.5–3 s delay
	// observed on Linux applies here.
	_, err = protocol.WaitForReenumeration(ctx,
		protocol.VID_AIC8800D80_OpWiFiBT, protocol.PID_AIC8800D80_OpWiFiBT,
		10*time.Second, 250*time.Millisecond)
	if err != nil {
		// Try the WiFi-only operational PID.
		alt, altErr := protocol.WaitForReenumeration(ctx,
			protocol.VID_AIC8800D80_OpWiFi, protocol.PID_AIC8800D80_OpWiFi,
			2*time.Second, 250*time.Millisecond)
		if altErr != nil {
			// Diagnose the failure mode: a firmware crash usually resets
			// the chip back to BootROM (fast iteration possible); a
			// totally silent device needs a power cycle.
			if _, boErr := protocol.OpenByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM); boErr == nil {
				log.Printf("[AIC] firmware did not boot — device is back in boot ROM (crash/reset); can re-upload without a power cycle")
				return fmt.Errorf("waiting for operational re-enumeration: firmware crashed back to boot ROM: %w", err)
			}
			return fmt.Errorf("waiting for operational re-enumeration: %w (device silent — power-cycle required)", err)
		}
		_ = alt
	}
	log.Printf("[AIC] device operational after %s (%d bytes uploaded)", res.Duration, res.BytesUploaded)
	return nil
}

// applyPatchTables mirrors aicbt_patch_table_load: write every (addr,
// value) pair, override the BT-mode table with driver defaults, pause
// 100ms after PWRON entries.
//
// CRITICAL Linux parity detail: aicbt_patch_info_unpack TRUNCATES the
// INF table (type 0) to its first 4 pairs (base_len) — pairs 5+ are
// metadata (ext_patch_nb and the ext (id,addr) list), NOT memory
// writes. Writing them sends a bogus value to ADDRESS ZERO, corrupting
// the boot ROM's memory map — observed on hardware as error frame
// 0xf105 and a halt exactly at the 0x170000 fmacfw window boundary.
func applyPatchTables(dev *protocol.USBDevice, tables []protocol.PatchTable) error {
	return applyPatchTablesTo(func(addr, val uint32) error {
		return protocol.MemWrite(dev, addr, val)
	}, tables)
}

// applyPatchTablesTo is the testable core of applyPatchTables: applies
// every (addr, value) pair via the supplied writer.
func applyPatchTablesTo(write func(addr, val uint32) error, tables []protocol.PatchTable) error {
	for _, t := range tables {
		pairs := t.Data
		if t.Type == protocol.AICBTPTInf {
			// Log the full INF table for diagnostics before truncating.
			for i := 0; i+1 < len(t.Data); i += 2 {
				log.Printf("[AIC] INF pair %d: write 0x%08x = 0x%08x", i/2, t.Data[i], t.Data[i+1])
			}
			const baseLen = 4
			n := len(pairs) / 2
			if n > baseLen {
				log.Printf("[AIC] INF table: writing only first %d pairs (skipping %d metadata pairs)", baseLen, n-baseLen)
				pairs = pairs[:baseLen*2]
			}
		}
		if t.Type == protocol.AICBTPTBTMode {
			// D80 defaults (aicbt_info[PRODUCT_ID_AIC8800D80]):
			// btmode=BT_ONLY_COANT(5), btport=MB(1), baud=1.5M,
			// flowctrl=1, lpm=0, txpwr=0x00006F2F; bsp hwinfo=-1,
			// cpmode=WORK(0).
			if len(t.Data) >= 18 {
				t.Data[1] = 1 // hwinfo < 0
				t.Data[3] = 0xFFFFFFFF
				t.Data[5] = 0 // cpmode WORK
				t.Data[7] = 5 // btmode BT_ONLY_COANT
				t.Data[9] = 1 // btport MB
				t.Data[11] = 1500000
				t.Data[13] = 1 // uart flowctrl enable
				t.Data[15] = 0 // lpm disable
				t.Data[17] = 0x00006F2F
			}
		}
		if t.Type == 0x06 {
			// patch version string — informational only
			log.Printf("[AIC] patch version: %q", string(u32sToBytes(t.Data)))
			continue
		}
		for i := 0; i+1 < len(pairs); i += 2 {
			if err := write(pairs[i], pairs[i+1]); err != nil {
				return fmt.Errorf("table %q pair %d (0x%08x): %w", t.Name, i/2, pairs[i], err)
			}
		}
		if t.Type == protocol.AICBTPTPWRON {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return nil
}

// applyPatchConfig mirrors aicwf_patch_config_8800d80: locate the patch
// descriptor inside the (already uploaded) fmacfw image via DBG_MEM_READ,
// write the PTCH/HCTP magics + pair list, then zero the block sizes.
// loadRetentionClasses reads the --retention-map output: one
// "0x<addr> <class>" line per 16B address, class ∈ {both, word, blk,
// none}. Missing addresses default to word writes (best effort).
func loadRetentionClasses(path string) (map[uint32]string, error) {
	if path == "" {
		path = "/tmp/aic-retention.txt"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read retention map %s: %w", path, err)
	}
	classes := map[uint32]string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		addr, err := strconv.ParseUint(fields[0][2:], 16, 32)
		if err != nil {
			continue
		}
		classes[uint32(addr)] = fields[1]
	}
	return classes, nil
}

// buildHybridOps lays out the hybrid plan: 1KB chunks below the wall,
// then 16B-aligned window chunks in [wall, windowEnd) written via the
// retaining path — one 16B op where the block path retains, otherwise
// four 4B word ops. Zone1's halo (0x1701c0..0x170210) is never written.
func buildHybridOps(fmac []byte, ram, wall, windowEnd uint32, classes map[uint32]string) []protocol.ChunkOp {
	var ops []protocol.ChunkOp
	belowEnd := int(wall - ram)
	for off := 0; off < belowEnd; off += protocol.BlockWriteChunkBytes {
		ops = append(ops, protocol.ChunkOp{Addr: ram + uint32(off), Block: fmac[off : off+protocol.BlockWriteChunkBytes]})
	}
	for a := wall; a < windowEnd; a += 16 {
		if a >= 0x1701c0 && a < 0x170210 {
			continue
		}
		off := int(a - ram)
		if classes[a] == "blk" {
			ops = append(ops, protocol.ChunkOp{Addr: a, Block: fmac[off : off+16]})
		} else {
			for w := uint32(0); w < 16; w += 4 {
				ops = append(ops, protocol.ChunkOp{Addr: a + w, Block: fmac[off+int(w) : off+int(w)+4]})
			}
		}
	}
	return ops
}

func applyPatchConfig(dev *protocol.USBDevice, ramFMACFW uint32, fmac []byte, v3Profile bool) error {
	// patch_tbl_d80 with USE_5G (CONFIG_USE_5G=y in the vendor Makefile).
	patchPairs := [][2]uint32{
		{0x00b4, 0xf3010001},
		{0x0170, 0x0001000A}, // rx aggr counter
		{0x0188, 0x01},       // user_ext_flags: PWROFST_COVER_CALIB
	}
	startAddr := uint32(0x001D7000)
	patchAddr := startAddr
	if v3Profile {
		// Radxa SDK V3 profile (aic_compat_8800d80.c, legacy-mcu1): this
		// firmware generation carries no bufBase field, so the pair
		// buffer is fixed at 0x1D7000, and the pairs are the 2.4G-only
		// 2-entry table (USE_5G is not set in the V3 loader build).
		patchPairs = [][2]uint32{
			{0x00b4, 0xf3010000}, // rx aggr
			{0x0170, 0x00000002}, // rx aggr counter
		}
		log.Printf("[AIC] V3 loader profile: fixed pair buffer 0x1D7000, %d pairs", len(patchPairs))
	}

	rdPatchOfst := 0x0198
	configBase := binary.LittleEndian.Uint32(fmac[rdPatchOfst : rdPatchOfst+4])
	strBase := binary.LittleEndian.Uint32(fmac[rdPatchOfst+8 : rdPatchOfst+12])
	log.Printf("[AIC] patch config: base=0x%08x str=0x%08x", configBase, strBase)

	if !v3Profile {
		rdVersionOfst := 0x01C
		rdVersion := binary.LittleEndian.Uint32(fmac[rdVersionOfst : rdVersionOfst+4])
		log.Printf("[AIC] fw_version=0x%08x", rdVersion)
		if rdVersion > 0x06090100 {
			bufBase := binary.LittleEndian.Uint32(fmac[rdPatchOfst+12 : rdPatchOfst+16])
			startAddr = bufBase
			patchAddr = bufBase
		}
	}

	w := func(addr, val uint32) error {
		if err := protocol.MemWrite(dev, addr, val); err != nil {
			return fmt.Errorf("write 0x%08x=0x%08x: %w", addr, val, err)
		}
		time.Sleep(50 * time.Millisecond)
		return nil
	}
	// offsetof(aic_patch_t): magic_num@0 pair_start@4 magic_num_2@8
	// pair_count@12 block_size[0]@48
	if err := w(strBase+0, 0x48435450); err != nil { // "PTCH"
		return err
	}
	if err := w(strBase+8, 0x50544348); err != nil { // "HCTP"
		return err
	}
	if err := w(strBase+4, patchAddr); err != nil {
		return err
	}
	if err := w(strBase+12, uint32(len(patchPairs))); err != nil {
		return err
	}
	for i, pair := range patchPairs {
		if err := w(startAddr+uint32(8*i), pair[0]+configBase); err != nil {
			return err
		}
		if err := w(startAddr+uint32(8*i)+4, pair[1]); err != nil {
			return err
		}
	}
	for i := uint32(0); i < 4; i++ {
		if err := w(strBase+48+4*i, 0); err != nil {
			return err
		}
	}
	return nil
}

func u32sToBytes(v []uint32) []byte {
	b := make([]byte, len(v)*4)
	for i, x := range v {
		b[4*i] = byte(x)
		b[4*i+1] = byte(x >> 8)
		b[4*i+2] = byte(x >> 16)
		b[4*i+3] = byte(x >> 24)
	}
	return b
}

// bundleNameFor returns the firmware blob filename for the given chip
// revision and blob kind. We hardcode the mapping rather than scanning
// the directory for safety.
func bundleNameFor(chipID uint8, kind string) (string, error) {
	usesU02 := chipID == protocol.ChipRevU02 || chipID == protocol.ChipRevU03 ||
		chipID == protocol.ChipRevU04 || chipID == protocol.ChipRevU05
	switch kind {
	case "adid":
		if usesU02 {
			return protocol.FWAdidBaseName8800D80U02, nil
		}
		return protocol.FWAdidBaseName8800D80, nil
	case "patch":
		if usesU02 {
			return protocol.FWPatchBaseName8800D80U02, nil
		}
		return protocol.FWPatchBaseName8800D80, nil
	case "fmacfw":
		if usesU02 {
			return protocol.FWBaseName8800D80U02, nil
		}
		return protocol.FWBaseName8800D80, nil
	case "patch_table":
		if usesU02 {
			return protocol.FWPatchTableName8800D80U02, nil
		}
		return protocol.FWPatchTableName8800D80, nil
	}
	return "", errors.New("unknown blob kind")
}

// homeDir returns the user's home directory or an empty string.
func homeDir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	return "", errors.New("HOME not set")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
