package lmac

import (
	"testing"
)

func TestScanStartReqEncode(t *testing.T) {
	req := ScanStartReq{
		Band:     Band2G,
		Channels: []ChannelInfo{{Prim20Ch: 1, Center1: 1, Center2: 0, Width: ChanWidth20}},
		SSIDs:    []string{"foo", "bar"},
		BSSID:    BroadcastBSSID,
		Duration: 1000,
	}
	buf, err := req.Encode()
	if err != nil {
		t.Fatal(err)
	}
	h, payload, err := SplitMessage(buf)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != SCANUStartReq {
		t.Fatalf("id: 0x%04x", h.ID)
	}
	if h.ParamLen != uint16(len(payload)) {
		t.Fatalf("param_len drift")
	}
	// Decode back and check round-trip.
	var got ScanStartReq
	if err := got.Decode(payload); err != nil {
		t.Fatal(err)
	}
	if got.Band != Band2G {
		t.Errorf("band drift: %d", got.Band)
	}
	if len(got.Channels) != 1 || got.Channels[0].Prim20Ch != 1 {
		t.Errorf("channels drift: %+v", got.Channels)
	}
	if len(got.SSIDs) != 2 || got.SSIDs[0] != "foo" || got.SSIDs[1] != "bar" {
		t.Errorf("ssids drift: %+v", got.SSIDs)
	}
}

func TestScanStartReqTooManyChannels(t *testing.T) {
	req := &ScanStartReq{
		Band:     Band2G,
		Channels: make([]ChannelInfo, MaxChannelsInReq+1),
	}
	if _, err := req.Encode(); err == nil {
		t.Fatal("expected too-many-channels error")
	}
}

func TestScanStartReqTooManySSIDs(t *testing.T) {
	req := &ScanStartReq{
		Band:  Band2G,
		SSIDs: []string{"a", "b", "c", "d"},
	}
	if _, err := req.Encode(); err == nil {
		t.Fatal("expected too-many-ssids error")
	}
}

func TestScanResultIndDecode(t *testing.T) {
	// Hand-built SCANU_RESULT_IND payload (struct scanu_result_ind):
	//   u16 length; u16 framectrl; u16 center_freq; u8 band; u8 sta_idx; u8 inst_nbr; s8 rssi; u16 pad;
	// payload[] (offset 12) is the FULL 802.11 management frame — it starts
	// with its own frame_control, so BSSID (addr3) is at mgmt offset 16 and
	// the tagged IEs at offset 36.
	payload := []byte{
		43, 0x00, // length = 43 (full mgmt frame incl. frame_control)
		0x80, 0x00, // framectrl (beacon) — convenience copy in the ind
		0x8a, 0x09, // center_freq = 2442 (ch 7)
		0x00,       // band = 0 (2.4G)
		0xFF,       // sta_idx
		0x00,       // inst_nbr
		0xC4,       // rssi = -60
		0x00, 0x00, // pad
		// mgmt frame body at offset 12 (full ieee80211_mgmt):
		0x80, 0x00, // frame_control (mgmt offset 0)
		0x00, 0x00, // duration (mgmt offset 2)
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // addr1/DA (mgmt offset 4)
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, // addr2/SA (mgmt offset 10)
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, // addr3/BSSID (mgmt offset 16)
		0x00, 0x00, // seq_ctrl (mgmt offset 22)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // timestamp (mgmt offset 24)
		0x64, 0x00, // beacon_int (mgmt offset 32)
		0x01, 0x00, // capab (mgmt offset 34)
		// IEs (mgmt offset 36):
		0x00, 0x05, 'h', 'e', 'l', 'l', 'o', // Tag 0: SSID "hello"
	}
	var res ScanResultInd
	if err := res.Decode(payload); err != nil {
		t.Fatal(err)
	}
	if res.Channel != 7 {
		t.Errorf("channel: %d", res.Channel)
	}
	if res.RSSI != -60 {
		t.Errorf("rssi: %d", res.RSSI)
	}
	if res.BSSID != ([6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}) {
		t.Errorf("bssid: %v", res.BSSID)
	}
	if res.SSID != "hello" {
		t.Errorf("ssid: %q", res.SSID)
	}
}

func TestScanResultIndShort(t *testing.T) {
	var res ScanResultInd
	if err := res.Decode([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected short-payload error")
	}
}
