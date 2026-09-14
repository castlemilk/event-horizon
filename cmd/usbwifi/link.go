package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// runCmdLink brings the whole dongle link up with one command: flash if the
// chip is in a flashable state, start the MAC stack, associate, run the WPA2
// handshake, take a DHCP lease, and hand the link to a utun bridge.
//
// It exists because the working sequence was three separate invocations that
// had to be run in the right order, and getting the order wrong wasted a
// physical replug every time. The steps are not merged, though — they still
// run as SEPARATE USB SESSIONS, because the firmware requires it:
//
//   - every command after stack_start in the same session goes unacknowledged
//   - a second MM_RESET on a running stack permanently kills RX until re-flash
//
// session.close() releases the device deterministically, so closing and
// reopening inside one process is equivalent to the two-process flow that has
// always worked. What this removes is the manual ordering, not the constraint.
func runCmdLink(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("link", flag.ExitOnError)
	ssid := fs.String("ssid", "", "SSID to associate with (required)")
	pass := fs.String("pass", "", "WPA2 passphrase (empty = open network)")
	channel := fs.Int("channel", 0, "channel of the target BSS (0 = any)")
	bssid := fs.String("bssid", "", "target BSSID aa:bb:cc:dd:ee:ff (recommended; required for hidden APs)")
	route := fs.String("route", "192.168.100.1", "comma-separated hosts to route through the bridge")
	fwDir := fs.String("firmware-dir", "", "firmware directory (default ~/.event-horizon/firmware/aic8800D80-hybrid)")
	skipFlash := fs.Bool("skip-flash", false, "assume the firmware is already loaded and current")
	force := fs.Bool("force", false, "proceed even when the chip is in a state whose radio is not trustworthy")
	waitReplug := fs.Duration("wait-replug", 0, "when the chip needs a power cycle, wait this long for it to be unplugged and replugged, then continue (0 = do not wait)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *ssid == "" {
		fmt.Println("link: --ssid is required")
		return 2
	}

	// One link at a time, software-enforced. A linkstate record whose
	// writer is still alive means a link is running: a second CLI would
	// claim the USB device mid-flight, and the two processes then fight
	// over the radio with no way to report it coherently. Refuse rather
	// than race.
	if ls, ok := ReadLinkState(time.Now()); ok && ls.SSID != "" {
		fmt.Printf("link: a link is already active on %s (%s, pid %d). "+
			"Stop it first (or use the daemon's link API), and clear a stale record with "+
			"rm ~/.event-horizon/linkstate.json if the process is actually dead.\n",
			ls.SSID, ls.Iface, ls.PID)
		return 1
	}

	dir := *fwDir
	if dir == "" {
		dir = defaultFirmwareDir()
	}

	// --- 1. work out what state the chip is in -------------------------
	// Everything here is in-process: protocol.DetectAICStage reads the USB bus
	// through libusb, and runBootstrap does the ZeroCD eject and the firmware
	// upload in Go. There is no shell script and no ioreg — the binary is
	// self-contained, so it behaves the same from a dev checkout and from
	// inside the app bundle.
	stage, err := protocol.DetectAICStage(ctx)
	if err != nil {
		fmt.Println("link: no AIC8800D80 on the USB bus. Plug the dongle in and run again.")
		return 1
	}

	switch stage {
	case protocol.StageZeroCD, protocol.StageBootROM:
		if *skipFlash {
			fmt.Println("link: --skip-flash was given, but the chip is not running firmware yet; ignoring it.")
		}
		if !firmwareSetPresent(dir) {
			fmt.Printf("link: no firmware set at %s\n", dir)
			fmt.Println("      Three of the four blobs are public and the fourth lives on the dongle:")
			fmt.Println("        ./bin/usbwifi firmware fetch --target=aic8800D80 --out=~/.event-horizon/firmware")
			fmt.Println("        ./bin/usbwifi firmware carve      # needs: brew install innoextract")
			return 1
		}
		reportLink(LinkFlashing, "flashing firmware")
		fmt.Printf("link: flashing firmware from %s ...\n", dir)
		if rc := runBootstrap([]string{"--firmware-dir", dir}); rc != 0 {
			fmt.Println("link: firmware bootstrap failed.")
			return rc
		}

	case protocol.StageOperational:
		// Firmware is already running. That is only safe if nothing has used it
		// yet: this firmware allows one MM_RESET per instance and the connect
		// spends it, so a second run against the same instance goes deaf
		// without saying so. Refuse rather than return results that cannot be
		// trusted.
		if !*skipFlash && !*force {
			fmt.Println("link: the chip is already running firmware, so this may be a used")
			fmt.Println("      instance — the radio allows one MM_RESET per flash and the connect")
			fmt.Println("      spends it. A second run goes deaf without reporting anything.")
			if *waitReplug <= 0 {
				fmt.Println("      Unplug the dongle for ~10s and replug for a clean flash, or pass")
				fmt.Println("      --skip-flash if you know this instance is untouched.")
				return 1
			}
			// Wait for the power cycle rather than dead-ending on it. The
			// replug is the one step software cannot perform, but noticing it
			// and carrying on is exactly what a state machine should do.
			reportLink(LinkNeedsReplug, "unplug the dongle for ~10s and plug it back in")
			fmt.Printf("      >>> UNPLUG THE DONGLE for ~10s, then plug it back in <<<\n")
			fmt.Printf("      (waiting up to %s; the flash resumes by itself)\n", *waitReplug)
			if !waitForFlashable(ctx, *waitReplug) {
				fmt.Println("link: no replug seen — the chip is still running its used firmware.")
				return 1
			}
			fmt.Println("link: replug detected — continuing.")
		}
		if *skipFlash || *force {
			fmt.Println("link: reusing the running firmware instance (trustworthy only if")
			fmt.Println("      nothing has associated on it yet).")
			break
		}
		fmt.Printf("link: flashing firmware from %s ...\n", dir)
		if rc := runBootstrap([]string{"--firmware-dir", dir}); rc != 0 {
			fmt.Println("link: firmware bootstrap failed.")
			return rc
		}
	default:
		fmt.Println("link: the dongle is in an indeterminate USB state. Replug it and run again.")
		return 1
	}

	// --- 2. stack_start, in its own session ----------------------------
	fmt.Println("link: starting the MAC stack ...")
	if rc := runCmdBringup(ctx, []string{"--stack"}); rc != 0 {
		fmt.Println("link: stack_start failed — the firmware instance is not usable.")
		fmt.Println("      Replug the dongle and run again.")
		return rc
	}
	// Let the stack settle before the next session claims the device.
	select {
	case <-time.After(500 * time.Millisecond):
	case <-ctx.Done():
		return 1
	}

	// --- 3. associate, handshake, DHCP, bridge -------------------------
	bringup := []string{
		"--connect", *ssid,
		"--connect-pass", *pass,
		"--bridge",
		"--bridge-route", *route,
	}
	if *channel != 0 {
		bringup = append(bringup, "--connect-channel", fmt.Sprint(*channel))
	}
	if *bssid != "" {
		bringup = append(bringup, "--connect-bssid", *bssid)
	}
	// bridgeLinkSSID feeds the linkstate file (see bridge.go): the CLI
	// holds the USB claim while the link is up, so the daemon cannot see
	// the dongle and would otherwise report NO_DONGLE for a working link.
	bridgeLinkSSID = *ssid

	reportLink(LinkAssociating, "associating with "+*ssid)
	fmt.Printf("link: associating with %q ...\n", *ssid)
	return runCmdBringup(ctx, bringup)
}

