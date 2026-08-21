package lmac

import (
	"encoding/binary"
	"fmt"
)

// Header mirrors struct lmac_msg from lmac_msg.h. All fields little-endian.
// The trailing param[] array is encoded separately by the caller.
type Header struct {
	ID       uint16 // message id
	DestID   uint16 // destination task id
	SrcID    uint16 // source task id (DRVTaskID = 100 for host-initiated)
	ParamLen uint16 // byte length of the param[] payload
}

// HeaderSize is the fixed prefix length.
const HeaderSize = 8

// DRVTaskID is the source task id for host-initiated messages (DRV_TASK_ID
// in rwnx_cmds.h).
const DRVTaskID uint16 = 100

// Encode writes the 8-byte header into dst (must be >= HeaderSize bytes).
func (h Header) Encode(dst []byte) {
	binary.LittleEndian.PutUint16(dst[0:2], h.ID)
	binary.LittleEndian.PutUint16(dst[2:4], h.DestID)
	binary.LittleEndian.PutUint16(dst[4:6], h.SrcID)
	binary.LittleEndian.PutUint16(dst[6:8], h.ParamLen)
}

// Decode parses the 8-byte header from buf (must be >= HeaderSize bytes).
func (h *Header) Decode(buf []byte) error {
	if len(buf) < HeaderSize {
		return fmt.Errorf("lmac header: short buffer (%d bytes)", len(buf))
	}
	h.ID = binary.LittleEndian.Uint16(buf[0:2])
	h.DestID = binary.LittleEndian.Uint16(buf[2:4])
	h.SrcID = binary.LittleEndian.Uint16(buf[4:6])
	h.ParamLen = binary.LittleEndian.Uint16(buf[6:8])
	return nil
}

// SplitMessage decodes the header from buf and returns (header, payload).
// The payload is a sub-slice of buf (no copy).
func SplitMessage(buf []byte) (Header, []byte, error) {
	var h Header
	if err := h.Decode(buf); err != nil {
		return h, nil, err
	}
	if HeaderSize+int(h.ParamLen) > len(buf) {
		return h, nil, fmt.Errorf("lmac: short message (have %d want %d)", len(buf), HeaderSize+int(h.ParamLen))
	}
	return h, buf[HeaderSize : HeaderSize+int(h.ParamLen)], nil
}
