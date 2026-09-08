package lmac

import (
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
)

// EAPOL / 802.11i 4-way handshake for WPA2-PSK (CCMP).
//
// The firmware's controlled port is host-driven (ConnCtrlPortHost), so the
// AP's EAPOL-Key frames arrive as ordinary RX data frames and our replies go
// out as TX data frames (TxData with ethertype 0x888e). Key installation is
// via MM_KEY_ADD. There is no firmware supplicant to speak of.

// EAPOL frame constants.
const (
	EAPOLVersion   uint8  = 1
	EAPOLTypeKey   uint8  = 3
	EAPOLEthertype uint16 = 0x888e

	KeyDescTypeRSN uint8 = 2

	// Key-info bits (802.11i fig 8-19).
	KeyInfoVerMask   uint16 = 0x0007
	KeyInfoVerHMACMD5 uint16 = 0x0001 // + RC4 (TKIP-era)
	KeyInfoVerHMACSHA1 uint16 = 0x0002 // HMAC-SHA1 + AES unwrap (CCMP)
	KeyInfoPairwise  uint16 = 0x0008
	KeyInfoIndexMask uint16 = 0x0030
	KeyInfoInstall   uint16 = 0x0040
	KeyInfoACK       uint16 = 0x0080
	KeyInfoMIC       uint16 = 0x0100
	KeyInfoSecure    uint16 = 0x0200
	KeyInfoError     uint16 = 0x0400
	KeyInfoRequest   uint16 = 0x0800
	KeyInfoEncrypted uint16 = 0x1000
)

// KeyFrame is an EAPOL-Key descriptor (type 2, RSN). Wire layout:
//
//	802.1X hdr: ver@0, type@1(=3), len@2:2 BE
//	desc_type@4, key_info@5:2 BE, key_len@7:2 BE, replay@9:8 BE,
//	nonce@17:32, iv@49:16, rsc@65:8, mic@73:16, keydatalen@89:2 BE,
//	keydata@91:var
type KeyFrame struct {
	KeyInfo  uint16
	KeyLen   uint16
	Replay   uint64
	Nonce    [32]byte
	IV       [16]byte
	RSC      [8]byte
	MIC      [16]byte
	KeyData  []byte
}

const keyFrameFixed = 4 + 91 // 802.1X hdr + fixed descriptor

func (k *KeyFrame) Decode(frame []byte) error {
	if len(frame) < keyFrameFixed {
		return fmt.Errorf("eapol: short key frame (%d)", len(frame))
	}
	if frame[1] != EAPOLTypeKey || frame[4] != KeyDescTypeRSN {
		return fmt.Errorf("eapol: not an RSN key frame (type=%d desc=%d)", frame[1], frame[4])
	}
	k.KeyInfo = binary.BigEndian.Uint16(frame[5:7])
	k.KeyLen = binary.BigEndian.Uint16(frame[7:9])
	k.Replay = binary.BigEndian.Uint64(frame[9:17])
	copy(k.Nonce[:], frame[17:49])
	copy(k.IV[:], frame[49:65])
	copy(k.RSC[:], frame[65:73])
	copy(k.MIC[:], frame[73:89])
	kdl := int(binary.BigEndian.Uint16(frame[89:91]))
	if len(frame) < keyFrameFixed+kdl {
		return fmt.Errorf("eapol: short key data (%d < %d)", len(frame), keyFrameFixed+kdl)
	}
	k.KeyData = append([]byte(nil), frame[keyFrameFixed:keyFrameFixed+kdl]...)
	return nil
}

// Encode serializes the frame (MIC as held — zero it before computing).
func (k *KeyFrame) Encode() []byte {
	out := make([]byte, keyFrameFixed+len(k.KeyData))
	out[0] = EAPOLVersion
	out[1] = EAPOLTypeKey
	binary.BigEndian.PutUint16(out[2:4], uint16(91+len(k.KeyData)))
	out[4] = KeyDescTypeRSN
	binary.BigEndian.PutUint16(out[5:7], k.KeyInfo)
	binary.BigEndian.PutUint16(out[7:9], k.KeyLen)
	binary.BigEndian.PutUint64(out[9:17], k.Replay)
	copy(out[17:49], k.Nonce[:])
	copy(out[49:65], k.IV[:])
	copy(out[65:73], k.RSC[:])
	copy(out[73:89], k.MIC[:])
	binary.BigEndian.PutUint16(out[89:91], uint16(len(k.KeyData)))
	copy(out[91:], k.KeyData)
	return out
}

// PairwiseMsg reports whether this is a pairwise (vs group) key frame.
func (k *KeyFrame) Pairwise() bool { return k.KeyInfo&KeyInfoPairwise != 0 }

// Msg classifies the 4-way step from the key-info flags (from the AP's view:
// msg1 = ack+!mic, msg3 = ack+mic+secure+install+encrypted).
func (k *KeyFrame) Msg() int {
	ack := k.KeyInfo&KeyInfoACK != 0
	mic := k.KeyInfo&KeyInfoMIC != 0
	sec := k.KeyInfo&KeyInfoSecure != 0
	switch {
	case ack && !mic && !sec:
		return 1
	case !ack && mic && !sec:
		return 2
	case ack && mic && sec:
		return 3
	case !ack && mic && sec:
		return 4
	default:
		return 0
	}
}

