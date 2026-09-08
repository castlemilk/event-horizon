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

// KeyFrame is an EAPOL-Key descriptor (type 2, RSN). Wire layout
// (IEEE 802.11-2016 12.7.2 fig 12-33) — note the 8-byte Reserved field
// between the RSC and the MIC. It is easy to miss (it is "Key ID" in the
// pre-RSN descriptor and reserved ever since), and omitting it shifts the
// MIC, the key-data length and the key data 8 bytes early, which makes every
// frame we send unparseable and every MIC we check wrong:
//
//	802.1X hdr: ver@0, type@1(=3), len@2:2 BE
//	desc_type@4, key_info@5:2 BE, key_len@7:2 BE, replay@9:8 BE,
//	nonce@17:32, iv@49:16, rsc@65:8, reserved@73:8, mic@81:16,
//	keydatalen@97:2 BE, keydata@99:var
type KeyFrame struct {
	// Version is the 802.1X header version as received (1 or 2). It is part
	// of the MIC-protected bytes, so a frame we re-encode must reproduce it.
	Version  uint8
	KeyInfo  uint16
	KeyLen   uint16
	Replay   uint64
	Nonce    [32]byte
	IV       [16]byte
	RSC      [8]byte
	Reserved [8]byte
	MIC      [16]byte
	KeyData  []byte
	// Raw is the exact frame Decode was handed. The MIC covers these bytes
	// verbatim, so VerifyMIC must use them rather than a re-encoding.
	Raw []byte
}

const keyFrameFixed = 4 + 95 // 802.1X hdr + fixed descriptor

func (k *KeyFrame) Decode(frame []byte) error {
	if len(frame) < keyFrameFixed {
		return fmt.Errorf("eapol: short key frame (%d)", len(frame))
	}
	if frame[1] != EAPOLTypeKey || frame[4] != KeyDescTypeRSN {
		return fmt.Errorf("eapol: not an RSN key frame (type=%d desc=%d)", frame[1], frame[4])
	}
	k.Version = frame[0]
	k.KeyInfo = binary.BigEndian.Uint16(frame[5:7])
	// We only implement descriptor version 2 (HMAC-SHA1 MIC + AES key wrap).
	// Version 0/3 use AES-128-CMAC (SHA256 AKMs, SAE, PMF); failing here names
	// the reason instead of surfacing as an inexplicable MIC mismatch later.
	if v := k.KeyInfo & KeyInfoVerMask; v != KeyInfoVerHMACSHA1 {
		return fmt.Errorf("eapol: unsupported key descriptor version %d (only 2/HMAC-SHA1 implemented)", v)
	}
	k.KeyLen = binary.BigEndian.Uint16(frame[7:9])
	k.Replay = binary.BigEndian.Uint64(frame[9:17])
	copy(k.Nonce[:], frame[17:49])
	copy(k.IV[:], frame[49:65])
	copy(k.RSC[:], frame[65:73])
	copy(k.Reserved[:], frame[73:81])
	copy(k.MIC[:], frame[81:97])
	kdl := int(binary.BigEndian.Uint16(frame[97:99]))
	if len(frame) < keyFrameFixed+kdl {
		return fmt.Errorf("eapol: short key data (%d < %d)", len(frame), keyFrameFixed+kdl)
	}
	k.KeyData = append([]byte(nil), frame[keyFrameFixed:keyFrameFixed+kdl]...)
	k.Raw = append([]byte(nil), frame[:keyFrameFixed+kdl]...)
	return nil
}

// Encode serializes the frame (MIC as held — zero it before computing).
func (k *KeyFrame) Encode() []byte {
	out := make([]byte, keyFrameFixed+len(k.KeyData))
	out[0] = k.Version
	if out[0] == 0 {
		out[0] = EAPOLVersion
	}
	out[1] = EAPOLTypeKey
	// The 802.1X length field covers the body only, not the 4-byte header.
	binary.BigEndian.PutUint16(out[2:4], uint16(keyFrameFixed-4+len(k.KeyData)))
	out[4] = KeyDescTypeRSN
	binary.BigEndian.PutUint16(out[5:7], k.KeyInfo)
	binary.BigEndian.PutUint16(out[7:9], k.KeyLen)
	binary.BigEndian.PutUint64(out[9:17], k.Replay)
	copy(out[17:49], k.Nonce[:])
	copy(out[49:65], k.IV[:])
	copy(out[65:73], k.RSC[:])
	copy(out[73:81], k.Reserved[:])
	copy(out[81:97], k.MIC[:])
	binary.BigEndian.PutUint16(out[97:99], uint16(len(k.KeyData)))
	copy(out[99:], k.KeyData)
	return out
}

