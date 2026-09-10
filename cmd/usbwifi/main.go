package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/castlemilk/event-horizon/pkg/api"
	"github.com/castlemilk/event-horizon/pkg/usb"
	"github.com/castlemilk/event-horizon/pkg/wifi"
)

func main() {
	// Subcommand dispatch. The first non-flag argument is treated as a
	// subcommand name; if it matches a registered subcommand, hand off
	// to it and return. Otherwise fall through to the daemon flags.
	if len(os.Args) > 1 && !startsWithDash(os.Args[1]) {
		switch os.Args[1] {
		case "aicloader":
			os.Exit(runAICLoader(os.Args[2:]))
		case "bootstrap":
			os.Exit(runBootstrap(os.Args[2:]))
		case "firmware":
			os.Exit(runFirmwareCmd(os.Args[2:]))
		case "driver":
			os.Exit(runDriverCmd(os.Args[2:]))
		case "cmdctl":
			os.Exit(runCmdCtl(os.Args[2:]))
		case "help", "-h", "--help":
			printRootUsage()
			return
		}
	}

	stop := flag.Bool("stop", false, "Signal a running daemon to exit, then return")
	targetSSID := flag.String("ssid", "", "SSID for the HOST's built-in Wi-Fi adapter to join (this is NOT the USB dongle — see `cmdctl link`)")
	passphrase := flag.String("passphrase", "", "passphrase for the host adapter (required for a real association)")
	apiPort := flag.Int("port", 8990, "HTTP API Server port")
	simulate := flag.Bool("simulate", false, "Force simulated 802.11 handshake instead of a real association")
	flag.Parse()

	// --stop exists so the daemon can be restarted without a password
	// prompt. It runs as root for libusb and utun, so only root can
	// signal it; the sudoers rule that lets this binary start under
	// `sudo -n` also covers running it with --stop, whereas `sudo pkill`
	// is a different command and is not covered. Without this, a
	// relaunch from a non-interactive context (task, a script, an IDE)
	// silently leaves the old daemon serving and redeploys nothing.
	if *stop {
		os.Exit(stopRunningDaemon())
	}

	writePIDFile()

	fmt.Println("================================================================")
	fmt.Println("  📡 Event Horizon USB Wi-Fi & Network Manager Daemon v1.0.0")
	fmt.Println("================================================================")

	// 1. USB Auto-Detection & ModeSwitch
	log.Printf("[INIT] Scanning USB bus for Wi-Fi dongles (Ugreen / AIC / Realtek)...")
	devInfo, err := usb.CheckAndSwitchDevices()
	if err != nil {
		log.Printf("[USB] Info: %v (continuing with real interface discovery)", err)
	} else {
		log.Printf("[USB] Initialized dongle: %s (VID 0x%04x, PID 0x%04x)",
			devInfo.Name, devInfo.VendorID, devInfo.ProductID)
	}

	// 2. Initialize Wi-Fi Scanner Engine (real 802.11 discovery)
	scanner := wifi.NewScanner()
	if err := scanner.ScanRealNetworks(); err != nil {
		log.Printf("[WIFI] Initial real scan failed: %v", err)
	}

	// 3. Start HTTP / REST API Server
	apiServer := api.NewServer(scanner, *apiPort)
	apiServer.SimulateConnections = *simulate

	// Give the API control of the real dongle link. /api/wifi/connect drives
	// the HOST's CoreWLAN interface and correctly refuses to claim the dongle;
	// /api/wifi/link is the one that actually brings the radio up, using the
	// same code path as `cmdctl link` so the two cannot diverge.
	linkSvc := NewLinkService()
	apiServer.LinkStatus = func() any { return linkSvc.Status() }
	apiServer.LinkStart = func(ssid, pass string, channel int, bssid, route string) (any, error) {
		return linkSvc.Start(LinkOptions{SSID: ssid, Pass: pass, Channel: channel, BSSID: bssid, Route: route})
	}
	apiServer.LinkStop = linkSvc.Stop
	defer linkSvc.Stop()

	apiServer.Start()

	// 4. No utun here — the link owns it.
	//
	// This used to create one unconditionally and configure it as
	// 192.168.100.2/24 via 192.168.100.1: an address invented at compile time.
	// The dongle's real address comes from DHCP on whatever network it joins
	// (192.168.2.243/24 via 192.168.2.1 on the network this was developed
	// against), so the interface asserted a lease no server had issued and
	// claimed a route to a terminal it could not reach.
	//
	// It also collided with the real thing: `usbwifi cmdctl link` brings the
	// dongle up and creates its own utun with the lease it actually obtained,
	// plus a host route for the terminal. Two utuns both claiming to own
	// 192.168.100.1 is a coin toss over which one the kernel routes through.
	//
	// The daemon observes the link; it does not invent an interface.
	log.Printf("[TUN] no utun created by the daemon — use `usbwifi cmdctl link` to bring the dongle up")

	// 5. Optional association of the HOST's built-in adapter.
	//
	// This drives CoreWLAN/networksetup, i.e. the Mac's own Wi-Fi — it has
	// nothing to do with the USB dongle. The logs used to say "Performing real
	// Wi-Fi association" and then SetConnected(ssid), so the daemon's API
	// reported a connection that a reader would naturally attribute to the
	// dongle. The dongle is brought up by `usbwifi cmdctl link`.
	if *targetSSID != "" && *passphrase != "" && !*simulate {
		log.Printf("[HOST-WIFI] Associating the HOST adapter (not the dongle) to '%s'...", *targetSSID)
		if err := wifi.AssociateViaCoreWLAN(*targetSSID, *passphrase); err != nil {
			log.Printf("[HOST-WIFI] CoreWLAN association failed (%v); falling back to networksetup", err)
			if iface, ifaceErr := wifi.FindWiFiInterface(); ifaceErr == nil {
				if err := wifi.AssociateToNetwork(iface, *targetSSID, *passphrase); err != nil {
					log.Printf("[HOST-WIFI] networksetup association failed: %v", err)
				} else {
					scanner.SetConnected(*targetSSID)
					log.Printf("[HOST-WIFI] Host adapter connected to '%s'", *targetSSID)
				}
			} else {
				log.Printf("[HOST-WIFI] No host Wi-Fi interface found: %v", ifaceErr)
			}
		} else {
			scanner.SetConnected(*targetSSID)
			log.Printf("[HOST-WIFI] Host adapter connected to '%s'", *targetSSID)
		}
	} else if *targetSSID != "" {
		time.Sleep(100 * time.Millisecond)
		_, _ = scanner.SelectHotspot(*targetSSID)
		log.Printf("[HOST-WIFI] Target '%s' selected (no passphrase supplied; NOT connected)", *targetSSID)
	}

	fmt.Println("\n----------------------------------------------------------------")
	fmt.Printf("  ✅ Daemon Active!\n")
	fmt.Printf("  🌐 Web API: http://127.0.0.1:%d/api/wifi/scan\n", *apiPort)
	fmt.Printf("  🔗 Connect Endpoint: http://127.0.0.1:%d/api/wifi/connect\n", *apiPort)
	fmt.Println("----------------------------------------------------------------")

	// Wait for SIGINT / SIGTERM signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Printf("[SHUTDOWN] Stopping USB Wi-Fi Hotspot Daemon. Goodbye!")
}

