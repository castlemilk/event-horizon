package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
	"github.com/castlemilk/event-horizon/pkg/tun"
)

// runBridge joins the dongle's layer-2 link to a macOS utun, so ordinary
// sockets — and therefore the existing Starlink client in pkg/starlink — can
// use the radio without any awareness of it.
//
// The utun is layer 3: the kernel hands us bare IP packets behind a 4-byte
// address-family prefix, and expects the same back. The dongle is layer 2: it
// wants an Ethernet DA/SA/ethertype in the TX hostdesc and delivers 802.11
// MPDUs we have already converted to Ethernet. So this is the translation:
//
//	utun -> strip AF prefix -> IP packet -> TxData{DA: next hop} -> bulk OUT
//	bulk IN -> ExtractEthernet -> IP payload -> AF prefix -> utun
//
// ARP is ours to answer: there is no host stack on this link, so if the
// gateway asks who owns our address, nobody replies unless we do.
//
// Host en0 is deliberately untouched. We add a host route for the terminal
// only, so normal traffic keeps its existing path.
// bridgeLinkSSID carries the SSID the current link associated with, set
// by runCmdLink before the bring-up runs. Package-level because runBridge
// is the tail of the same CLI invocation — this is link-scope state, not
// daemon state — and threading it through four call layers would buy
// nothing. Written into the linkstate file so the daemon can report the
// SSID of a link it cannot see over USB.
var bridgeLinkSSID string

