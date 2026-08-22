// cmdctl subcommand. Invoked as:
//
//	sudo ./bin/usbwifi cmdctl send mm_version_req
//	sudo ./bin/usbwifi cmdctl send scan_start_req --channels 1,6,11
//	sudo ./bin/usbwifi cmdctl listen --duration 10s
//
// Opens the dongle at Operational stage and speaks LMAC host-target
// commands over the bulk endpoints — the user-space replacement for the
// DriverKit path.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/event"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// runCmdCtl is the entrypoint for the `cmdctl` subcommand.
func runCmdCtl(args []string) int {
	log.SetPrefix("[cmdctl] ")
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if len(args) == 0 {
		usageCmdCtl()
		return 1
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch args[0] {
	case "send":
		return runCmdSend(ctx, args[1:])
	case "listen":
		return runCmdListen(ctx, args[1:])
	case "probe":
		return runCmdProbe(ctx)
	case "help", "-h", "--help":
		usageCmdCtl()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "cmdctl: unknown command %q\n\n", args[0])
		usageCmdCtl()
		return 1
	}
}

// session is the wired command channel: USB session + event loop +
// submitter sharing one RX pump.
type session struct {
	sess      *protocol.Session
	loop      *event.Loop
	submitter *lmac.Submitter
	dispatch  *event.Dispatch
	ackCh     chan uint16
	loopDone  chan error
	cancel    context.CancelFunc
}

// openSession opens the operational device and starts the event loop.
func openSession(ctx context.Context) (*session, error) {
	dev, err := protocol.OpenOperational(ctx)
	if err != nil {
		return nil, err
	}
	s := &session{
		sess:  dev,
		ackCh: make(chan uint16, 64),
	}
	s.dispatch = &event.Dispatch{
		OnScanResult: func(r lmac.ScanResultInd) {
			lock := r.SSID
			if lock == "" {
				lock = "<hidden>"
			}
			fmt.Printf("  %-32s ch=%-3d band=%s rssi=%d bssid=%02x:%02x:%02x:%02x:%02x:%02x\n",
				lock, r.Channel, bandName(r.Band), r.RSSI,
				r.BSSID[0], r.BSSID[1], r.BSSID[2], r.BSSID[3], r.BSSID[4], r.BSSID[5])
		},
		OnVersion: func(c lmac.VersionCfm) {
			fmt.Printf("  firmware version : %s (lmac=0x%08x)\n", c.VersionString, c.VersionLMAC)
			fmt.Printf("  machw: 0x%08x / 0x%08x | phy: 0x%08x / 0x%08x\n",
				c.VersionMacHW1, c.VersionMacHW2, c.VersionPHY1, c.VersionPHY2)
			fmt.Printf("  features=0x%08x max_sta=%d max_vif=%d\n",
				c.Features, c.MaxStaNb, c.MaxVifNb)
		},
		OnAnyUnknown: func(msgID uint16, _ []byte) {
			log.Printf("unhandled msg id 0x%04x", msgID)
		},
	}

	src := event.NewBulkFrameSource(dev, 200)
	tee := &ackTeeSource{inner: src, acks: s.ackCh}
	s.loop = event.NewLoop(tee, s.dispatch)
	s.submitter = lmac.NewSubmitter(dev, lmac.AckChannel(s.ackCh))

	// The loop gets its own cancellable context so close() can stop it
	// deterministically — without this the process hung forever holding
	// exclusive USB ownership, starving later runs of all RX.
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.loopDone = make(chan error, 1)
	go func() { s.loopDone <- s.loop.Run(loopCtx) }()

	// Give the loop a moment to start pumping.
	time.Sleep(50 * time.Millisecond)
	return s, nil
}

// close stops the loop and releases the USB session.
func (s *session) close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.loopDone != nil {
		select {
		case <-s.loopDone:
		case <-time.After(3 * time.Second):
			log.Printf("event loop did not stop within 3s; releasing device anyway")
		}
	}
	s.sess.Close()
}

// ackTeeSource tees every config frame's msg id into the submitter's ACK
// channel while the event loop consumes frames.
type ackTeeSource struct {
	inner event.FrameSource
	acks  chan<- uint16
}

