package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// zeroCDDiskBytes is the exact size of the AIC8800D80 clone's ZeroCD volume
// (the fake driver "flash" disk, FAT16 label "UGREEN"). It is the only stable
// discriminator for the eject target — media name and label are generic.
const zeroCDDiskBytes = "3784704"

// runBootstrap drives a dongle from whatever stage it is in to Operational,
// automating every step that does not require a physical VBUS drop.
//
// The state machine (verified on the UGREEN AX900 / AIC8800D80 clone):
//
//	ZeroCD MSC (a69c:5723) --diskutil eject--> live BootROM (a69c:8d80)
//	live BootROM           --firmware upload--> Operational (a69c:8d81)
//
// The libusb SCSI eject the loader ships cannot win the bulk pipes from
// macOS's mass-storage driver, so we let the kernel driver deliver the eject
// via `diskutil eject`. A firmware crash — or an MSC that auto-switched on a
// ZeroCD timeout — leaves a *wedged* BootROM that enumerates but answers no
// DBG transfers; only unplugging the dongle recovers it. bootstrap detects
// that case and (with --wait, the default) waits for the replug, then finishes
// the job automatically.
func runBootstrap(args []string) int {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	firmwareDir := fs.String("firmware-dir", "", "Firmware blob directory (default: ~/.event-horizon/firmware/aic8800D80)")
	fwName := fs.String("fw-name", "", "Override the main firmware blob name")
	wait := fs.Bool("wait", true, "Wait for a replug when the boot ROM is wedged or no device is present")
	fs.Usage = usageBootstrap
	if err := fs.Parse(args); err != nil {
		return 1
	}

	log.SetPrefix("[bootstrap] ")
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Nothing else may hold the device while we open it.
	stopDeviceHolders()

	for {
		if ctx.Err() != nil {
			return 1
		}
		stage, err := protocol.DetectAICStage(ctx)
		if err != nil {
			// No AIC device on the bus at all.
			if *wait {
				log.Printf("no AIC8800D80 on the bus — plug the dongle in...")
				if !waitForAnyAIC(ctx) {
					return 1
				}
				continue
			}
			log.Printf("no AIC8800D80 on the bus: %v", err)
			return 1
		}

		switch stage {
		case protocol.StageOperational:
			log.Printf("device is operational (a69c:8d81) — nothing to do")
			return 0

		case protocol.StageZeroCD:
			log.Printf("ZeroCD mass-storage mode (a69c:5723) — ejecting via the kernel driver")
			if err := ejectZeroCD(ctx); err != nil {
				log.Printf("eject failed: %v", err)
				return 1
			}
			if !waitForStage(ctx, protocol.StageBootROM, 30*time.Second) {
				log.Printf("boot ROM did not appear after eject")
				return 1
			}
			time.Sleep(2 * time.Second) // let the ROM settle before the first transfer
			continue

		case protocol.StageBootROM:
			log.Printf("boot ROM (a69c:8d80) — uploading firmware")
			res, err := uploadFirmware(ctx, *firmwareDir, *fwName)
			if err == nil && res != nil && res.ToStage == protocol.StageOperational {
				log.Printf("SUCCESS: %s -> %s in %s (%d bytes, chip 0x%02x)",
					res.FromStage, res.ToStage, res.Duration, res.BytesUploaded, res.ChipRev)
				fmt.Println("Device is now operational (a69c:8d81).")
				return 0
			}
			log.Printf("upload did not reach operational: %v", err)
			// A wedged boot ROM only recovers with a physical replug.
			if !*wait {
				return 1
			}
			log.Printf(">>> UNPLUG THE DONGLE for ~10s, then plug it back in <<<")
			log.Printf("    (a crashed or auto-switched boot ROM is deaf; a VBUS drop is the only reset)")
			if !waitForStage(ctx, protocol.StageZeroCD, 0) {
				return 1
			}
			continue

		default:
			if *wait {
				log.Printf("device in an indeterminate stage — waiting for a clean re-enumeration...")
				if !waitForAnyAIC(ctx) {
					return 1
				}
				continue
			}
			return 1
		}
	}
}

// uploadFirmware runs the loader against a live boot ROM.
func uploadFirmware(ctx context.Context, dir, name string) (*aic8800d80.LoadFirmwareResult, error) {
	opts := []aic8800d80.LoaderOption{}
	if dir != "" {
		opts = append(opts, aic8800d80.WithFirmwareDir(dir))
	}
	if name != "" {
		opts = append(opts, aic8800d80.WithFirmwareName(name))
	}
	loader := aic8800d80.NewLoader(opts...)
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return loader.LoadFirmware(runCtx)
}

