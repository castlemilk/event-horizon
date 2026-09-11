package event

import (
	"context"
	"sync"
	"testing"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
)

func TestDispatchRoutesScanResult(t *testing.T) {
	var got lmac.ScanResultInd
	var seen bool
	var mu sync.Mutex
	d := &Dispatch{OnScanResult: func(r lmac.ScanResultInd) {
		mu.Lock()
		defer mu.Unlock()
		got = r
		seen = true
	}}
	// Build a valid SCANU_RESULT_IND payload (struct scanu_result_ind).
	// payload[] (offset 12) is the full 802.11 mgmt frame, starting with its
	// own frame_control, so BSSID (addr3) is at mgmt offset 16 and IEs at 36.
	payload := []byte{
		43, 0x00, // length
		0x80, 0x00, // framectrl (convenience copy)
		0x8a, 0x09, // center_freq = 2442 (ch 7)
		0x00,       // band = 0
		0xFF,       // sta_idx
		0x00,       // inst_nbr
		0xC4,       // rssi = -60
		0x00, 0x00, // pad
		// full ieee80211_mgmt frame at offset 12:
		0x80, 0x00, // frame_control (mgmt offset 0)
		0x00, 0x00, // duration (mgmt offset 2)
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // addr1/DA (offset 4)
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, // addr2/SA (offset 10)
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, // addr3/BSSID (offset 16)
		0x00, 0x00, // seq_ctrl (offset 22)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // timestamp (offset 24)
		0x64, 0x00, // beacon_int (offset 32)
		0x01, 0x00, // capab (offset 34)
		// IEs (mgmt offset 36):
		0x00, 0x05, 'h', 'e', 'l', 'l', 'o', // Tag 0: SSID "hello"
	}
	if err := d.Handle(context.Background(), lmac.SCANUResultInd, payload); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !seen {
		t.Fatal("OnScanResult not called")
	}
	if got.SSID != "hello" {
		t.Errorf("ssid: %q", got.SSID)
	}
}

func TestDispatchRoutesVersionCfm(t *testing.T) {
	var got lmac.VersionCfm
	var seen bool
	var mu sync.Mutex
	d := &Dispatch{OnVersion: func(c lmac.VersionCfm) {
		mu.Lock()
		defer mu.Unlock()
		got = c
		seen = true
	}}
	// VersionCfm payload: struct mm_version_cfm (28 bytes).
	payload := []byte{
		0xa9, 0x53, 0x13, 0x1a, // version_lmac: 0x1a1353a9 -> "26.19.83.169"
		0x00, 0x01, 0x09, 0x06, // version_machw_1
		0xfb, 0xfd, 0x02, 0x00, // version_machw_2
		0x47, 0x40, 0x01, 0x00, // version_phy_1
		0x11, 0x41, 0xe2, 0x5e, // version_phy_2
	}
	if err := d.Handle(context.Background(), lmac.MMVersionCfm, payload); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !seen {
		t.Fatal("OnVersion not called")
	}
	if got.VersionString != "26.19.83.169" {
		t.Errorf("version string: %q", got.VersionString)
	}
}

func TestDispatchUnknownMsgID(t *testing.T) {
	var called bool
	d := &Dispatch{OnAnyUnknown: func(_ uint16, _ []byte) { called = true }}
	if err := d.Handle(context.Background(), 0xBEEF, []byte{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("OnAnyUnknown not called")
	}
}

func TestDispatchNilCallbacksAreNoOp(t *testing.T) {
	d := &Dispatch{} // all callbacks nil
	if err := d.Handle(context.Background(), lmac.SCANUResultInd, []byte{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Handle(context.Background(), lmac.MMVersionCfm, []byte{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Handle(context.Background(), 0xBEEF, []byte{}); err != nil {
		t.Fatal(err)
	}
}
