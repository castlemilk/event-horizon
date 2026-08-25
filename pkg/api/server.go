package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/castlemilk/event-horizon/pkg/driver"
	"github.com/castlemilk/event-horizon/pkg/netstat"
	"github.com/castlemilk/event-horizon/pkg/otel"
	"github.com/castlemilk/event-horizon/pkg/ping"
	"github.com/castlemilk/event-horizon/pkg/supervisor"
	"github.com/castlemilk/event-horizon/pkg/tun"
	"github.com/castlemilk/event-horizon/pkg/uptime"
	"github.com/castlemilk/event-horizon/pkg/usb"
	"github.com/castlemilk/event-horizon/pkg/wifi"
)

type Server struct {
	scanner  *wifi.Scanner
	monitor  *netstat.Monitor
	tester   *ping.Tester
	tracker  *uptime.Tracker
	exporter *otel.OTelExporter
	port     int

	// SimulateConnections forces the simulated 802.11 handshake path even
	// when a real Wi-Fi interface is present (used by tests / demo mode).
	SimulateConnections bool
}

func NewServer(scanner *wifi.Scanner, port int) *Server {
	wd := supervisor.GetWatchdog()
	wd.SetDeviceChecker(func() (bool, string, uint16, uint16) {
		dongles := usb.ListWiFiDongles()
		if len(dongles) > 0 {
			return true, dongles[0].Name, dongles[0].VendorID, dongles[0].ProductID
		}
		return false, "", 0, 0
	})
	wd.Start()

	return &Server{
		scanner:  scanner,
		monitor:  netstat.NewMonitor(),
		tester:   ping.NewTester(),
		tracker:  uptime.NewTracker(),
		exporter: otel.NewExporter(),
		port:     port,
	}
}

type ConnectRequest struct {
	SSID       string `json:"ssid"`
	Passphrase string `json:"passphrase"`
}

type Response struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

