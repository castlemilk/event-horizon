package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"dorja-labs/starlink-sdk/pkg/netstat"
	"dorja-labs/starlink-sdk/pkg/otel"
	"dorja-labs/starlink-sdk/pkg/ping"
	"dorja-labs/starlink-sdk/pkg/uptime"
	"dorja-labs/starlink-sdk/pkg/usb"
	"dorja-labs/starlink-sdk/pkg/wifi"
)

type Server struct {
	scanner  *wifi.Scanner
	monitor  *netstat.Monitor
	tester   *ping.Tester
	tracker  *uptime.Tracker
	exporter *otel.OTelExporter
	port     int
}

func NewServer(scanner *wifi.Scanner, port int) *Server {
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

	// GET /api/wifi/scan - List all discovered Wi-Fi hotspots
	mux.HandleFunc("/api/wifi/scan", corsHandler(func(w http.ResponseWriter, r *http.Request) {
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

		conn := wifi.NewWPAConnection(req.SSID, req.Passphrase, ap.BSSID)
		go func() {
			if err := conn.Connect(); err != nil {
				log.Printf("[API] Connection error: %v", err)
			}
		}()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status:  "success",
			Message: fmt.Sprintf("Initiating connection to '%s'", req.SSID),
			Data:    ap,
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

	// GET /api/hardware/topology - 3-tier mapping: USB Driver -> BSD Interface -> Network Connection
	mux.HandleFunc("/api/hardware/topology", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		topology := usb.GetHardwareTopology()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   topology,
		})
	}))

	// GET /api/diagnostics/ping?interface=en0 - Live TCP/ICMP ping diagnostic test bound to given interface
	mux.HandleFunc("/api/diagnostics/ping", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		iface := r.URL.Query().Get("interface")
		if iface == "" {
			iface = "en0"
		}
		results := s.tester.RunDiagnosticsOnInterface(iface)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status: "success",
			Data:   results,
		})
	}))

	// GET /api/diagnostics/speedtest?interface=en0 - Live HTTP download/upload speed test bound to interface
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
