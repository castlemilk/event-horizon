package lmac

import (
	"encoding/binary"
	"fmt"
	"net"
)

// Ethernet + ARP + IPv4 + UDP + DHCP + ICMP codecs for the post-association
// data path. Frames ride TxData (hostdesc + payload-after-Ethernet-header)
// outbound and arrive as data-frame payloads inbound; see ExtractEthernet
// for the inbound offset handling.
//
// All multi-byte fields are big-endian (network order) except where noted.

const (
	EtherTypeIP   uint16 = 0x0800
	EtherTypeARP  uint16 = 0x0806
	EtherSize     int    = 14
	IPProtoICMP   uint8  = 1
	IPProtoUDP    uint8  = 17
	DHCPMagic     uint32 = 0x63825363
	DHCPClientPort uint16 = 68
	DHCPServerPort uint16 = 67
	// dhcpMinLen is the fixed BOOTP message size (RFC 951): 236 bytes of BOOTP
	// plus 64 of vendor/option space.
	dhcpMinLen int = 300
)

// Ethernet frame (no VLAN).
type Ethernet struct {
	DA        [6]byte
	SA        [6]byte
	Ethertype uint16
	Payload   []byte
}

func (e *Ethernet) Encode() []byte {
	out := make([]byte, EtherSize+len(e.Payload))
	copy(out[0:6], e.DA[:])
	copy(out[6:12], e.SA[:])
	binary.BigEndian.PutUint16(out[12:14], e.Ethertype)
	copy(out[14:], e.Payload)
	return out
}

func (e *Ethernet) Decode(frame []byte) error {
	if len(frame) < EtherSize {
		return fmt.Errorf("ethernet: short frame (%d)", len(frame))
	}
	copy(e.DA[:], frame[0:6])
	copy(e.SA[:], frame[6:12])
	e.Ethertype = binary.BigEndian.Uint16(frame[12:14])
	e.Payload = append([]byte(nil), frame[14:]...)
	return nil
}

// ---------------------------------------------------------------------------
// RX data-record geometry, from the reference USB fullmac driver.
//
// A USB RX *record* is laid out as:
//
//   off  0 .. 55  struct hw_rxhdr, sizeof == 56:
//                   off  0  hw_vect.len:16 | reserved:8 | mpdu_cnt:6 | ampdu_cnt:2
//                           <-- these first 4 bytes ARE the 4-byte USB record
//                               header: len:16 == pkt_len, reserved:8 == the
//                               record type byte (rwnx_rx.h:238-240, and
//                               aicwf_txrxif.c:686 memcpy's from the record
//                               start straight into a buffer that rwnx_rx.c:2260
//                               casts to (struct hw_rxhdr *)).
//                   off  4  hw_vect.tsf_lo
//                   off  8  hw_vect.tsf_hi
//                   off 12  hw_vect.rx_vect1   (__packed, 16 bytes)
//                   off 28  hw_vect.rx_vect2   (8 bytes)
//                   off 36  hw_vect status word (decr_status at bits 2..4)
//                   off 40  phy_channel_info_desc (8 bytes)
//                   off 48  hw_rxhdr flags word
//                   off 52  hw_rxhdr.pattern
//   off 56 .. 59  4 bytes of alignment padding
//                 (rwnx_rx.c:2229 msdu_offset = 56+2 = 58;
//                  rwnx_rx.c:2407 skb_pull(msdu_offset + 2) => 60 == RX_HWHRD_LEN)
//   off 60 ..     the 802.11 MPDU, exactly hw_vect.len bytes. NOT Ethernet.
//
// protocol.rxstream hands event.Dispatch record[4:] (rxstream.go:142), so every
// offset below is (struct offset - 4). `record` here is that OnDataFrame payload.
// ---------------------------------------------------------------------------

const (
	// RxHWHdrLen is RX_HWHRD_LEN (aicwf_txrxif.h:33) — the distance from the
	// START OF THE USB RECORD to the first MPDU byte.
	RxHWHdrLen = 60

	// RxMSDUOffset is the same thing measured from the OnDataFrame payload,
	// which begins 4 bytes into the record.
	RxMSDUOffset = RxHWHdrLen - 4 // 56

	rxStatusWordOff = 36 - 4 // 32 — hw_vect status word (decr_status)
	rxFlagsWordOff  = 48 - 4 // 44 — hw_rxhdr flags word
)

