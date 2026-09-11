package lmac

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildRxRecord assembles an OnDataFrame payload (a USB record minus its
// 4-byte header) carrying one 802.11 data frame, so the tests exercise the
// exact geometry the firmware delivers: 56 bytes of hw_rxhdr remainder, then
// the MPDU at offset RxMSDUOffset.
func buildRxRecord(mpdu []byte, decr uint8, flags uint32) []byte {
	rec := make([]byte, RxMSDUOffset+len(mpdu))
	binary.LittleEndian.PutUint32(rec[rxStatusWordOff:rxStatusWordOff+4],
		uint32(decr&0x07)<<2|1<<13) // decr_status + frm_successful_rx
	binary.LittleEndian.PutUint32(rec[rxFlagsWordOff:rxFlagsWordOff+4], flags)
	copy(rec[RxMSDUOffset:], mpdu)
	return rec
}

// mpdu builds an AP->STA (ToDS=0 FromDS=1) data frame.
func mpdu(qos bool, ourMAC, bssid, src [6]byte, secLen int, ethertype uint16, body []byte) []byte {
	fc0 := byte(0x08) // protover 0, type 2 (data), subtype 0
	if qos {
		fc0 |= 0x80 // QoS data
	}
	out := []byte{fc0, 0x02} // fc1: FromDS=1
	out = append(out, 0x00, 0x00)
	out = append(out, ourMAC[:]...) // addr1 = RA = us
	out = append(out, bssid[:]...)  // addr2 = TA = the AP
	out = append(out, src[:]...)    // addr3 = the true originator
	out = append(out, 0x00, 0x00)   // sequence control
	if qos {
		out = append(out, 0x00, 0x00) // QoS control, A-MSDU bit clear
	}
	out = append(out, make([]byte, secLen)...)             // CCMP header, left in place
	out = append(out, 0xAA, 0xAA, 0x03, 0x00, 0x00, 0x00)  // LLC/SNAP
	out = append(out, byte(ethertype>>8), byte(ethertype)) // ethertype, big-endian
	return append(out, body...)
}

func TestExtractEthernet80211(t *testing.T) {
	our := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	bssid := [6]byte{0xd2, 0xe8, 0xf0, 0x50, 0xf8, 0x32}
	src := [6]byte{0xd2, 0xe8, 0xf0, 0x50, 0xf8, 0x00}
	body := []byte{0xde, 0xad, 0xbe, 0xef}

	for _, tc := range []struct {
		name   string
		qos    bool
		decr   uint8
		secLen int
	}{
		{"non-QoS CCMP", false, DecrCCMP128, 8},
		{"QoS CCMP", true, DecrCCMP128, 8},
		{"unencrypted", false, DecrUnenc, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := buildRxRecord(mpdu(tc.qos, our, bssid, src, tc.secLen, EtherTypeIP, body), tc.decr, 0)
			eth, _, err := ExtractEthernet(rec, our)
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			if eth.DA != our {
				t.Errorf("DA = %x, want addr1 (%x)", eth.DA, our)
			}
			if eth.SA != src {
				t.Errorf("SA = %x, want addr3 (%x) — addr2 is the AP, not the originator", eth.SA, src)
			}
			if eth.Ethertype != EtherTypeIP {
				t.Errorf("ethertype = %#x, want %#x", eth.Ethertype, EtherTypeIP)
			}
			if !bytes.Equal(eth.Payload, body) {
				t.Errorf("payload = %x, want %x", eth.Payload, body)
			}
		})
	}
}