func (t *ackTeeSource) Next(ctx context.Context) (protocol.RxFrame, error) {
	f, err := t.inner.Next(ctx)
	if err == nil && f.IsConfig() && len(f.Payload) >= 2 {
		select {
		case t.acks <- f.MsgID():
		default: // drop if the submitter isn't waiting; buffered channel absorbs bursts
		}
	}
	return f, err
}

func bandName(b uint8) string {
	if b == lmac.Band5G {
		return "5G"
	}
	return "2G"
}

// runCmdSend handles `cmdctl send <msg> [flags]`.
func runCmdSend(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "send: missing message name (mm_version_req | scan_start_req)")
		return 1
	}
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	ssid := fs.String("ssid", "", "SSID filter for scan_start_req (empty = wildcard)")
	channels := fs.String("channels", "1,6,11", "Comma-separated 2.4GHz channels for scan_start_req")
	band := fs.String("band", "2g", "Scan band: 2g or 5g")
	duration := fs.Duration("scan-duration", 8*time.Second, "How long to collect scan results")
	timeout := fs.Duration("timeout", 4*time.Second, "Per-command ACK timeout")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	s, err := openSession(ctx)
	if err != nil {
		log.Printf("open session: %v", err)
		return 1
	}
	defer s.close()

	switch args[0] {
	case "mm_version_req":
		subCtx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		if err := s.submitter.Submit(subCtx, lmac.VersionReq{}); err != nil {
			log.Printf("submit mm_version_req: %v", err)
			return 1
		}
		// The CFM is printed by dispatch.OnVersion; give it a beat to arrive.
		time.Sleep(200 * time.Millisecond)
		return 0

	case "scan_start_req":
		var chans []lmac.ChannelInfo
		for _, c := range strings.Split(*channels, ",") {
			v, err := strconv.Atoi(strings.TrimSpace(c))
			if err != nil || v <= 0 || v > 196 {
				fmt.Fprintf(os.Stderr, "bad channel %q\n", c)
				return 1
			}
			b := lmac.Band2G
			if *band == "5g" {
				b = lmac.Band5G
			}
			chans = append(chans, lmac.ChannelInfo{Prim20Ch: uint8(v), Center1: uint8(v), Width: lmac.ChanWidth20})
			_ = b // band set below on the request itself
		}
		b := lmac.Band2G
		if *band == "5g" {
			b = lmac.Band5G
		}
		req := &lmac.ScanStartReq{
			Band:     b,
			Channels: chans,
			BSSID:    lmac.BroadcastBSSID,
		}
		if *ssid != "" {
			req.SSIDs = []string{*ssid}
		}
		subCtx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		fmt.Printf("scanning band=%s channels=%s ...\n", *band, *channels)
		if err := s.submitter.Submit(subCtx, req); err != nil {
			log.Printf("submit scan_start_req: %v", err)
			return 1
		}
		fmt.Println("BSSIDs found:")
		select {
		case <-ctx.Done():
		case <-time.After(*duration):
		}
		return 0

	default:
		fmt.Fprintf(os.Stderr, "send: unknown message %q\n", args[0])
		return 1
	}
}

// runCmdListen handles `cmdctl listen` — passive RX tap printing every
// config frame seen on bulk IN.
func runCmdListen(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("listen", flag.ExitOnError)
	duration := fs.Duration("duration", 10*time.Second, "How long to listen")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	dev, err := protocol.OpenOperational(ctx)
	if err != nil {
		log.Printf("open session: %v", err)
		return 1
	}
	defer dev.Close()

	counts := map[uint16]int{}
	d := &event.Dispatch{
		OnScanResult: func(r lmac.ScanResultInd) {
			fmt.Printf("  SCANU_RESULT_IND ssid=%q ch=%d rssi=%d\n", r.SSID, r.Channel, r.RSSI)
		},
		OnVersion: func(c lmac.VersionCfm) {
			fmt.Printf("  MM_VERSION_CFM %q\n", c.VersionString)
		},
		OnAnyUnknown: func(msgID uint16, p []byte) {
			n := len(p)
			if n > 16 {
				n = 16
			}
			fmt.Printf("  msg 0x%04x len=%d % x\n", msgID, len(p), p[:n])
		},
	}
	loop := event.NewLoop(event.NewBulkFrameSource(dev, 200), d)
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()

	fmt.Printf("listening for %s (Ctrl-C to stop)...\n", *duration)
	select {
	case <-ctx.Done():
	case <-time.After(*duration):
	}
	dev.Close() // unblock the bulk read
	<-done

	ids := make([]uint16, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		fmt.Printf("total msg 0x%04x: %d\n", id, counts[id])
	}
	return 0
}

