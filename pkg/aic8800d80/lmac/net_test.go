package lmac

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestChecksumVector(t *testing.T) {
	// IPv4 header checksum example: 20-byte header of an ICMP echo to
	// 8.8.8.8 — verify self-consistency (encode then checksum == 0).
	p := &IPv4{Src: ip4("192.168.1.10"), Dst: ip4("8.8.8.8"), Proto: IPProtoICMP,
		Payload: (&ICMPEcho{ID: 0x1234, Seq: 1, Data: []byte("hello")}).Encode(true)}
	enc := p.Encode()
	if c := Checksum(enc[:20]); c != 0 {
		t.Errorf("ipv4 checksum over encoded header = 0x%04x, want 0", c)
	}
	var d IPv4
	if err := d.Decode(enc); err != nil {
		t.Fatalf("ipv4 decode: %v", err)
	}
	if d.Src != p.Src || d.Dst != p.Dst || d.Proto != IPProtoICMP {
		t.Errorf("ipv4 round-trip mismatch: %+v", d)
	}
	var echo ICMPEcho
	if err := echo.Decode(d.Payload); err != nil {
		t.Fatalf("icmp decode: %v", err)
	}
	if echo.ID != 0x1234 || echo.Seq != 1 || string(echo.Data) != "hello" {
		t.Errorf("icmp round-trip mismatch: %+v", echo)
	}
}

func TestARPEncode(t *testing.T) {
	a := &ARP{Op: 1, SenderMAC: [6]byte{2, 0x11, 0x22, 0x33, 0x44, 0x55},
		SenderIP: ip4("0.0.0.0"), TargetIP: ip4("192.168.1.1")}
	enc := a.Encode()
	if len(enc) != 28 || binary.BigEndian.Uint16(enc[6:8]) != 1 {
		t.Errorf("arp encode: len=%d op=%d", len(enc), binary.BigEndian.Uint16(enc[6:8]))
	}
	var d ARP
	if err := d.Decode(enc); err != nil {
		t.Fatalf("arp decode: %v", err)
	}
	if d.TargetIP != a.TargetIP || d.SenderMAC != a.SenderMAC {
		t.Errorf("arp round-trip mismatch")
	}
}

func TestDHCPRoundTrip(t *testing.T) {
	mac := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	var xid [4]byte
	copy(xid[:], []byte{1, 2, 3, 4})
	d := DHCP{XID: xid}
	disc := d.Discover(mac)
	if len(disc) < 244 || disc[0] != 1 || binary.BigEndian.Uint32(disc[236:240]) != DHCPMagic {
		t.Fatalf("discover malformed: len=%d", len(disc))
	}
	// Fake an offer: BOOTP reply with yiaddr + subnet/router/dns/server options.
	offer := make([]byte, 300)
	offer[0] = 2
	copy(offer[4:8], xid[:])
	offerIP := ip4("192.168.1.50")
	copy(offer[16:20], offerIP[:])
	binary.BigEndian.PutUint32(offer[236:240], DHCPMagic)
	opts := []byte{53, 1, 2, 54, 4, 192, 168, 1, 1, 1, 4, 255, 255, 255, 0,
		3, 4, 192, 168, 1, 1, 6, 4, 192, 168, 1, 1, 51, 4, 0, 1, 81, 128, 255}
	copy(offer[240:], opts)
	got, err := ParseDHCP(offer)
	if err != nil {
		t.Fatalf("parse offer: %v", err)
	}
	if got.MsgType != DHCPOffer || got.YIAddr != ip4("192.168.1.50") ||
		got.Router != ip4("192.168.1.1") || got.Subnet != ip4("255.255.255.0") {
		t.Errorf("offer parse mismatch: %+v", got)
	}
	req := got.Request(mac, got.YIAddr)
	if !bytes.Contains(req, []byte{50, 4, 192, 168, 1, 50}) {
		t.Errorf("request missing requested-IP option")
	}
}

// This test used to assert the opposite: that a bare Ethernet frame sat behind
// a 60-byte hardware header, and that a free-floating scan should find one at
// any offset. Both were wrong. The firmware delivers an 802.11 MPDU at
// RxMSDUOffset, never an Ethernet header, and the scan-for-a-plausible-header
// approach is what silently turned 802.11 headers into garbage — it produced a
// "scan result" whose BSSID was a duration field followed by our own MAC, and
// it made every DHCP offer undecodable.
//
// The positive cases now live in rx80211_test.go; what belongs here is the
// guarantee that the discredited shapes are REJECTED rather than half-decoded.
func TestExtractEthernetRejectsBareEthernet(t *testing.T) {
	ourMAC := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	eth := (&Ethernet{DA: ourMAC, SA: [6]byte{0xd2, 0xe8, 0xf0, 0x50, 0xf8, 0x32},
		Ethertype: EtherTypeIP, Payload: []byte{0x45, 0, 0, 20}}).Encode()

	for _, tc := range []struct {
		name string
		pad  int
	}{
		{"bare Ethernet behind a 60-byte header", 60},
		{"Ethernet at an arbitrary offset", 47},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ExtractEthernet(append(make([]byte, tc.pad), eth...), ourMAC); err == nil {
				t.Error("decoded a frame that is not an 802.11 data MPDU")
			}
		})
	}
}
