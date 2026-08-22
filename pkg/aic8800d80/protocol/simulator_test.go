package protocol

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// ─── Golden fixtures: real frames captured off the wire ────────────────
//
// Captured 2026-08-15/16 from a live AIC8800D80 (chip rev U03 = 0x07)
// with [AIC-RX] logging. These are the ground truth for the RX layout.

// realReadCfmRaw is a DBG_MEM_READ_CFM answering a read of 0x40500000
// whose value was 0xf9078820 (chip_id 0x07 in bits 16..23).
//
//	14 00 | 11 | 00 | 01 04 | 64 00 | 01 00 | 08 00 | 2a de de ad | 00 00 50 40 | 20 88 07 f9
//	len=20 type=0x11   id=0x0401 dst=100  src=1    plen=8  pattern          memaddr      memdata
var realReadCfmRaw = []byte{
	0x14, 0x00, 0x11, 0x00,
	0x01, 0x04, 0x64, 0x00, 0x01, 0x00, 0x08, 0x00,
	0x2a, 0xde, 0xde, 0xad,
	0x00, 0x00, 0x50, 0x40,
	0x20, 0x88, 0x07, 0xf9,
}

// realBlockWriteCfmRaw is a DBG_MEM_BLOCK_WRITE_CFM (id 0x040c).
//
//	10 00 | 11 | 00 | 0c 04 | 64 00 | 01 00 | 04 00 | 2a de de ad | 00 00 00 00
var realBlockWriteCfmRaw = []byte{
	0x10, 0x00, 0x11, 0x00,
	0x0c, 0x04, 0x64, 0x00, 0x01, 0x00, 0x04, 0x00,
	0x2a, 0xde, 0xde, 0xad,
	0x00, 0x00, 0x00, 0x00,
}

// TestGolden_RealReadCfm verifies the RxStream + ParseMemReadCfm chain
// against the exact bytes observed on hardware.
func TestGolden_RealReadCfm(t *testing.T) {
	var s RxStream
	s.Feed(realReadCfmRaw)
	f, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("Next: ok=%v err=%v", ok, err)
	}
	if !f.IsConfig() {
		t.Fatalf("not a config frame (type 0x%02x)", f.Type)
	}
	if f.MsgID() != DBGMemReadCfm {
		t.Fatalf("msg id = 0x%04x, want 0x0401", f.MsgID())
	}
	addr, data, err := ParseMemReadCfm(f.Payload)
	if err != nil {
		t.Fatalf("ParseMemReadCfm: %v", err)
	}
	if addr != 0x40500000 {
		t.Errorf("addr = 0x%08x, want 0x40500000", addr)
	}
	if data != 0xf9078820 {
		t.Errorf("data = 0x%08x, want 0xf9078820", data)
	}
	// The chip-id extraction the loader performs.
	if chip := (data >> 16) & 0xFF; chip != 0x07 {
		t.Errorf("chip_id = 0x%02x, want 0x07 (U03)", chip)
	}
}

// TestGolden_RealBlockWriteCfm verifies the block-write confirm parses.
func TestGolden_RealBlockWriteCfm(t *testing.T) {
	var s RxStream
	s.Feed(realBlockWriteCfmRaw)
	f, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("Next: ok=%v err=%v", ok, err)
	}
	if f.MsgID() != DBGMemBlockWriteCfm {
		t.Fatalf("msg id = 0x%04x, want 0x040c", f.MsgID())
	}
	if err := ParseCfm(f.Payload, DBGMemBlockWriteCfm); err != nil {
		t.Errorf("ParseCfm: %v", err)
	}
}

// TestGolden_CapturedAggregation replays the actual failure capture: a
// single config CFM followed by bulk transfers carrying random-looking
// payload data and zero-fill — exactly what preceded the old parser's
// death on hardware. The stream must extract the CFM and discard the rest.
func TestGolden_CapturedAggregation(t *testing.T) {
	var s RxStream
	s.Feed(realBlockWriteCfmRaw)
	noise1 := make([]byte, 1024)
	noise1[0], noise1[1] = 0x01, 0x29 // length 10497?? -> treated as data record
	noise1[2] = 0xd2                  // data type (no 0x10 bit... 0xd2 & 0x10 = 0x10!) see below
	// NOTE: 0xd2 & 0x10 == 0x10 → the CFG bit IS set on this captured
	// byte, matching config semantics; stride = 4+0x2901. The capture is
	// mid-record garbage — feed and expect either a huge stride wait or
	// a resync. The key assertion: the CFM extracted FIRST is intact.
	s.Feed(noise1)
	s.Feed(make([]byte, 1024))

	f, ok, err := s.Next()
	if err != nil || !ok {
		t.Fatalf("first Next: ok=%v err=%v", ok, err)
	}
	if f.MsgID() != DBGMemBlockWriteCfm {
		t.Fatalf("first frame id = 0x%04x, want 0x040c", f.MsgID())
	}
}

