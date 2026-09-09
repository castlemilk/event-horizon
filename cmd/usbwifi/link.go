package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
	st := detectUSBState()
	fmt.Printf("link: dongle is %s\n", st)
	switch st {
	case usbAbsent:
		fmt.Println("link: no AIC dongle found on USB. Plug it in and run again.")
		return 1

	case usbMSC, usbBootROM:
		if *skipFlash {
			fmt.Println("link: --skip-flash given, but the chip is not running firmware yet; ignoring it.")
		}
		fmt.Println("link: flashing firmware ...")
		if rc := flashFirmware(dir); rc != 0 {
			return rc
		}

	case usbOperational:
		// Firmware is already running. That is fine only if nothing has used
		// it yet: this firmware allows exactly one MM_RESET per instance, and
		// the connect step spends it. A second run against the same instance
		// silently produces a deaf radio, so refuse by default rather than
		// hand back results that cannot be trusted.
		if !*skipFlash && !*force {
			fmt.Println("link: the chip is already running firmware, so this may be a")
			fmt.Println("      used instance — the radio allows one MM_RESET per flash and a")
			fmt.Println("      second run against it goes deaf without saying so.")
			fmt.Println("      Unplug the dongle for ~10s and replug to get a clean flash,")
			fmt.Println("      or pass --skip-flash if you know this instance is untouched.")
			return 1
		}
		fmt.Println("link: reusing the running firmware instance (results are only")
		fmt.Println("      trustworthy if nothing has associated on it yet).")
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

// USB states the AIC chip presents, in the order it moves through them.
type usbState string

const (
	usbAbsent      usbState = "absent"
	usbMSC         usbState = "ZeroCD mass-storage (a69c:5723) — flashable"
	usbBootROM     usbState = "boot ROM (a69c:8d80) — flashable"
	usbOperational usbState = "operational (368b:8d85) — firmware already running"
)

// detectUSBState reports which identity the dongle is currently presenting.
// ioreg is the same source scripts/aic-zerocd-eject.sh matches on, so the two
// agree about what state the chip is in.
func detectUSBState() usbState {
	out, err := exec.Command("ioreg", "-r", "-c", "IOUSBHostDevice", "-l").Output()
	if err != nil {
		return usbAbsent
	}
	s := string(out)
	switch {
	case strings.Contains(s, `"idProduct" = 22307`): // 0x5723
		return usbMSC
	case strings.Contains(s, `"idProduct" = 36224`): // 0x8d80
		return usbBootROM
	case strings.Contains(s, `"idProduct" = 36229`): // 0x8d85
		return usbOperational
	}
	return usbAbsent
}

// defaultFirmwareDir is the hybrid firmware set that actually works on this
// chip — the fmacfw carved out of the vendor's Windows driver plus the Amlogic
// patch blobs. See docs/HANDOVER-aic8800d80.md section 2 for why every other
// combination is wrong.
func defaultFirmwareDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".event-horizon/firmware/aic8800D80-hybrid"
	}
	return filepath.Join(home, ".event-horizon", "firmware", "aic8800D80-hybrid")
}

// flashFirmware shells out to the recovery script, which owns the ZeroCD eject
// dance (the kernel's mass-storage driver has to deliver the SCSI eject —
// libusb can never win those pipes) and then runs the loader.
//
// The script is located relative to this executable rather than the working
// directory, because it is normally invoked through sudo from anywhere.
func flashFirmware(dir string) int {
	script, err := findScript("aic-zerocd-eject.sh")
	if err != nil {
		fmt.Printf("link: %v\n", err)
		return 1
	}
	cmd := exec.Command(script, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("link: firmware flash failed: %v\n", err)
		return 1
	}
	return 0
}

// findScript locates a repo script from the running binary's location
// (bin/usbwifi -> ../scripts/<name>), falling back to the working directory.
func findScript(name string) (string, error) {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "..", "scripts", name))
	}
	candidates = append(candidates, filepath.Join("scripts", name))
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return filepath.Abs(c)
		}
	}
	return "", fmt.Errorf("cannot find scripts/%s (looked in %s)", name, strings.Join(candidates, ", "))
}
