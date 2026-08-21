//go:build aichw

// Hardware integration test. Requires:
//   - the AIC8800D80 dongle plugged in and running operational firmware
//     (sudo ./bin/usbwifi aicloader --kill-daemon --firmware-dir=...)
//   - root (libusb claims need privileges on macOS)
//
// Run with:
//   sudo go test -tags aichw -count=1 -v ./pkg/aic8800d80/hosttarget/
package hosttarget

import (
	"context"
	"testing"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/event"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// TestVersionRoundTrip submits MM_VERSION_REQ and expects an ACK within
// the default timeout.
func TestVersionRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := protocol.OpenOperational(ctx)
	if err != nil {
		t.Skipf("no operational device: %v", err)
	}
	defer sess.Close()

	ackCh := make(lmac.AckChannel, 64)
	loop := event.NewLoop(event.NewBulkFrameSource(sess, 200), &event.Dispatch{})
	submitter := lmac.NewSubmitter(sess, ackCh)
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()

	if err := submitter.Submit(ctx, lmac.VersionReq{}); err != nil {
		t.Fatalf("submit mm_version_req: %v", err)
	}

	cancel()
	sess.Close()
	<-done
}

// TestScanResults submits SCANU_START_REQ on 2.4GHz channels 1/6/11 and
// prints every BSS seen for 8 seconds. Fails only if no results arrive
// at all (an environment with zero APs is unlikely but possible).
func TestScanResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := protocol.OpenOperational(ctx)
	if err != nil {
		t.Skipf("no operational device: %v", err)
	}
	defer sess.Close()

	var results []lmac.ScanResultInd
	d := &event.Dispatch{
		OnScanResult: func(r lmac.ScanResultInd) { results = append(results, r) },
	}
	ackCh := make(lmac.AckChannel, 64)
	loop := event.NewLoop(&ackTee{inner: event.NewBulkFrameSource(sess, 200), acks: ackCh}, d)
	submitter := lmac.NewSubmitter(sess, ackCh)
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()

	req := &lmac.ScanStartReq{
		Band:     lmac.Band2G,
		Channels: []lmac.ChannelInfo{{Prim20Ch: 1}, {Prim20Ch: 6}, {Prim20Ch: 11}},
		BSSID:    lmac.BroadcastBSSID,
	}
	if err := submitter.Submit(ctx, req); err != nil {
		t.Fatalf("submit scan_start_req: %v", err)
	}
	time.Sleep(8 * time.Second)

	cancel()
	sess.Close()
	<-done

	t.Logf("scan found %d BSSIDs", len(results))
	for _, r := range results {
		t.Logf("  ssid=%q ch=%d rssi=%d bssid=%02x:%02x:%02x:%02x:%02x:%02x",
			r.SSID, r.Channel, r.RSSI,
			r.BSSID[0], r.BSSID[1], r.BSSID[2], r.BSSID[3], r.BSSID[4], r.BSSID[5])
	}
	if len(results) == 0 {
		t.Fatal("scan returned no results")
	}
}

// ackTee tees config frame ids into the submitter's ACK channel.
type ackTee struct {
	inner event.FrameSource
	acks  lmac.AckChannel
}

func (t *ackTee) Next(ctx context.Context) (protocol.RxFrame, error) {
	f, err := t.inner.Next(ctx)
	if err == nil && f.IsConfig() && len(f.Payload) >= 2 {
		select {
		case t.acks <- f.MsgID():
		default:
		}
	}
	return f, err
}
