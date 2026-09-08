package lmac

import (
	"encoding/binary"
	"fmt"
)

// USB data-record framing for host->firmware TX
// (aicwf_usb.c:aicwf_usb_bus_txdata):
//
//	[0:2] total LE12, [2]=0x01 (data), [3]=0x00 (reserved)
//	[4:32] hostdesc (txdesc_api), [32:] payload, padded to 4 bytes
//
// where total is the WHOLE record length, including this 4-byte header and the
// padding. This differs from WrapCommand (type 0x11) used for LMAC commands.
//
// NEGATIVE RESULT — do not "restore" the 8-byte header. aicwf_usb.c has TWO TX
// framings. aicwf_usb_aggr() (usb_header[8], length repeated three times,
// len = hostdesc+payload+4) is compiled ONLY under CONFIG_USB_TX_AGGR, which
// the reference Makefile:89 sets to `n`; the path actually built is
// aicwf_usb_bus_txdata() (aicwf_usb.c:1733, usb_header[4]). We emitted the
// aggregated form for a full session of debugging: the firmware reads byte 2
// as the record type, saw 0x99 (a length byte) instead of 0x01, and silently
// discarded every data frame. The USB write still returned success, so it
// looked exactly like dead TX hardware — EAPOL msg2 was "sent", the AP never
// saw it, retransmitted msg1, and finally sent SM_DISCONNECT_IND reason 15
// (4-way handshake timeout).
const (
	txDataRecordType = 0x01
	usbHeaderSize    = 4  // aicwf_usb.c:1733 (u8 usb_header[4])
	hostdescSize     = 28 // sizeof(struct txdesc_api)
	txAlignment      = 4  // TX_ALIGNMENT, aicwf_txrxif.h:27
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
	// ConfirmIdx requests a TX confirm: status_desc_addr = bit31|idx and
	// the firmware returns idx in a 0x12 DATA_CFM record. -1 = no confirm.
	ConfirmIdx int
}

func (t *TxData) Encode() ([]byte, error) {
	if len(t.Payload) > 2304 {
		return nil, fmt.Errorf("txdata: payload too large (%d)", len(t.Payload))
	}
	// The length field is the padded total, header included — the reference
	// pads first, then writes buf_len (aicwf_usb.c:1774-1783).
	total := usbHeaderSize + hostdescSize + len(t.Payload)
	if m := total % txAlignment; m != 0 {
		total += txAlignment - m
	}
	out := make([]byte, total)
	out[0] = byte(total & 0xff)
	out[1] = byte((total >> 8) & 0x0f)
	out[2] = txDataRecordType
	out[3] = 0x00
	p := out[usbHeaderSize : usbHeaderSize+hostdescSize]
	binary.LittleEndian.PutUint16(p[0:2], uint16(len(t.Payload))) // packet_len
	// p[2:4] flags_ext = 0.
	if t.ConfirmIdx >= 0 {
		binary.LittleEndian.PutUint32(p[4:8], 0x80000000|uint32(t.ConfirmIdx))
	}
	// else status_desc_addr = 0 (no TX confirm)
	copy(p[8:14], t.DA[:])
	copy(p[14:20], t.SA[:])
	// ethertype travels big-endian (rwnx_tx.c copies h_proto raw).
	binary.BigEndian.PutUint16(p[20:22], t.Ethertype)
	p[22] = 3 // ac = hardware queue: VO (EAPOL is voice-priority)
	p[23] = 7 // tid 7 (voice): tid 0xFF marks non-QoS, which the
	// firmware may refuse to transmit on a QoS association
	p[24] = t.VifIdx
	p[25] = t.StaIdx
	// p[26:28] flags = 0
	copy(out[usbHeaderSize+hostdescSize:], t.Payload)
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