// hw_vect.decr_status values (rwnx_rx.h:45-53).
const (
	DecrUnenc   uint8 = 0
	DecrWEP     uint8 = 1
	DecrTKIP    uint8 = 2
	DecrCCMP128 uint8 = 3
	DecrCCMP256 uint8 = 4
	DecrGCMP128 uint8 = 5
	DecrGCMP256 uint8 = 6
	DecrWAPI    uint8 = 7
)

// InvalidSTA is RWNX_INVALID_STA / an invalid VIF index.
const InvalidSTA uint8 = 0xFF

// RxHdr is the part of struct hw_rxhdr the host actually has to consult.
type RxHdr struct {
	DecrStatus      uint8 // hw_vect.decr_status, bits 2..4 of the status word
	FrmSuccessfulRx bool  // clear => bad FCS (rwnx_rx.c:1148 sets RADIOTAP_F_BADFCS)
	FCSErr          bool
	PHYErr          bool

	IsAMSDU     bool // flags_is_amsdu — MSDU is an A-MSDU sub-frame list
	Is80211MPDU bool // flags_is_80211_mpdu — MANAGEMENT frame, not data
	Is4Addr     bool // flags_is_4addr — WDS; header is 30 bytes not 24
	NewPeer     bool
	UserPrio    bool
	NeedReord   bool
	Upload      bool // flags_upload — firmware says "hand this to the host"
	MonitorVif  bool

	VifIdx uint8 // 0xFF if invalid
	StaIdx uint8 // 0xFF if invalid
	DstIdx uint8 // 0xFF if unknown
}

// ParseRxHdr decodes the hw_rxhdr fields out of an OnDataFrame payload
// (= USB record[4:]).
func ParseRxHdr(record []byte) (RxHdr, error) {
	if len(record) < RxMSDUOffset {
		return RxHdr{}, fmt.Errorf("rxhdr: short record (%d < %d)", len(record), RxMSDUOffset)
	}
	st := binary.LittleEndian.Uint32(record[rxStatusWordOff : rxStatusWordOff+4])
	fl := binary.LittleEndian.Uint32(record[rxFlagsWordOff : rxFlagsWordOff+4])
	return RxHdr{
		// status word bit layout (LSB first, GCC bitfield order):
		// 0 rx_vect2_valid | 1 resp_frame | 2..4 decr_status | 5 rx_fifo_oflow |
		// 6 undef_err | 7 phy_err | 8 fcs_err | 9 addr_mismatch | 10 ga_frame |
		// 11..12 current_ac | 13 frm_successful_rx | 14 desc_done_rx | ...
		DecrStatus:      uint8(st>>2) & 0x07,
		PHYErr:          st&(1<<7) != 0,
		FCSErr:          st&(1<<8) != 0,
		FrmSuccessfulRx: st&(1<<13) != 0,

		// flags word bit layout:
		// 0 is_amsdu | 1 is_80211_mpdu | 2 is_4addr | 3 new_peer | 4 user_prio |
		// 5 need_reord | 6 upload | 7 is_monitor_vif | 8..15 vif_idx |
		// 16..23 sta_idx | 24..31 dst_idx
		IsAMSDU:     fl&(1<<0) != 0,
		Is80211MPDU: fl&(1<<1) != 0,
		Is4Addr:     fl&(1<<2) != 0,
		NewPeer:     fl&(1<<3) != 0,
		UserPrio:    fl&(1<<4) != 0,
		NeedReord:   fl&(1<<5) != 0,
		Upload:      fl&(1<<6) != 0,
		MonitorVif:  fl&(1<<7) != 0,
		VifIdx:      uint8(fl >> 8),
		StaIdx:      uint8(fl >> 16),
		DstIdx:      uint8(fl >> 24),
	}, nil
}

// secHdrLen returns how many bytes of cipher header sit between the 802.11
// header and the LLC/SNAP header. rwnx_rx.c:2499-2523.
func secHdrLen(decr uint8) int {
	switch decr {
	case DecrWEP:
		return 4
	case DecrTKIP, DecrCCMP128, DecrCCMP256, DecrGCMP128, DecrGCMP256:
		return 8
	case DecrWAPI:
		return 18
	default: // DecrUnenc
		return 0
	}
}

