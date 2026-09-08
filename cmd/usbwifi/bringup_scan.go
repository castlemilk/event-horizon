package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/event"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
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
	connectBSSID := fs.String("connect-bssid", "", "target a specific BSSID (aa:bb:cc:dd:ee:ff) — required style for HIDDEN APs, which do not answer a broadcast-BSSID probe")
	dump := fs.Bool("dump", false, "hex-dump every received frame (raw diagnostics)")
	prescan := fs.Bool("prescan", false, "issue a scan before --connect to populate the BSS list")
	skipNet := fs.Bool("skip-net", false, "stop after association+EAPOL (skip DHCP/ping validation)")
	stack := fs.Bool("stack", false, "send MM_SET_STACK_START first — MANDATORY per the vendor driver but it "+
		"disrupts the bulk pipes irrecoverably under macOS libusb (a kernel driver recovers them; we can't), "+
		"so it is off by default; the radio still receives intermittently without it")
	stackOnly := fs.Bool("stack-only", false, "send only MM_SET_STACK_START then exit")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	var (
		mu      sync.Mutex
		results []lmac.ScanResultInd
		seen    = map[[6]byte]bool{}
	)
	// Fallback station MAC if the device does not return one. Declared
	// before the dispatch so the data-frame hook can anchor Ethernet.
	mac := [6]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}
	vifCh := make(chan lmac.AddIfCfm, 4)
	startCfmCh := make(chan lmac.ScanStartCfm, 4)
	doneCh := make(chan struct{}, 1)
	macCh := make(chan [6]byte, 4)
	connCfmCh := make(chan uint8, 4)
	connIndCh := make(chan lmac.ConnectInd, 4)
	memReadCh := make(chan []byte, 4)
	eapolCh := make(chan []byte, 16)
	netCh := make(chan lmac.Ethernet, 32)

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
		OnDataFrame: func(p []byte) {
			// EAPOL extractor: find ethertype 88 8e, then hand the clean
			// 802.1X frame (ver/type/len + descriptor) to the supplicant.
			// The firmware delivers RX data with an hw header, so scan
			// rather than assuming an offset.
			for i := 0; i+8 <= len(p); i++ {
				if p[i] != 0x88 || p[i+1] != 0x8e {
					continue
				}
				e := p[i+2:]
				if len(e) < 4 || (e[0] != 1 && e[0] != 2) ||
					(e[1] != 0 && e[1] != 1 && e[1] != 3) {
					continue
				}
				bodyLen := int(e[2])<<8 | int(e[3])
				if len(e) < 4+bodyLen {
					continue
				}
				select {
				case eapolCh <- append([]byte(nil), e[:4+bodyLen]...):
				default:
				}
				return
			}
			// Non-EAPOL data (DHCP/ARP/IP post-handshake): anchor an
			// Ethernet frame and forward it to the network validator.
			if eth, _, err := lmac.ExtractEthernet(p, mac); err == nil &&
				eth.Ethertype != lmac.EAPOLEthertype {
				select {
				case netCh <- eth:
				default:
				}
			}
		},
		OnRaw: func(msgID uint16, p []byte) {
			// Always surface SM-task (0x18xx) traffic: the association result
			// is the one thing we cannot otherwise observe, and it has only
			// ever been seen a session late. Printing it unconditionally shows
			// whether anything arrives during the connect wait.
			if msgID>>10 == uint16(lmac.TaskSM) {
				n := len(p)
				if n > 12 {
					n = 12
				}
				fmt.Printf("  [SM 0x%04x len=%d] % x\n", msgID, len(p), p[:n])
			}
			if msgID == lmac.DBGMemReadCfm {
				select {
				case memReadCh <- append([]byte(nil), p...):
				default:
				}
			}
			if !*dump {
				return
			}
			// Skip the empty 60-byte RX-buffer data frames (pure noise).
			if msgID == 0xFFFF {
				allZero := true
				for _, b := range p {
					if b != 0 {
						allZero = false
						break
					}
				}
				if allZero {
					return
				}
			}
			tag := "cfg"
			if msgID == 0xFFFF {
				tag = "data"
			}
			fmt.Printf("  [raw %s 0x%04x len=%d] % x\n", tag, msgID, len(p), p)
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

	if *dump {
		protocol.SetRxDebug(true)
	}

	// Nothing else may hold the device.
	stopDeviceHolders()

	s, err := openSessionWith(ctx, d)
	if err != nil {
		log.Printf("open operational session: %v (is the dongle bootstrapped? run `usbwifi bootstrap`)", err)
		return 1
	}
	defer s.close()

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
	// Windows order (from disassembling the vendor's aicusbwifi.sys for this
	// chip, 368b:8d85): RF / power / calibration config runs BEFORE stack_start,
	// so the MAC/PHY starts already configured. Our old order sent stack_start
	// FIRST, which starts an unconfigured stack that faults and wedges the
	// command pipe (every later command then times out). Config first, stack
	// after.
	fmt.Println("sending RF sequence (before stack_start, matching the vendor driver)...")
	// RF frontend register writes + rf_config (msg 0x69), lifted from the
	// vendor Windows driver for this chip. Without them the radio CFMs every
	// command but stays deaf — it never hears beacons or reaches the AP, so
	// association never completes. These must precede stack_start.
	// LMAC-framed, fire-and-forget: the operational firmware needs the
	// WrapCommand prefix (protocol.MemWrite's boot-ROM framing is malformed
	// here) and doesn't reliably CFM these, so don't block on an ACK.
	for _, w := range []struct{ addr, val uint32 }{
		{0x40344058, 0x00800000},
		{0x40200028, 0x0021047e},
		{0x40200024, 0x0000011d},
	} {
		if f, err := (lmac.DbgMemWriteReq{Addr: w.addr, Val: w.val}).Encode(); err == nil {
			if err := s.sess.BulkOut(ctx, lmac.WrapCommand(f)); err != nil {
				fmt.Printf("  rf reg 0x%08x=0x%08x: %v\n", w.addr, w.val, err)
			}
		}
	}
	submitTimed("rf_config 0x0069", lmac.RFConfigReq{}, 4*time.Second)

	// patch_config (from the vendor's aicusbwifi.sys, fn 0x140044ab0): read a
	// base pointer out of firmware RAM at 0x110180, then poke the tx-adaptivity
	// / MAC-config registers relative to it. This is part of what brings the RX
	// frontend to life. Uses the LMAC-framed mem read/write (operational fw).
	readMem := func(addr uint32) (uint32, bool) {
		for len(memReadCh) > 0 {
			<-memReadCh // drain stale
		}
		if f, err := (lmac.DbgMemReadReq{Addr: addr}).Encode(); err == nil {
			_ = s.sess.BulkOut(ctx, lmac.WrapCommand(f))
		}
		select {
		case p := <-memReadCh:
			if len(p) >= 8 {
				return binary.LittleEndian.Uint32(p[4:8]), true
			}
		case <-time.After(2 * time.Second):
		}
		return 0, false
	}
	// Guard: 0x110180 reads junk (e.g. 0x45592b00/0x55592b00) until the
	// firmware populates it later in init than we run. Writing the table
	// to a junk base sprays registers into unmapped RAM and wedges tasks
	// nondeterministically — only apply inside plausible firmware RAM.
	if base, ok := readMem(0x00110180); ok && base >= 0x00100000 && base < 0x00220000 {
		fmt.Printf("  patch_config base = 0x%08x\n", base)
		for _, w := range []struct {
			off, val uint32
		}{{0x04, 0x0000320a}, {0x94, 0x00000000}, {0xf8, 0x00010138}} {
			if f, err := (lmac.DbgMemWriteReq{Addr: base + w.off, Val: w.val}).Encode(); err == nil {
				_ = s.sess.BulkOut(ctx, lmac.WrapCommand(f))
			}
		}
	} else {
		fmt.Printf("  patch_config: read of 0x110180 failed (base=0x%08x ok=%v)\n", base, ok)
	}

	submitTimed("txpwr_lvl 0x0077", lmac.TxpwrLvlReq{}, 4*time.Second)
	submitTimed("rf_calib 0x006B", lmac.RFCalibReq{}, 6*time.Second)
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
	_ = rf

	// stack_start AFTER config: the firmware gates RX/scan/connect on
	// is_stack_start=1 and starts the MAC/PHY. With config already applied it
	// comes up cleanly; a short settle lets it stabilise before MAC init.
	if *stack || *stackOnly {
		fmt.Println("sending stack_start (starts the MAC stack)...")
		submitTimed("stack_start 0x007B", lmac.StackStartReq{}, 6*time.Second)
		// ALWAYS stop here. Every command sent after stack_start in the SAME
		// session gets no CFM (measured: reset/me_config/chan/start/coex/add_if
		// all time out), yet they ARE delivered — so continuing would apply a
		// reset that the next session's MAC init then repeats, and a SECOND
		// MM_RESET permanently kills RX until the firmware is re-flashed.
		// Correct flow is two runs: `bringup --stack` (RF config + stack_start),
		// then `bringup ...` (MAC init + scan) in a fresh session = one reset.
		fmt.Println("stack_start sent — now run bringup again (without --stack) to init the MAC and scan.")
		return 0
	}

	fmt.Println("bringing up station interface (reset -> me_config -> chan_config -> start -> coex -> add_if)...")
	// ACK-tolerant: right after stack_start the firmware often skips the CFM
	// for the first command (mm_reset), which used to abort the run. That
	// forced a 2-run workaround (run 1 --stack, run 2 the rest) — and the
	// second run's extra MM_RESET on an already-running stack KILLS RX (only
	// the run right after a fresh flash ever received). Keep going on a missing
	// ACK so the whole vendor sequence (RF -> stack_start -> reset -> me_config
	// -> chan -> start -> coex -> add_if -> scan) runs in ONE session with a
	// single reset, exactly like the Windows driver.
	submitTimed("mm_reset_req", lmac.ResetReq{}, *stepTimeout)
	submitTimed("me_config_req", lmac.ConfigReq{HTSupported: true}, *stepTimeout)
	submitTimed("me_chan_config_req", lmac.ChanConfigReq{}, *stepTimeout)
	submitTimed("mm_start_req", &lmac.StartReq{}, *stepTimeout)
	submitTimed("mm_set_coex_req", lmac.CoexReq{}, *stepTimeout)
	submitTimed("mm_add_if_req", &lmac.AddIfReq{Type: lmac.IfTypeSTA, Addr: mac}, *stepTimeout)

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
		if *connectBSSID != "" {
			var b [6]byte
			if n, _ := fmt.Sscanf(*connectBSSID, "%02x:%02x:%02x:%02x:%02x:%02x",
				&b[0], &b[1], &b[2], &b[3], &b[4], &b[5]); n == 6 {
				creq.BSSID = b
				fmt.Printf("  targeting BSSID %s directly (hidden-AP path)\n", *connectBSSID)
			} else {
				log.Printf("bad --connect-bssid %q", *connectBSSID)
				return 1
			}
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
			// BROAD scan (no SSID filter): this firmware treats the ssid array
			// as a match FILTER and returns zero results for a directed scan,
			// which left the firmware's BSS list EMPTY — SM_CONNECT then fails
			// with status_code=1 and an all-zero BSSID (it cannot find the BSS).
			sreq := &lmac.ScanStartReq{Band: lmac.Band2G, BSSID: lmac.BroadcastBSSID, VifIdx: vif,
				Duration: 120}
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
		fmt.Println("  SM_CONNECT_REQ sent; waiting up to 90s for SM_CONNECT_IND ...")
		deadline := time.After(90 * time.Second)
		// Passive wait, matching the vendor driver (which sends SM_CONNECT,
		// waits for the CFM, and takes the IND async without polling). A
		// 4s GetMacAddr flush-poll was tried and never surfaced the IND, so
		// it is removed — the poll traffic may disturb the association.
		for {
			select {
			case st := <-connCfmCh:
				fmt.Printf("  SM_CONNECT_CFM status=%d (accepted; awaiting association)\n", st)
			case ind := <-connIndCh:
				if ind.StatusCode == 0 {
					fmt.Printf("CONNECTED to %q: bssid=%02x:%02x:%02x:%02x:%02x:%02x aid=%d band=%d freq=%d\n",
						*connectSSID, ind.BSSID[0], ind.BSSID[1], ind.BSSID[2], ind.BSSID[3], ind.BSSID[4], ind.BSSID[5],
						ind.AID, ind.Band, ind.CenterFreq)
					// Association done. With CONTROL_PORT_HOST the AP now
					// starts the EAPOL 4-way handshake (msg1 on the data
					// path). Run the host supplicant: derive keys, answer
					// msg1/msg3, install PTK/GTK, open the port — then
					// DHCP + ping to prove the data path carries IP.
					if wpa2 {
						if rc := runEapolHandshake(ctx, s, vif, ind.APIdx, ind.BSSID, mac, *connectSSID, *connectPass, eapolCh); rc != 0 {
							return rc
						}
						if *skipNet {
							return 0
						}
						return runDhcpPing(ctx, s, vif, mac, ind.BSSID, netCh)
					}
					return 0
				}
				fmt.Printf("association FAILED: status_code=%d\n", ind.StatusCode)
				return 1
			case <-deadline:
				fmt.Println("no SM_CONNECT_IND within 90s — association did not complete")
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

	// duration is the per-channel dwell in TU (1024us). Left at 0 the firmware
	// uses a minimal dwell and misses most beacons (beacon interval is ~100 TU).
	// NOTE: adding a zero-length "wildcard" SSID (ssid_cnt=1) to force an
	// active scan makes this firmware return ZERO results — it treats the ssid
	// array as a match FILTER, and an empty entry matches nothing. Leave
	// ssid_cnt=0 (passive/broadcast).
	req := &lmac.ScanStartReq{Band: b, Channels: chans, BSSID: lmac.BroadcastBSSID, VifIdx: vif, Duration: 120}
	fmt.Printf("scanning vif=%d band=%s channels=%s (dwell %v) ...\n", vif, *band, *channels, *duration)
	// Fire-and-forget: this firmware often does not ACK scan_start (same as
	// SM_CONNECT), so blocking on the submitter's ACK bails before the scan
	// even runs. Send raw; results arrive asynchronously via OnScanResult.
	// NOTE: re-issuing scan_start during an in-flight scan RESTARTS it, so the
	// scan never completes and reports nothing (measured: 0 BSS). Send it once.
	frame, ferr := req.Encode()
	if ferr != nil {
		log.Printf("encode scan_start: %v", ferr)
		return 1
	}
	if err := s.sess.BulkOut(ctx, lmac.WrapCommand(frame)); err != nil {
		log.Printf("scan_start send: %v", err)
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
