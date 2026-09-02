package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/event"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
)

// runCmdBringup runs the full station bring-up sequence in ONE session and
// then scans — the single-session flow the firmware needs but that
// `cmdctl send <one message>` cannot provide (each send opens a fresh
// session, so reset/start/add_if state never persists to the scan).
//
// Sequence (reference: rwnx fullmac init path):
//
//	MM_RESET_REQ -> MM_START_REQ -> MM_ADD_IF_REQ(STA) -> [ME config] -> SCANU_START_REQ
//
// The vif index returned by MM_ADD_IF_CFM is fed into the scan request; a
// scan against a nonexistent vif is what produced the earlier garbage
// (vif=169, status=83, zero results).
func runCmdBringup(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("bringup", flag.ExitOnError)
	channels := fs.String("channels", "1,6,11", "comma-separated channel list to scan")
	band := fs.String("band", "2g", "band: 2g or 5g")
	duration := fs.Duration("scan-duration", 6*time.Second, "scan dwell time")
	stepTimeout := fs.Duration("timeout", 4*time.Second, "per-step CFM timeout")
	rf := fs.Bool("rf", false, "send the reference RF/stack-start sequence (set_stack_start, rf_calib, "+
		"get_macaddr) before the MAC init — WEDGES the Amlogic fmacfw bundle, for other variants only")
	connectSSID := fs.String("connect", "", "after bring-up, associate to this SSID via SM_CONNECT (skips scan)")
	connectChan := fs.Int("connect-channel", 0, "channel of the --connect SSID (0 = any)")
	connectPass := fs.String("connect-pass", "", "WPA2 passphrase for --connect (empty = open network)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	_ = connectPass // WPA2 key path not yet wired; open networks only for now

	var (
		mu      sync.Mutex
		results []lmac.ScanResultInd
		seen    = map[[6]byte]bool{}
	)
	vifCh := make(chan lmac.AddIfCfm, 4)
	startCfmCh := make(chan lmac.ScanStartCfm, 4)
	doneCh := make(chan struct{}, 1)
	macCh := make(chan [6]byte, 4)
	connCfmCh := make(chan uint8, 4)
	connIndCh := make(chan lmac.ConnectInd, 4)

	d := &event.Dispatch{
		OnResetCfm: func() { fmt.Println("  MM_RESET_CFM ok") },
		OnStartCfm: func() { fmt.Println("  MM_START_CFM ok") },
		OnVersion: func(c lmac.VersionCfm) {
			fmt.Printf("  fw machw=0x%08x lmac=0x%08x\n", c.VersionMacHW1, c.VersionLMAC)
		},
		OnAddIfCfm: func(c lmac.AddIfCfm) {
			select {
			case vifCh <- c:
			default:
			}
		},
		OnMacAddr: func(c lmac.MacAddrCfm) {
			select {
			case macCh <- c.MAC:
			default:
			}
		},
		OnConnectCfm: func(status uint8) {
			select {
			case connCfmCh <- status:
			default:
			}
		},
		OnConnectInd: func(ind lmac.ConnectInd) {
			select {
			case connIndCh <- ind:
			default:
			}
		},
		OnScanStartCfm: func(c lmac.ScanStartCfm) {
			select {
			case startCfmCh <- c:
			default:
			}
		},
		OnScanResult: func(r lmac.ScanResultInd) {
			mu.Lock()
			defer mu.Unlock()
			if seen[r.BSSID] {
				return
			}
			seen[r.BSSID] = true
			results = append(results, r)
			lock := r.SSID
			if lock == "" {
				lock = "<hidden>"
			}
			fmt.Printf("  %-32s ch=%-3d rssi=%d bssid=%02x:%02x:%02x:%02x:%02x:%02x\n",
				lock, r.Channel, r.RSSI,
				r.BSSID[0], r.BSSID[1], r.BSSID[2], r.BSSID[3], r.BSSID[4], r.BSSID[5])
		},
		OnScanDone: func() {
			select {
			case doneCh <- struct{}{}:
			default:
			}
		},
		OnAnyUnknown: func(msgID uint16, p []byte) {
			n := len(p)
			if n > 16 {
				n = 16
			}
			fmt.Printf("  [rx] msg 0x%04x (task %d) len=%d % x\n", msgID, msgID>>10, len(p), p[:n])
		},
	}

	// Nothing else may hold the device.
	stopDeviceHolders()

	s, err := openSessionWith(ctx, d)
	if err != nil {
		log.Printf("open operational session: %v (is the dongle bootstrapped? run `usbwifi bootstrap`)", err)
		return 1
	}
	defer s.close()

	submit := func(name string, msg lmac.Builder) bool {
		c, cancel := context.WithTimeout(ctx, *stepTimeout)
		defer cancel()
		if err := s.submitter.Submit(c, msg); err != nil {
			log.Printf("%s: %v", name, err)
			return false
		}
		return true
	}
	// submitOpt is for messages the reference sends fire-and-forget (NULL cfm):
	// a missing CFM is not fatal, so warn and continue.
	submitOpt := func(name string, msg lmac.Builder) {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := s.submitter.Submit(c, msg); err != nil {
			log.Printf("%s (non-fatal): %v", name, err)
		}
	}

	// Fallback station MAC if the device does not return one.
	mac := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}

	// The host-driven RF/stack-start sequence from the reference driver
	// (MM_SET_STACK_START_REQ 0x7B, MM_SET_RF_CALIB_REQ 0x69,
	// MM_SET_TXPWR_IDX_LVL_REQ 0x77, MM_GET_MAC_ADDR_REQ 0x73) WEDGES the
	// Amlogic fmacfw bundle: every request after set_stack_start times out.
	// That build boots with the stack started and the radio auto-calibrated,
	// so those host messages hit unhandled paths. Gated behind --rf for other
	// firmware variants that need it.
	if *rf {
		fmt.Println("sending RF/stack-start sequence (--rf)...")
		if !submit("mm_set_stack_start_req", lmac.StackStartReq{}) {
			return 1
		}
		time.Sleep(2 * time.Second)
		submitOpt("mm_set_rf_calib_req", lmac.RFCalibReq{})
		submitOpt("mm_get_mac_addr_req", lmac.GetMacAddrReq{})
		select {
		case m := <-macCh:
			if m != ([6]byte{}) {
				mac = m
				fmt.Printf("  device MAC = %02x:%02x:%02x:%02x:%02x:%02x\n", m[0], m[1], m[2], m[3], m[4], m[5])
			}
		case <-time.After(*stepTimeout):
		}
	}

	fmt.Println("bringing up station interface (reset -> me_config -> chan_config -> start -> coex -> add_if)...")
	if !submit("mm_reset_req", lmac.ResetReq{}) {
		return 1
	}
	if !submit("me_config_req", lmac.ConfigReq{}) {
		return 1
	}
	if !submit("me_chan_config_req", lmac.ChanConfigReq{}) {
		return 1
	}
	if !submit("mm_start_req", &lmac.StartReq{}) {
		return 1
	}
	if !submit("mm_set_coex_req", lmac.CoexReq{}) {
		return 1
	}
	if !submit("mm_add_if_req", &lmac.AddIfReq{Type: lmac.IfTypeSTA, Addr: mac}) {
		return 1
	}

	var vif uint8
	select {
	case c := <-vifCh:
		if c.Status != 0 {
			log.Printf("mm_add_if_cfm reported status=%d", c.Status)
		}
		vif = c.InstNbr
		fmt.Printf("  station vif index = %d\n", vif)
	case <-time.After(*stepTimeout):
		fmt.Println("  (no MM_ADD_IF_CFM captured; using vif 0)")
	}

	// Direct-connect path: SM_CONNECT_REQ carries the SSID/channel itself and
	// the firmware runs its own connect-time scan, so this does not depend on
	// the (silent) SCANU path. Open networks only for now.
	if *connectSSID != "" {
		creq := &lmac.ConnectReq{
			SSID:     *connectSSID,
			Band:     lmac.Band2G,
			Channel:  uint8(*connectChan),
			VifIdx:   vif,
			AuthType: lmac.AuthOpen,
			Flags:    0,
		}
		if *band == "5g" {
			creq.Band = lmac.Band5G
		}
		fmt.Printf("connecting to %q (vif=%d, channel=%d, open) ...\n", *connectSSID, vif, *connectChan)
		cctx, ccancel := context.WithTimeout(ctx, *stepTimeout)
		if err := s.submitter.Submit(cctx, creq); err != nil {
			ccancel()
			log.Printf("sm_connect_req: %v", err)
			return 1
		}
		ccancel()
		select {
		case st := <-connCfmCh:
			fmt.Printf("  SM_CONNECT_CFM status=%d (0 = accepted, awaiting association)\n", st)
		case <-time.After(2 * time.Second):
			fmt.Println("  (no SM_CONNECT_CFM)")
		}
		select {
		case ind := <-connIndCh:
			if ind.StatusCode == 0 {
				fmt.Printf("CONNECTED to %q: bssid=%02x:%02x:%02x:%02x:%02x:%02x aid=%d band=%d freq=%d\n",
					*connectSSID, ind.BSSID[0], ind.BSSID[1], ind.BSSID[2], ind.BSSID[3], ind.BSSID[4], ind.BSSID[5],
					ind.AID, ind.Band, ind.CenterFreq)
				return 0
			}
			fmt.Printf("association FAILED: status_code=%d\n", ind.StatusCode)
			return 1
		case <-time.After(15 * time.Second):
			fmt.Println("no SM_CONNECT_IND within 15s — association did not complete (radio likely not calibrated on this firmware)")
			return 1
		}
	}

	// Parse channel list.
	b := lmac.Band2G
	if *band == "5g" {
		b = lmac.Band5G
	}
	var chans []lmac.ChannelInfo
	for _, c := range strings.Split(*channels, ",") {
		v, err := strconv.Atoi(strings.TrimSpace(c))
		if err != nil || v <= 0 || v > 196 {
			log.Printf("bad channel %q", c)
			return 1
		}
		chans = append(chans, lmac.ChannelInfo{Prim20Ch: uint8(v), Center1: uint8(v), Width: lmac.ChanWidth20})
	}

	req := &lmac.ScanStartReq{Band: b, Channels: chans, BSSID: lmac.BroadcastBSSID, VifIdx: vif}
	fmt.Printf("scanning vif=%d band=%s channels=%s (dwell %v) ...\n", vif, *band, *channels, *duration)
	scanCtx, cancel := context.WithTimeout(ctx, *duration+5*time.Second)
	defer cancel()
	if err := s.submitter.Submit(scanCtx, req); err != nil {
		log.Printf("scan_start_req: %v", err)
		return 1
	}
	select {
	case c := <-startCfmCh:
		fmt.Printf("  SCANU_START_CFM status=%d\n", c.Status)
	case <-time.After(*stepTimeout):
	}

	select {
	case <-doneCh:
	case <-time.After(*duration):
	case <-ctx.Done():
	}

	mu.Lock()
	n := len(results)
	mu.Unlock()
	fmt.Printf("scan complete: %d unique BSSID(s) from the dongle radio\n", n)
	if n == 0 {
		fmt.Println("no results — the radio did not report any BSS (see init sequence / payload layout)")
		return 1
	}
	return 0
}