// ExtractEthernet converts ONE received USB data-record payload (that is,
// record[4:] — exactly what event.Dispatch hands OnDataFrame) into an Ethernet
// frame, mirroring rwnx_rxdataind_aicwf() in the reference driver.
//
// The record does NOT contain an Ethernet header anywhere. It contains a
// 56-byte struct hw_rxhdr, 4 pad bytes, then a raw 802.11 MPDU at offset 56.
// The old "scan for DA/SA/ethertype" version could never work; EAPOL only ever
// matched because it brute-forced the literal 88 8e that lives inside LLC/SNAP.
//
// Returned int is the offset WITHIN record at which the L3 payload begins
// (i.e. just past LLC/SNAP) — useful for logging; it varies with QoS/HTC/cipher.
func ExtractEthernet(record []byte, ourMAC [6]byte) (Ethernet, int, error) {
	hdr, err := ParseRxHdr(record)
	if err != nil {
		return Ethernet{}, -1, err
	}
	if hdr.Is80211MPDU {
		// rwnx_rx.c:2701 — management frame, goes to rwnx_rx_mgmt_any, never
		// becomes Ethernet. Beacons delivered on the data path land here.
		return Ethernet{}, -1, fmt.Errorf("rx: management MPDU, not data")
	}
	if hdr.IsAMSDU {
		// A-MSDU carries N sub-frames; one Ethernet return value cannot express
		// it. See ExtractEthernetAll below.
		return Ethernet{}, -1, fmt.Errorf("rx: A-MSDU (use ExtractEthernetAll)")
	}

	msdu := record[RxMSDUOffset:]
	if len(msdu) < 24 {
		return Ethernet{}, -1, fmt.Errorf("rx: MPDU too short (%d < 24)", len(msdu))
	}

	fc0, fc1 := msdu[0], msdu[1]

	// rwnx_rx.c:2422 — protocol version 0 AND type == 2 (Data).
	// fc0 bits: [1:0] protover, [3:2] type, [7:4] subtype.
	if fc0&0x0f != 0x08 {
		return Ethernet{}, -1, fmt.Errorf("rx: not a data frame (fc0=0x%02x)", fc0)
	}
	// Subtypes 4..7 and 12..15 are the "No Data" (Null / QoS-Null / CF-*) ones:
	// subtype bit 2 set => no frame body at all.
	if fc0&0x40 != 0 {
		return Ethernet{}, -1, fmt.Errorf("rx: null data frame (fc0=0x%02x)", fc0)
	}

	toFromDS := fc1 & 0x03
	fourAddr := toFromDS == 0x03 // equivalently hdr.Is4Addr

	// --- 802.11 MAC header length ---------------------------------------
	//   0  frame control (2)
	//   2  duration/id  (2)
	//   4  addr1        (6)   -- RA
	//  10  addr2        (6)   -- TA
	//  16  addr3        (6)
	//  22  sequence control (2)   [ frag_num = low nibble of byte 22 ]
	//  24  addr4 (6, only when ToDS && FromDS)
	//  24 or 30  QoS control (2, only when subtype bit 7 of fc0 set)
	//  +2        HT Control (4, only when fc1 Order bit set)
	hdrLen := 24
	qosOff := 24
	if fourAddr {
		hdrLen += 6 // rwnx_rx.c:2412
		qosOff = 30 // rwnx_rx.c:2426
	}
	isQoS := fc0&0x80 != 0 // rwnx_rx.c:2423
	if isQoS {
		hdrLen += 2 // rwnx_rx.c:2424
	}
	if fc1&0x80 != 0 {
		hdrLen += 4 // Order bit => +HT Control, rwnx_rx.c:2445
	}
	if len(msdu) < hdrLen {
		return Ethernet{}, -1, fmt.Errorf("rx: MPDU shorter than header (%d < %d)", len(msdu), hdrLen)
	}

	// Defence in depth: the QoS A-MSDU-present bit, independent of flags_is_amsdu
	// (rwnx_rx.c:2440 recomputes it from here and overwrites the flag).
	if isQoS {
		if qosOff+2 > len(msdu) {
			return Ethernet{}, -1, fmt.Errorf("rx: truncated QoS control")
		}
		if msdu[qosOff]&0x80 != 0 {
			return Ethernet{}, -1, fmt.Errorf("rx: A-MSDU (QoS bit) (use ExtractEthernetAll)")
		}
	}

	// Fragments must be reassembled before conversion (rwnx_rx.c:2554+).
	// MoreFrag is fc1 bit 2; frag number is the low nibble of seq-ctl byte 22.
	if fc1&0x04 != 0 || msdu[22]&0x0f != 0 {
		return Ethernet{}, -1, fmt.Errorf("rx: 802.11 fragment (morefrag=%v frag=%d), not reassembled",
			fc1&0x04 != 0, msdu[22]&0x0f)
	}

	// --- cipher header + LLC/SNAP ---------------------------------------
	// The hardware decrypts but LEAVES the cipher header in place: the driver
	// adds it to pull_len (rwnx_rx.c:2504) and reads the ethertype past it
	// (rwnx_rx.c:2506). The trailing MIC/ICV/FCS is already gone — nothing in
	// the reference ever trims the tail.
	llcOff := hdrLen + secHdrLen(hdr.DecrStatus)

	// The Protected bit says encrypted; if decr_status disagrees (0), trust the
	// wire and assume a CCMP-sized 8-byte header, then structurally verify.
	if secHdrLen(hdr.DecrStatus) == 0 && fc1&0x40 != 0 {
		if !looksLikeSNAP(msdu, llcOff) && looksLikeSNAP(msdu, hdrLen+8) {
			llcOff = hdrLen + 8
		}
	}
	if !looksLikeSNAP(msdu, llcOff) {
		// Last-resort structural recovery: try with and without the cipher hdr.
		switch {
		case looksLikeSNAP(msdu, hdrLen):
			llcOff = hdrLen
		case looksLikeSNAP(msdu, hdrLen+8):
			llcOff = hdrLen + 8
		default:
			return Ethernet{}, -1, fmt.Errorf(
				"rx: no LLC/SNAP at %d (hdrLen=%d decr=%d len=%d)", llcOff, hdrLen, hdr.DecrStatus, len(msdu))
		}
	}

	// LLC/SNAP is 8 bytes: DSAP AA | SSAP AA | ctrl 03 | OUI 00 00 00 |
	// ethertype (2, BIG endian). rwnx_rx.c reads the ethertype at +6 and pulls
	// the whole 8 (pull_len += hdr_len + 8, rwnx_rx.c:2497).
	et := binary.BigEndian.Uint16(msdu[llcOff+6 : llcOff+8])
	body := msdu[llcOff+8:]

	// --- addresses -------------------------------------------------------
	// rwnx_rx.c:2449-2474. `ra` in the reference becomes the Ethernet DA and
	// `ta` the SA (see the memcpy at 2695-2697).
	var da, sa [6]byte
	switch toFromDS {
	case 0x00: // IBSS / adhoc. The reference leaves these ZEROED — a real bug;
		// 802.11-11 Table 9-26 says DA=addr1, SA=addr2.
		copy(da[:], msdu[4:10])
		copy(sa[:], msdu[10:16])
	case 0x01: // ToDS=1 FromDS=0 — STA -> AP
		copy(da[:], msdu[16:22]) // addr3
		copy(sa[:], msdu[10:16]) // addr2
	case 0x02: // ToDS=0 FromDS=1 — AP -> STA. THIS IS OUR CASE.
		copy(da[:], msdu[4:10])  // addr1 == our MAC (or a group address)
		copy(sa[:], msdu[16:22]) // addr3 == the true originator
	case 0x03: // WDS / 4-addr
		if len(msdu) < 30 {
			return Ethernet{}, -1, fmt.Errorf("rx: 4addr frame too short")
		}
		copy(da[:], msdu[16:22]) // addr3
		copy(sa[:], msdu[24:30]) // addr4
	}

	// Sanity: a unicast DA that is not ours means the hardware address filter
	// let something through, or our offsets are wrong. Loud beats silent.
	if da[0]&0x01 == 0 && da != ourMAC {
		return Ethernet{}, -1, fmt.Errorf("rx: unicast DA %02x:%02x:%02x:%02x:%02x:%02x is not ours",
			da[0], da[1], da[2], da[3], da[4], da[5])
	}

	return Ethernet{
			DA:        da,
			SA:        sa,
			Ethertype: et,
			Payload:   append([]byte(nil), body...),
		},
		RxMSDUOffset + llcOff + 8,
		nil
}