// startsWithDash reports whether a string begins with "-".
func startsWithDash(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

// printRootUsage prints the top-level help banner.
func printRootUsage() {
	fmt.Println("Event Horizon Daemon")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  aicloader    User-space firmware loader for AIC8800D80 USB Wi-Fi 6 adapters")
	fmt.Println("  firmware     Fetch and verify proprietary firmware blobs")
	fmt.Println("  driver       Install/uninstall the DriverKit driver")
	fmt.Println("  cmdctl       User-space LMAC command channel; `cmdctl link` brings a dongle up")
	fmt.Println("  help         Show this message")
	fmt.Println()
	fmt.Println("Flags (default daemon mode):")
	fmt.Println("  --ssid <ssid>          SSID for the HOST adapter (not the dongle)")
	fmt.Println("  --passphrase <pwd>     Passphrase for the host adapter")
	fmt.Println("  --port <port>          HTTP API server port (default 8990)")
	fmt.Println("  --simulate             Force simulated 802.11 handshake")
	fmt.Println()
	fmt.Println("Run `./bin/usbwifi <subcommand> --help` for subcommand options.")
}

// pidFilePath is where the daemon records its PID so a later --stop can
// find it. /var/run is root-writable, which the daemon already is.
const pidFilePath = "/var/run/usbwifi.pid"

// writePIDFile records this process for a later --stop. Failure is not
// fatal: --stop falls back to scanning for the process by name.
func writePIDFile() {
	if err := os.WriteFile(pidFilePath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		log.Printf("[INIT] could not write %s: %v (--stop will fall back to a process scan)", pidFilePath, err)
		return
	}
}

// stopRunningDaemon signals any running daemon to exit and waits for the
// API port to close. Returns a process exit code.
func stopRunningDaemon() int {
	pid := readPIDFile()
	if pid == 0 {
		pid = findDaemonPID()
	}
	if pid == 0 {
		fmt.Println("no running usbwifi daemon found")
		return 0
	}
	if pid == os.Getpid() {
		return 0
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not address pid %d: %v\n", pid, err)
		return 1
	}
	// SIGTERM first so the daemon can tear the utun interface down.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "could not signal pid %d: %v\n", pid, err)
		return 1
	}

	for range 40 { // up to 4s
		if !processAlive(pid) {
			_ = os.Remove(pidFilePath)
			fmt.Printf("stopped usbwifi daemon (pid %d)\n", pid)
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}

	// It did not go quietly.
	_ = proc.Signal(syscall.SIGKILL)
	time.Sleep(300 * time.Millisecond)
	_ = os.Remove(pidFilePath)
	fmt.Printf("force-killed usbwifi daemon (pid %d)\n", pid)
	return 0
}

func readPIDFile() int {
	b, err := os.ReadFile(pidFilePath)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 || !processAlive(pid) {
		return 0
	}
	return pid
}

// findDaemonPID locates a running daemon when no usable pidfile exists,
// which covers daemons started before --stop existed.
func findDaemonPID() int {
	out, err := exec.Command("pgrep", "-x", "usbwifi").Output()
	if err != nil {
		return 0
	}
	self := os.Getpid()
	for _, line := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(line)
		if err != nil || pid == self {
			continue
		}
		return pid
	}
	return 0
}

// processAlive reports whether pid is still running. Signal 0 performs
// the permission and existence checks without delivering anything.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
