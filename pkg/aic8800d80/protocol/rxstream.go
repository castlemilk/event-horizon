package protocol

import (
	"encoding/binary"
	"fmt"
	"os"
)

// rxDebug traces every record boundary (AIC_RX_DEBUG=1) — used to verify the
// data-frame stride, the prime suspect for the dead RX data path.
var rxDebug = os.Getenv("AIC_RX_DEBUG") != ""

// SetRxDebug enables record-boundary tracing at runtime (sudo strips the
// environment, so the CLI wires this to its --dump flag).
func SetRxDebug(on bool) { rxDebug = on }

// RX frame types (aicwf_usb.h usb_type). Low bits distinguish
// config/data; the CFG bit is 0x10.
const (
	USBTypeCfg       = 0x10
	USBTypeCfgCmdRsp = 0x11
	rxHWHRDLens      = 60 // RX_HWHRD_LEN — data-frame hardware header
	rxAlignment      = 4  // RX_ALIGNMENT
)

// E2AMsgHeaderSize is the byte length of the RX ipc_e2a_msg header that
// precedes the param[] payload on config frames:
//
//	u16 id; u16 dummy_dest_id; u16 dummy_src_id; u16 param_len; u32 pattern;
//
// It is 12 bytes — 4 more than the 8-byte TX lmac_msg header, because the
// firmware stamps an extra u32 `pattern` word before the parameters. Reading
// the payload at the 8-byte TX offset lands in `pattern` (a fixed magic),
// which desynchronises every CFM/IND decode by 4 bytes.
const E2AMsgHeaderSize = 12

// RxFrame is one extracted frame from the bulk IN stream.
type RxFrame struct {
	Type    uint8  // raw type byte (buf[2])
	Payload []byte // bytes after the 4-byte record header (the ipc_e2a_msg)
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

// Param returns the message parameters — the bytes after the 12-byte
// ipc_e2a_msg header. Returns nil if the frame is too short to contain a
// header.
func (f RxFrame) Param() []byte {
	if len(f.Payload) < E2AMsgHeaderSize {
		return nil
	}
	return f.Payload[E2AMsgHeaderSize:]
}

// ParamLen returns the firmware-declared parameter length (payload[6:8]).
func (f RxFrame) ParamLen() uint16 {
	if len(f.Payload) < 8 {
		return 0
	}
	return binary.LittleEndian.Uint16(f.Payload[6:8])
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
	if rxDebug {
		fmt.Printf("[rxstream] pktLen=%d type=0x%02x stride=%d buffered=%d\n", pktLen, typ, stride, len(s.buf))
	}
	// Degenerate/corrupt record: drop the whole buffer and resynchronise on the
	// next USB transfer, which always starts on a record boundary. (Byte-wise
	// resync was tried and is measurably WORSE — it wanders through the
	// remaining garbage and mis-frames the good records behind it: 0 scan
	// results vs 1. Do not reintroduce it.)
	// An out-of-range stride means we are mis-aligned in the stream (observed:
	// pktLen=55358 type=0x0d stride=55424 — 0x0d is not even a valid type).
	// Without the upper bound Next() returned "need more bytes" forever waiting
	// for a 55KB record that never arrives: the stream STALLED permanently and
	// the entire RX data path went dead — which is why scan results and
	// SM_CONNECT_CFM/IND (both delivered inside 0xFFFF data frames) were never
	// seen. Treat it as corrupt and resync on the next USB transfer, which
	// always starts on a record boundary.
	const maxRecord = 4096 // a record cannot exceed the 4096-byte read buffer
	if stride < 4 || stride > maxRecord {
		s.buf = nil
		return RxFrame{}, false, fmt.Errorf("rx stream: CORRUPT record (len=%d type=0x%02x stride=%d) — whole transfer dropped", pktLen, typ, stride)
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