// looksLikeSNAP reports whether an LLC/SNAP header (AA AA 03 <oui 3> <et 2>)
// starts at off. The OUI is deliberately not checked against 00-00-00: the
// bridge-tunnel OUI 00-00-F8 is also 8 bytes and is skipped identically.
func looksLikeSNAP(b []byte, off int) bool {
	if off < 0 || off+8 > len(b) {
		return false
	}
	return b[off] == 0xAA && b[off+1] == 0xAA && b[off+2] == 0x03
}

// ExtractEthernetAll handles the A-MSDU case, returning every sub-frame.
// Sub-frame list starts at pull_len-8 == hdrLen + cipher header
// (rwnx_rx.c:2542), each sub-frame is
// [DA 6][SA 6][len 2][LLC/SNAP 8][payload], padded up to a 4-byte boundary
// except for the last (rwnx_rx.c:2155-2175).
func ExtractEthernetAll(record []byte, ourMAC [6]byte) ([]Ethernet, error) {
	if e, _, err := ExtractEthernet(record, ourMAC); err == nil {
		return []Ethernet{e}, nil
	}
	hdr, err := ParseRxHdr(record)
	if err != nil {
		return nil, err
	}
	if !hdr.IsAMSDU || hdr.Is80211MPDU {
		return nil, fmt.Errorf("rx: not an A-MSDU data frame")
	}
	msdu := record[RxMSDUOffset:]
	if len(msdu) < 26 {
		return nil, fmt.Errorf("rx: A-MSDU too short")
	}
	fc0, fc1 := msdu[0], msdu[1]
	hdrLen := 24
	if fc1&0x03 == 0x03 {
		hdrLen += 6
	}
	if fc0&0x80 != 0 {
		hdrLen += 2
	}
	if fc1&0x80 != 0 {
		hdrLen += 4
	}
	off := hdrLen + secHdrLen(hdr.DecrStatus)

	var out []Ethernet
	for off+14 <= len(msdu) {
		subLen := int(binary.BigEndian.Uint16(msdu[off+12 : off+14]))
		if subLen < 8 || off+14+subLen > len(msdu) {
			break
		}
		var da, sa [6]byte
		copy(da[:], msdu[off:off+6])
		copy(sa[:], msdu[off+6:off+12])
		snap := msdu[off+14 : off+14+subLen]
		if !looksLikeSNAP(snap, 0) {
			break
		}
		out = append(out, Ethernet{
			DA:        da,
			SA:        sa,
			Ethertype: binary.BigEndian.Uint16(snap[6:8]),
			Payload:   append([]byte(nil), snap[8:]...),
		})
		adv := subLen + 14
		if off+adv < len(msdu) {
			adv = (adv + 3) &^ 3 // roundup(sublen+14, 4)
		}
		off += adv
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("rx: no A-MSDU sub-frames decoded")
	}
	return out, nil
}

