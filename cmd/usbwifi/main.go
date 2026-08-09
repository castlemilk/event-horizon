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
	targetSSID := flag.String("ssid", "aliens exist", "Target Wi-Fi SSID to connect")
	apiPort := flag.Int("port", 8990, "HTTP API Server port")
	mockMode := flag.Bool("mock", true, "Enable Wi-Fi scan simulation engine")
	flag.Parse()

	fmt.Println("================================================================")
	fmt.Println("  📡 Event Horizon USB Wi-Fi & Network Manager Daemon v1.0.0")
	fmt.Println("================================================================")

	// 1. USB Auto-Detection & ModeSwitch
	log.Printf("[INIT] Scanning USB bus for Wi-Fi dongles (Ugreen / AIC / Realtek)...")
	devInfo, err := usb.CheckAndSwitchDevices()
	if err != nil {
		log.Printf("[USB] Info: %v (Continuing in user-space emulation mode)", err)
	} else {
		log.Printf("[USB] Successfully initialized device: %s (VID: 0x%04x, PID: 0x%04x)",
			devInfo.Name, devInfo.VendorID, devInfo.ProductID)
	}

	// 2. Initialize Wi-Fi Scanner Engine
	scanner := wifi.NewScanner()
	if *mockMode {
		log.Printf("[WIFI] Starting 802.11 Beacon & Probe Scanner...")
		scanner.StartMockScanner()
	}

	// 3. Start HTTP / REST API Server
	apiServer := api.NewServer(scanner, *apiPort)
	apiServer.Start()

	// 4. Initialize macOS Virtual utun Interface
	utunDev, err := tun.NewUtun()
	if err != nil {
		log.Printf("[TUN] Warning: %v (utun creation requires root/sudo privileges)", err)
	} else {
		defer utunDev.Close()
		utunDev.ConfigureIP("192.168.100.2", "255.255.255.0", "192.168.100.1")
		utunDev.AddStarlinkRoute()
	}

	// 5. Select Default Target Hotspot
	if *targetSSID != "" {
		time.Sleep(500 * time.Millisecond)
		log.Printf("[HOTSPOT] Automatically selecting target hotspot: '%s'", *targetSSID)
		ap, _ := scanner.SelectHotspot(*targetSSID)
		conn := wifi.NewWPAConnection(ap.SSID, "", ap.BSSID)
		go conn.Connect()
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
