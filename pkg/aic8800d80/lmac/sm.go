package lmac

import (
	"encoding/binary"
	"fmt"
)

// Auth types (WLAN_AUTH_* — the values sm_connect_req.auth_type carries).
const (
	AuthOpen      uint8 = 0
	AuthSharedKey uint8 = 1
	AuthFT        uint8 = 2
	AuthSAE       uint8 = 3
)

// Connection flags (enum mac_connection_flags).
const (
	ConnCtrlPortHost  uint32 = 1 << 0 // host runs the controlled port (EAPOL)
	ConnCtrlPortNoEnc uint32 = 1 << 1
	ConnDisableHT     uint32 = 1 << 2
	ConnWPAWPA2InUse  uint32 = 1 << 3
	ConnMFPInUse      uint32 = 1 << 4
	ConnReassoc       uint32 = 1 << 5
)

// WPA2PSKCCMPRsnIE is the standard RSN information element a station puts in
// its (re)association request for WPA2-Personal with CCMP (AES): version 1,
// group=CCMP, 1 pairwise=CCMP, 1 AKM=PSK, no RSN capabilities. The AP
// associates the station on the strength of this IE; the PTK/GTK are then
// negotiated by the EAPOL 4-way handshake over the controlled port.
var WPA2PSKCCMPRsnIE = []byte{
	0x30, 0x14, // RSN element id, length 20
	0x01, 0x00, // version 1
	0x00, 0x0f, 0xac, 0x04, // group cipher CCMP
	0x01, 0x00, // pairwise count 1
	0x00, 0x0f, 0xac, 0x04, // pairwise CCMP
	0x01, 0x00, // AKM count 1
	0x00, 0x0f, 0xac, 0x02, // AKM PSK
	0x80, 0x00, // RSN capabilities: MFPC (PMF-capable) — many modern APs
	// reject a WPA2 assoc without this with status=1.
}

// ConnectReq is SM_CONNECT_REQ (struct sm_connect_req, 320 bytes):
//
//	mac_ssid ssid@0(33); pad@33; mac_addr bssid@34(6); mac_chan_def chan@40(6);
//	pad@46; u32 flags@48; u16 ctrl_port_ethertype@52; u16 ie_len@54;
//	u16 listen_interval@56; bool dont_wait_bcmc@58; u8 auth_type@59;
//	u8 uapsd_queues@60; u8 vif_idx@61; pad@62; u32 ie_buf[64]@64(256)
type ConnectReq struct {
	SSID     string
	BSSID    [6]byte // wildcard (all-zero -> filled ff) if unspecified
	Band     uint8
	Channel  uint8 // primary channel; 0 -> freq set to 0xFFFF (any)
	VifIdx   uint8
	AuthType uint8
	Flags    uint32
	IE       []byte // RSN/extra IEs (empty for open)
}

const connectReqSize = 320

func (r *ConnectReq) Encode() ([]byte, error) {
	if len(r.SSID) > 32 {
		return nil, fmt.Errorf("connect: ssid %q too long", r.SSID)
	}
	if len(r.IE) > 256 {
		return nil, fmt.Errorf("connect: ie too long (%d > 256)", len(r.IE))
	}
	buf := make([]byte, HeaderSize+connectReqSize)
	Header{ID: SMConnectReq, DestID: uint16(TaskSM), SrcID: DRVTaskID, ParamLen: connectReqSize}.Encode(buf)
	p := buf[HeaderSize:]

	// ssid @0 (mac_ssid: u8 length; u8 array[32])
	p[0] = uint8(len(r.SSID))
	copy(p[1:33], r.SSID)

	// bssid @34 (wildcard if all-zero)
	bssid := r.BSSID
	if bssid == ([6]byte{}) {
		bssid = BroadcastBSSID
	}
	copy(p[34:40], bssid[:])

	// chan @40 (mac_chan_def: u16 freq; u8 band; u8 flags; s8 tx_power)
	freq := uint16(0xFFFF)
	if r.Channel != 0 {
		freq = ChannelFreq(r.Band, r.Channel)
	}
	binary.LittleEndian.PutUint16(p[40:42], freq)
	p[42] = r.Band
	p[43] = 0  // flags
	p[44] = 20 // tx_power
	// p[45] pad

	binary.LittleEndian.PutUint32(p[48:52], r.Flags)
	// ctrl_port_ethertype must be EAPOL 0x888E in wire (big-endian) byte order,
	// i.e. bytes 88 8e — that is the value 0x8e88 written little-endian. The
	// firmware compares against 0x8e88 (rwnx_tx.c). Writing 0x888e LE emits
	// 8e 88 (backwards) and EAPOL is not recognised as control-port traffic.
	binary.LittleEndian.PutUint16(p[52:54], 0x8e88) // ctrl_port_ethertype = EAPOL (wire 88 8e)
	binary.LittleEndian.PutUint16(p[54:56], uint16(len(r.IE)))
	binary.LittleEndian.PutUint16(p[56:58], 0) // listen_interval
	p[58] = 0                                  // dont_wait_bcmc
	p[59] = r.AuthType
	p[60] = 0        // uapsd_queues
	p[61] = r.VifIdx // vif_idx
	// p[62:64] pad
	copy(p[64:64+len(r.IE)], r.IE)
	return buf, nil
}