// Checksum computes the RFC 1071 ones-complement sum.
func Checksum(data []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// ARP packet (Ethernet/IPv4).
type ARP struct {
	Op       uint16 // 1=request, 2=reply
	SenderMAC [6]byte
	SenderIP  [4]byte
	TargetMAC [6]byte
	TargetIP  [4]byte
}

func (a *ARP) Encode() []byte {
	out := make([]byte, 28)
	binary.BigEndian.PutUint16(out[0:2], 1) // Ethernet
	binary.BigEndian.PutUint16(out[2:4], EtherTypeIP)
	out[4], out[5] = 6, 4
	binary.BigEndian.PutUint16(out[6:8], a.Op)
	copy(out[8:14], a.SenderMAC[:])
	copy(out[14:18], a.SenderIP[:])
	copy(out[18:24], a.TargetMAC[:])
	copy(out[24:28], a.TargetIP[:])
	return out
}

func (a *ARP) Decode(payload []byte) error {
	if len(payload) < 28 {
		return fmt.Errorf("arp: short (%d)", len(payload))
	}
	a.Op = binary.BigEndian.Uint16(payload[6:8])
	copy(a.SenderMAC[:], payload[8:14])
	copy(a.SenderIP[:], payload[14:18])
	copy(a.TargetMAC[:], payload[18:24])
	copy(a.TargetIP[:], payload[24:28])
	return nil
}

// IPv4 header (no options) + payload.
type IPv4 struct {
	Src     [4]byte
	Dst     [4]byte
	Proto   uint8
	TTL     uint8
	Payload []byte
}

func (p *IPv4) Encode() []byte {
	hdr := make([]byte, 20)
	hdr[0] = 0x45
	total := 20 + len(p.Payload)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(total))
	// identification + flags/frag = 0; TTL default 64.
	ttl := p.TTL
	if ttl == 0 {
		ttl = 64
	}
	hdr[8] = ttl
	hdr[9] = p.Proto
	copy(hdr[12:16], p.Src[:])
	copy(hdr[16:20], p.Dst[:])
	binary.BigEndian.PutUint16(hdr[10:12], Checksum(hdr))
	return append(hdr, p.Payload...)
}

