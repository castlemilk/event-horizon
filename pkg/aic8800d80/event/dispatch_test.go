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
	// Build a valid SCANU_RESULT_IND payload (24-byte fixed header + ie + ssid slot).
	payload := make([]byte, 24+0+32)
	payload[0] = 0x07 // channel (u32 LE = 7)
	payload[8] = 0xC4 // rssi = -60
	// ie_len = 0 (already zero)
	// ssid slot at offset 22: len=5 + "hello"
	payload[22] = 5
	copy(payload[23:28], []byte("hello"))
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
	if err := d.Handle(context.Background(), 0xFFFF, []byte{}); err != nil {
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
