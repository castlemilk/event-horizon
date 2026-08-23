// aicloader subcommand. Invoked as:
//
//   sudo ./bin/usbwifi aicloader --firmware-dir ~/.event-horizon/firmware/aic8800D80
//   sudo ./bin/usbwifi aicloader --status         # detect stage, no upload
//
// Drives the AIC8800D80 from BootROM (VID:0a69c PID:8d80) or ZeroCD
// (VID:1111 PID:1111) into Operational (0a69c:8d81 or 0a69c:8d83).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// printEnvDiagnostics reports anything that could be interfering with
// the USB session: the Swift app RESPAWNS the usbwifi daemon (its
// RuntimeSupervisor), and the daemon enumerates + opens every USB device
// every 3 seconds — including the dongle mid-upload. It also shows which
// process (if any) holds exclusive ownership of the device.
func printEnvDiagnostics() {
	fmt.Fprintln(os.Stderr, "\n── environment diagnostics ──")
	out, _ := exec.Command("pgrep", "-fl", "usbwifi|EventHorizonApp").CombinedOutput()
	if len(out) == 0 {
		fmt.Fprintln(os.Stderr, "usbwifi/EventHorizonApp: none running (good)")
	} else {
		fmt.Fprintf(os.Stderr, "INTERFERING PROCESSES (kill before retrying — the app respawns the daemon which opens the dongle every 3s):\n%s", out)
	}
	if out, _ := exec.Command("sh", "-c", "ioreg -p IOUSB -l | grep -B 6 'UsbExclusiveOwner' | grep -E 'USB Product Name|UsbExclusiveOwner'").CombinedOutput(); len(out) > 0 {
		fmt.Fprintf(os.Stderr, "USB exclusive ownership:\n%s", out)
	}
	fmt.Fprintln(os.Stderr, "current AIC stage:")
	if st, err := exec.Command(os.Args[0], "aicloader", "--status").CombinedOutput(); err == nil {
		fmt.Fprintf(os.Stderr, "%s", st)
	} else {
		fmt.Fprintf(os.Stderr, "(status probe failed: %v)\n%s\n", err, st)
	}
	fmt.Fprintln(os.Stderr, "────────────────────────────")
}