func (p *IPv4) Decode(payload []byte) error {
	if len(payload) < 20 || payload[0]>>4 != 4 || payload[0]&0x0f < 5 {
		return fmt.Errorf("ipv4: bad header")
	}
	hlen := int(payload[0]&0x0f) * 4
	if len(payload) < hlen {
		return fmt.Errorf("ipv4: short (%d < %d)", len(payload), hlen)
	}
	total := int(binary.BigEndian.Uint16(payload[2:4]))
	if total > len(payload) || total < hlen {
		return fmt.Errorf("ipv4: bad total %d", total)
	}
	p.TTL, p.Proto = payload[8], payload[9]
	copy(p.Src[:], payload[12:16])
	copy(p.Dst[:], payload[16:20])
	p.Payload = append([]byte(nil), payload[hlen:total]...)
	return nil
}

// UDP datagram.
type UDP struct {
	SrcPort uint16
	DstPort uint16
	Payload []byte
}

func (u *UDP) Encode(srcIP, dstIP [4]byte) []byte {
	out := make([]byte, 8+len(u.Payload))
	binary.BigEndian.PutUint16(out[0:2], u.SrcPort)
	binary.BigEndian.PutUint16(out[2:4], u.DstPort)
	binary.BigEndian.PutUint16(out[4:6], uint16(8+len(u.Payload)))
	// checksum: computed over pseudo-header; zero means "none" for IPv4 and
	// is acceptable, but fill it properly for picky servers.
	copy(out[8:], u.Payload)
	sum := udpChecksum(srcIP, dstIP, out)
	binary.BigEndian.PutUint16(out[6:8], sum)
	return out
}

func udpChecksum(srcIP, dstIP [4]byte, udp []byte) uint16 {
	pseudo := make([]byte, 12+len(udp))
	copy(pseudo[0:4], srcIP[:])
	copy(pseudo[4:8], dstIP[:])
	pseudo[8] = 0
	pseudo[9] = IPProtoUDP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(udp)))
	copy(pseudo[12:], udp)
	if c := Checksum(pseudo); c != 0 {
		return c
	}
	return 0xffff // zero checksum is transmitted as all-ones
}

// DHCP message types.
const (
	DHCPDiscover = 1
	DHCPOffer    = 2
	DHCPRequest  = 3
	DHCPAck      = 5
)

// DHCP builds/parses minimal client messages (BOOTP + magic + options).
type DHCP struct {
	XID     [4]byte
	MsgType uint8
	YIAddr  [4]byte // your IP (offer/ack)
	ServerID [4]byte
	Subnet  [4]byte
	Router  [4]byte
	DNS     [4]byte
	Lease   uint32
}

