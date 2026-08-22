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
	MaxChannelsInReq = 42 // SCAN_CHANNEL_MAX in lmac_msg.h (14 2.4G + 28 5G)
	MaxSSIDsInReq    = 3  // SCAN_SSID_MAX in lmac_msg.h
	ScanReqSize      = 376
)

// ChannelInfo mirrors struct scan_chan_info.
type ChannelInfo struct {
	Prim20Ch uint8
	Center1  uint8
	Center2  uint8
	Width    uint8
}

// ChannelFreq calculates the center frequency in MHz for a given Wi-Fi channel.
func ChannelFreq(band uint8, ch uint8) uint16 {
	if band == Band2G {
		if ch == 14 {
			return 2484
		}
		if ch >= 1 && ch <= 13 {
			return 2407 + uint16(ch)*5
		}
		return 2412
	}
	if ch >= 36 {
		return 5000 + uint16(ch)*5
	}
	return 5180
}

// FreqToChannel converts a MHz center frequency back to a channel number.
func FreqToChannel(band uint8, freq uint16) uint8 {
	if band == Band2G {
		if freq == 2484 {
			return 14
		}
		if freq >= 2412 && freq <= 2472 {
			return uint8((freq - 2407) / 5)
		}
		return 1
	}
	if freq >= 5000 {
		return uint8((freq - 5000) / 5)
	}
	return 36
}

// ScanStartReq mirrors struct scanu_start_req (lmac_msg.h).
//
// Layout (376 bytes):
//   struct mac_chan_def chan[42]; // 42 * 6 = 252 bytes
//   struct mac_ssid ssid[3];      // 3 * 33 = 99 bytes (offset 252..350)
//   struct mac_addr bssid;        // 6 bytes (offset 352..357, 2-byte aligned)
//   u32_l add_ies;                // 4 bytes (offset 360..363, 4-byte aligned)
//   u16_l add_ie_len;             // 2 bytes (offset 364..365)
//   u8_l vif_idx;                 // 1 byte (offset 366)
//   u8_l chan_cnt;                // 1 byte (offset 367)
//   u8_l ssid_cnt;                // 1 byte (offset 368)
//   bool no_cck;                  // 1 byte (offset 369)
//   u32_l duration;               // 4 bytes (offset 372..375, 4-byte aligned)
type ScanStartReq struct {
	Band       uint8
	Channels   []ChannelInfo // up to MaxChannelsInReq
	SSIDs      []string      // up to MaxSSIDsInReq
	BSSID      [6]byte
	VifIdx     uint8
	Duration   uint32
}

func (r *ScanStartReq) Encode() ([]byte, error) {
	if len(r.Channels) > MaxChannelsInReq {
		return nil, fmt.Errorf("scan: too many channels (%d > %d)", len(r.Channels), MaxChannelsInReq)
	}
	if len(r.SSIDs) > MaxSSIDsInReq {
		return nil, fmt.Errorf("scan: too many SSIDs (%d > %d)", len(r.SSIDs), MaxSSIDsInReq)
	}
	buf := make([]byte, HeaderSize+ScanReqSize)
	Header{ID: SCANUStartReq, DestID: uint16(TaskSCANU), SrcID: DRVTaskID, ParamLen: uint16(ScanReqSize)}.Encode(buf)
	p := buf[HeaderSize:]

	// 1. struct mac_chan_def chan[42] (42 * 6 = 252 bytes)
	for i, ch := range r.Channels {
		if i >= MaxChannelsInReq {
			break
		}
		off := i * 6
		freq := ChannelFreq(r.Band, ch.Prim20Ch)
		binary.LittleEndian.PutUint16(p[off:off+2], freq)
		p[off+2] = r.Band // band
		p[off+3] = 0      // flags
		p[off+4] = 20     // tx_power
		p[off+5] = 0      // pad
	}

	// 2. struct mac_ssid ssid[3] (3 * 33 = 99 bytes, offset 252..350)
	for i, s := range r.SSIDs {
		if i >= MaxSSIDsInReq {
			break
		}
		off := 252 + i*33
		if len(s) > 32 {
			return nil, fmt.Errorf("scan: ssid %q too long (%d > 32)", s, len(s))
		}
		p[off] = uint8(len(s))
		copy(p[off+1:off+33], s)
	}

	// 3. struct mac_addr bssid (6 bytes, aligned to 2 -> offset 352)
	copy(p[352:358], r.BSSID[:])

	// 4. add_ies (u32, aligned to 4 -> offset 360)
	binary.LittleEndian.PutUint32(p[360:364], 0)

	// 5. add_ie_len (u16 -> offset 364)
	binary.LittleEndian.PutUint16(p[364:366], 0)

	// 6. vif_idx (u8 -> offset 366)
	p[366] = r.VifIdx

	// 7. chan_cnt (u8 -> offset 367)
	p[367] = uint8(len(r.Channels))

	// 8. ssid_cnt (u8 -> offset 368)
	p[368] = uint8(len(r.SSIDs))

	// 9. no_cck (bool -> offset 369)
	p[369] = 0

	// 10. duration (u32, aligned to 4 -> offset 372)
	binary.LittleEndian.PutUint32(p[372:376], r.Duration)

	return buf, nil
}

// Decode parses a SCANU_START_REQ payload back into r (for round-trip tests).
func (r *ScanStartReq) Decode(payload []byte) error {
	if len(payload) < ScanReqSize {
		return fmt.Errorf("scan start: short payload (%d < %d)", len(payload), ScanReqSize)
	}
	chanCnt := int(payload[367])
	ssidCnt := int(payload[368])
	r.VifIdx = payload[366]
	r.Duration = binary.LittleEndian.Uint32(payload[372:376])
	copy(r.BSSID[:], payload[352:358])

	r.Channels = make([]ChannelInfo, 0, chanCnt)
	for i := 0; i < chanCnt && i < MaxChannelsInReq; i++ {
		off := i * 6
		freq := binary.LittleEndian.Uint16(payload[off : off+2])
		band := payload[off+2]
		r.Band = band
		chNum := FreqToChannel(band, freq)
		r.Channels = append(r.Channels, ChannelInfo{Prim20Ch: chNum, Center1: chNum, Width: ChanWidth20})
	}

	r.SSIDs = make([]string, 0, ssidCnt)
	for i := 0; i < ssidCnt && i < MaxSSIDsInReq; i++ {
		off := 252 + i*33
		sLen := int(payload[off])
		if sLen > 32 {
			sLen = 32
		}
		r.SSIDs = append(r.SSIDs, string(payload[off+1:off+1+sLen]))
	}

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

// ScanStartCfm is SCANU_START_CFM / SCANU_START_CFM_ADDTIONAL (struct scanu_start_cfm in lmac_msg.h).
type ScanStartCfm struct {
	VifIdx    uint8
	Status    uint8
	ResultCnt uint8
}

func (c *ScanStartCfm) Decode(payload []byte) error {
	if len(payload) < 2 {
		return fmt.Errorf("scan start cfm: short payload (%d bytes)", len(payload))
	}
	c.VifIdx = payload[0]
	c.Status = payload[1]
	if len(payload) >= 3 {
		c.ResultCnt = payload[2]
	}
	return nil
}
