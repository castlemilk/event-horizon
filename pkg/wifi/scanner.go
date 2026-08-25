package wifi

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// coreWLANScanScript drives a real scan of the system Wi-Fi radio through the
// CoreWLAN framework. It is executed via the `swift` interpreter so CoreWLAN
// inherits the invoking shell's TCC/Location permission (a compiled binary
// loses this entitlement and returns an empty scan).
const coreWLANScanScript = `import CoreWLAN
import Foundation
let client = CWWiFiClient.shared()
guard let iface = client.interface() else { exit(1) }
do {
    let networks = try iface.scanForNetworks(withName: nil)
    for n in networks {
        let ssid = n.ssid ?? "<hidden>"
        let bssid = n.bssid ?? ""
        print("\(ssid)\t\(bssid)\t\(n.rssiValue)\t\(n.wlanChannel?.channelNumber ?? 0)")
    }
} catch {
    exit(2)
}
`

// scanInterval throttles how often the underlying CoreWLAN radio scan runs.
// Scans are expensive (seconds of Swift interpreter startup + radio sweep), so
// API polls reuse the cached result for this long.
const scanInterval = 15 * time.Second

type Scanner struct {
	mu           sync.RWMutex
	discoveredAP map[string]*AccessPoint
	selectedSSID string
	connectedSSID string
	selectedAP   *AccessPoint
	lastScan     time.Time
}

func NewScanner() *Scanner {
	return &Scanner{
		discoveredAP: make(map[string]*AccessPoint),
	}
}

// apKey builds a stable cache key. CoreWLAN often returns an empty BSSID, so
// the SSID+channel pair is used when no BSSID is available.
func apKey(ssid, bssid string, channel uint8) string {
	if bssid != "" {
		return bssid
	}
	return fmt.Sprintf("%s|ch%d", ssid, channel)
}

// ScanRealNetworks performs a real 802.11 scan of the system Wi-Fi radio and
// ingests the observed networks into the discovered cache. The result is
// throttled to scanInterval; pass force=true to bypass the throttle.
func (s *Scanner) ScanRealNetworks(force ...bool) error {
	s.mu.RLock()
	fresh := !s.lastScan.IsZero() && time.Since(s.lastScan) < scanInterval
	s.mu.RUnlock()
	if fresh && !(len(force) > 0 && force[0]) {
		return nil
	}

	script, err := s.writeScanScript()
	if err != nil {
		return err
	}
	defer os.Remove(script)

	cmd := execCommandAsConsoleUser("swift", script)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("corewlan scan failed: %w", err)
	}

	// Auto-detect current active system SSID if not explicitly set
	if s.connectedSSID == "" {
		if iface, err := FindWiFiInterface(); err == nil && iface != "" {
			if cur, err := CurrentSSID(iface); err == nil && cur != "" {
				s.connectedSSID = cur
			}
		}
	}

	ingested := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		ssid := parts[0]
		bssid := ""
		if len(parts) > 1 {
			bssid = parts[1]
		}
		rssi, _ := strconv.ParseInt(parts[2], 10, 8)
		channel := uint8(0)
		if len(parts) > 3 {
			if ch, err := strconv.ParseUint(parts[3], 10, 8); err == nil {
				channel = uint8(ch)
			}
		}

		ap := &AccessPoint{
			SSID:     ssid,
			BSSID:    bssid,
			RSSI:     int8(rssi),
			Channel:  channel,
			Security: "",
		}
		ap.IsSelected = (ssid != "" && (ssid == s.connectedSSID || (s.connectedSSID == "" && ssid == s.selectedSSID)))

		s.mu.Lock()
		key := apKey(ssid, bssid, channel)
		if existing, found := s.discoveredAP[key]; found {
			existing.RSSI = ap.RSSI
			existing.Channel = ap.Channel
			existing.BSSID = bssid
		} else {
			log.Printf("[WIFI-SCAN] Discovered Hotspot: %-25s | BSSID: %s | RSSI: %d dBm | Channel: %d",
				ap.SSID, ap.BSSID, ap.RSSI, ap.Channel)
			s.discoveredAP[key] = ap
		}
		if ap.IsSelected {
			s.selectedAP = ap
		}
		s.lastScan = time.Now()
		s.mu.Unlock()
		ingested++
	}

	log.Printf("[WIFI-SCAN] Real scan complete: %d networks observed", ingested)
	return nil
}