func (d *DHCP) build(msgType uint8, reqIP [4]byte) []byte {
	out := make([]byte, 236)
	out[0] = 1 // BOOTREQUEST
	out[1] = 1 // Ethernet
	out[2] = 6
	// hops, server name, file = 0.
	copy(out[4:8], d.XID[:])
	// secs, flags (broadcast bit so the AP relays without an IP).
	binary.BigEndian.PutUint16(out[10:12], 0x8000)
	// ciaddr = 0 (no IP yet); chaddr = client MAC set by caller below.
	opts := []byte{99, 130, 83, 99} // magic
	opts = append(opts, 53, 1, msgType)            // message type
	opts = append(opts, 55, 3, 1, 3, 6)            // request subnet, router, DNS
	if msgType == DHCPRequest {
		opts = append(opts, 50, 4, reqIP[0], reqIP[1], reqIP[2], reqIP[3])
		if d.ServerID != ([4]byte{}) {
			opts = append(opts, 54, 4, d.ServerID[0], d.ServerID[1], d.ServerID[2], d.ServerID[3])
		}
	}
	opts = append(opts, 255) // end
	pkt := append(out, opts...)
	// BOOTP fixes the message at 300 octets (RFC 951 / RFC 2131 sec 2), and
	// every real client (dhclient, udhcpc, dhcpcd) pads to it. Servers, relay
	// agents and AP DHCP-snooping engines commonly drop anything shorter — our
	// unpadded 249-byte DISCOVER was a likely reason for the silence.
	if len(pkt) < dhcpMinLen {
		pkt = append(pkt, make([]byte, dhcpMinLen-len(pkt))...)
	}
	return pkt
}

// Discover builds a DHCPDISCOVER with the client MAC filled in.
func (d *DHCP) Discover(mac [6]byte) []byte {
	pkt := d.build(DHCPDiscover, [4]byte{})
	copy(pkt[28:34], mac[:])
	return pkt
}

// Request builds a DHCPREQUEST for the offered IP.
func (d *DHCP) Request(mac [6]byte, reqIP [4]byte) []byte {
	pkt := d.build(DHCPRequest, reqIP)
	copy(pkt[28:34], mac[:])
	return pkt
}

// ParseDHCP parses an offer/ack (BOOTP + options) into DHCP.
func ParseDHCP(pkt []byte) (DHCP, error) {
	var d DHCP
	if len(pkt) < 240 || binary.BigEndian.Uint32(pkt[236:240]) != DHCPMagic {
		return d, fmt.Errorf("dhcp: bad magic or short (%d)", len(pkt))
	}
	copy(d.XID[:], pkt[4:8])
	copy(d.YIAddr[:], pkt[16:20])
	opts := pkt[240:]
	for i := 0; i+1 < len(opts); {
		code := opts[i]
		if code == 255 {
			break
		}
		if code == 0 {
			i++
			continue
		}
		if i+1 >= len(opts) {
			break
		}
		ln := int(opts[i+1])
		if i+2+ln > len(opts) {
			break
		}
		body := opts[i+2 : i+2+ln]
		switch code {
		case 53:
			d.MsgType = body[0]
		case 54:
			copy(d.ServerID[:], body)
		case 51:
			d.Lease = binary.BigEndian.Uint32(body)
		case 1:
			copy(d.Subnet[:], body)
		case 3:
			copy(d.Router[:], body[:4])
		case 6:
			copy(d.DNS[:], body[:4])
		}
		i += 2 + ln
	}
	return d, nil
}

// ICMP echo (ping).
type ICMPEcho struct {
	ID   uint16
	Seq  uint16
	Data []byte
}

func (e *ICMPEcho) Encode(request bool) []byte {
	out := make([]byte, 8+len(e.Data))
	if request {
		out[0] = 8
	} else {
		out[0] = 0
	}
	out[1] = 0
	binary.BigEndian.PutUint16(out[4:6], e.ID)
	binary.BigEndian.PutUint16(out[6:8], e.Seq)
	copy(out[8:], e.Data)
	binary.BigEndian.PutUint16(out[2:4], Checksum(out))
	return out
}

func (e *ICMPEcho) Decode(payload []byte) error {
	if len(payload) < 8 || (payload[0] != 8 && payload[0] != 0) {
		return fmt.Errorf("icmp: not echo (%d bytes)", len(payload))
	}
	e.ID = binary.BigEndian.Uint16(payload[4:6])
	e.Seq = binary.BigEndian.Uint16(payload[6:8])
	e.Data = append([]byte(nil), payload[8:]...)
	return nil
}

func ip4(s string) [4]byte {
	var b [4]byte
	copy(b[:], net.ParseIP(s).To4())
	return b
}