// MICOffset is where the 16-byte MIC field starts in an encoded frame. The
// MIC is computed over the whole frame with exactly these bytes zeroed.
const MICOffset = 81

// PairwiseMsg reports whether this is a pairwise (vs group) key frame.
func (k *KeyFrame) Pairwise() bool { return k.KeyInfo&KeyInfoPairwise != 0 }

// Msg classifies the 4-way step from the key-info flags (from the AP's view:
// msg1 = ack+!mic, msg3 = ack+mic+secure+install+encrypted).
func (k *KeyFrame) Msg() int {
	ack := k.KeyInfo&KeyInfoACK != 0
	mic := k.KeyInfo&KeyInfoMIC != 0
	sec := k.KeyInfo&KeyInfoSecure != 0
	switch {
	// msg1 is the only AP->STA pairwise frame with no MIC (there is no PTK
	// yet), so do not also require Secure to be clear: an AP that sets it
	// still means msg1, and misreading it as "unknown" stalls the handshake.
	case ack && !mic:
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
//
// It MICs the bytes as received (k.Raw), not a re-encoding: the MIC covers the
// frame from the 802.1X version byte onward, so any field we fail to reproduce
// exactly — the version byte, a non-zero Reserved, trailing key-data padding —
// would break the check on a frame that is actually valid.
func VerifyMIC(kck []byte, k *KeyFrame) bool {
	// Frames we originated have no Raw; for those Encode() is by definition
	// the authoritative serialization, so it is safe to fall back to it.
	src := k.Raw
	if len(src) < keyFrameFixed {
		src = k.Encode()
	}
	enc := append([]byte(nil), src...)
	for i := MICOffset; i < MICOffset+16; i++ {
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

// ParseKeyData decrypts a msg3 Key Data field and returns the GTK it carries.
//
// Order matters and is the opposite of what the layout suggests: for key
// descriptor version 2 the ENTIRE Key Data field is one AES-key-wrapped blob
// (802.11-2016 12.7.2 j), so it must be unwrapped ONCE and only then parsed as
// a KDE list. The GTK inside the GTK KDE is already plaintext — unwrapping it
// again, or walking KDEs over the ciphertext, silently yields a wrong key that
// installs cleanly and then fails to decrypt every broadcast frame.
//
// The plaintext is a KDE sequence: the AP's RSN IE (0x30), a GTK KDE
// (0xdd, OUI 00-0f-ac, data type 1: keyid/tx byte, 1 reserved byte, then the
// GTK), optionally an IGTK KDE, then 0xdd 0x00 padding.
func ParseKeyData(kek, keyData []byte, encrypted bool) (gtk []byte, keyIdx uint8, err error) {
	plain := keyData
	if encrypted {
		if plain, err = UnwrapKey(kek, keyData); err != nil {
			return nil, 0, fmt.Errorf("key data unwrap: %w", err)
		}
	}
	for off := 0; off+2 <= len(plain); {
		eid, elen := plain[off], int(plain[off+1])
		if elen == 0 || off+2+elen > len(plain) {
			break // 0xdd 0x00 pad, or a truncated element: stop cleanly.
		}
		body := plain[off+2 : off+2+elen]
		if eid == 0xdd && len(body) >= 6 &&
			body[0] == 0x00 && body[1] == 0x0f && body[2] == 0xac && body[3] == 0x01 {
			gtk = append([]byte(nil), body[6:]...)
			if len(gtk) != 16 {
				return nil, 0, fmt.Errorf("gtk: unexpected length %d (want 16 for CCMP)", len(gtk))
			}
			return gtk, body[4] & 0x03, nil
		}
		off += 2 + elen
	}
	return nil, 0, fmt.Errorf("gtk: no GTK KDE in %d bytes of key data", len(plain))
}
