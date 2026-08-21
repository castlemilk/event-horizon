package lmac

import (
	"encoding/binary"
	"fmt"
	"io"
)

// TLV is a (tag, value) pair. Tag is u16 LE, value length is u16 LE.
type TLV struct {
	Tag   uint16
	Value []byte
}

// PutTLV writes a single TLV: [tag:2 LE][len:2 LE][value:len].
func PutTLV(w io.Writer, tag uint16, value []byte) error {
	if err := binary.Write(w, binary.LittleEndian, tag); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(len(value))); err != nil {
		return err
	}
	if len(value) == 0 {
		return nil
	}
	_, err := w.Write(value)
	return err
}

// GetAllTLV parses a flat sequence of TLVs into *out (appending). Returns the
// unconsumed tail (must be empty for a well-formed message).
func GetAllTLV(buf []byte, out *[]TLV) ([]byte, error) {
	for len(buf) >= 4 {
		tag := binary.LittleEndian.Uint16(buf[0:2])
		ln := binary.LittleEndian.Uint16(buf[2:4])
		end := 4 + int(ln)
		if end > len(buf) {
			return nil, fmt.Errorf("tlv: truncated value (tag 0x%04x want %d have %d)", tag, ln, len(buf)-4)
		}
		*out = append(*out, TLV{Tag: tag, Value: append([]byte(nil), buf[4:end]...)})
		buf = buf[end:]
	}
	return buf, nil
}