// waitForFlashable polls until the chip re-enumerates in a state that can be
// flashed, i.e. after a physical power cycle. Returns false on timeout.
//
// Polling is the only option: a VBUS drop tears the device off the bus, so
// there is no handle left to watch. The interval is short enough to feel
// immediate and long enough not to spin.
func waitForFlashable(ctx context.Context, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	sawGone := false
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(750 * time.Millisecond):
		}
		stage, err := protocol.DetectAICStage(ctx)
		if err != nil {
			// Off the bus: the unplug half of the cycle. Require this before
			// accepting a flashable state, so a dongle that was already
			// sitting in ZeroCD is not mistaken for a fresh replug.
			sawGone = true
			continue
		}
		if sawGone && (stage == protocol.StageZeroCD || stage == protocol.StageBootROM) {
			return true
		}
	}
	return false
}

// defaultFirmwareDir locates the blob set the loader needs.
//
// A user-installed set wins: it is the one that has been exercised on hardware,
// and firmware pairings are chip-specific enough that silently preferring a
// bundled copy could flash the wrong image. Otherwise fall back to a copy
// shipped beside the binary, so the packaged app works on a machine that has
// never run the dev tooling.
func defaultFirmwareDir() string {
	const set = "aic8800D80-hybrid"
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, ".event-horizon", "firmware", set); firmwareSetPresent(p) {
			return p
		}
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		for _, p := range []string{
			filepath.Join(dir, "firmware", set),                    // beside the binary
			filepath.Join(dir, "..", "Resources", "firmware", set), // inside the app bundle
		} {
			if firmwareSetPresent(p) {
				if abs, err := filepath.Abs(p); err == nil {
					return abs
				}
				return p
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".event-horizon", "firmware", set)
	}
	return filepath.Join(".event-horizon", "firmware", set)
}

// firmwareSetPresent reports whether a directory holds every blob the loader
// needs. A directory that merely exists is not enough: a half-populated one
// fails mid-flash, and recovering from that costs a physical replug.
func firmwareSetPresent(dir string) bool {
	for _, f := range []string{
		"fmacfw_8800d80_u02_ipc.bin",
		"fw_adid_8800d80_u02.bin",
		"fw_patch_8800d80_u02.bin",
		"fw_patch_table_8800d80_u02.bin",
	} {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil || st.Size() == 0 {
			return false
		}
	}
	return true
}
