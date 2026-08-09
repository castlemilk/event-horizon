package wifi

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// 802.11 Frame Types and Subtypes
const (
	TypeManagement = 0x00
	TypeControl    = 0x01
	TypeData       = 0x02

	SubtypeBeacon        = 0x08
	SubtypeProbeRequest  = 0x04
	SubtypeProbeResponse = 0x05
	SubtypeAuth          = 0x0B
	SubtypeAssocReq      = 0x00
	SubtypeAssocResp     = 0x01
	SubtypeQoSData       = 0x08
)

// Frame80211 represents a parsed 802.11 MAC Frame
type Frame80211 struct {
	FrameControl uint16
	Duration     uint16
	Address1     [6]byte // Receiver / BSSID
	Address2     [6]byte // Transmitter / Source MAC
	Address3     [6]byte // BSSID / Destination MAC
	SeqControl   uint16
	Payload      []byte
	RSSI         int8
}

// AccessPoint represents a discovered Wi-Fi network/hotspot
type AccessPoint struct {
	SSID      string `json:"ssid"`
	BSSID     string `json:"bssid"`
	RSSI      int8   `json:"rssi"`
	Channel   uint8  `json:"channel"`
	Security  string `json:"security"`
	IsSelected bool   `json:"is_selected"`
}

// ParseFrame parses a raw 802.11 frame packet buffer
func ParseFrame(data []byte, rssi int8) (*Frame80211, error) {
	if len(data) < 24 {
		return nil, fmt.Errorf("frame buffer too short")
	}

	frame := &Frame80211{
		FrameControl: binary.LittleEndian.Uint16(data[0:2]),
		Duration:     binary.LittleEndian.Uint16(data[2:4]),
		SeqControl:   binary.LittleEndian.Uint16(data[22:24]),
		Payload:      data[24:],
		RSSI:         rssi,
	}

	copy(frame.Address1[:], data[4:10])
	copy(frame.Address2[:], data[10:16])
	copy(frame.Address3[:], data[16:22])

	return frame, nil
}

// ParseBeacon extracts AccessPoint details from an 802.11 Beacon or Probe Response frame
func (f *Frame80211) ParseBeacon() (*AccessPoint, error) {
	if len(f.Payload) < 12 {
		return nil, fmt.Errorf("invalid beacon payload length")
	}

	// Skip Timestamp (8 bytes) + Beacon Interval (2 bytes) + Capability Info (2 bytes)
	offset := 12
	payload := f.Payload

	ap := &AccessPoint{
		BSSID:    fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", f.Address3[0], f.Address3[1], f.Address3[2], f.Address3[3], f.Address3[4], f.Address3[5]),
		RSSI:     f.RSSI,
		Security: "Open",
	}

	// Parse Information Elements (IEs)
	for offset+2 <= len(payload) {
		ieID := payload[offset]
		ieLen := int(payload[offset+1])
		if offset+2+ieLen > len(payload) {
			break
		}

		ieData := payload[offset+2 : offset+2+ieLen]

		switch ieID {
		case 0: // SSID IE
			ap.SSID = string(ieData)
		case 3: // DS Parameter Set (Channel)
			if ieLen >= 1 {
				ap.Channel = ieData[0]
			}
		case 48: // RSN / WPA2 IE
			ap.Security = "WPA2-PSK"
		case 221: // Vendor Specific IE (WPA1 or WPA3/WPS)
			if bytes.HasPrefix(ieData, []byte{0x00, 0x50, 0xf2, 0x01}) {
				ap.Security = "WPA-PSK"
			}
		}

		offset += 2 + ieLen
	}

	if ap.SSID == "" {
		ap.SSID = "<Hidden SSID>"
	}

	return ap, nil
}

// BuildProbeRequest constructs an 802.11 Probe Request frame for SSID scanning
func BuildProbeRequest(srcMAC [6]byte) []byte {
	buf := new(bytes.Buffer)

	// Frame Control: Management Probe Request (0x0004)
	binary.Write(buf, binary.LittleEndian, uint16(0x0004))
	// Duration
	binary.Write(buf, binary.LittleEndian, uint16(0x0000))
	// Address 1: Broadcast (ff:ff:ff:ff:ff:ff)
	buf.Write([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	// Address 2: Source MAC
	buf.Write(srcMAC[:])
	// Address 3: Broadcast BSSID
	buf.Write([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	// Sequence Control
	binary.Write(buf, binary.LittleEndian, uint16(0x0000))

	// Tagged Parameters: SSID (Broadcast - empty string)
	buf.Write([]byte{0x00, 0x00}) // Tag 0 (SSID), Length 0

	// Supported Rates (1, 2, 5.5, 11 Mbps)
	buf.Write([]byte{0x01, 0x04, 0x82, 0x84, 0x8b, 0x96})

	return buf.Bytes()
}
