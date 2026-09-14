package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castlemilk/event-horizon/pkg/usb"
	"github.com/castlemilk/event-horizon/pkg/wifi"
)

func TestAPIServerEndpoints(t *testing.T) {
	scanner := wifi.NewScanner()
	scanner.StartMockScanner()

	serverPort := 8999
	server := NewServer(scanner, serverPort)
	server.SimulateConnections = true
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

// TestStarlinkStatusDonglePresence checks the property the replug feature
// exists for: a physically absent dongle must read as absent with a replug
// instruction, never as "not associated" (which sends the operator to
// re-run the bring-up for hardware in a drawer). A ZeroCD dongle must read
// as present-needing-bring-up, never as needing a replug it already had.
func TestStarlinkStatusDonglePresence(t *testing.T) {
	prev := fetchDongles
	defer func() { fetchDongles = prev }()
	// Earlier tests in this package POST /api/wifi/connect (process-global
	// SSID) and write linkstate records (2s daemon-side cache); clear both
	// so this test observes the states it stubbed, not their leftovers.
	// HOME is sandboxed too: a live bridge on the dev machine writes a
	// real ~/.event-horizon/linkstate.json, and this test must not see it.
	usb.SetDongleConnected("")
	usb.InvalidateLinkStateCache()
	t.Setenv("HOME", t.TempDir())
	usb.InvalidateLinkStateCache()

	scanner := wifi.NewScanner()
	scanner.StartMockScanner()
	server := NewServer(scanner, 18999)
	server.Start()
	time.Sleep(200 * time.Millisecond)

	get := func() map[string]any {
		t.Helper()
		resp, err := http.Get("http://127.0.0.1:18999/api/starlink/status")
		if err != nil {
			t.Fatalf("GET /api/starlink/status: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		return body.Data
	}

	// Absent hardware: replug instruction, not association wording.
	fetchDongles = func() ([]usb.DeviceInfo, bool) { return nil, true }
	data := get()
	if data["dongle_present"] != false {
		t.Errorf("dongle_present = %v, want false", data["dongle_present"])
	}
	if data["status"] != "NO_DONGLE" {
		t.Errorf("status = %v, want NO_DONGLE", data["status"])
	}
	if reason, _ := data["reason"].(string); !strings.Contains(reason, "replug") {
		t.Errorf("reason does not suggest a replug: %q", reason)
	}

	// ZeroCD storage mode: present, bring-up instruction, no replug wording
	// beyond naming the mode.
	fetchDongles = func() ([]usb.DeviceInfo, bool) {
		return []usb.DeviceInfo{{VendorID: 0x0bda, ProductID: 0x1a2b, Name: "ZeroCD", IsStorage: true}}, true
	}
	data = get()
	if data["dongle_present"] != true {
		t.Errorf("dongle_present = %v, want true", data["dongle_present"])
	}
	if data["usb_stage"] != "zerocd" {
		t.Errorf("usb_stage = %v, want zerocd", data["usb_stage"])
	}
	if reason, _ := data["reason"].(string); !strings.Contains(reason, "/api/wifi/link") {
		t.Errorf("reason does not point at the link bring-up: %q", reason)
	}

	// Operational but unassociated: the old message, unchanged.
	fetchDongles = func() ([]usb.DeviceInfo, bool) {
		return []usb.DeviceInfo{{VendorID: 0x0bda, ProductID: 0xc811, Name: "WLAN", IsWlan: true}}, true
	}
	data = get()
	if data["usb_stage"] != "operational" {
		t.Errorf("usb_stage = %v, want operational", data["usb_stage"])
	}
	if reason, _ := data["reason"].(string); !strings.Contains(reason, "not associated") {
		t.Errorf("reason changed for the unassociated case: %q", reason)
	}

	// Cold cache: no completed pass yet. Presence is unknown, so the
	// response must not claim absent (which would flash a replug
	// instruction on every daemon start) nor present.
	fetchDongles = func() ([]usb.DeviceInfo, bool) { return nil, false }
	data = get()
	if _, ok := data["dongle_present"]; ok {
		t.Errorf("cold cache must omit dongle_present, got %v", data["dongle_present"])
	}
	if data["usb_stage"] != "probing" {
		t.Errorf("usb_stage = %v, want probing", data["usb_stage"])
	}
	if reason, _ := data["reason"].(string); strings.Contains(reason, "replug") {
		t.Errorf("cold cache must not suggest a replug: %q", reason)
	}
}

// TestStarlinkStatusLinkedFallback covers the remaining case: the USB
// bus is empty (the link CLI holds the device exclusively) but a live
// linkstate record exists. The endpoint must report LINKED with the
// recorded SSID — not NO_DONGLE (the link is working) and not the stale
// cached SSID from before (the record is the authority, and it names
// the check explicitly).
func TestStarlinkStatusLinkedFallback(t *testing.T) {
	prev := fetchDongles
	defer func() { fetchDongles = prev }()
	usb.SetDongleConnected("Stale Network")
	usb.InvalidateLinkStateCache()

	scanner := wifi.NewScanner()
	scanner.StartMockScanner()
	server := NewServer(scanner, 18997)
	server.Start()
	time.Sleep(200 * time.Millisecond)

	get := func() map[string]any {
		t.Helper()
		resp, err := http.Get("http://127.0.0.1:18997/api/starlink/status")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.Data
	}

	// Bus empty, no CLI record: stale cached SSID must not leak.
	fetchDongles = func() ([]usb.DeviceInfo, bool) { return nil, true }
	if d := get(); d["ssid"] != "" || d["associated"] != false {
		t.Fatalf("stale SSID leaked with no link record: %+v", d)
	}

	// Bus empty, live CLI record for a DIFFERENT network: the record
	// wins, and the reason says why the daemon can't see the radio.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	rec := map[string]any{
		"ssid": "Uncle Rad-Guest", "iface": "utun11", "ip": "192.168.2.243",
		"pid": float64(os.Getpid()), "startedAt": time.Now().Format(time.RFC3339Nano),
	}
	b, _ := json.Marshal(rec)
	_ = os.MkdirAll(filepath.Join(dir, ".event-horizon"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, ".event-horizon", "linkstate.json"), b, 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	usb.InvalidateLinkStateCache()

	data := get()
	if data["ssid"] != "Uncle Rad-Guest" {
		t.Errorf("ssid = %v, want the linkstate record (not the stale cache)", data["ssid"])
	}
	if data["associated"] != true {
		t.Errorf("associated = %v, want true while the link record is live", data["associated"])
	}
	if data["status"] != "LINKED" {
		t.Errorf("status = %v, want LINKED", data["status"])
	}
	if reason, _ := data["reason"].(string); !strings.Contains(reason, "holds the USB claim") {
		t.Errorf("reason does not explain the blind daemon: %q", reason)
	}
}
// TestStarlinkStatusClearsStaleSSIDOnUnplug locks the bug where a dongle
// that was associated, then physically disappeared, would continue to
// report its last-known SSID + "associated: true" — sending the menu
// bar app and the Web UI into a confidently-wrong state that no one
// could reproduce because it looked healthy. The fix: gate the cached
// SSID on a confirmed-dongle reading; when the dongle is absent,
// `ssid` and `associated` are empty/false no matter what the cache
// remembers.
func TestStarlinkStatusClearsStaleSSIDOnUnplug(t *testing.T) {
	prev := fetchDongles
	defer func() { fetchDongles = prev }()
	// Same sandboxing as above: the LINKED test runs first and leaves a
	// live linkstate record behind (2s daemon-side cache + a live bridge
	// on the dev machine writing the real file). Without this, that
	// record leaks in and this test observes LINKED instead of NO_DONGLE.
	usb.SetDongleConnected("") // start clean
	usb.InvalidateLinkStateCache()
	t.Setenv("HOME", t.TempDir())
	usb.InvalidateLinkStateCache()

	scanner := wifi.NewScanner()
	scanner.StartMockScanner()
	server := NewServer(scanner, 18998)
	server.Start()
	time.Sleep(200 * time.Millisecond)

	get := func() map[string]any {
		t.Helper()
		resp, err := http.Get("http://127.0.0.1:18998/api/starlink/status")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.Data
	}

	// Step 1: dongle present, associated. SetDongleConnected is the same
	// path the link service uses when an association succeeds.
	usb.SetDongleConnected("Uncle Rad-Guest")
	fetchDongles = func() ([]usb.DeviceInfo, bool) {
		return []usb.DeviceInfo{{VendorID: 0xa69c, ProductID: 0xc811, Name: "WLAN", IsWlan: true}}, true
	}
	if d := get(); d["ssid"] != "Uncle Rad-Guest" || d["associated"] != true {
		t.Fatalf("baseline not associated: %+v", d)
	}

	// Step 2: dongle physically unplugged. Cache still holds the SSID.
	// The handler must clear both `ssid` and `associated`, never claim
	// "associated with aliens exist" while only a ZeroCD ghost remains.
	fetchDongles = func() ([]usb.DeviceInfo, bool) { return nil, true }
	data := get()
	if data["ssid"] != "" {
		t.Errorf("dongle unplugged but ssid still cached: %q", data["ssid"])
	}
	if data["associated"] != false {
		t.Errorf("dongle unplugged but associated still true: %v", data["associated"])
	}
	if data["status"] != "NO_DONGLE" {
		t.Errorf("status = %v, want NO_DONGLE", data["status"])
	}
}