// ejectZeroCD finds the ZeroCD volume by its exact byte size and asks the
// kernel's mass-storage driver to eject it (SCSI START STOP UNIT / LoEj),
// which is the mode-switch the chip acts on. libusb cannot do this on macOS
// because the mass-storage driver owns the interface.
func ejectZeroCD(ctx context.Context) error {
	disk, err := findZeroCDDisk()
	if err != nil {
		return err
	}
	log.Printf("ZeroCD volume is %s — sending the commanded eject", disk)
	if out, err := exec.CommandContext(ctx, "diskutil", "eject", disk).CombinedOutput(); err != nil {
		// One retry after an explicit unmount, matching manual recovery.
		_ = exec.CommandContext(ctx, "diskutil", "unmountDisk", disk).Run()
		if out2, err2 := exec.CommandContext(ctx, "diskutil", "eject", disk).CombinedOutput(); err2 != nil {
			return fmt.Errorf("diskutil eject %s: %v (%s / %s)", disk, err2, strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
		}
	}
	return nil
}

// findZeroCDDisk returns the /dev/diskN whose total size matches the ZeroCD
// volume exactly.
func findZeroCDDisk() (string, error) {
	list, err := exec.Command("diskutil", "list").Output()
	if err != nil {
		return "", fmt.Errorf("diskutil list: %w", err)
	}
	sc := bufio.NewScanner(strings.NewReader(string(list)))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "/dev/disk") {
			continue
		}
		disk := fields[0]
		info, err := exec.Command("diskutil", "info", disk).Output()
		if err != nil {
			continue
		}
		if strings.Contains(string(info), zeroCDDiskBytes) {
			return disk, nil
		}
	}
	return "", fmt.Errorf("no attached disk matches the ZeroCD size (%s bytes)", zeroCDDiskBytes)
}

// waitForStage polls until the device reaches want, or timeout elapses
// (timeout == 0 waits indefinitely, honouring ctx cancellation).
func waitForStage(ctx context.Context, want protocol.Stage, timeout time.Duration) bool {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		if ctx.Err() != nil {
			return false
		}
		if stage, err := protocol.DetectAICStage(ctx); err == nil && stage == want {
			return true
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return false
		}
		time.Sleep(1 * time.Second)
	}
}

// waitForAnyAIC blocks until any AIC stage is detectable.
func waitForAnyAIC(ctx context.Context) bool {
	for {
		if ctx.Err() != nil {
			return false
		}
		if _, err := protocol.DetectAICStage(ctx); err == nil {
			return true
		}
		time.Sleep(1 * time.Second)
	}
}

// stopDeviceHolders frees the dongle of any process that could be holding it.
// pkill skips its own pid, so this does not signal the running bootstrap.
//
// It deliberately does NOT kill the Event Horizon app, though it used to, with
// SIGKILL, on both call sites. The app never opens the device — it is a SwiftUI
// front end that talks HTTP to this daemon and has no libusb anywhere in it —
// so killing it freed nothing. The actual worry was the app's supervisor
// spawning a RIVAL daemon mid-flash, and that is now handled where it belongs:
// ensureDaemonRunning returns early when a daemon of its own build is already
// answering, so it has no reason to start a second one.
//
// The cost of getting this wrong was invisible and kept being misread as a
// crash: every successful bring-up flashes firmware, so every successful
// bring-up SIGKILLed the app. It disappeared from the menu bar immediately
// after each replug, left no crash report because SIGKILL produces none, and
// looked for all the world like it had died on its own.
func stopDeviceHolders() {
	log.Printf("stopping any rival daemon so the device is free (the app is left alone; it holds no USB session)...")
	_ = exec.Command("pkill", "-TERM", "-x", "usbwifi").Run()
	time.Sleep(500 * time.Millisecond)
	_ = exec.Command("pkill", "-9", "-x", "usbwifi").Run()
	_ = exec.Command("pkill", "-9", "-x", "usbwifi-mcp").Run()
	for range 20 {
		if exec.Command("pgrep", "-x", "usbwifi").Run() != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
}

func usageBootstrap() {
	fmt.Fprintf(os.Stderr, `bootstrap — drive an AIC8800D80 dongle from any stage to Operational

Usage:
  sudo ./bin/usbwifi bootstrap [options]

It detects the current USB stage and does whatever is needed:
  ZeroCD mass-storage (a69c:5723) -> kernel-driver eject -> boot ROM
  boot ROM (a69c:8d80)            -> firmware upload      -> operational
  operational (a69c:8d81)         -> nothing to do

A crashed or auto-switched boot ROM is wedged and only a physical replug
recovers it; with --wait (default) bootstrap prompts for the replug and then
finishes automatically.

Options:
  --firmware-dir <path>   Firmware blob directory (default: ~/.event-horizon/firmware/aic8800D80)
  --fw-name <name>        Override the main firmware blob name
  --wait=false            Do not wait for a replug; fail fast instead

Examples:
  sudo ./bin/usbwifi bootstrap
  sudo ./bin/usbwifi bootstrap --wait=false
`)
}
