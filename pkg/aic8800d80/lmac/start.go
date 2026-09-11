package lmac

import (
	"encoding/binary"
	"fmt"
)

// StartReq mirrors struct mm_start_req (72 bytes).
type StartReq struct {
	UAPSDTimeout  uint32
	LPClkAccuracy uint16
}

func (r *StartReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize+72)
	Header{ID: MMStartReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: 72}.Encode(buf)
	p := buf[HeaderSize:]
	// phy_cfg: 64 bytes zeros (Karst/Trident default)
	uapsd := r.UAPSDTimeout
	if uapsd == 0 {
		uapsd = 300
	}
	lpClk := r.LPClkAccuracy
	if lpClk == 0 {
		lpClk = 20
	}
	binary.LittleEndian.PutUint32(p[64:68], uapsd)
	binary.LittleEndian.PutUint16(p[68:70], lpClk)
	return buf, nil
}

// Interface types for mm_add_if_req.type (lmac_msg.h enum: MM_STA=0,
// MM_IBSS=1, MM_AP=2, MM_MESH_POINT=3, MM_MONITOR=4).
const (
	IfTypeSTA       uint8 = 0
	IfTypeIBSS      uint8 = 1
	IfTypeAP        uint8 = 2
	IfTypeMeshPoint uint8 = 3
	IfTypeMonitor   uint8 = 4
)

// AddIfReq mirrors struct mm_add_if_req (10 bytes). Because struct mac_addr
// is u16-aligned, there is a pad byte after `type`, so the MAC starts at
// offset 2 — not 1:
//
//	u8 type; u8 _pad; u8 addr[6]; bool p2p; u8 _pad;
type AddIfReq struct {
	Type uint8 // IfType* — zero value is IfTypeSTA
	Addr [6]byte
	P2P  bool
}

func (r *AddIfReq) Encode() ([]byte, error) {
	const paramLen = 10
	buf := make([]byte, HeaderSize+paramLen)
	Header{ID: MMAddIfReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: paramLen}.Encode(buf)
	p := buf[HeaderSize:]
	p[0] = r.Type // IfTypeSTA (0) by default
	p[1] = 0      // alignment pad
	copy(p[2:8], r.Addr[:])
	if r.P2P {
		p[8] = 1
	}
	return buf, nil
}

// AddIfCfm mirrors struct mm_add_if_cfm (2 bytes).
type AddIfCfm struct {
	Status  uint8
	InstNbr uint8
}

func (c *AddIfCfm) Decode(payload []byte) error {
	if len(payload) < 2 {
		return fmt.Errorf("mm_add_if_cfm: short payload")
	}
	c.Status = payload[0]
	c.InstNbr = payload[1]
	return nil
}

// CoexReq is MM_SET_COEX_REQ (struct mm_set_coex_req, 16 bytes) — BT/WiFi
// coexistence config. The reference driver sends it once, right after
// MM_START_REQ, on this combo WiFi+BT chip:
//
//	u8 bt_on; u8 disable_coexnull; u8 enable_nullcts;
//	u8 enable_periodic_timer; u8 coex_timeslot_set; (3 pad); u32 coex_timeslot[2];
type CoexReq struct{}

func (CoexReq) Encode() ([]byte, error) {
	const paramLen = 16
	buf := make([]byte, HeaderSize+paramLen)
	Header{ID: MMSetCoexReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: paramLen}.Encode(buf)
	p := buf[HeaderSize:]
	p[0] = 1 // bt_on
	p[1] = 0 // disable_coexnull
	p[2] = 1 // enable_nullcts
	p[3] = 0 // enable_periodic_timer
	p[4] = 0 // coex_timeslot_set
	// coex_timeslot[2] @8..15 left zero.
	return buf, nil
}

// ResetReq mirrors MM_RESET_REQ (0 bytes param).
type ResetReq struct{}

func (r ResetReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize)
	Header{ID: MMResetReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: 0}.Encode(buf)
	return buf, nil
}