// A broadcast DHCP offer (group-addressed, GTK-decrypted) must decode: with the
// BROADCAST flag set in our DISCOVER, this is how the reply comes back.
func TestExtractEthernetBroadcast(t *testing.T) {
	our := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	bcast := [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	bssid := [6]byte{0xd2, 0xe8, 0xf0, 0x50, 0xf8, 0x32}
	server := [6]byte{0xd2, 0xe8, 0xf0, 0x50, 0xf8, 0x01}

	rec := buildRxRecord(mpdu(false, bcast, bssid, server, 8, EtherTypeIP, []byte{1, 2, 3}), DecrCCMP128, 0)
	eth, _, err := ExtractEthernet(rec, our)
	if err != nil {
		t.Fatalf("broadcast extract: %v", err)
	}
	if eth.DA != bcast || eth.SA != server {
		t.Errorf("DA/SA = %x / %x, want broadcast / %x", eth.DA, eth.SA, server)
	}
}

// The old decoder read the 802.11 header as an Ethernet header, which is how a
// bogus scan result with BSSID 00:00:02:11:22:33 (duration || our MAC) appeared.
func TestExtractEthernetRejectsNonData(t *testing.T) {
	our := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	bssid := [6]byte{0xd2, 0xe8, 0xf0, 0x50, 0xf8, 0x32}

	// flags_is_80211_mpdu set => management frame, must not become Ethernet.
	rec := buildRxRecord(mpdu(false, our, bssid, bssid, 0, EtherTypeIP, nil), DecrUnenc, 1<<1)
	if _, _, err := ExtractEthernet(rec, our); err == nil {
		t.Error("management MPDU decoded as Ethernet")
	}

	// A beacon (type 0 mgmt) must be rejected on the frame-control check.
	beacon := buildRxRecord(append([]byte{0x80, 0x00}, make([]byte, 60)...), DecrUnenc, 0)
	if _, _, err := ExtractEthernet(beacon, our); err == nil {
		t.Error("beacon decoded as a data frame")
	}
}

func TestConnectIndOffsets(t *testing.T) {
	p := make([]byte, ConnectIndSize)
	copy(p[2:8], []byte{0xd2, 0xe8, 0xf0, 0x50, 0xf8, 0x32})
	p[8], p[9], p[10], p[11] = 1, 0, 3, 7 // roamed, vif_idx, ap_idx, ch_idx
	binary.LittleEndian.PutUint16(p[820:822], 42)
	p[822] = 1
	binary.LittleEndian.PutUint16(p[824:826], 2412)

	var c ConnectInd
	if err := c.Decode(p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.APIdx != 3 || c.ChIdx != 7 || !c.Roamed {
		t.Errorf("head fields wrong: roamed=%v vif=%d ap=%d ch=%d", c.Roamed, c.VifIdx, c.APIdx, c.ChIdx)
	}
	// The regression these pin: at 818/820/822 all three read as zero, which is
	// exactly the "aid=0 band=0 freq=0" every run printed.
	if c.AID != 42 {
		t.Errorf("AID = %d, want 42 (aid is at 820, not 818)", c.AID)
	}
	if c.Band != 1 {
		t.Errorf("Band = %d, want 1 (band is at 822, not 820)", c.Band)
	}
	if c.CenterFreq != 2412 {
		t.Errorf("CenterFreq = %d, want 2412 (center_freq is at 824, not 822)", c.CenterFreq)
	}
	// A clipped payload must report, not silently yield zeros.
	if err := c.Decode(p[:840]); err == nil {
		t.Error("truncated connect ind decoded without error")
	}
}

// BOOTP fixes the message at 300 octets; short DISCOVERs get dropped by many
// servers and DHCP-snooping APs.
func TestDHCPDiscoverPadding(t *testing.T) {
	d := DHCP{XID: [4]byte{1, 2, 3, 4}}
	pkt := d.Discover([6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55})
	if len(pkt) < 300 {
		t.Errorf("DISCOVER = %d bytes, want >= 300 (BOOTP minimum)", len(pkt))
	}
	if got := binary.BigEndian.Uint32(pkt[236:240]); got != DHCPMagic {
		t.Errorf("magic cookie = %#x, want %#x", got, DHCPMagic)
	}
	if !bytes.Contains(pkt[240:], []byte{53, 1, 1}) {
		t.Error("option 53 (DHCPDISCOVER) missing")
	}
	if !bytes.Contains(pkt[240:], []byte{255}) {
		t.Error("option end marker missing")
	}
}
