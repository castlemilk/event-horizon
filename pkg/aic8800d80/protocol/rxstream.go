package protocol

import (
	"encoding/binary"
	"fmt"
)

// RX frame types (aicwf_usb.h usb_type). Low bits distinguish
// config/data; the CFG bit is 0x10.
const (
	USBTypeCfg       = 0x10
	USBTypeCfgCmdRsp = 0x11
	rxHWHRDLens      = 60 // RX_HWHRD_LEN — data-frame hardware header
	rxAlignment      = 4  // RX_ALIGNMENT
)

// RxFrame is one extracted frame from the bulk IN stream.
type RxFrame struct {
	Type    uint8  // raw type byte (buf[2])
	Payload []byte // bytes after the 4-byte record header
}

// IsConfig reports whether the frame is a config/command frame.
func (f RxFrame) IsConfig() bool { return f.Type&USBTypeCfg == USBTypeCfg }

// MsgID returns the ipc_e2a_msg id for config frames (payload[0:2]).
// Returns 0 for data frames or short payloads.
func (f RxFrame) MsgID() uint16 {
	if !f.IsConfig() || len(f.Payload) < 2 {
		return 0
	}
	return binary.LittleEndian.Uint16(f.Payload[0:2])
}

// RxStream reassembles length-prefixed frames from arbitrary bulk IN
// chunk boundaries. The device aggregates multiple frames per USB
// transfer (and splits frames across transfers), so callers must feed
// every received chunk in order and drain extracted frames — mirroring
// Linux aicwf_process_rxframes.
//
// Record layout: [len:2 LE][type:1][pad:1][len bytes]. The stride to
// the next record depends on the frame type (matches Linux):
//
//	config: 4 + len
//	data:   4 + roundup(len + 60, 4)
type RxStream struct {
	buf []byte
}

// Feed appends a received chunk.
func (s *RxStream) Feed(chunk []byte) {
	s.buf = append(s.buf, chunk...)
}

// Next extracts the next complete frame, or ok=false if more bytes are
// needed. The frame's bytes are removed from the stream either way only
// when extracted; incomplete records stay buffered.
func (s *RxStream) Next() (f RxFrame, ok bool, err error) {
	if len(s.buf) < 4 {
		return RxFrame{}, false, nil
	}
	pktLen := int(binary.LittleEndian.Uint16(s.buf[0:2]))
	typ := s.buf[2]
	var stride int
	if typ&USBTypeCfg == USBTypeCfg {
		stride = 4 + pktLen
	} else {
		aggr := pktLen + rxHWHRDLens
		stride = 4 + ((aggr + rxAlignment - 1) / rxAlignment * rxAlignment)
	}
	if stride < 4 {
		// Degenerate/corrupt record: drop the whole buffer, the stream
		// cannot be resynchronized reliably.
		s.buf = nil
		return RxFrame{}, false, fmt.Errorf("rx stream: corrupt record (len=%d type=0x%02x)", pktLen, typ)
	}
	if len(s.buf) < stride {
		return RxFrame{}, false, nil // need more bytes
	}
	rec := s.buf[:stride]
	s.buf = s.buf[stride:]
	body := rec[4:]
	if typ&USBTypeCfg == USBTypeCfg {
		return RxFrame{Type: typ, Payload: body}, true, nil
	}
	// Data frames carry a hardware header before the 802.11 payload;
	// pass the raw body through — callers that care can parse further.
	return RxFrame{Type: typ, Payload: body}, true, nil
}
