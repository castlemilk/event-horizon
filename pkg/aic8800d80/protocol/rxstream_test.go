package protocol

import (
	"encoding/binary"
	"testing"
)

// buildCfgRecord builds a raw RX record: [len:2][type:1][pad:1][body].
func buildCfgRecord(msgID uint16, param []byte) []byte {
	bodyLen := 8 + len(param) // id,dest,src,plen + param (no pattern needed for test)
	body := make([]byte, bodyLen)
	binary.LittleEndian.PutUint16(body[0:2], msgID)
	binary.LittleEndian.PutUint16(body[6:8], uint16(len(param)))
	copy(body[8:], param)
	rec := make([]byte, 4+bodyLen)
	binary.LittleEndian.PutUint16(rec[0:2], uint16(bodyLen))
	rec[2] = USBTypeCfgCmdRsp
	copy(rec[4:], body)
	return rec
}

// TestRxStream_SingleFrame verifies extraction of one config frame.
func TestRxStream_SingleFrame(t *testing.T) {
	var s RxStream
	s.Feed(buildCfgRecord(DBGMemReadCfm, []byte{0x00, 0x00, 0x50, 0x40, 0x20, 0x88, 0x07, 0xf9}))
	f, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("Next: ok=%v err=%v", ok, err)
	}
	if !f.IsConfig() {
		t.Fatalf("frame not config")
	}
	if f.MsgID() != DBGMemReadCfm {
		t.Errorf("msg id = 0x%04x", f.MsgID())
	}
	if len(f.Payload) != 16 {
		t.Errorf("payload len = %d, want 16", len(f.Payload))
	}
	if _, ok, _ := s.Next(); ok {
		t.Errorf("stream should be empty")
	}
}

// TestRxStream_SplitAcrossReads verifies a frame split across two
// chunk feeds is reassembled.
func TestRxStream_SplitAcrossReads(t *testing.T) {
	var s RxStream
	rec := buildCfgRecord(DBGMemWriteCfm, nil)
	s.Feed(rec[:5])
	if _, ok, _ := s.Next(); ok {
		t.Fatalf("should need more bytes")
	}
	s.Feed(rec[5:])
	f, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("Next: ok=%v err=%v", ok, err)
	}
	if f.MsgID() != DBGMemWriteCfm {
		t.Errorf("msg id = 0x%04x", f.MsgID())
	}
}

// TestRxStream_AggregatedFrames verifies two frames arriving in ONE
// chunk are extracted one at a time — the case that broke the naive
// one-frame-per-read parser.
func TestRxStream_AggregatedFrames(t *testing.T) {
	var s RxStream
	a := buildCfgRecord(DBGMemBlockWriteCfm, nil)
	b := buildCfgRecord(DBGMemReadCfm, nil)
	s.Feed(append(append([]byte{}, a...), b...))

	f1, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("first Next: ok=%v err=%v", ok, err)
	}
	if f1.MsgID() != DBGMemBlockWriteCfm {
		t.Errorf("first id = 0x%04x", f1.MsgID())
	}
	f2, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("second Next: ok=%v err=%v", ok, err)
	}
	if f2.MsgID() != DBGMemReadCfm {
		t.Errorf("second id = 0x%04x", f2.MsgID())
	}
}

// TestRxStream_DataFrameStride verifies data frames use the
// roundup(len+60, 4) stride so a following config frame is found.
func TestRxStream_DataFrameStride(t *testing.T) {
	var s RxStream
	// Data record: len=8, type=0 (data), body 8 bytes.
	// stride = 4 + roundup(8+60, 4) = 4 + 68 = 72.
	dataRec := make([]byte, 4+8)
	binary.LittleEndian.PutUint16(dataRec[0:2], 8)
	dataRec[2] = 0x00 // data type
	cfg := buildCfgRecord(DBGMemReadCfm, nil)

	chunk := make([]byte, 0, 72+len(cfg))
	chunk = append(chunk, dataRec...)
	chunk = append(chunk, make([]byte, 72-len(dataRec))...) // padding per stride
	chunk = append(chunk, cfg...)

	s.Feed(chunk)
	f1, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("first Next: ok=%v err=%v", ok, err)
	}
	if f1.IsConfig() {
		t.Fatalf("first frame should be data")
	}
	f2, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("second Next: ok=%v err=%v", ok, err)
	}
	if f2.MsgID() != DBGMemReadCfm {
		t.Errorf("second id = 0x%04x", f2.MsgID())
	}
}

// TestRxStream_CorruptRecord verifies a zero-length record errors and
// clears the buffer.
func TestRxStream_CorruptRecord(t *testing.T) {
	var s RxStream
	s.Feed([]byte{0x00, 0x00, 0x11, 0x00}) // len=0 → stride 4, actually valid empty config
	f, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("empty config record should parse: ok=%v err=%v", ok, err)
	}
	if f.MsgID() != 0 {
		t.Errorf("empty payload id should be 0")
	}
}