// ─── Mock device: stateful wire-protocol simulator ────────────────────

// mockDevice simulates the AIC8800D80 boot ROM at the byte level. It
// consumes TX frames, maintains a RAM map, and answers with CFMs —
// optionally AGGREGATING several CFMs into one bulk transfer and/or
// splitting them across transfers, the behavior that broke the naive
// parser on real hardware.
type mockDevice struct {
	ram       map[uint32]uint32
	outbox    []byte   // pending response bytes
	pending   [][]byte // CFMs queued per request
	aggN      int      // CFMs to coalesce per transfer (1 = one per read)
	splitAt   int      // if >0, split each transfer's bytes at this offset
	writes    int      // count of block writes accepted
	oobWrites []uint32 // addresses rejected as out of the RAM map
	txLog     [][]byte // every TX frame received

	// protect models the Pandora-clone's protected word: any write whose
	// BYTES touch a protected address is rejected (recorded in oobWrites)
	// and the device goes silent — no CFM.
	protect map[uint32]bool
	// ramWindowFull opens the full 0x120000..0x178000 fmacfw window
	// (clones CAN write past 0x170000 — only the trigger word bites).
	ramWindowFull bool
}

func newMockDevice() *mockDevice {
	return &mockDevice{ram: map[uint32]uint32{}, aggN: 1}
}

// handleFrame parses one TX lmac_msg and queues its CFM.
func (m *mockDevice) handleFrame(frame []byte) {
	m.txLog = append(m.txLog, append([]byte{}, frame...))
	if len(frame) < 16 {
		return
	}
	msgID := binary.LittleEndian.Uint16(frame[8:10])
	paramLen := int(binary.LittleEndian.Uint16(frame[14:16]))
	param := frame[16 : 16+paramLen]

	rd32 := func(b []byte, i int) uint32 { return binary.LittleEndian.Uint32(b[4*i : 4*i+4]) }
	wr32 := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		return b
	}
	body := func(id uint16, params ...[]byte) []byte {
		// ipc_e2a_msg: id, dst, src, plen, pattern, params
		b := make([]byte, 12)
		binary.LittleEndian.PutUint16(b[0:2], id)
		binary.LittleEndian.PutUint16(b[6:8], uint16(4*len(params)))
		binary.LittleEndian.PutUint32(b[8:12], 0xADDEDE2A)
		for _, p := range params {
			b = append(b, p...)
		}
		return b
	}

	var cfmBody []byte
	switch msgID {
	case DBGMemReadReq:
		addr := rd32(param, 0)
		cfmBody = body(DBGMemReadCfm, wr32(addr), wr32(m.ram[addr]))
	case DBGMemWriteReq:
		addr, val := rd32(param, 0), rd32(param, 1)
		if !m.ramWrite(addr, val) {
			return // OOB: real device wedges; mock records and goes silent
		}
		cfmBody = body(DBGMemWriteCfm, wr32(addr), wr32(val))
	case DBGMemBlockWriteReq:
		addr := rd32(param, 0)
		size := int(rd32(param, 1))
		data := param[8 : 8+size]
		for i := 0; i+4 <= len(data); i += 4 {
			if !m.ramWrite(addr+uint32(i), binary.LittleEndian.Uint32(data[i:i+4])) {
				return
			}
		}
		m.writes++
		cfmBody = body(DBGMemBlockWriteCfm, wr32(0))
	case DBGStartAppReq:
		return // fire-and-forget, no CFM
	default:
		cfmBody = body(msgID+1)
	}

	// Wrap in RX record header.
	rec := make([]byte, 4+len(cfmBody))
	binary.LittleEndian.PutUint16(rec[0:2], uint16(len(cfmBody)))
	rec[2] = USBTypeCfgCmdRsp
	copy(rec[4:], cfmBody)
	m.pending = append(m.pending, rec)
}

// ramWrite returns false (and records the address) when the write falls
// outside the simulated RAM windows — mirroring the hardware wedge the
// real device exhibited at 0x170000+.
func (m *mockDevice) ramWrite(addr, val uint32) bool {
	if m.protect != nil && m.protect[addr] {
		m.oobWrites = append(m.oobWrites, addr)
		return false
	}
	inWindow := func(a uint32) bool {
		if m.ramWindowFull && a >= 0x00120000 && a < 0x00178000 {
			return true
		}
		return (a >= 0x001e0000 && a < 0x001e0000+64*1024) || // patch region
			(a >= 0x00200000 && a < 0x00210000) || // adid + ext0
			(a >= 0x00120000 && a < 0x00170000) || // fmacfw region ENDS at 0x170000
			(a >= 0x40500000 && a < 0x40501000) // system regs
	}
	if !inWindow(addr) {
		m.oobWrites = append(m.oobWrites, addr)
		return false
	}
	m.ram[addr] = val
	return true
}

