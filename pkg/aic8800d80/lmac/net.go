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

// ExtractEthernet finds an Ethernet frame inside a raw data-frame payload.
// The firmware prepends a hardware RX header of uncertain length (observed
// 46-60 bytes depending on frame type), so anchor on structure: try the
// canonical offset first, then scan for a plausible header (DA is broadcast,
// our MAC, or a unicast address followed by a known ethertype).
func ExtractEthernet(p []byte, ourMAC [6]byte) (Ethernet, int, error) {
	try := func(off int) (Ethernet, bool) {
		var e Ethernet
		if off+EtherSize > len(p) {
			return e, false
		}
		if err := e.Decode(p[off:]); err != nil {
			return e, false
		}
		switch e.Ethertype {
		case EtherTypeIP, EtherTypeARP, EAPOLEthertype:
		default:
			return e, false
		}
		return e, true
	}
	// Canonical: right after the 60-byte hardware RX header.
	if e, ok := try(60); ok {
		return e, 60, nil
	}
	for off := 0; off+EtherSize <= len(p); off++ {
		da := p[off : off+6]
		isBcast := da[0] == 0xff && da[1] == 0xff && da[2] == 0xff &&
			da[3] == 0xff && da[4] == 0xff && da[5] == 0xff
		isOurs := da[0] == ourMAC[0] && da[1] == ourMAC[1] && da[2] == ourMAC[2] &&
			da[3] == ourMAC[3] && da[4] == ourMAC[4] && da[5] == ourMAC[5]
		if !isBcast && !isOurs {
			continue
		}
		if e, ok := try(off); ok {
			return e, off, nil
		}
	}
	return Ethernet{}, -1, fmt.Errorf("ethernet: no frame anchored in %d bytes", len(p))
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
