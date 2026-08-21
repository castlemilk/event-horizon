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
	// Layout: u32 version + u32 mac_version + u32 fw_build_id + u32 fw_build_date
	//         + NUL-terminated version string (padded).
	payload := []byte{
		0x01, 0x02, 0x03, 0x04, // version
		0x05, 0x06, 0x07, 0x08, // mac_version
		0x09, 0x0A, 0x0B, 0x0C, // fw_build_id
		0x0D, 0x0E, 0x0F, 0x10, // fw_build_date
		// version string "1.2.3" + NUL padding
		'1', '.', '2', '.', '3', 0x00, 0x00, 0x00,
	}
	var cfm VersionCfm
	if err := cfm.Decode(payload); err != nil {
		t.Fatal(err)
	}
	if cfm.Version != 0x04030201 {
		t.Errorf("version: got 0x%08x", cfm.Version)
	}
	if cfm.MacVersion != 0x08070605 {
		t.Errorf("mac_version: got 0x%08x", cfm.MacVersion)
	}
	if cfm.FwBuildID != 0x0C0B0A09 {
		t.Errorf("fw_build_id: got 0x%08x", cfm.FwBuildID)
	}
	if cfm.FwBuildDate != 0x100F0E0D {
		t.Errorf("fw_build_date: got 0x%08x", cfm.FwBuildDate)
	}
	if cfm.VersionString != "1.2.3" {
		t.Errorf("version string: %q", cfm.VersionString)
	}
}

func TestVersionCfmShort(t *testing.T) {
	var cfm VersionCfm
	if err := cfm.Decode([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected short-payload error")
	}
}