// ConnectInd is SM_CONNECT_IND (struct sm_connect_ind, 852 bytes). We decode
// the fields we act on; the association IE buffer is left unparsed.
// ConnectIndSize is sizeof(struct sm_connect_ind) on the firmware's 32-bit
// little-endian target, and the param_len the firmware reports for it.
const ConnectIndSize = 852

type ConnectInd struct {
	StatusCode uint16
	BSSID      [6]byte
	Roamed     bool
	VifIdx     uint8
	// APIdx is the AP's station index. It is the sta_idx for MM_KEY_ADD and
	// for every outbound data frame's hostdesc, so it is load-bearing.
	APIdx      uint8
	ChIdx      uint8
	AID        uint16
	Band       uint8
	CenterFreq uint16
}

func (c *ConnectInd) Decode(payload []byte) error {
	if len(payload) < 20 {
		return fmt.Errorf("connect ind: short payload (%d)", len(payload))
	}
	c.StatusCode = binary.LittleEndian.Uint16(payload[0:2])
	copy(c.BSSID[:], payload[2:8])
	c.Roamed = payload[8] != 0
	c.VifIdx = payload[9]
	c.APIdx = payload[10]
	c.ChIdx = payload[11]
	// Tail after the assoc IE buffer. The layout is fixed by two compiler-forced
	// pads: 2 bytes at 18..19, because `u32_l assoc_ie_buf[200]` is 4-aligned
	// (which is what puts the IE buffer at 20 and ends it at 820), and 1 byte at
	// 823, because center_freq is a 2-aligned u16. Hence aid@820, band@822,
	// center_freq@824, and sizeof == 852 — matching the param_len the firmware
	// actually reports.
	//
	// NEGATIVE RESULT: these were once "corrected" to 818/820/822 with a >= 824
	// guard. That is wrong — it assumes assoc_ie_buf starts at 18, which no C
	// compiler can produce — and it made every run print aid=0 band=0 freq=0.
	if len(payload) < ConnectIndSize {
		// The dispatcher gates on param_len == ConnectIndSize, so a short
		// payload here means it was clipped in transit. Say so rather than
		// silently reporting zeros for aid/band/freq.
		return fmt.Errorf("connect ind: truncated (%d < %d), tail fields unavailable",
			len(payload), ConnectIndSize)
	}
	c.AID = binary.LittleEndian.Uint16(payload[820:822])
	c.Band = payload[822]
	c.CenterFreq = binary.LittleEndian.Uint16(payload[824:826])
	return nil
}

// DisconnectInd is SM_DISCONNECT_IND (0x1805, struct sm_disconnect_ind):
// reason_code u16 @0, vif_idx u8 @2, ft_over_ds bool @3, reassoc u8 @4.
//
// This is the firmware telling us the link died and why, and it is the single
// most informative message on the association path. It went undecoded for a
// whole debugging session: the raw line "[SM 0x1805] 0f 00 ..." was reason 15,
// "4-way handshake timeout", i.e. the AP never got a valid msg2 — which was
// exactly the bug being chased at the time.
type DisconnectInd struct {
	ReasonCode uint16
	VifIdx     uint8
	FTOverDS   bool
	Reassoc    uint8
}

const DisconnectIndSize = 6

func (d *DisconnectInd) Decode(payload []byte) error {
	if len(payload) < 5 {
		return fmt.Errorf("disconnect ind: short payload (%d)", len(payload))
	}
	d.ReasonCode = binary.LittleEndian.Uint16(payload[0:2])
	d.VifIdx = payload[2]
	d.FTOverDS = payload[3] != 0
	d.Reassoc = payload[4]
	return nil
}

// ReasonName maps an 802.11 reason code (IEEE 802.11-2020 table 9-49) to text.
// Only the codes a station actually meets are named; the rest report numerically.
func ReasonName(code uint16) string {
	switch code {
	case 1:
		return "unspecified"
	case 2:
		return "previous authentication no longer valid"
	case 3:
		return "deauthenticated: leaving"
	case 4:
		return "disassociated: inactivity"
	case 6:
		return "class-2 frame from non-authenticated STA"
	case 7:
		return "class-3 frame from non-associated STA"
	case 8:
		return "disassociated: leaving BSS"
	case 9:
		return "STA not authenticated"
	case 14:
		return "MIC failure"
	case 15:
		return "4-way handshake timeout"
	case 16:
		return "group-key handshake timeout"
	case 17:
		return "handshake element mismatch"
	case 18:
		return "invalid group cipher"
	case 19:
		return "invalid pairwise cipher"
	case 20:
		return "invalid AKMP"
	case 23:
		return "802.1X authentication failed"
	case 24:
		return "cipher suite rejected by policy"
	default:
		return "unknown"
	}
}

func (d DisconnectInd) String() string {
	return fmt.Sprintf("reason=%d (%s) vif=%d reassoc=%d",
		d.ReasonCode, ReasonName(d.ReasonCode), d.VifIdx, d.Reassoc)
}
