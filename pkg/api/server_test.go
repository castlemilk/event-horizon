package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/castlemilk/event-horizon/pkg/wifi"
)

func TestAPIServerEndpoints(t *testing.T) {
	scanner := wifi.NewScanner()
	scanner.StartMockScanner()

	serverPort := 8999
	server := NewServer(scanner, serverPort)
	server.Start()

	time.Sleep(200 * time.Millisecond)

	baseURL := "http://127.0.0.1:8999"

	// 1. GET /api/status
	resp, err := http.Get(baseURL + "/api/status")
	if err != nil {
		t.Fatalf("Failed to GET /api/status: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. GET /api/wifi/scan
	resp, err = http.Get(baseURL + "/api/wifi/scan")
	if err != nil {
		t.Fatalf("Failed to GET /api/wifi/scan: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 3. GET /api/hardware/topology
	resp, err = http.Get(baseURL + "/api/hardware/topology")
	if err != nil {
		t.Fatalf("Failed to GET /api/hardware/topology: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. POST /api/wifi/connect
	payload := map[string]string{
		"ssid":       "SFH",
		"passphrase": "cnh12345",
	}
	jsonBody, _ := json.Marshal(payload)
	resp, err = http.Post(baseURL+"/api/wifi/connect", "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		t.Fatalf("Failed to POST /api/wifi/connect: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
