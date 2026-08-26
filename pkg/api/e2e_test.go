package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castlemilk/event-horizon/pkg/wifi"
)

func TestFullE2EServerSuite(t *testing.T) {
	scanner := wifi.NewScanner()
	scanner.StartMockScanner()

	serverPort := 8998
	server := NewServer(scanner, serverPort)
	server.SimulateConnections = true
	server.Start()

	// Wait briefly for HTTP server listener setup
	time.Sleep(150 * time.Millisecond)

	baseURL := "http://127.0.0.1:8998"

	// 1. E2E GET /api/status
	t.Run("E2E Status Endpoint", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/status")
		if err != nil {
			t.Fatalf("GET /api/status failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode status response JSON: %v", err)
		}

		if body["status"] != "success" {
			t.Errorf("Expected status='success', got %v", body["status"])
		}
	})

	// 2. E2E GET /api/wifi/scan
	t.Run("E2E Scan Endpoint", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/wifi/scan")
		if err != nil {
			t.Fatalf("GET /api/wifi/scan failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode scan JSON: %v", err)
		}

		hotspots, ok := body["data"].([]interface{})
		if !ok || len(hotspots) == 0 {
			t.Errorf("Expected non-empty hotspots list in scan endpoint data field")
		}
	})

	// 3. E2E GET /api/hardware/topology
	t.Run("E2E Topology Endpoint", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/hardware/topology")
		if err != nil {
			t.Fatalf("GET /api/hardware/topology failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}
	})

	// 4. E2E GET /metrics (Prometheus OTel)
	t.Run("E2E Prometheus Metrics Endpoint", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read /metrics body: %v", err)
		}

		bodyStr := string(data)
		if !strings.Contains(bodyStr, "net_bytes_rx_total") {
			t.Errorf("Expected Prometheus metric 'net_bytes_rx_total' in /metrics output")
		}
	})

	// 5. E2E POST /api/wifi/connect
	t.Run("E2E Connect Hotspot Endpoint", func(t *testing.T) {
		hotspots := server.scanner.ListHotspots()
		targetSSID := "SFH"
		if len(hotspots) > 0 {
			targetSSID = hotspots[0].SSID
		}
		payload := map[string]string{
			"ssid":       targetSSID,
			"passphrase": "starlink_secret",
		}
		jsonBytes, _ := json.Marshal(payload)

		resp, err := http.Post(baseURL+"/api/wifi/connect", "application/json", bytes.NewBuffer(jsonBytes))
		if err != nil {
			t.Fatalf("POST /api/wifi/connect failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}
	})

	// 6. E2E GET /api/diagnostics/ping?interface=en0
	t.Run("E2E Ping Endpoint", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/diagnostics/ping?interface=en0")
		if err != nil {
			t.Fatalf("GET /api/diagnostics/ping failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}
	})

	// 7. E2E GET /api/diagnostics/speedtest?interface=en0
	t.Run("E2E SpeedTest Endpoint", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/diagnostics/speedtest?interface=en0")
		if err != nil {
			t.Fatalf("GET /api/diagnostics/speedtest failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}
	})
}
