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
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *ssid == "" {
		fmt.Println("link: --ssid is required")
		return 2
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
			fmt.Println("      The blobs this chip needs are carved from the vendor driver on the")
			fmt.Println("      dongle's own ZeroCD volume; see docs/HANDOVER-aic8800d80.md section 2.")
			return 1
		}
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
			fmt.Println("      Unplug the dongle for ~10s and replug for a clean flash, or pass")
			fmt.Println("      --skip-flash if you know this instance is untouched.")
			return 1
		}
		fmt.Println("link: reusing the running firmware instance (trustworthy only if")
		fmt.Println("      nothing has associated on it yet).")

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
	fmt.Printf("link: associating with %q ...\n", *ssid)
	return runCmdBringup(ctx, bringup)
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