// runCmdProbe is a diagnostic: dumps discovered endpoints, sniffs raw
// bulk IN traffic, and fires MM_VERSION_REQ on each candidate OUT pipe,
// printing everything the device sends back.
func runCmdProbe(ctx context.Context) int {
	s, err := openSession(ctx)
	if err != nil {
		log.Printf("open session: %v", err)
		return 1
	}
	defer s.close()
	d := s.sess.Device()
	log.Printf("endpoints: bulk_in=0x%02x bulk_out=0x%02x msg_in=0x%02x msg_out=0x%02x",
		d.BulkInEndpoint(), d.BulkOutEndpoint(), d.MsgInEndpoint(), d.MsgOutEndpoint())
	if dump, err := d.DumpConfig(); err == nil {
		fmt.Print(dump)
	} else {
		log.Printf("dump config: %v", err)
	}

	// Raw RX tap in the background: every chunk received on bulk IN.
	rawStop := make(chan struct{})
	rxDone := make(chan struct{})
	var rxBytes int
	errCounts := map[string]int{}
	go func() {
		defer close(rxDone)
		buf := make([]byte, 512)
		for {
			select {
			case <-rawStop:
				return
			default:
			}
			n, rerr := d.BulkRecv(d.BulkInEndpoint(), buf, 100)
			if n > 0 {
				rxBytes += n
				log.Printf("RX %3d bytes: % x", n, buf[:n])
			} else if rerr != nil {
				msg := rerr.Error()
				if !strings.Contains(msg, "TIMEOUT") {
					errCounts[msg]++
				}
			}
		}
	}()

	msg := make([]byte, 8)
	lmacHdr := lmac.Header{ID: lmac.MMVersionReq, DestID: uint16(lmac.TaskMM), SrcID: lmac.DRVTaskID}
	lmacHdr.Encode(msg)
	wrapped := lmac.WrapCommand(msg)

	// Mirror Linux init order: reset precedes version.
	reset := make([]byte, 8)
	lmac.Header{ID: lmac.MMResetReq, DestID: uint16(lmac.TaskMM), SrcID: lmac.DRVTaskID}.Encode(reset)
	wrappedReset := lmac.WrapCommand(reset)

	ep := d.MsgOutEndpoint()
	log.Printf("sending MM_RESET_REQ (%d bytes) on ep 0x%02x ...", len(wrappedReset), ep)
	if _, err := d.BulkSend(ep, wrappedReset, 1000); err != nil {
		log.Printf("  reset send failed: %v", err)
	}
	time.Sleep(1 * time.Second)
	log.Printf("sending MM_VERSION_REQ (%d bytes) on ep 0x%02x ...", len(wrapped), ep)
	if _, err := d.BulkSend(ep, wrapped, 1000); err != nil {
		log.Printf("  send failed: %v", err)
	}
	time.Sleep(2 * time.Second)
	close(rawStop)
	<-rxDone // join before teardown — a mid-flight read + libusb_exit segfaults
	log.Printf("probe done: %d RX bytes total", rxBytes)
	for m, c := range errCounts {
		log.Printf("read errors (%dx): %s", c, m)
	}
	return 0
}

// protocolBulkIN returns the device's bulk IN endpoint.
func protocolBulkIN(d *protocol.USBDevice) uint8 { return d.BulkInEndpoint() }

// usageCmdCtl prints help for the cmdctl subcommand.
func usageCmdCtl() {
	fmt.Fprintf(os.Stderr, `cmdctl — user-space LMAC command channel for the AIC8800D80

Usage:
  sudo ./bin/usbwifi cmdctl <command> [options]

Commands:
  send mm_version_req                 Query firmware version (MM_VERSION_REQ)
  send scan_start_req [options]       Start a scan and print results
       --ssid <name>                  Filter to one SSID (default wildcard)
       --channels 1,6,11              Channels to probe (default 1,6,11)
       --band 2g|5g                   Band (default 2g)
       --scan-duration 8s             Result collection window
       --timeout 4s                   ACK timeout
  listen [--duration 10s]             Passive tap on bulk IN config frames
  probe                               Endpoint dump + raw RX sniff + TX retry
`)
}
