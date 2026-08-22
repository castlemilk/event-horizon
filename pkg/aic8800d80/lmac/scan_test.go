package lmac

import (
	"bytes"
	"encoding/binary"
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
	// Hand-built SCANU_RESULT_IND payload (struct scan_result):
	//   u32 channel; u8 band; u8 width; u16 padding;
	//   u8 rssi; u8 rssi_min; u8 rssi_max; u8 padding;
	//   u8 bssid[6]; u8 padding[2];
	//   u16 ie_len; u8 ie[ie_len];
	//   struct mac_ssid ssid (u8 len + 31 bytes)
	payload := []byte{
		0x07, 0x00, 0x00, 0x00, // channel (u32 LE)
		0x00, 0x00, 0x00, 0x00, // band, width, padding(u16)
		0xC4, 0xC0, 0xD0, 0x00, // rssi=-60, rssi_min=-64, rssi_max=-48, pad
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x00, // bssid + padding
		0x04, 0x00, // ie_len = 4
		0x01, 0x02, 0x03, 0x04, // ie[4]
		0x05, 'h', 'e', 'l', 'l', 'o', 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
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
	if res.RSSIMin != -64 {
		t.Errorf("rssi_min: %d", res.RSSIMin)
	}
	if res.RSSIMax != -48 {
		t.Errorf("rssi_max: %d", res.RSSIMax)
	}
	if res.BSSID != ([6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}) {
		t.Errorf("bssid: %v", res.BSSID)
	}
	if res.SSID != "hello" {
		t.Errorf("ssid: %q", res.SSID)
	}
	if !bytes.Equal(res.IE, []byte{0x01, 0x02, 0x03, 0x04}) {
		t.Errorf("ie: %x", res.IE)
	}
}

func TestScanResultIndShort(t *testing.T) {
	var res ScanResultInd
	if err := res.Decode([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected short-payload error")
	}
}

func TestScanResultIndSSIDTooLong(t *testing.T) {
	// SSID slot: len=255 (corrupt), bytes follow. Decoder must clamp to 31.
	payload := make([]byte, 24+0+32)
	binary.LittleEndian.PutUint16(payload[20:22], 0) // ie_len = 0
	payload[22] = 255                                 // ssid len (corrupt)
	var res ScanResultInd
	if err := res.Decode(payload); err != nil {
		t.Fatal(err)
	}
	if res.SSID != "" {
		t.Errorf("expected empty SSID on corrupt len, got %q", res.SSID)
	}
}
