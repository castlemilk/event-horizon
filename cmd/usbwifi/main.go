package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/castlemilk/event-horizon/pkg/api"
	"github.com/castlemilk/event-horizon/pkg/tun"
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

	targetSSID := flag.String("ssid", "", "Target Wi-Fi SSID to connect (real association only when a passphrase is supplied)")
	passphrase := flag.String("passphrase", "", "WPA2/WPA3 passphrase for the target SSID (required for a real connection)")
	apiPort := flag.Int("port", 8990, "HTTP API Server port")
	simulate := flag.Bool("simulate", false, "Force simulated 802.11 handshake instead of a real association")
	flag.Parse()

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
	apiServer.Start()

	// 4. Initialize macOS Virtual utun Interface
	utunDev, err := tun.NewUtun()
	if err != nil {
		log.Printf("[TUN] Warning: %v (utun creation requires root/sudo privileges)", err)
	} else {
		defer utunDev.Close()
		utunDev.ConfigureIP("192.168.100.2", "255.255.255.0", "192.168.100.1")
		utunDev.AddStarlinkRoute()
		pump := tun.StartPacketPump(utunDev)
		defer pump.Stop()
	}

	// 5. Optional Real Association to a target hotspot
	if *targetSSID != "" && *passphrase != "" && !*simulate {
		log.Printf("[HOTSPOT] Performing real Wi-Fi association to '%s'...", *targetSSID)
		if err := wifi.AssociateViaCoreWLAN(*targetSSID, *passphrase); err != nil {
			log.Printf("[HOTSPOT] CoreWLAN association failed (%v); falling back to networksetup", err)
			if iface, ifaceErr := wifi.FindWiFiInterface(); ifaceErr == nil {
				if err := wifi.AssociateToNetwork(iface, *targetSSID, *passphrase); err != nil {
					log.Printf("[HOTSPOT] networksetup association failed: %v", err)
				} else {
					scanner.SetConnected(*targetSSID)
					log.Printf("[HOTSPOT] Successfully connected to '%s'", *targetSSID)
				}
			} else {
				log.Printf("[HOTSPOT] No Wi-Fi interface found: %v", ifaceErr)
			}
		} else {
			scanner.SetConnected(*targetSSID)
			log.Printf("[HOTSPOT] Successfully connected to '%s'", *targetSSID)
		}
	} else if *targetSSID != "" {
		time.Sleep(100 * time.Millisecond)
		_, _ = scanner.SelectHotspot(*targetSSID)
		log.Printf("[HOTSPOT] Target '%s' selected (no passphrase supplied; not connected)", *targetSSID)
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
	fmt.Println("  cmdctl       User-space LMAC command channel (version query, scan, listen)")
	fmt.Println("  help         Show this message")
	fmt.Println()
	fmt.Println("Flags (default daemon mode):")
	fmt.Println("  --ssid <ssid>          Target Wi-Fi SSID")
	fmt.Println("  --passphrase <pwd>     WPA2/WPA3 passphrase")
	fmt.Println("  --port <port>          HTTP API server port (default 8990)")
	fmt.Println("  --simulate             Force simulated 802.11 handshake")
	fmt.Println()
	fmt.Println("Run `./bin/usbwifi <subcommand> --help` for subcommand options.")
}
