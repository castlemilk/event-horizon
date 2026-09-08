package lmac

import (
	"bytes"
	"testing"
)

// Pins the USB TX record framing to aicwf_usb_bus_txdata() — the path the
// reference actually compiles (CONFIG_USB_TX_AGGR = n, Makefile:89).
//
// This is the test that was missing. We shipped the aggregated 8-byte header
// from aicwf_usb_aggr() instead, which is only built under that disabled
// config. The firmware reads byte 2 as the record type; with the 8-byte header
// it sees a length byte (0x99) rather than 0x01 and drops the frame, while the
// USB write still succeeds — indistinguishable from broken TX hardware.
func TestTxDataRecordFraming(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAB}, 121) // a real EAPOL msg2 length
	tx := &TxData{
		DA:         [6]byte{0xd2, 0xe8, 0xf0, 0x50, 0xf8, 0x32},
		SA:         [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55},
		Ethertype:  EAPOLEthertype,
		VifIdx:     0,
		StaIdx:     0,
		Payload:    payload,
		ConfirmIdx: 0,
	}
	rec, err := tx.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// 4 + 28 + 121 = 153, padded to 156.
	if len(rec) != 156 {
		t.Fatalf("record = %d bytes, want 156 (4 hdr + 28 hostdesc + 121 payload, 4-aligned)", len(rec))
	}
	if got := int(rec[0]) | int(rec[1]&0x0f)<<8; got != len(rec) {
		t.Errorf("length field = %d, want the padded total %d", got, len(rec))
	}
	if rec[2] != txDataRecordType {
		t.Errorf("record type byte = %#x, want %#x — an 8-byte aggregation header "+
			"puts a length byte here and the firmware drops the frame", rec[2], txDataRecordType)
	}
	if rec[3] != 0 {
		t.Errorf("reserved byte = %#x, want 0", rec[3])
	}

	// hostdesc starts immediately after the 4-byte header.
	hd := rec[4:32]
	if got := int(hd[0]) | int(hd[1])<<8; got != len(payload) {
		t.Errorf("hostdesc packet_len = %d, want %d", got, len(payload))
	}
	if got := uint32(hd[4]) | uint32(hd[5])<<8 | uint32(hd[6])<<16 | uint32(hd[7])<<24; got != 0x80000000 {
		t.Errorf("status_desc_addr = %#x, want bit31|0 — the reference sets need_cfm "+
			"for every EAPOL frame (rwnx_tx.c:676-680)", got)
	}
	if !bytes.Equal(hd[8:14], tx.DA[:]) || !bytes.Equal(hd[14:20], tx.SA[:]) {
		t.Errorf("DA/SA misplaced: %x / %x", hd[8:14], hd[14:20])
	}
	if hd[20] != 0x88 || hd[21] != 0x8e {
		t.Errorf("ethertype = %x, want 88 8e on the wire (LE u16 0x8e88, which is "+
			"what the firmware compares against)", hd[20:22])
	}
	if hd[24] != 0 || hd[25] != 0 {
		t.Errorf("vif/sta idx = %d/%d, want 0/0", hd[24], hd[25])
	}
	if !bytes.Equal(rec[32:32+len(payload)], payload) {
		t.Error("payload not at offset 32")
	}
	for i, b := range rec[32+len(payload):] {
		if b != 0 {
			t.Errorf("pad byte %d = %#x, want 0", i, b)
		}
	}
}

// A payload that lands the record exactly on a 4-byte boundary must not gain a
// spurious pad — the length field has to stay equal to the real record size.
func TestTxDataNoPadWhenAligned(t *testing.T) {
	tx := &TxData{Payload: bytes.Repeat([]byte{1}, 120), ConfirmIdx: -1}
	rec, err := tx.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(rec) != 152 {
		t.Fatalf("record = %d bytes, want 152 (already aligned)", len(rec))
	}
	if got := int(rec[0]) | int(rec[1]&0x0f)<<8; got != 152 {
		t.Errorf("length field = %d, want 152", got)
	}
	if got := uint32(rec[8]) | uint32(rec[9])<<8 | uint32(rec[10])<<16 | uint32(rec[11])<<24; got != 0 {
		t.Errorf("status_desc_addr = %#x, want 0 when ConfirmIdx is -1", got)
	}
}
