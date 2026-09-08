package lmac

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// The descriptor layout is pinned against a hand-built, standard-layout frame
// rather than against our own Encode. A self-consistent WRONG layout
// round-trips perfectly, which is exactly how the missing 8-byte Reserved
// field survived: every field from the Key MIC onward sat 8 bytes early, all
// five existing tests passed, and the AP silently dropped every msg2 we sent.
func TestKeyFrameStandardLayout(t *testing.T) {
	if got := len((&KeyFrame{}).Encode()); got != 99 {
		t.Fatalf("fixed EAPOL-Key frame = %d bytes, want 99 (4 hdr + 95 descriptor)", got)
	}

	keyData := []byte{0xdd, 0x02, 0xaa, 0xbb}
	f := make([]byte, 99+len(keyData))
	f[0], f[1] = 2, EAPOLTypeKey // 802.1X version 2 (hostapd's default), type Key
	binary.BigEndian.PutUint16(f[2:4], uint16(95+len(keyData)))
	f[4] = KeyDescTypeRSN
	binary.BigEndian.PutUint16(f[5:7], KeyInfoVerHMACSHA1|KeyInfoPairwise|KeyInfoACK)
	binary.BigEndian.PutUint16(f[7:9], 16)
	binary.BigEndian.PutUint64(f[9:17], 0x0102030405060708)
	fill := func(lo, hi int, v byte) {
		for i := lo; i < hi; i++ {
			f[i] = v
		}
	}
	fill(17, 49, 0x11) // key nonce
	fill(49, 65, 0x22) // EAPOL key IV
	fill(65, 73, 0x33) // key RSC
	fill(73, 81, 0x44) // RESERVED — the field that was missing
	fill(81, 97, 0x55) // key MIC
	binary.BigEndian.PutUint16(f[97:99], uint16(len(keyData)))
	copy(f[99:], keyData)

	var k KeyFrame
	if err := k.Decode(f); err != nil {
		t.Fatalf("decode standard frame: %v", err)
	}
	if k.Version != 2 {
		t.Errorf("version = %d, want 2", k.Version)
	}
	if k.Replay != 0x0102030405060708 {
		t.Errorf("replay = %#x", k.Replay)
	}
	if k.Nonce[0] != 0x11 || k.IV[0] != 0x22 || k.RSC[0] != 0x33 || k.Reserved[0] != 0x44 {
		t.Errorf("fixed fields misaligned: nonce=%#x iv=%#x rsc=%#x reserved=%#x",
			k.Nonce[0], k.IV[0], k.RSC[0], k.Reserved[0])
	}
	if k.MIC != [16]byte(bytes.Repeat([]byte{0x55}, 16)) {
		t.Errorf("MIC read from the wrong offset: %x", k.MIC)
	}
	if !bytes.Equal(k.KeyData, keyData) {
		t.Errorf("key data = %x, want %x", k.KeyData, keyData)
	}
	if k.Msg() != 1 {
		t.Errorf("ACK without MIC should classify as msg1, got %d", k.Msg())
	}
	// Re-encoding must reproduce the received bytes exactly — the MIC is
	// computed over them, so any field we cannot round-trip breaks verification
	// on a frame that is actually valid.
	if got := k.Encode(); !bytes.Equal(got, f) {
		t.Errorf("re-encode differs from the received frame:\n got %x\nwant %x", got, f)
	}
}

// RFC 6070 PBKDF2-HMAC-SHA1 vectors (validates the PMK KDF core).
func TestPBKDF2SHA1(t *testing.T) {
	got := hex.EncodeToString(pbkdf2SHA1([]byte("password"), []byte("salt"), 1, 20))
	if want := "0c60c80f961f0e71f3a9b524af6012062fe037a6"; got != want {
		t.Errorf("c=1: got %s want %s", got, want)
	}
	got = hex.EncodeToString(pbkdf2SHA1([]byte("password"), []byte("salt"), 4096, 20))
	if want := "4b007901b765489abead49d926f721d065a429c1"; got != want {
		t.Errorf("c=4096: got %s want %s", got, want)
	}
}

// IEEE 802.11i J.1 test vector: passphrase "password", SSID "IEEE".
func TestPMKVector(t *testing.T) {
	got := hex.EncodeToString(PMK("password", "IEEE"))
	// Oracle: python3 hashlib.pbkdf2_hmac('sha1', b'password', b'IEEE', 4096, 32).
	want := "f42c6fc52df0ebef9ebb4b90b38a5f902e83fe1b135a70e23aed762e9710a12e"
	if got != want {
		t.Errorf("PMK: got %s want %s", got, want)
	}
}

// 802.11i PTK/PRF-X self-consistency: PRF output is deterministic and the
// MIC verify round-trips through encode.
func TestPTKAndMICRoundTrip(t *testing.T) {
	pmk := PMK("password", "IEEE")
	aa := []byte{0xd2, 0xe8, 0xf0, 0x50, 0xf8, 0x32}
	sa := []byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	an := bytes.Repeat([]byte{0x11}, 32)
	sn := bytes.Repeat([]byte{0x22}, 32)
	ptk := PTK(pmk, aa, sa, an, sn)
	if len(ptk) != 48 {
		t.Fatalf("PTK len %d", len(ptk))
	}
	if bytes.Equal(ptk, PTK(pmk, sa, aa, sn, an)) == false {
		t.Errorf("PTK not order-independent in AA/SA/nonces")
	}
	kck := ptk[:16]
	k := &KeyFrame{KeyInfo: KeyInfoVerHMACSHA1 | KeyInfoPairwise | KeyInfoMIC, KeyLen: 16, Replay: 1}
	copy(k.Nonce[:], sn)
	enc := k.Encode()
	mic := ComputeMIC(kck, enc)
	copy(k.MIC[:], mic)
	if !VerifyMIC(kck, k) {
		t.Errorf("MIC verify failed on self-made frame")
	}
	k.MIC[0] ^= 0xff
	if VerifyMIC(kck, k) {
		t.Errorf("MIC verify passed on tampered frame")
	}
}

// AES unwrap round-trip via a fixed KEK (RFC 3394 test vector 4.1/4.2 style:
// KEK 000102...0f wrapping 001122...).
func TestAESUnwrapRoundTrip(t *testing.T) {
	kek, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	// Oracle: RFC 3394 §4.1 vector — wrap of 00112233445566778899aabbccddeeff
	// with KEK 000102...0f (verified with independent Python implementation).
	wrapped, _ := hex.DecodeString("1fa68b0a8112b447aef34bd8fb5a7b829d3e862371d2cfe5")
	got, err := UnwrapKey(kek, wrapped)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if want := "00112233445566778899aabbccddeeff"; hex.EncodeToString(got) != want {
		t.Errorf("unwrap: got %x want %s", got, want)
	}
	if _, err := UnwrapKey(kek, append(wrapped[:len(wrapped)-1], wrapped[len(wrapped)-1]^1)); err == nil {
		t.Errorf("unwrap passed on tampered blob")
	}
}

func TestKeyFrameDecodeClassify(t *testing.T) {
	k := &KeyFrame{KeyInfo: KeyInfoVerHMACSHA1 | KeyInfoPairwise | KeyInfoACK, Replay: 7}
	copy(k.Nonce[:], bytes.Repeat([]byte{0xab}, 32))
	enc := k.Encode()
	var d KeyFrame
	if err := d.Decode(enc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !d.Pairwise() || d.Msg() != 1 || d.Replay != 7 {
		t.Errorf("classify: pairwise=%v msg=%d replay=%d", d.Pairwise(), d.Msg(), d.Replay)
	}
}
