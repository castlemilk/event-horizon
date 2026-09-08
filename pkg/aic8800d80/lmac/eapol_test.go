package lmac

import (
	"bytes"
	"encoding/hex"
	"testing"
)

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
