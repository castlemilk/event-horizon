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
	payload := []byte{
		41, 0x00, // length
		0x80, 0x00, // framectrl
		0x8a, 0x09, // center_freq = 2442 (ch 7)
		0x00,       // band = 0
		0xFF,       // sta_idx
		0x00,       // inst_nbr
		0xC4,       // rssi = -60
		0x00, 0x00, // pad
		// mgmt frame body at offset 12:
		0x00, 0x00, // duration
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // DA
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, // SA
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, // BSSID
		0x00, 0x00, // seq_ctrl
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // timestamp (8)
		0x64, 0x00, // beacon_int (2)
		0x01, 0x00, // capab (2)
		// IEs (offset 34 in mgmt):
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
