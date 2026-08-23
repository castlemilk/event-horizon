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

// AddIfReq mirrors struct mm_add_if_req (8 bytes).
type AddIfReq struct {
	Type uint8 // 1 = STA, 5 = Monitor
	Addr [6]byte
	P2P  bool
}

func (r *AddIfReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize+8)
	Header{ID: MMAddIfReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: 8}.Encode(buf)
	p := buf[HeaderSize:]
	t := r.Type
	if t == 0 {
		t = 1 // default STA
	}
	p[0] = t
	copy(p[1:7], r.Addr[:])
	if r.P2P {
		p[7] = 1
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

// ResetReq mirrors MM_RESET_REQ (0 bytes param).
type ResetReq struct{}

func (r ResetReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize)
	Header{ID: MMResetReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: 0}.Encode(buf)
	return buf, nil
}