func runBridge(ctx context.Context, s *session, vif, apIdx uint8, staMAC [6]byte,
	myIP, mask, gw [4]byte, gwMAC [6]byte, netCh <-chan lmac.Ethernet, routes []string, linkDown *atomic.Bool) int {

	iface, err := tun.NewUtun()
	if err != nil {
		fmt.Printf("  BRIDGE: utun create failed: %v\n", err)
		return 1
	}
	defer iface.Close()

	if err := iface.ConfigureIP(ipStr(myIP), ipStr(mask), ipStr(gw)); err != nil {
		fmt.Printf("  BRIDGE: ifconfig failed: %v\n", err)
		return 1
	}
	fmt.Printf("  BRIDGE: %s up with %v/%v via %v\n", iface.Name, ipStr(myIP), ipStr(mask), ipStr(gw))

	for _, r := range routes {
		// Ensure, not just add: a previous link that died without
		// cleaning up leaves a route at a dead utun, and a bare add
		// fails with "File exists" while the dish stays blackholed.
		// EnsureHostRoute replaces it only after proving the old
		// interface idle, and refuses to steal a live one.
		if err := iface.EnsureHostRoute(r, 3*time.Second); err != nil {
			fmt.Printf("  BRIDGE: route %s: %v\n", r, err)
			return 1
		}
		fmt.Printf("  BRIDGE: host route %s -> %s\n", r, iface.Name)
	}

	// ARP cache. The gateway is already resolved; anything off-subnet goes
	// through it, so for the Starlink terminal this is all we need.
	var mu sync.Mutex
	arp := map[[4]byte][6]byte{gw: gwMAC}

	sameSubnet := func(ip [4]byte) bool {
		for i := 0; i < 4; i++ {
			if ip[i]&mask[i] != myIP[i]&mask[i] {
				return false
			}
		}
		return true
	}

	sendARPRequest := func(target [4]byte) {
		req := (&lmac.ARP{Op: 1, SenderMAC: staMAC, SenderIP: myIP, TargetIP: target}).Encode()
		tx := &lmac.TxData{DA: [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, SA: staMAC,
			Ethertype: lmac.EtherTypeARP, VifIdx: vif, StaIdx: apIdx, Payload: req}
		if f, err := tx.Encode(); err == nil {
			_ = s.sess.BulkOutData(ctx, f)
		}
	}

	// nextHop resolves the destination MAC. Off-subnet traffic goes to the
	// gateway, which is ordinary IP routing. On-subnet traffic needs ARP; if we
	// have no entry we kick off a request and drop this packet — IP is
	// best-effort and the retransmit finds the cache warm. Dropping is honest;
	// blocking the whole TX path on one unresolved address is not.
	nextHop := func(dst [4]byte) ([6]byte, bool) {
		if !sameSubnet(dst) {
			return gwMAC, true
		}
		mu.Lock()
		mac, ok := arp[dst]
		mu.Unlock()
		if !ok {
			sendARPRequest(dst)
		}
		return mac, ok
	}

	stop := make(chan struct{})
	var once sync.Once
	shutdown := func() { once.Do(func() { close(stop); iface.Close() }) }
	defer shutdown()

	var txPkts, rxPkts, txDrop uint64

	// --- utun -> dongle -------------------------------------------------
	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := iface.File.Read(buf)
			if err != nil {
				select {
				case <-stop:
				default:
					fmt.Printf("  BRIDGE: utun read: %v\n", err)
				}
				shutdown()
				return
			}
			if n < 4+20 {
				continue
			}
			if fam := binary.BigEndian.Uint32(buf[:4]); fam != 2 {
				continue // IPv6 and link-layer families are not bridged
			}
			pkt := buf[4:n]

			var dst [4]byte
			copy(dst[:], pkt[16:20])
			mac, ok := nextHop(dst)
			if !ok {
				txDrop++
				continue
			}
			tx := &lmac.TxData{DA: mac, SA: staMAC, Ethertype: lmac.EtherTypeIP,
				VifIdx: vif, StaIdx: apIdx, Payload: append([]byte(nil), pkt...)}
			frame, err := tx.Encode()
			if err != nil {
				txDrop++
				continue
			}
			if err := s.sess.BulkOutData(ctx, frame); err != nil {
				fmt.Printf("  BRIDGE: tx: %v\n", err)
				txDrop++
				continue
			}
			txPkts++
		}
	}()

	// --- dongle -> utun -------------------------------------------------
	go func() {
		frame := make([]byte, 4+2048)
		binary.BigEndian.PutUint32(frame[:4], 2) // AF_INET
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				shutdown()
				return
			case e := <-netCh:
				switch e.Ethertype {
				case lmac.EtherTypeARP:
					var a lmac.ARP
					if err := a.Decode(e.Payload); err != nil {
						continue
					}
					mu.Lock()
					if a.SenderIP != ([4]byte{}) {
						arp[a.SenderIP] = a.SenderMAC
					}
					mu.Unlock()
					// Answer requests for our address — nothing else on this
					// link will, and the gateway will not forward to us until
					// it has our MAC.
					if a.Op == 1 && a.TargetIP == myIP {
						rep := (&lmac.ARP{Op: 2, SenderMAC: staMAC, SenderIP: myIP,
							TargetMAC: a.SenderMAC, TargetIP: a.SenderIP}).Encode()
						tx := &lmac.TxData{DA: a.SenderMAC, SA: staMAC,
							Ethertype: lmac.EtherTypeARP, VifIdx: vif, StaIdx: apIdx, Payload: rep}
						if f, err := tx.Encode(); err == nil {
							_ = s.sess.BulkOutData(ctx, f)
						}
					}
				case lmac.EtherTypeIP:
					if len(e.Payload) < 20 || 4+len(e.Payload) > len(frame) {
						continue
					}
					copy(frame[4:], e.Payload)
					if _, err := iface.File.Write(frame[:4+len(e.Payload)]); err != nil {
						select {
						case <-stop:
						default:
							fmt.Printf("  BRIDGE: utun write: %v\n", err)
						}
						shutdown()
						return
					}
					rxPkts++
				}
			}
		}
	}()

	reportLink(LinkUp, fmt.Sprintf("bridged on %s as %v", iface.Name, ipStr(myIP)))
	// Record the live link for the daemon (see linkstate.go): the CLI
	// holds the USB claim, so the daemon's own scan reads NO_DONGLE for
	// exactly the period when the link is working. Removed on every
	// exit path below.
	_ = WriteLinkState(LinkStateFile{
		SSID:      bridgeLinkSSID,
		Iface:     iface.Name,
		IP:        ipStr(myIP),
		Gateway:   ipStr(gw),
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	})
	defer RemoveLinkState()
	fmt.Printf("  BRIDGE: running — point clients at %v through %s (ctrl-c to stop)\n",
		ipStr(myIP), iface.Name)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			// A deauth leaves the utun and the route in place, so without this
			// the bridge looks healthy while nothing crosses it. Say so, and
			// stop rather than pretending to carry traffic.
			if linkDown != nil && linkDown.Load() {
				fmt.Printf("  BRIDGE: LINK IS DOWN — the firmware reported a disconnect. "+
					"Final counters tx=%d rx=%d dropped=%d\n", txPkts, rxPkts, txDrop)
				reportLink(LinkDown, "the firmware reported a disconnect; a replug is needed")
				fmt.Println("  BRIDGE: stopping; re-run `usbwifi cmdctl link` after a replug.")
				return 1
			}
			fmt.Printf("  BRIDGE: tx=%d rx=%d dropped=%d\n", txPkts, rxPkts, txDrop)
		case <-stop:
			fmt.Printf("  BRIDGE: stopped (tx=%d rx=%d dropped=%d)\n", txPkts, rxPkts, txDrop)
			return 1
		case <-ctx.Done():
			fmt.Printf("  BRIDGE: stopped (tx=%d rx=%d dropped=%d)\n", txPkts, rxPkts, txDrop)
			return 0
		}
	}
}
