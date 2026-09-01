package lmac

import "fmt"

// ConfigReq is ME_CONFIG_REQ (struct me_config_req, 112 bytes). It carries
// the MAC engine's HT/VHT/HE capabilities and feature toggles. The firmware
// requires it before it will honour a scan. A station that only needs to
// scan (and connect on legacy/HT rates) can send a minimal config: all
// capability blocks zeroed and every *_supp flag false. Offsets verified by
// compiling the reference structs (LP64): tx_lft@100, phy_bw_max@102,
// ht_supp@103, vht_supp@104, he_supp@105, he_ul_on@106, ps_on@107,
// ant_div_on@108, dpsm@109.
type ConfigReq struct {
	HTSupported bool // advertise 802.11n; false keeps the config legacy-only
}

const meConfigSize = 112

func (r ConfigReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize+meConfigSize)
	Header{ID: MEConfigReq, DestID: uint16(TaskME), SrcID: DRVTaskID, ParamLen: meConfigSize}.Encode(buf)
	p := buf[HeaderSize:]
	// ht_cap[0:32], vht_cap[32:44], he_cap[44:100] all left zero.
	// tx_lft (u16 @100) = 0 (no BlockAck lifetime limit).
	// phy_bw_max (u8 @102) = 0 (20 MHz).
	if r.HTSupported {
		p[103] = 1 // ht_supp
	}
	// vht_supp/he_supp/he_ul_on/ps_on/ant_div_on/dpsm all 0.
	return buf, nil
}

// Standard channel sets used to populate ME_CHAN_CONFIG_REQ. The firmware
// rejects (or silently ignores) a scan on channels it has not been told are
// legal, so this table must cover every channel we intend to scan.
var (
	chans2G4 = []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	chans5G  = []uint8{36, 40, 44, 48, 52, 56, 60, 64,
		100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140,
		149, 153, 157, 161, 165}
)

// MEChanConfigReq is ME_CHAN_CONFIG_REQ (struct me_chan_config_req, 254 bytes):
//
//	struct mac_chan_def chan2G4[14];  // 14 * 6 = 84 bytes  (offset 0)
//	struct mac_chan_def chan5G[28];   // 28 * 6 = 168 bytes (offset 84)
//	u8 chan2G4_cnt;                   // offset 252
//	u8 chan5G_cnt;                    // offset 253
//
// Each mac_chan_def is 6 bytes: u16 freq; u8 band; u8 flags; s8 tx_power; u8 pad.
type ChanConfigReq struct{}

const meChanConfigSize = 254

func (ChanConfigReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize+meChanConfigSize)
	Header{ID: MEChanConfigReq, DestID: uint16(TaskME), SrcID: DRVTaskID, ParamLen: meChanConfigSize}.Encode(buf)
	p := buf[HeaderSize:]

	putChan := func(off int, band, ch uint8) {
		freq := ChannelFreq(band, ch)
		p[off] = byte(freq)
		p[off+1] = byte(freq >> 8)
		p[off+2] = band
		p[off+3] = 0  // flags
		p[off+4] = 20 // tx_power dBm
		p[off+5] = 0  // pad
	}

	if len(chans2G4) > 14 || len(chans5G) > 28 {
		return nil, fmt.Errorf("me_chan_config: channel table too large")
	}
	for i, ch := range chans2G4 {
		putChan(i*6, Band2G, ch)
	}
	for i, ch := range chans5G {
		putChan(84+i*6, Band5G, ch)
	}
	p[252] = uint8(len(chans2G4))
	p[253] = uint8(len(chans5G))
	return buf, nil
}