// runAICLoader is the entrypoint for the `aicloader` subcommand.
// Returns the process exit code (0 success, 1 error).
func runAICLoader(args []string) int {
	fs := flag.NewFlagSet("aicloader", flag.ExitOnError)
	firmwareDir := fs.String("firmware-dir", "", "Directory containing the firmware blobs (default: ~/.event-horizon/firmware/aic8800D80)")
	statusOnly := fs.Bool("status", false, "Detect current stage and exit without uploading firmware")
	debug := fs.Bool("debug", false, "Verbose USB transfer logging")
	killDaemon := fs.Bool("kill-daemon", false, "Stop the running usbwifi daemon before opening the device (avoids LIBUSB_ERROR_NOT_FOUND on macOS USB exclusive ownership)")
	probe := fs.Bool("probe", false, "Probe the boot ROM: dump system/window registers and map readable memory around the 0x170000 fault boundary")
	probeWrite := fs.Bool("probe-write", false, "EXPERIMENT: test write methods at/around the 0x170000 wall on a fresh boot ROM (small blocks, word writes, boundary straddles). Answers whether any write path can place data past the wall.")
	probeWindow := fs.Bool("probe-window", false, "EXPERIMENT: sweep the wedge-zone boundaries past zone ends (word + 16B writes and readbacks at each address; first wedge stops). Optional start index to continue a sweep after a power cycle.")
	poisonMap := fs.Bool("poison-map", false, "EXPERIMENT: word-write every 4B address in 0x170210..0x1776b8 (30,392 B total, skips known zones) to test the ~9.1KB write-budget theory vs a single poison at 0x172430. Completes -> no budget, full window loadable. Write wedge -> poison (appended to /tmp/aic-poisons.txt), resume with start index.")
	retentionMap := fs.Bool("retention-map", false, "EXPERIMENT: map which write path (word vs 16B) retains at each 16B address in 0x170000..0x17238f. 20B writes/address, 11,280 B total — one power cycle. Writes /tmp/aic-retention.txt for the AIC_HYBRID loader mode.")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	log.SetPrefix("[aicloader] ")
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Trap SIGINT / SIGTERM for clean shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Status-only mode: detect and report.
	if *statusOnly {
		stage, err := protocol.DetectAICStage(ctx)
		if err != nil {
			log.Printf("detect stage: %v", err)
			return 1
		}
		fmt.Printf("AIC8800D80 stage: %s\n", stage)
		switch stage {
		case protocol.StageZeroCD:
			fmt.Println("Run `sudo ./bin/usbwifi aicloader` to mode-switch + upload firmware.")
		case protocol.StageBootROM:
			fmt.Println("Run `sudo ./bin/usbwifi aicloader --kill-daemon --firmware-dir=<dir>` to upload firmware.")
		case protocol.StageOperational:
			fmt.Println("Device is already operational. A kernel driver is required for `enX` binding.")
		}
		return 0
	}

	// Probe mode: map the boot ROM's memory and registers. Read-only —
	// safe on a fresh device. Run when the device is at BootROM stage
	// after a replug.
	// Stop the running daemon if requested. The daemon keeps the USB
	// device claimed; we need to release it before opening our own.
	if *killDaemon {
		log.Printf("stopping running usbwifi / usbwifi-mcp daemon...")
		_ = exec.Command("pkill", "-9", "-f", "usbwifi").Run()
		_ = exec.Command("pkill", "-9", "-f", "usbwifi-mcp").Run()
		time.Sleep(500 * time.Millisecond)
	}

	if *probe {
		return runAICProbe()
	}
	if *probeWrite {
		return runAICProbeWrite()
	}
	if *probeWindow {
		startIdx := 0
		if fs.NArg() > 0 {
			if v, err := strconv.Atoi(fs.Arg(0)); err == nil {
				startIdx = v
			}
		}
		return runAICProbeWindow(startIdx)
	}
	if *poisonMap {
		startIdx := 0
		if fs.NArg() > 0 {
			v, err := strconv.Atoi(fs.Arg(0))
			if err != nil {
				fmt.Fprintf(os.Stderr, "bad --poison-map start index %q: %v\n", fs.Arg(0), err)
				return 1
			}
			startIdx = v
		}
		return runAICPoisonMap(startIdx)
	}
	if *retentionMap {
		startIdx := 0
		if fs.NArg() > 0 {
			v, err := strconv.Atoi(fs.Arg(0))
			if err != nil {
				fmt.Fprintf(os.Stderr, "bad --retention-map start index %q: %v\n", fs.Arg(0), err)
				return 1
			}
			startIdx = v
		}
		return runAIRetentionMap(startIdx)
	}

	// Build the loader.
	opts := []aic8800d80.LoaderOption{}
	if *debug {
		opts = append(opts, aic8800d80.WithDebug())
	}
	if *firmwareDir != "" {
		opts = append(opts, aic8800d80.WithFirmwareDir(*firmwareDir))
	}
	loader := aic8800d80.NewLoader(opts...)

	// Run with a generous timeout.
	runCtx, cancelRun := context.WithTimeout(ctx, 60*time.Second)
	defer cancelRun()

	res, err := loader.LoadFirmware(runCtx)
	if err != nil {
		log.Printf("FAILED: %v", err)
		if res != nil {
			log.Printf("progress: %d bytes uploaded in %s", res.BytesUploaded, res.Duration)
		}
		printEnvDiagnostics()
		return 1
	}

	log.Printf("SUCCESS: %s -> %s in %s (%d bytes uploaded, chip 0x%02x)",
		res.FromStage, res.ToStage, res.Duration, res.BytesUploaded, res.ChipRev)

	if res.ToStage == protocol.StageOperational {
		fmt.Println("Device is now operational. A DriverKit driver is required to expose it as enX.")
		fmt.Println("See docs/aic8800d80-macos-driver-plan.md for the kernel-side plan.")
	}
	return 0
}

// usageAICLoader prints the help for the aicloader subcommand.
func usageAICLoader() {
	fmt.Fprintf(os.Stderr, `aicloader — user-space firmware loader for AIC8800D80 USB Wi-Fi 6 adapters

Usage:
  sudo ./bin/usbwifi aicloader [options]

Options:
  --firmware-dir <path>   Directory containing the firmware blobs.
                          Default: ~/.event-horizon/firmware/aic8800D80
  --status                Detect current USB stage and exit.
  --debug                 Verbose USB transfer logging.
  --kill-daemon           Stop the running usbwifi daemon before opening
                          the device (macOS USB exclusive ownership).

Examples:
  ./bin/usbwifi aicloader --status
  sudo ./bin/usbwifi aicloader --kill-daemon --firmware-dir=/path/to/firmware
`)
}
