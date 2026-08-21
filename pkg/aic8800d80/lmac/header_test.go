package lmac

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	h := Header{ID: MMVersionReq, DestID: uint16(TaskLast), SrcID: 100, ParamLen: 8}
	buf := make([]byte, HeaderSize)
	h.Encode(buf)
	var got Header
	if err := got.Decode(buf); err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Fatalf("round-trip drifted: got %+v want %+v", got, h)
	}
}

func TestHeaderLayout(t *testing.T) {
	h := Header{ID: 0x1234, DestID: 0x0005, SrcID: 100, ParamLen: 64}
	buf := make([]byte, HeaderSize)
	h.Encode(buf)
	if binary.LittleEndian.Uint16(buf[0:2]) != 0x1234 {
		t.Errorf("id slot drift")
	}
	if binary.LittleEndian.Uint16(buf[2:4]) != 0x0005 {
		t.Errorf("dest_id slot drift")
	}
	if binary.LittleEndian.Uint16(buf[4:6]) != 100 {
		t.Errorf("src_id slot drift")
	}
	if binary.LittleEndian.Uint16(buf[6:8]) != 64 {
		t.Errorf("param_len slot drift")
	}
}

func TestSplitMessage(t *testing.T) {
	payload := []byte{0xAA, 0xBB, 0xCC}
	full := make([]byte, HeaderSize+len(payload))
	Header{ID: MMVersionReq, DestID: uint16(TaskLast), SrcID: DRVTaskID, ParamLen: uint16(len(payload))}.Encode(full)
	copy(full[HeaderSize:], payload)
	h, rest, err := SplitMessage(full)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != MMVersionReq || !bytes.Equal(rest, payload) {
		t.Fatalf("drift: hdr=%+v rest=%x", h, rest)
	}
}

func TestSplitMessageShort(t *testing.T) {
	if _, _, err := SplitMessage([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected error on short buffer")
	}
}
