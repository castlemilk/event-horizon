package lmac

import (
	"testing"
)

func TestVersionReqEmptyPayload(t *testing.T) {
	msg := VersionReq{}
	buf, err := msg.Encode()
	if err != nil {
		t.Fatal(err)
	}
	h, rest, err := SplitMessage(buf)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != MMVersionReq || h.ParamLen != 0 || len(rest) != 0 {
		t.Fatalf("drift: hdr=%+v rest=%x", h, rest)
	}
}

func TestVersionCfmDecode(t *testing.T) {
	// Layout: u32 version_lmac + u32 version_machw_1 + u32 version_machw_2 + u32 version_phy_1
	//         + u32 version_phy_2 + u32 features + u16 max_sta_nb + u8 max_vif_nb (+ padding)
	payload := []byte{
		0xa9, 0x53, 0x13, 0x1a, // version_lmac: 0x1a1353a9 -> 26.19.83.169
		0x00, 0x01, 0x09, 0x06, // version_machw_1: 0x06090100
		0xfb, 0xfd, 0x02, 0x00, // version_machw_2: 0x0002fdfb
		0x47, 0x40, 0x01, 0x00, // version_phy_1: 0x00014047
		0x11, 0x41, 0xe2, 0x5e, // version_phy_2: 0x5ee24111
		0x00, 0x00, 0x02, 0x01, // features: 0x01020000
		0xd5, 0x77, // max_sta_nb: 0x77d5
		0xe8,       // max_vif_nb: 0xe8
		0x01,       // padding
	}
	var cfm VersionCfm
	if err := cfm.Decode(payload); err != nil {
		t.Fatal(err)
	}
	if cfm.VersionLMAC != 0x1a1353a9 {
		t.Errorf("version_lmac: got 0x%08x", cfm.VersionLMAC)
	}
	if cfm.VersionMacHW1 != 0x06090100 {
		t.Errorf("version_machw_1: got 0x%08x", cfm.VersionMacHW1)
	}
	if cfm.VersionMacHW2 != 0x0002fdfb {
		t.Errorf("version_machw_2: got 0x%08x", cfm.VersionMacHW2)
	}
	if cfm.VersionPHY1 != 0x00014047 {
		t.Errorf("version_phy_1: got 0x%08x", cfm.VersionPHY1)
	}
	if cfm.VersionPHY2 != 0x5ee24111 {
		t.Errorf("version_phy_2: got 0x%08x", cfm.VersionPHY2)
	}
	if cfm.Features != 0x01020000 {
		t.Errorf("features: got 0x%08x", cfm.Features)
	}
	if cfm.MaxStaNb != 0x77d5 {
		t.Errorf("max_sta_nb: got 0x%04x", cfm.MaxStaNb)
	}
	if cfm.MaxVifNb != 0xe8 {
		t.Errorf("max_vif_nb: got 0x%02x", cfm.MaxVifNb)
	}
	if cfm.VersionString != "26.19.83.169" {
		t.Errorf("version string: got %q, expected %q", cfm.VersionString, "26.19.83.169")
	}
}

func TestVersionCfmShort(t *testing.T) {
	var cfm VersionCfm
	if err := cfm.Decode([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected short-payload error")
	}
}