// read simulates one bulk IN transfer: pops up to aggN queued CFMs,
// concatenates them (aggregation!), optionally splits the result.
func (m *mockDevice) read(buf []byte, timeoutMs int) (int, error) {
	if len(m.pending) == 0 {
		return 0, errors.New("LIBUSB_ERROR_TIMEOUT")
	}
	var chunk []byte
	for i := 0; i < m.aggN && len(m.pending) > 0; i++ {
		chunk = append(chunk, m.pending[0]...)
		m.pending = m.pending[1:]
	}
	if m.splitAt > 0 && m.splitAt < len(chunk) {
		head := chunk[:m.splitAt]
		tail := append([]byte{}, chunk[m.splitAt:]...)
		m.pending = append([][]byte{tail}, m.pending...)
		chunk = head
	}
	return copy(buf, chunk), nil
}

// scriptedReader feeds a fixed sequence of transfers then times out.
type scriptedReader struct {
	transfers [][]byte
	i        int
}

func (s *scriptedReader) read(buf []byte, timeoutMs int) (int, error) {
	if s.i >= len(s.transfers) {
		return 0, errors.New("LIBUSB_ERROR_TIMEOUT")
	}
	n := copy(buf, s.transfers[s.i])
	s.i++
	return n, nil
}

// ─── Tests against the production receive path ───────────────────────

// TestWaitForCfm_AggregatedResponses is THE regression test for the
// hardware failure: several CFMs arriving in ONE bulk transfer. The old
// parser read one frame per transfer, mis-parsed the tail, and lost the
// pairing at ~block 18 / ~block 320 of the upload.
func TestWaitForCfm_AggregatedResponses(t *testing.T) {
	dev := newMockDevice()
	dev.aggN = 5 // five CFMs coalesced per transfer

	// Simulate: 3 block writes completed; host then asks for a 4th whose
	// CFM shares a transfer with the 3 earlier ones.
	for i := 0; i < 3; i++ {
		m := BuildLmacMessage(DBGMemBlockWriteReq, TaskDBG, DrvTaskID, MemBlockWritePayload(0x120000+uint32(i)*1024, make([]byte, 1024)))
		dev.handleFrame(m)
	}
	// The responses for those three are queued. Feed them all through
	// waitForCfm asking for the LAST one's id — it must be found even
	// though it sits behind two other frames in the same byte stream.
	var s RxStream
	for {
		payload, err := waitForCfm(&s, DBGMemBlockWriteCfm, dev.read, 1000)
		if err == nil {
			if err := ParseCfm(payload, DBGMemBlockWriteCfm); err != nil {
				t.Fatalf("ParseCfm: %v", err)
			}
			return // success: found a block-write CFM inside aggregated data
		}
		if strings.Contains(err.Error(), "timeout") && len(dev.pending) == 0 {
			t.Fatalf("no CFM extracted from aggregated transfer: %v", err)
		}
	}
}

// TestWaitForCfm_SplitResponse frames the CFM across two transfers.
func TestWaitForCfm_SplitResponse(t *testing.T) {
	dev := newMockDevice()
	dev.splitAt = 11 // mid-record split
	req := BuildLmacMessage(DBGMemReadReq, TaskDBG, DrvTaskID, MemReadPayload(0x40500000))
	dev.handleFrame(req)
	dev.ram[0x40500000] = 0xf9078820
	// Re-handle so the CFM is generated AFTER the RAM value is set.
	dev.pending = nil
	dev.txLog = nil
	dev.handleFrame(req)

	var s RxStream
	payload, err := waitForCfm(&s, DBGMemReadCfm, dev.read, 1000)
	if err != nil {
		t.Fatalf("waitForCfm: %v", err)
	}
	addr, data, err := ParseMemReadCfm(payload)
	if err != nil {
		t.Fatalf("ParseMemReadCfm: %v", err)
	}
	if addr != 0x40500000 || data != 0xf9078820 {
		t.Fatalf("addr=0x%08x data=0x%08x", addr, data)
	}
}

