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
	dump := fs.Bool("dump", false, "hex-dump every received frame (raw diagnostics)")
	prescan := fs.Bool("prescan", false, "issue a scan before --connect to populate the BSS list")
	if err := fs.Parse(args); err != nil {
		return 1
	}

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
		OnRaw: func(msgID uint16, p []byte) {
			if !*dump {
				return
			}
			n := len(p)
			if n > 64 {
				n = 64
			}
			tag := "cfg"
			if msgID == 0xFFFF {
				tag = "data"
			}
			fmt.Printf("  [raw %s 0x%04x len=%d] % x\n", tag, msgID, len(p), p[:n])
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

	// Fallback station MAC if the device does not return one.
	mac := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}

	// RF/stack-start sequence with the CORRECTED message ids. rf_calib (0x006B)
	// is confirmed working; testing the rest with per-message reporting and
	// generous timeouts (stack_start starts the whole MAC/PHY, so it can be
	// slow). submitTimed logs whether the CFM arrived.
	submitTimed := func(name string, msg lmac.Builder, d time.Duration) {
		c, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		if err := s.submitter.Submit(c, msg); err != nil {
			fmt.Printf("  %s: NO CFM (%v)\n", name, err)
		} else {
			fmt.Printf("  %s: CFM ok\n", name)
		}
	}
	if *rf {
		fmt.Println("sending RF/stack-start sequence (--rf, corrected ids)...")
		submitTimed("stack_start 0x007D", lmac.StackStartReq{}, 10*time.Second)
		submitTimed("txpwr_idx_lvl 0x0079", lmac.TxpwrLvlReq{}, 4*time.Second)
		submitTimed("rf_calib 0x006B", lmac.RFCalibReq{}, 4*time.Second)
		submitTimed("get_macaddr 0x0075", lmac.GetMacAddrReq{}, 4*time.Second)
		select {
		case m := <-macCh:
			if m != ([6]byte{}) {
				mac = m
				fmt.Printf("  device MAC = %02x:%02x:%02x:%02x:%02x:%02x\n", m[0], m[1], m[2], m[3], m[4], m[5])
			} else {
				fmt.Println("  device MAC = 00:00:00:00:00:00 (efuse blank; using LAA)")
			}
		case <-time.After(1 * time.Second):
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
			AuthType: lmac.AuthOpen, // WPA2 uses open 802.11 auth, then EAPOL
			Flags:    0,
		}
		if *band == "5g" {
			creq.Band = lmac.Band5G
		}
		wpa2 := *connectPass != ""
		if wpa2 {
			// WPA2-PSK: advertise the RSN IE and mark the controlled port as
			// host-driven. Association (status=0) completes on this alone; the
			// 4-way handshake + MM_KEY_ADD follow to open the data path.
			creq.Flags = lmac.ConnWPAWPA2InUse | lmac.ConnCtrlPortHost
			creq.IE = lmac.WPA2PSKCCMPRsnIE
			fmt.Println("  WPA2 mode: RSN IE + control-port-host flags set")
		}
		// Scan first to populate the firmware's BSS list (the reference flow is
		// scan -> connect; a cold connect may not find the AP). Fire-and-forget.
		// Opt-in: on this firmware a preceding scan sometimes suppresses the
		// SM_CONNECT response, so it is off by default.
		if *prescan {
			sreq := &lmac.ScanStartReq{Band: lmac.Band2G, BSSID: lmac.BroadcastBSSID, VifIdx: vif}
			if *connectChan != 0 {
				sreq.Channels = []lmac.ChannelInfo{{Prim20Ch: uint8(*connectChan), Center1: uint8(*connectChan), Width: lmac.ChanWidth20}}
			} else {
				for _, ch := range []uint8{1, 6, 11} {
					sreq.Channels = append(sreq.Channels, lmac.ChannelInfo{Prim20Ch: ch, Center1: ch, Width: lmac.ChanWidth20})
				}
			}
			if sf, err := sreq.Encode(); err == nil {
				_ = s.sess.BulkOut(ctx, lmac.WrapCommand(sf))
				fmt.Println("  pre-connect scan issued; settling 6s...")
				time.Sleep(6 * time.Second)
			}
		}

		fmt.Printf("connecting to %q (vif=%d, channel=%d, open) ...\n", *connectSSID, vif, *connectChan)
		// Fire-and-forget: this firmware does not reliably send the SM_CONNECT_CFM
		// ack (just as it skips the scan-start ack), so blocking on the submitter
		// ack would bail before the real result. Send raw and wait for the async
		// SM_CONNECT_IND, which is the authoritative association result.
		frame, err := creq.Encode()
		if err != nil {
			log.Printf("encode sm_connect_req: %v", err)
			return 1
		}
		if err := s.sess.BulkOut(ctx, lmac.WrapCommand(frame)); err != nil {
			log.Printf("send sm_connect_req: %v", err)
			return 1
		}
		fmt.Println("  SM_CONNECT_REQ sent; waiting up to 25s for SM_CONNECT_IND ...")
		deadline := time.After(25 * time.Second)
		for {
			select {
			case st := <-connCfmCh:
				fmt.Printf("  SM_CONNECT_CFM status=%d (accepted; awaiting association)\n", st)
			case ind := <-connIndCh:
				if ind.StatusCode == 0 {
					fmt.Printf("CONNECTED to %q: bssid=%02x:%02x:%02x:%02x:%02x:%02x aid=%d band=%d freq=%d\n",
						*connectSSID, ind.BSSID[0], ind.BSSID[1], ind.BSSID[2], ind.BSSID[3], ind.BSSID[4], ind.BSSID[5],
						ind.AID, ind.Band, ind.CenterFreq)
					return 0
				}
				fmt.Printf("association FAILED: status_code=%d\n", ind.StatusCode)
				return 1
			case <-deadline:
				fmt.Println("no SM_CONNECT_IND within 25s — association did not complete")
				return 1
			}
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