// PMK derives the pairwise master key: PBKDF2-HMAC-SHA1(pass, ssid, 4096, 32).
func PMK(passphrase, ssid string) []byte {
	return pbkdf2SHA1([]byte(passphrase), []byte(ssid), 4096, 32)
}

func pbkdf2SHA1(pass, salt []byte, iter, keyLen int) []byte {
	hLen := sha1.Size
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	var counter [4]byte
	for b := 1; b <= blocks; b++ {
		binary.BigEndian.PutUint32(counter[:], uint32(b))
		mac := hmac.New(sha1.New, pass)
		mac.Write(salt)
		mac.Write(counter[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			mac = hmac.New(sha1.New, pass)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

// PTK derives the 48-byte pairwise transient key for CCMP
// (KCK|KEK|TK, 16 bytes each):
//
//	PRF-X(PMK, "Pairwise key expansion",
//	      Min(AA,SA) || Max(AA,SA) || Min(AN,S N) || Max(AN,SN))
func PTK(pmk, aa, sa, aNonce, sNonce []byte) []byte {
	var minA, maxA, minN, maxN []byte
	if cmpBytes(aa, sa) < 0 {
		minA, maxA = aa, sa
	} else {
		minA, maxA = sa, aa
	}
	if cmpBytes(aNonce, sNonce) < 0 {
		minN, maxN = aNonce, sNonce
	} else {
		minN, maxN = sNonce, aNonce
	}
	data := append([]byte(nil), minA...)
	data = append(data, maxA...)
	data = append(data, minN...)
	data = append(data, maxN...)
	return prfX(pmk, "Pairwise key expansion", data, 48)
}

func prfX(key []byte, label string, data []byte, outLen int) []byte {
	out := []byte{}
	for i := 0; len(out) < outLen; i++ {
		mac := hmac.New(sha1.New, key)
		mac.Write([]byte(label))
		mac.Write([]byte{0x00})
		mac.Write(data)
		mac.Write([]byte{byte(i)})
		out = append(out, mac.Sum(nil)...)
	}
	return out[:outLen]
}

func cmpBytes(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	default:
		return 0
	}
}

// ComputeMIC returns the 16-byte MIC (first 128 bits of HMAC-SHA1 over the
// encoded frame with its MIC field zeroed).
func ComputeMIC(kck, frame []byte) []byte {
	mac := hmac.New(sha1.New, kck)
	mac.Write(frame)
	return mac.Sum(nil)[:16]
}

// VerifyMIC checks the frame's MIC against KCK.
func VerifyMIC(kck []byte, k *KeyFrame) bool {
	enc := k.Encode()
	for i := 73; i < 89; i++ {
		enc[i] = 0
	}
	mac := hmac.New(sha1.New, kck)
	mac.Write(enc)
	return hmac.Equal(mac.Sum(nil)[:16], k.MIC[:])
}

// UnwrapKey unwraps an AES key-wrapped blob (RFC 3394) with KEK.
func UnwrapKey(kek, wrapped []byte) ([]byte, error) {
	if len(wrapped) < 16 || len(wrapped)%8 != 0 {
		return nil, fmt.Errorf("unwrap: bad length %d", len(wrapped))
	}
	c, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	n := len(wrapped)/8 - 1
	a := append([]byte(nil), wrapped[:8]...)
	r := make([][]byte, n+1)
	for i := 1; i <= n; i++ {
		r[i] = append([]byte(nil), wrapped[i*8:i*8+8]...)
	}
	for j := 5; j >= 0; j-- {
		for i := n; i >= 1; i-- {
			t := uint64(n*j + i)
			at := make([]byte, 8)
			binary.BigEndian.PutUint64(at, binary.BigEndian.Uint64(a)^t)
			blk := append(at, r[i]...)
			dec := make([]byte, 16)
			c.Decrypt(dec, blk)
			copy(a, dec[:8])
			copy(r[i], dec[8:])
		}
	}
	// Integrity check: A must be the IV a6a6a6a6a6a6a6a6.
	for i, b := range []byte{0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6} {
		if a[i] != b {
			return nil, fmt.Errorf("unwrap: integrity check failed")
		}
	}
	out := []byte{}
	for i := 1; i <= n; i++ {
		out = append(out, r[i]...)
	}
	return out, nil
}

// GTK extracts the group temporal key from a msg3 key-data blob. For WPA2
// the key data holds a GTK KDE (0xdd, OUI 00-0f-ac, type 1): the wrapped GTK
// starts at keydata[8:].
func GTK(KEK, keyData []byte) (gtk []byte, keyIdx uint8, err error) {
	if len(keyData) < 10 || keyData[0] != 0xdd {
		return nil, 0, fmt.Errorf("gtk: not a GTK KDE (%d bytes)", len(keyData))
	}
	keyIdx = keyData[6] & 0x03
	gtk, err = UnwrapKey(KEK, keyData[8:])
	return gtk, keyIdx, err
}