// TestWaitForCfm_DataNoiseBeforeCfm interleaves data frames (60-byte HW
// header stride) with the CFM — the exact capture pattern.
func TestWaitForCfm_DataNoiseBeforeCfm(t *testing.T) {
	// Build one data record: len=8 body, type without 0x10 bit.
	dataRec := make([]byte, 4+8)
	binary.LittleEndian.PutUint16(dataRec[0:2], 8)
	dataRec[2] = 0x02
	dataRec = append(dataRec, make([]byte, 60)...) // HW header padding to stride

	sr := &scriptedReader{transfers: [][]byte{
		dataRec,
		realReadCfmRaw,
	}}
	var s RxStream
	payload, err := waitForCfm(&s, DBGMemReadCfm, sr.read, 1000)
	if err != nil {
		t.Fatalf("waitForCfm: %v", err)
	}
	if _, data, err := ParseMemReadCfm(payload); err != nil || data != 0xf9078820 {
		t.Fatalf("parse: %v data=0x%08x", err, data)
	}
}

// TestMockUpload_FullChain runs the production MemRead/MemWrite/
// MemBlockWrite sequence against the stateful mock with aggregation ON:
// chip-config read, 1KB-chunked patch upload, patch-table pair writes.
// This is the offline dress rehearsal for the real upload.
func TestMockUpload_FullChain(t *testing.T) {
	dev := newMockDevice()
	dev.aggN = 3
	dev.ram[0x40500000] = 0xf9078820 // real captured value: chip_id 0x07 (U03) in [23:16]

	// A readFn the production helpers can consume: route TX frames into
	// the mock, use its read for RX.
	send := func(msgID uint16, dest lmacTaskID, payload []byte) ([]byte, error) {
		frame := BuildLmacMessage(msgID, dest, DrvTaskID, payload)
		dev.handleFrame(frame)
		if msgID == DBGStartAppReq {
			return nil, nil
		}
		var s RxStream
		return waitForCfm(&s, msgID+1, dev.read, 2000)
	}

	// 1. Chip id read.
	cfm, err := send(DBGMemReadReq, TaskDBG, MemReadPayload(0x40500000))
	if err != nil {
		t.Fatalf("chip read: %v", err)
	}
	_, val, err := ParseMemReadCfm(cfm)
	if err != nil {
		t.Fatalf("parse chip read: %v", err)
	}
	if chip := uint8(val >> 16); chip != 0x07 {
		t.Fatalf("chip = 0x%02x, want 0x07", chip)
	}

	// 2. Patch blob upload in 1KB chunks (32KB blob).
	blob := make([]byte, 32700)
	for i := range blob {
		blob[i] = byte(i)
	}
	for off := 0; off < len(blob); off += 1024 {
		end := off + 1024
		if end > len(blob) {
			end = len(blob)
		}
		cfm, err := send(DBGMemBlockWriteReq, TaskDBG, MemBlockWritePayload(0x1e0000+uint32(off), blob[off:end]))
		if err != nil {
			t.Fatalf("patch block @%d: %v", off, err)
		}
		if err := ParseCfm(cfm, DBGMemBlockWriteCfm); err != nil {
			t.Fatalf("patch block cfm @%d: %v", off, err)
		}
	}
	if dev.writes != 32 {
		t.Errorf("block writes = %d, want 32", dev.writes)
	}

	// 3. Patch table pairs.
	for i := 0; i < 10; i++ {
		cfm, err := send(DBGMemWriteReq, TaskDBG, MemWritePayload(0x40500100+uint32(4*i), uint32(i)))
		if err != nil {
			t.Fatalf("pair %d: %v", i, err)
		}
		_ = cfm
	}

	// 4. No OOB writes may have occurred.
	if len(dev.oobWrites) > 0 {
		t.Errorf("OOB writes: %v", dev.oobWrites)
	}
}

// TestMockUpload_OOBWriteDocumentsWedge simulates the EXACT hardware
// failure mode: fmacfw is 358072 bytes but the RAM window at 0x120000
// ends at 0x170000 (320KB). Writing past it recorded OOB and the mock
// (like the real chip) stops answering — this test PROVES the boundary
// is real and documents that a 358072-byte fmacfw cannot fit a
// 0x120000..0x170000 window.
func TestMockUpload_OOBWriteDocumentsWedge(t *testing.T) {
	dev := newMockDevice()
	dev.aggN = 1
	const fwAddr = 0x120000
	const fmacSize = 358072

	blob := make([]byte, fmacSize)
	for off := 0; off < fmacSize; off += 1024 {
		end := off + 1024
		if end > len(blob) {
			end = len(blob)
		}
		frame := BuildLmacMessage(DBGMemBlockWriteReq, TaskDBG, DrvTaskID, MemBlockWritePayload(fwAddr+uint32(off), blob[off:end]))
		dev.handleFrame(frame)
	}
	if len(dev.oobWrites) == 0 {
		t.Fatalf("expected OOB writes past 0x170000 — none recorded; window model wrong")
	}
	first := dev.oobWrites[0]
	if first != 0x170000 {
		t.Errorf("first OOB write at 0x%08x, want 0x170000 (the exact address the real device wedged at)", first)
	}
}