func (s *Scanner) writeScanScript() (string, error) {
	candidates := []string{"/tmp", os.TempDir()}
	var lastErr error
	for _, dir := range candidates {
		path := filepath.Join(dir, fmt.Sprintf("eh-corewlan-scan-%d.swift", time.Now().UnixNano()))
		if err := os.WriteFile(path, []byte(coreWLANScanScript), 0o600); err == nil {
			return path, nil
		} else {
			lastErr = err
		}
	}
	return "", lastErr
}

// ProcessIncomingFrame inspects 802.11 frames and updates the discovered
// AccessPoint cache. This is the dongle discovery feed: frames captured from
// the USB Wi-Fi dongle's radio are parsed here, so the cache reflects what the
// dongle observes.
func (s *Scanner) ProcessIncomingFrame(raw []byte, rssi int8) {
	frame, err := ParseFrame(raw, rssi)
	if err != nil {
		return
	}

	// 802.11 Beacon (0x08) or Probe Response (0x05)
	subtype := (frame.FrameControl >> 4) & 0x0F
	if (frame.FrameControl & 0x0C) == 0x00 && (subtype == SubtypeBeacon || subtype == SubtypeProbeResponse) {
		ap, err := frame.ParseBeacon()
		if err != nil {
			return
		}

		ap.IsSelected = (ap.SSID == s.connectedSSID || (s.connectedSSID == "" && ap.SSID == s.selectedSSID))

		s.mu.Lock()
		defer s.mu.Unlock()

		key := apKey(ap.SSID, ap.BSSID, ap.Channel)
		if existing, found := s.discoveredAP[key]; found {
			existing.RSSI = ap.RSSI
			existing.Channel = ap.Channel
		} else {
			log.Printf("[WIFI-SCAN] Dongle Discovered Hotspot: %-25s | BSSID: %s | RSSI: %d dBm | Security: %s",
				ap.SSID, ap.BSSID, ap.RSSI, ap.Security)
			s.discoveredAP[key] = ap
		}

		if ap.IsSelected {
			s.selectedAP = ap
		}
	}
}

// ListHotspots returns a snapshot list of all discovered Wi-Fi hotspots, deterministically sorted
func (s *Scanner) ListHotspots() []*AccessPoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*AccessPoint
	for _, ap := range s.discoveredAP {
		ap.IsSelected = (ap.SSID != "" && (ap.SSID == s.connectedSSID || (s.connectedSSID == "" && ap.SSID == s.selectedSSID)))
		result = append(result, ap)
	}

	// Deterministic Sort: Selected AP first, then by SSID alphabetically
	sort.Slice(result, func(i, j int) bool {
		if result[i].IsSelected != result[j].IsSelected {
			return result[i].IsSelected // Selected item always on top
		}
		return result[i].SSID < result[j].SSID
	})

	return result
}

// SetConnected marks the given SSID as the actively connected network.
func (s *Scanner) SetConnected(ssid string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connectedSSID = ssid
	for _, ap := range s.discoveredAP {
		ap.IsSelected = (ap.SSID == ssid)
		if ap.IsSelected {
			s.selectedAP = ap
		}
	}
	log.Printf("[WIFI-SCAN] Connected to hotspot: %s", ssid)
}

// ConnectedSSID returns the SSID currently marked as connected, or "".
func (s *Scanner) ConnectedSSID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connectedSSID
}

// SelectHotspot sets the active target Wi-Fi SSID.
func (s *Scanner) SelectHotspot(ssid string) (*AccessPoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.selectedSSID = ssid
	for _, ap := range s.discoveredAP {
		ap.IsSelected = (ap.SSID == ssid)
		if ap.IsSelected {
			s.selectedAP = ap
		}
	}

	log.Printf("[WIFI-SCAN] Target Hotspot Selected: %s", ssid)
	if ap := s.selectedAP; ap != nil && ap.SSID == ssid {
		return ap, nil
	}
	return &AccessPoint{SSID: ssid, IsSelected: true}, nil
}

// StartMockScanner populates mock access points for unit tests.
func (s *Scanner) StartMockScanner() {
	s.mu.Lock()
	defer s.mu.Unlock()
	mockAPs := []*AccessPoint{
		{SSID: "SFH", BSSID: "00:13:02:8f:9a:33", RSSI: -45, Channel: 6, Security: "WPA2-PSK"},
		{SSID: "aliens exist", BSSID: "00:13:02:8f:9a:34", RSSI: -62, Channel: 11, Security: "WPA2-PSK"},
	}
	for _, ap := range mockAPs {
		s.discoveredAP[apKey(ap.SSID, ap.BSSID, ap.Channel)] = ap
	}
}

