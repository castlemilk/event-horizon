package lmac

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestTLVPutGetRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := PutTLV(&buf, 0x0042, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if err := PutTLV(&buf, 0x0100, []byte{0xaa, 0xbb}); err != nil {
		t.Fatal(err)
	}
	out := buf.Bytes()
	var items []TLV
	rest, err := GetAllTLV(out, &items)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 0 {
		t.Fatalf("trailing bytes: %d", len(rest))
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Tag != 0x0042 || !bytes.Equal(items[0].Value, []byte{1, 2, 3, 4}) {
		t.Errorf("item 0: %+v", items[0])
	}
	if items[1].Tag != 0x0100 || !bytes.Equal(items[1].Value, []byte{0xaa, 0xbb}) {
		t.Errorf("item 1: %+v", items[1])
	}
}

func TestTLVEmptyValue(t *testing.T) {
	var buf bytes.Buffer
	if err := PutTLV(&buf, 0x0001, nil); err != nil {
		t.Fatal(err)
	}
	var items []TLV
	if _, err := GetAllTLV(buf.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if items[0].Tag != 0x0001 || len(items[0].Value) != 0 {
		t.Errorf("empty TLV drift: %+v", items[0])
	}
}

func TestTLVHeaderSize(t *testing.T) {
	var buf bytes.Buffer
	if err := PutTLV(&buf, 0x0001, []byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	out := buf.Bytes()
	if binary.LittleEndian.Uint16(out[0:2]) != 0x0001 {
		t.Errorf("tag slot drift")
	}
	if binary.LittleEndian.Uint16(out[2:4]) != 4 {
		t.Errorf("len slot drift")
	}
}

func TestTLVTruncatedValue(t *testing.T) {
	// 4-byte header claiming 100 bytes of value, but only 2 bytes follow.
	buf := []byte{0x01, 0x00, 0x64, 0x00, 0xAA, 0xBB}
	var items []TLV
	if _, err := GetAllTLV(buf, &items); err == nil {
		t.Fatal("expected truncated-value error")
	}
}