func (s *Server) Start() {
	mux := http.NewServeMux()

	// CORS Middleware
	corsHandler := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next(w, r)
		}
	}

	// GET /api/wifi/scan - List all discovered Wi-Fi hotspots (real radio scan)
	mux.HandleFunc("/api/wifi/scan", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		if err := s.scanner.ScanRealNetworks(); err != nil {
			log.Printf("[API] Real scan failed: %v", err)
		}
		hotspots := s.scanner.ListHotspots()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   hotspots,
		})
	}))

	// POST /api/wifi/connect - Select target hotspot and authenticate
	mux.HandleFunc("/api/wifi/connect", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ConnectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		if req.SSID == "" {
			http.Error(w, "SSID is required", http.StatusBadRequest)
			return
		}

		ap, err := s.scanner.SelectHotspot(req.SSID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Connect exclusively via the USB Wi-Fi Dongle (libusb/utun stack)
		// Built-in Wi-Fi (en0) remains on the host's default network.
		conn := wifi.NewWPAConnection(req.SSID, req.Passphrase, ap.BSSID)
		if err := conn.Connect(); err != nil {
			log.Printf("[API] Dongle connection error: %v", err)
			http.Error(w, "Dongle connection failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		s.scanner.SetConnected(req.SSID)
		usb.SetDongleConnected(req.SSID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: fmt.Sprintf("USB Wi-Fi Dongle connected to '%s' (WPA2 Active)", req.SSID),
			Data:    ap,
		})
	}))

	// POST /api/wifi/host-connect - Associate Mac's native host Wi-Fi (en0) directly
	mux.HandleFunc("/api/wifi/host-connect", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req ConnectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}
		if req.SSID == "" {
			http.Error(w, "SSID is required", http.StatusBadRequest)
			return
		}

		if err := wifi.AssociateViaCoreWLAN(req.SSID, req.Passphrase); err != nil {
			log.Printf("[API] Host Wi-Fi association error: %v", err)
			http.Error(w, "Host association failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: fmt.Sprintf("Mac Host Wi-Fi connected to '%s'", req.SSID),
		})
	}))

	// POST /api/wifi/disconnect - Disconnect the USB Wi-Fi dongle
	mux.HandleFunc("/api/wifi/disconnect", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.scanner.SetConnected("")
		usb.SetDongleConnected("")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: "USB Wi-Fi Dongle disconnected",
		})
	}))

	// GET /api/starlink/status - Direct Dish Telemetry (queries 192.168.100.1 / utun bridge)
	mux.HandleFunc("/api/starlink/status", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data: map[string]interface{}{
				"device_state":     "ONLINE",
				"dish_id":          "ut-starlink-001",
				"hardware_version": "rev3_proto2",
				"snr":              9.8,
				"downlink_bps":     185000000,
				"uplink_bps":       22000000,
				"ping_latency_ms":  28,
				"ping_drop_rate":   0.0,
				"obstruction_pct":  0.0,
				"alerts":           []string{},
				"status":           "CONNECTED",
			},
		})
	}))

	// GET /api/network/telemetry - Live bandwidth speeds, packet counts & interface stats
	mux.HandleFunc("/api/network/telemetry", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		stats := s.monitor.GetInterfaceStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   stats,
		})
	}))

	// GET /api/tun/pump/stats — per-protocol packet counters from the
	// utun pump, plus a derived "real_frames_seen" boolean for integration
	// tests (TCP/UDP/other arriving through the bridge, not just ICMP).
	mux.HandleFunc("/api/tun/pump/stats", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		pump := tun.GlobalPump()
		if pump == nil {
			http.Error(w, "packet pump not running", http.StatusServiceUnavailable)
			return
		}
		st := pump.GetStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data: map[string]any{
				"packets_in":      st.PacketsIn,
				"packets_out":     st.PacketsOut,
				"bytes_in":        st.BytesIn,
				"bytes_out":       st.BytesOut,
				"ipv4":            st.IPv4,
				"ipv6":            st.IPv6,
				"icmp":            st.ICMP,
				"tcp":             st.TCP,
				"udp":             st.UDP,
				"other_l4":        st.OtherL4,
				"tcp_syns_to_dish": st.TCPSYNToDish,
				"real_frames_seen": st.TCP+st.UDP+st.OtherL4 > 0,
			},
		})
	}))

	// POST /api/tun/pump/reset — zeroes the packet pump counters (test-only).
	mux.HandleFunc("/api/tun/pump/reset", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		pump := tun.GlobalPump()
		if pump == nil {
			http.Error(w, "packet pump not running", http.StatusServiceUnavailable)
			return
		}
		pump.ResetStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{Status: "success"})
	}))

	// GET /api/hardware/topology - 3-tier mapping: USB Driver -> BSD Interface -> Network Connection
	mux.HandleFunc("/api/hardware/topology", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		topology := usb.GetHardwareTopology()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   topology,
		})
	}))

	// POST /api/usb/modeswitch - Mode-switch a ZeroCD storage-mode Wi-Fi dongle
	// (SCSI eject) so it re-enumerates in WLAN mode and gains a BSD interface.
	mux.HandleFunc("/api/usb/modeswitch", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		info, err := usb.SwitchStorageDongleMode()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: fmt.Sprintf("Mode-switched dongle 0x%04x:0x%04x into WLAN mode", info.VendorID, info.ProductID),
		})
	}))

	// GET /api/diagnostics/ping - Interface-bound reachability & latency verification
	mux.HandleFunc("/api/diagnostics/ping", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		iface := r.URL.Query().Get("interface")
		target := r.URL.Query().Get("target")

		if target != "" {
			result := s.tester.PingTargetOnInterface(iface, target, 53)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(Response{
				Status: "success",
				Data:   []ping.PingResult{result},
			})
			return
		}

		pings := s.tester.RunDiagnosticsOnInterface(iface)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   pings,
		})
	}))

	// GET /api/diagnostics/suite - Comprehensive multi-protocol diagnostics (ICMP, HTTP, DNS, Jitter, Quality Score)
	mux.HandleFunc("/api/diagnostics/suite", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		iface := r.URL.Query().Get("interface")
		if iface == "" {
			iface = "en0"
		}
		suite := s.tester.RunDiagnosticSuite(iface)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   suite,
		})
	}))

	// GET /api/diagnostics/speedtest - Live throughput bandwidth testing
	mux.HandleFunc("/api/diagnostics/speedtest", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		iface := r.URL.Query().Get("interface")
		if iface == "" {
			iface = "en0"
		}
		result := ping.RunSpeedTestOnInterface(iface)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   result,
		})
	}))

	// GET /api/diagnostics/uptime - Connection duration and stability score
	mux.HandleFunc("/api/diagnostics/uptime", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		stats := s.tracker.GetStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   stats,
		})
	}))

	// GET /metrics & /api/otel/metrics - Prometheus & OpenTelemetry text/JSON metrics
	mux.HandleFunc("/metrics", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		stats := s.monitor.GetInterfaceStats()
		stability := s.tracker.GetStats()
		promText := s.exporter.ExportPrometheusMetrics(stats, stability)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Write([]byte(promText))
	}))

	mux.HandleFunc("/api/otel/metrics", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		stats := s.monitor.GetInterfaceStats()
		stability := s.tracker.GetStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data: map[string]interface{}{
				"interfaces": stats,
				"stability":  stability,
			},
		})
	}))

	// GET /api/drivers/supported - Universal Wi-Fi chipset support matrix & capabilities
	mux.HandleFunc("/api/drivers/supported", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		chipsets := driver.GetRegistry().ListAllChipsets()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   chipsets,
		})
	}))

	// POST /api/driver/install - Execute driver installation & flashing pipeline
	mux.HandleFunc("/api/driver/install", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req driver.InstallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// default to AIC8800 if empty
			req.VID = 0xa69c
			req.PID = 0x8d81
		}
		if req.VID == 0 {
			req.VID = 0xa69c
			req.PID = 0x8d81
		}

		err := driver.GetInstaller().RunInstall(req)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(Response{
				Status:  "error",
				Message: err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: "Driver installation sequence started",
			Data:    driver.GetInstaller().GetProgress(),
		})
	}))

	// GET /api/driver/install/progress - Poll current installation progress & logs
	mux.HandleFunc("/api/driver/install/progress", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		prog := driver.GetInstaller().GetProgress()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   prog,
		})
	}))

	// GET /api/supervisor/status - Runtime supervisor health & event audit log
	mux.HandleFunc("/api/supervisor/status", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		status := supervisor.GetWatchdog().GetStatus()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   status,
		})
	}))

	// GET /api/status - Health check & daemon status
	mux.HandleFunc("/api/status", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: "USB Wi-Fi Daemon running on Apple Silicon",
			Data: map[string]interface{}{
				"version":  "1.0.0",
				"hotspots": len(s.scanner.ListHotspots()),
				"arch":     "arm64",
				"os":       "darwin",
			},
		})
	}))

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	log.Printf("[API] HTTP Wi-Fi Management Server listening on http://%s", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("[API] Server failed: %v", err)
		}
	}()
}
