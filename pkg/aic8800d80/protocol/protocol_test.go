package protocol

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// TestBuildLmacMessage_Header verifies the 16-byte header layout matches
// the Linux driver's aicbluetooth_cmds.c:cmd_msg_frame.
func TestBuildLmacMessage_Header(t *testing.T) {
	msg := BuildLmacMessage(DBGMemWriteReq, TaskDBG, DrvTaskID, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	if len(msg) != 20 {
		t.Fatalf("len = %d, want 20 (16 header + 4 payload)", len(msg))
	}

	// header is 16 bytes; payload is 4 bytes
	// len field = 8 (lmac_msg fields) + 4 (payload) + 4 = 16 = 0x0010
	if msg[0] != 0x10 || msg[1] != 0x00 {
		t.Errorf("len bytes = %02x %02x, want 0x10 0x00", msg[0], msg[1])
	}
	if msg[2] != 0x11 || msg[3] != 0x00 {
		t.Errorf("marker bytes = %02x %02x, want 0x11 0x00", msg[2], msg[3])
	}
	// bytes 4..7 = dummy word = 0
	for i := 4; i <= 7; i++ {
		if msg[i] != 0 {
			t.Errorf("dummy byte %d = %02x, want 0", i, msg[i])
		}
	}
	// msg_id (1026)
	if got := binary.LittleEndian.Uint16(msg[8:10]); got != DBGMemWriteReq {
		t.Errorf("msg_id = 0x%04x, want 0x%04x", got, DBGMemWriteReq)
	}
	// dest_id (1)
	if got := binary.LittleEndian.Uint16(msg[10:12]); got != uint16(TaskDBG) {
		t.Errorf("dest_id = %d, want %d", got, TaskDBG)
	}
	// src_id (100)
	if got := binary.LittleEndian.Uint16(msg[12:14]); got != uint16(DrvTaskID) {
		t.Errorf("src_id = %d, want %d", got, DrvTaskID)
	}
	// param_len (4)
	if got := binary.LittleEndian.Uint16(msg[14:16]); got != 4 {
		t.Errorf("param_len = %d, want 4", got)
	}
	// payload
	if !bytes.Equal(msg[16:], []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Errorf("payload = %x, want deadbeef", msg[16:])
	}
}

// TestBuildLmacMessage_EmptyPayload verifies the message compiles with no
// payload, which is the case for DBG_START_APP_REQ.
func TestBuildLmacMessage_EmptyPayload(t *testing.T) {
	msg := BuildLmacMessage(DBGStartAppReq, TaskDBG, DrvTaskID, nil)
	if len(msg) != 16 {
		t.Fatalf("len = %d, want 16 (header only)", len(msg))
	}
	if got := binary.LittleEndian.Uint16(msg[14:16]); got != 0 {
		t.Errorf("param_len = %d, want 0", got)
	}
}

// TestMemWritePayload verifies the DBG_MEM_WRITE_REQ payload
// is little-endian (a, value).
func TestMemWritePayload(t *testing.T) {
	b := MemWritePayload(0x40500000, 0xCAFEBABE)
	if len(b) != 8 {
		t.Fatalf("len = %d, want 8", len(b))
	}
	if got := binary.LittleEndian.Uint32(b[0:4]); got != 0x40500000 {
		t.Errorf("addr = 0x%08x, want 0x40500000", got)
	}
	if got := binary.LittleEndian.Uint32(b[4:8]); got != 0xCAFEBABE {
		t.Errorf("data = 0x%08x, want 0xCAFEBABE", got)
	}
}

// TestMemBlockWritePayload_Padding verifies that a sub-1024-byte block
// is zero-padded out to the full 1024 + 8 header size, matching the
// Linux driver's struct dbg_mem_block_write_req.
func TestMemBlockWritePayload_Padding(t *testing.T) {
	b := MemBlockWritePayload(0x100000, []byte{0x01, 0x02, 0x03})
	if len(b) != 8+1024 {
		t.Fatalf("len = %d, want %d", len(b), 8+1024)
	}
	if got := binary.LittleEndian.Uint32(b[0:4]); got != 0x100000 {
		t.Errorf("addr = 0x%08x", got)
	}
	if got := binary.LittleEndian.Uint32(b[4:8]); got != 3 {
		t.Errorf("memsize = %d, want 3", got)
	}
	if b[8] != 0x01 || b[9] != 0x02 || b[10] != 0x03 {
		t.Errorf("data prefix = %x", b[8:11])
	}
	// trailing padding must be zero
	for i := 11; i < len(b); i++ {
		if b[i] != 0 {
			t.Errorf("padding byte %d = 0x%02x, want 0", i, b[i])
		}
	}
}

// TestStartAppPayload verifies the DBG_START_APP_REQ payload.
func TestStartAppPayload(t *testing.T) {
	b := StartAppPayload(0x100000, HostStartAppAuto)
	if len(b) != 8 {
		t.Fatalf("len = %d, want 8", len(b))
	}
	if got := binary.LittleEndian.Uint32(b[0:4]); got != 0x100000 {
		t.Errorf("bootaddr = 0x%x, want 0x100000", got)
	}
	if got := binary.LittleEndian.Uint32(b[4:8]); got != HostStartAppAuto {
		t.Errorf("boottype = %d, want %d", got, HostStartAppAuto)
	}
}

// TestParseMemReadCfm verifies the parser correctly decodes a synthetic
// DBG_MEM_READ_CFM payload (ipc_e2a_msg: id@0, dest@2, src@4, plen@6,
// pattern@8, param@12).
func TestParseMemReadCfm(t *testing.T) {
	p := make([]byte, 20)
	binary.LittleEndian.PutUint16(p[0:2], DBGMemReadCfm)
	binary.LittleEndian.PutUint16(p[2:4], 100) // dummy_dest (DRV_TASK_ID)
	binary.LittleEndian.PutUint16(p[4:6], 1)   // dummy_src
	binary.LittleEndian.PutUint16(p[6:8], 8)   // param_len
	binary.LittleEndian.PutUint32(p[8:12], 0xADDEDE2A)
	binary.LittleEndian.PutUint32(p[12:16], 0x40500000)
	binary.LittleEndian.PutUint32(p[16:20], 0x12345678)

	addr, data, err := ParseMemReadCfm(p)
	if err != nil {
		t.Fatalf("ParseMemReadCfm: %v", err)
	}
	if addr != 0x40500000 {
		t.Errorf("addr = 0x%08x", addr)
	}
	if data != 0x12345678 {
		t.Errorf("data = 0x%08x", data)
	}
}

// TestParseMemReadCfm_TooShort verifies the parser rejects short buffers.
func TestParseMemReadCfm_TooShort(t *testing.T) {
	_, _, err := ParseMemReadCfm([]byte{0x00, 0x01})
	if err == nil {
		t.Errorf("expected error for short buffer")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("error = %v, want 'too short'", err)
	}
}

// TestParseMemReadCfm_BadMarker verifies the parser rejects frames with
// the wrong type byte.
func TestParseMemReadCfm_BadMarker(t *testing.T) {
	buf := make([]byte, 24)
	buf[2] = 0xFF // wrong
	if _, _, err := ParseMemReadCfm(buf); err == nil {
		t.Errorf("expected error for bad marker")
	}
}

// TestMakeMsgID verifies the message-id bit packing.
func TestMakeMsgID(t *testing.T) {
	if got := MakeMsgID(TaskDBG, 0); got != 1024 {
		t.Errorf("MakeMsgID(TaskDBG, 0) = %d, want 1024", got)
	}
	if got := MakeMsgID(TaskDBG, 1); got != 1025 {
		t.Errorf("MakeMsgID(TaskDBG, 1) = %d, want 1025", got)
	}
}

// TestStage_VIDPIDs verifies our canonical VID/PID values match known
// hardware.
func TestStage_VIDPIDs(t *testing.T) {
	if VID_ZeroCD_Device != 0x1111 || PID_ZeroCD_Device != 0x1111 {
		t.Errorf("ZeroCD %04x:%04x want 1111:1111", VID_ZeroCD_Device, PID_ZeroCD_Device)
	}
	if VID_AIC8800D80_BootROM != 0xa69c || PID_AIC8800D80_BootROM != 0x8d80 {
		t.Errorf("BootROM %04x:%04x want a69c:8d80", VID_AIC8800D80_BootROM, PID_AIC8800D80_BootROM)
	}
	if VID_AIC8800D80_OpWiFiBT != 0xa69c || PID_AIC8800D80_OpWiFiBT != 0x8d81 {
		t.Errorf("OpWiFiBT %04x:%04x want a69c:8d81", VID_AIC8800D80_OpWiFiBT, PID_AIC8800D80_OpWiFiBT)
	}
	if VID_AIC8800D80_OpWiFi != 0xa69c || PID_AIC8800D80_OpWiFi != 0x8d83 {
		t.Errorf("OpWiFi %04x:%04x want a69c:8d83", VID_AIC8800D80_OpWiFi, PID_AIC8800D80_OpWiFi)
	}
}

// TestEnumValues_SpotCheck verifies the enum values match the Linux
// driver's dbg_msg_tag enum.
func TestEnumValues_SpotCheck(t *testing.T) {
	cases := []struct {
		got, want uint16
		note      string
	}{
		{DBGMemReadReq, 1024, "DBG_MEM_READ_REQ"},
		{DBGMemWriteReq, 1026, "DBG_MEM_WRITE_REQ"},
		{DBGMemBlockWriteReq, 1035, "DBG_MEM_BLOCK_WRITE_REQ"},
		{DBGStartAppReq, 1037, "DBG_START_APP_REQ"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = 0x%04x (%d), want %d", c.note, c.got, c.got, c.want)
		}
	}
}

// TestChipRevRAMAddrs verifies the firmware blob addresses for each
// chip revision.
func TestChipRevRAMAddrs(t *testing.T) {
	if RAMFMACFWAddr8800D80 != 0x100000 {
		t.Errorf("RAMFMACFWAddr8800D80 = 0x%x, want 0x100000", RAMFMACFWAddr8800D80)
	}
	if RAMFMACFWAddr8800D80U02 != 0x120000 {
		t.Errorf("RAMFMACFWAddr8800D80U02 = 0x%x, want 0x120000", RAMFMACFWAddr8800D80U02)
	}
	if FWRAMAdidBaseAddr8800D80 != 0x002017E0 {
		t.Errorf("FWRAMAdidBaseAddr8800D80 = 0x%x, want 0x002017E0", FWRAMAdidBaseAddr8800D80)
	}
	if FWRAMPatchBaseAddr8800D80 != 0x0020B2B0 {
		t.Errorf("FWRAMPatchBaseAddr8800D80 = 0x%x, want 0x0020B2B0", FWRAMPatchBaseAddr8800D80)
	}
}

// TestStageString verifies human-readable labels.
func TestStageString(t *testing.T) {
	cases := []struct {
		s    Stage
		want string
	}{
		{StageZeroCD, "ZeroCD (Stage 0)"},
		{StageBootROM, "BootROM (Stage 1)"},
		{StageOperational, "Operational (Stage 2)"},
		{StageUnknown, "Unknown"},
		{Stage(99), "Unknown"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Stage(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}
