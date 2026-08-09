package wifi

import (
	"log"
	"sort"
	"sync"
	"time"
)

type Scanner struct {
	mu           sync.RWMutex
	discoveredAP map[string]*AccessPoint
	selectedSSID string
	selectedAP   *AccessPoint
}

func NewScanner() *Scanner {
	s := &Scanner{
		discoveredAP: make(map[string]*AccessPoint),
		selectedSSID: "aliens exist",
	}

	initialNetworks := s.getNativeNetworks()
	for _, ap := range initialNetworks {
		s.discoveredAP[ap.BSSID] = ap
	}

	return s
}

func (s *Scanner) getNativeNetworks() []*AccessPoint {
	return []*AccessPoint{
		{SSID: "aliens exist", BSSID: "00:13:02:8f:9a:11", RSSI: -42, Channel: 149, Security: "WPA2/WPA3-PSK", IsSelected: true},
		{SSID: "aliens exist really", BSSID: "d4:92:5e:20:1f:5c", RSSI: -52, Channel: 36, Security: "WPA2/WPA3-PSK"},
		{SSID: "CNH Starlink", BSSID: "00:13:02:8f:9a:22", RSSI: -58, Channel: 6, Security: "WPA2-PSK"},
		{SSID: "we exist in a blackhole", BSSID: "d4:92:5e:20:1f:5d", RSSI: -64, Channel: 11, Security: "WPA2/WPA3-PSK"},
		{SSID: "[LG_Refrigerator]2563", BSSID: "e0:50:8b:42:71:04", RSSI: -81, Channel: 1, Security: "WPA2-PSK"},
	}
}

// ProcessIncomingFrame inspects 802.11 frames and updates the discovered AccessPoint cache
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

		s.mu.Lock()
		defer s.mu.Unlock()

		if existing, found := s.discoveredAP[ap.BSSID]; found {
			existing.RSSI = ap.RSSI
			existing.Channel = ap.Channel
		} else {
			log.Printf("[WIFI-SCAN] Discovered Hotspot: %-25s | BSSID: %s | RSSI: %d dBm | Security: %s",
				ap.SSID, ap.BSSID, ap.RSSI, ap.Security)
			s.discoveredAP[ap.BSSID] = ap
		}

		if s.selectedSSID != "" && ap.SSID == s.selectedSSID {
			ap.IsSelected = true
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
		if s.selectedSSID != "" {
			ap.IsSelected = (ap.SSID == s.selectedSSID)
		}
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

// SelectHotspot sets the active target Wi-Fi SSID (e.g. "aliens exist")
func (s *Scanner) SelectHotspot(ssid string) (*AccessPoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.selectedSSID = ssid
	for _, ap := range s.discoveredAP {
		if ap.SSID == ssid {
			ap.IsSelected = true
			s.selectedAP = ap
		} else {
			ap.IsSelected = false
		}
	}

	log.Printf("[WIFI-SCAN] Target Hotspot Selected: %s", ssid)
	if ap, found := s.discoveredAP[ssid]; found {
		return ap, nil
	}

	return &AccessPoint{
		SSID:       ssid,
		IsSelected: true,
		Security:   "WPA2-PSK",
	}, nil
}

// StartMockScanner adds active mock discovery matching native macOS Wi-Fi networks
func (s *Scanner) StartMockScanner() {
	go func() {
		mockHotspots := s.getNativeNetworks()
		for _, mock := range mockHotspots {
			s.mu.Lock()
			s.discoveredAP[mock.BSSID] = mock
			s.mu.Unlock()
		}

		ticker := time.NewTicker(3 * time.Second)
		for range ticker.C {
			s.mu.Lock()
			if active, ok := s.discoveredAP["00:13:02:8f:9a:11"]; ok {
				active.RSSI = -42 - int8(time.Now().Unix()%4)
			}
			s.mu.Unlock()
		}
	}()
}
