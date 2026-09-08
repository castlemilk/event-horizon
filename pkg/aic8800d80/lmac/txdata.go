package lmac

import (
	"encoding/binary"
	"fmt"
)

// USB data-record framing for host->firmware TX (aicwf_usb.c:aicwf_usb_aggr):
//
//	[0:2] D LE12, [2:4] D repeat, [4:6] D repeat, [6]=0x01 (data), [7]=0x00
//
// where D = len(hostdesc) + len(payload) + 4. Records are 4-byte aligned.
// This differs from WrapCommand (type 0x11) used for LMAC control messages.
const (
	txDataRecordType = 0x01
	hostdescSize     = 28
)

// Cipher suites for MM_KEY_ADD (lmac_mac.h, mapped rwnx_main.c).
const (
	CipherWEP40    uint8 = 0
	CipherTKIP     uint8 = 1
	CipherCCMP     uint8 = 2
	CipherWEP104   uint8 = 3
	CipherBIPCMAC  uint8 = 5
	CipherGCMP128  uint8 = 6
	CipherGCMP256  uint8 = 7
	CipherCCMP256  uint8 = 8
)

// TxData is one host->firmware data frame: a 28-byte hostdesc followed by
// the Ethernet payload AFTER the 14-byte header (rwnx_tx.c strips DA/SA/
// ethertype into the descriptor; the firmware rebuilds 802.11 + LLC/SNAP +
// CCMP from the installed keys). Used for EAPOL, DHCP, ARP, IP.
type TxData struct {
	DA        [6]byte
	SA        [6]byte
	Ethertype uint16 // host order (e.g. 0x888e EAPOL, 0x0800 IP, 0x0806 ARP)
	VifIdx    uint8
	StaIdx    uint8 // 0xFF = unknown station
	Payload   []byte
}

func (t *TxData) Encode() ([]byte, error) {
	if len(t.Payload) > 2304 {
		return nil, fmt.Errorf("txdata: payload too large (%d)", len(t.Payload))
	}
	d := hostdescSize + len(t.Payload) + 4
	out := make([]byte, 8+hostdescSize+len(t.Payload))
	out[0] = byte(d & 0xff)
	out[1] = byte((d >> 8) & 0x0f)
	out[2] = out[0]
	out[3] = out[1]
	out[4] = out[0]
	out[5] = out[1]
	out[6] = txDataRecordType
	out[7] = 0x00
	p := out[8 : 8+hostdescSize]
	binary.LittleEndian.PutUint16(p[0:2], uint16(len(t.Payload))) // packet_len
	// p[2:4] flags_ext = 0; p[4:8] status_desc_addr = 0 (no TX confirm)
	copy(p[8:14], t.DA[:])
	copy(p[14:20], t.SA[:])
	// ethertype travels big-endian (rwnx_tx.c copies h_proto raw).
	binary.BigEndian.PutUint16(p[20:22], t.Ethertype)
	p[22] = 0    // ac
	p[23] = 0xFF // tid: non-QoS
	p[24] = t.VifIdx
	p[25] = t.StaIdx
	// p[26:28] flags = 0
	copy(out[8+hostdescSize:], t.Payload)
	// 4-byte alignment padding.
	if m := len(out) % 4; m != 0 {
		out = append(out, make([]byte, 4-m)...)
	}
	return out, nil
}

// KeyAddReq is MM_KEY_ADD_REQ (struct mm_key_add_req, 44 bytes). Layout
// verified by compiling the reference headers: key_idx@0, sta_idx@1 (0xFF =
// default/group key, else pairwise STA index), pad@2-3, key.length@4,
// key.array@8 (32B), cipher_suite@40, inst_nbr(vif)@41, spp@42,
// pairwise@43.
type KeyAddReq struct {
	KeyIdx  uint8
	StaIdx  uint8 // 0xFF for group/default keys
	Key     []byte
	Cipher  uint8 // CipherCCMP etc.
	VifIdx  uint8
	Pairwise bool
}

const keyAddReqSize = 44

func (r *KeyAddReq) Encode() ([]byte, error) {
	if len(r.Key) == 0 || len(r.Key) > 32 {
		return nil, fmt.Errorf("key_add: bad key length %d", len(r.Key))
	}
	buf := make([]byte, HeaderSize+keyAddReqSize)
	Header{ID: MMKeyAddReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: keyAddReqSize}.Encode(buf)
	p := buf[HeaderSize:]
	p[0] = r.KeyIdx
	p[1] = r.StaIdx
	p[4] = uint8(len(r.Key))
	copy(p[8:8+len(r.Key)], r.Key)
	p[40] = r.Cipher
	p[41] = r.VifIdx
	p[42] = 0 // spp
	if r.Pairwise {
		p[43] = 1
	}
	return buf, nil
}

// SetControlPortReq is ME_SET_CONTROL_PORT_REQ (0x1404, 2 bytes: sta_idx,
// open). Opens the 802.1X controlled port once keys are installed so data
// flows.
type SetControlPortReq struct {
	StaIdx uint8
	Open   bool
}

func (r SetControlPortReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize+2)
	Header{ID: MESetControlPortReq, DestID: uint16(TaskME), SrcID: DRVTaskID, ParamLen: 2}.Encode(buf)
	p := buf[HeaderSize:]
	p[0] = r.StaIdx
	if r.Open {
		p[1] = 1
	}
	return buf, nil
}
