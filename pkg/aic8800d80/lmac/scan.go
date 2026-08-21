package lmac

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Band values (lmac_msg.h PHY_BAND_*).
const (
	Band2G uint8 = 0
	Band5G uint8 = 1
)

// Channel width (CHNL_WIDTH_*).
const (
	ChanWidth20    uint8 = 0
	ChanWidth40    uint8 = 1
	ChanWidth80    uint8 = 2
	ChanWidth160   uint8 = 3
	ChanWidth80P80 uint8 = 4
)

// BroadcastBSSID = ff:ff:ff:ff:ff:ff (wildcard scan).
var BroadcastBSSID = [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

const (
	MaxChannelsInReq = 16 // SCANU_START_REQ channel array limit
	MaxSSIDsInReq    = 2  // SCANU_START_REQ SSID array limit
)

// ChannelInfo mirrors struct scan_chan_info.
type ChannelInfo struct {
	Prim20Ch uint8
	Center1  uint8
	Center2  uint8
	Width    uint8
}

// ScanStartReq mirrors struct scanu_start_req.
//
// Layout (lmac_msg.h struct scanu_start_req):
//   u8 band;
//   u8 padding[3];
//   struct mac_ssid ssid[2];   // u8 len + 31 bytes per slot
//   u8 ssid_len[2];            // (legacy; we encode lengths in slot[0])
//   struct scan_chan_info chan[MAX_CHANNELS]; // 4 bytes each
//   u8 n_channels;
//   u8 n_ssids;
//   struct mac_addr bssid;     // 6 bytes
//   u16 probe_delay;
//   u32 flags;
//
// For simplicity in v1 we hard-code the layout. Extend if firmware disagrees.
type ScanStartReq struct {
	Band       uint8
	Channels   []ChannelInfo // up to MaxChannelsInReq
	SSIDs      []string      // up to MaxSSIDsInReq
	BSSID      [6]byte
	ProbeDelay uint16
	Flags      uint32
}

func (r *ScanStartReq) Encode() ([]byte, error) {
	if len(r.Channels) > MaxChannelsInReq {
		return nil, fmt.Errorf("scan: too many channels (%d > %d)", len(r.Channels), MaxChannelsInReq)
	}
	if len(r.SSIDs) > MaxSSIDsInReq {
		return nil, fmt.Errorf("scan: too many SSIDs (%d > %d)", len(r.SSIDs), MaxSSIDsInReq)
	}
	// Compute param length.
	payloadLen := 4 + /*band+pad*/
		MaxSSIDsInReq*32 + /*ssid slots*/
		2 + /*ssid_len slot (legacy)*/
		MaxChannelsInReq*4 + /*chan slots*/
		1 + 1 + /*n_channels, n_ssids*/
		6 + /*bssid*/
		2 + /*probe_delay*/
		4 /*flags*/
	buf := make([]byte, HeaderSize+payloadLen)
	Header{ID: SCANUStartReq, DestID: uint16(TaskSCANU), SrcID: DRVTaskID, ParamLen: uint16(payloadLen)}.Encode(buf)
	p := buf[HeaderSize:]
	p[0] = r.Band
	p[1], p[2], p[3] = 0, 0, 0 // pad
	off := 4
	// SSIDs: fixed 2 slots, each 32 bytes (mac_ssid = u8 len + 31 bytes ssid).
	for i := 0; i < MaxSSIDsInReq; i++ {
		slot := p[off+i*32 : off+i*32+32]
		if i < len(r.SSIDs) {
			s := r.SSIDs[i]
			if len(s) > 32 {
				return nil, fmt.Errorf("scan: ssid %q too long (%d > 32)", s, len(s))
			}
			slot[0] = uint8(len(s))
			copy(slot[1:], s)
		}
	}
	off += MaxSSIDsInReq * 32
	off += 2 // ssid_len slot (unused in v1; slot[0] carries lengths)
	// Channels: fixed MAX_CHANNELS slots.
	for i := 0; i < MaxChannelsInReq; i++ {
		slot := p[off+i*4 : off+i*4+4]
		if i < len(r.Channels) {
			ch := r.Channels[i]
			slot[0], slot[1], slot[2], slot[3] = ch.Prim20Ch, ch.Center1, ch.Center2, ch.Width
		}
	}
	off += MaxChannelsInReq * 4
	p[off] = uint8(len(r.Channels))
	off++
	p[off] = uint8(len(r.SSIDs))
	off++
	copy(p[off:off+6], r.BSSID[:])
	off += 6
	binary.LittleEndian.PutUint16(p[off:off+2], r.ProbeDelay)
	off += 2
	binary.LittleEndian.PutUint32(p[off:off+4], r.Flags)
	return buf, nil
}

// Decode parses a SCANU_START_REQ payload back into r (for round-trip tests).
// Field layout must match Encode.
func (r *ScanStartReq) Decode(payload []byte) error {
	if len(payload) < 4 {
		return fmt.Errorf("scan start: short payload")
	}
	r.Band = payload[0]
	off := 4
	r.SSIDs = r.SSIDs[:0]
	for i := 0; i < MaxSSIDsInReq; i++ {
		slot := payload[off+i*32 : off+i*32+32]
		if slot[0] > 0 {
			r.SSIDs = append(r.SSIDs, string(bytes.TrimRight(slot[1:1+slot[0]], "\x00")))
		}
	}
	off += MaxSSIDsInReq*32 + 2
	nCh := int(payload[off+MaxChannelsInReq*4])
	r.Channels = make([]ChannelInfo, 0, nCh)
	for i := 0; i < nCh && i < MaxChannelsInReq; i++ {
		slot := payload[off+i*4 : off+i*4+4]
		r.Channels = append(r.Channels, ChannelInfo{Prim20Ch: slot[0], Center1: slot[1], Center2: slot[2], Width: slot[3]})
	}
	off += MaxChannelsInReq*4 + 2
	return nil
}

// ScanResultInd mirrors struct scan_result (lmac_msg.h). The firmware sends
// one of these per BSS seen during a scan.
type ScanResultInd struct {
	Channel uint16 // channel number
	Band    uint8  // 0 = 2G, 1 = 5G
	Width   uint8  // channel width
	RSSI    int8   // signal strength (signed)
	RSSIMin int8
	RSSIMax int8
	BSSID   [6]byte
	IE      []byte // information elements (raw 802.11 IE bytes)
	SSID    string
}

func (r *ScanResultInd) Decode(payload []byte) error {
	// Layout (struct scan_result in lmac_msg.h):
	//   u32 channel;
	//   u8 band; u8 width; u16 padding;
	//   u8 rssi; u8 rssi_min; u8 rssi_max; u8 padding;
	//   u8 bssid[6]; u8 padding[2];
	//   u16 ie_len; u8 ie[ie_len];
	//   struct mac_ssid ssid; // u8 len + 31 bytes
	if len(payload) < 24 {
		return fmt.Errorf("scan result: short payload (%d)", len(payload))
	}
	r.Channel = uint16(binary.LittleEndian.Uint32(payload[0:4]))
	r.Band = payload[4]
	r.Width = payload[5]
	r.RSSI = int8(payload[8])
	r.RSSIMin = int8(payload[9])
	r.RSSIMax = int8(payload[10])
	copy(r.BSSID[:], payload[12:18])
	ieLen := int(binary.LittleEndian.Uint16(payload[20:22]))
	if 22+ieLen+32 > len(payload) {
		return fmt.Errorf("scan result: short IE/SSID tail (%d)", len(payload))
	}
	r.IE = append([]byte(nil), payload[22:22+ieLen]...)
	ssidSlot := payload[22+ieLen : 22+ieLen+32]
	if ssidSlot[0] > 0 && int(ssidSlot[0]) <= 31 {
		r.SSID = string(bytes.TrimRight(ssidSlot[1:1+ssidSlot[0]], "\x00"))
	}
	return nil
}
