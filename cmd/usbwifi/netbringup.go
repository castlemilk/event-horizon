package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
)

// runDhcpPing runs the post-handshake network validation over the dongle's
// data path: DHCP (discover → offer → request → ack), ARP for the gateway,
// then ICMP echo to the gateway. It proves the data path carries real IP,
// which is the precondition for the Starlink interrogation. Returns 0 when
// at least one ping reply arrives.
func runDhcpPing(ctx context.Context, s *session, vif uint8, staMAC, apMAC [6]byte, netCh <-chan lmac.Ethernet) int {
	bcast := [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	zeroIP := [4]byte{}
	bcastIP := [4]byte{255, 255, 255, 255}

	sendIP := func(da [6]byte, srcIP, dstIP [4]byte, proto uint8, payload []byte) error {
		ip := (&lmac.IPv4{Src: srcIP, Dst: dstIP, Proto: proto, Payload: payload}).Encode()
		tx := &lmac.TxData{DA: da, SA: staMAC, Ethertype: lmac.EtherTypeIP,
			VifIdx: vif, StaIdx: 0xFF, Payload: ip}
		frame, err := tx.Encode()
		if err != nil {
			return err
		}
		return s.sess.BulkOutData(ctx, frame)
	}

	var xid [4]byte
	if _, err := rand.Read(xid[:]); err != nil {
		log.Printf("rand xid: %v", err)
		return 1
	}
	dhcp := lmac.DHCP{XID: xid}

	// --- DHCP discover/offer ---
	fmt.Println("  NET: DHCP discover ...")
	var offer lmac.DHCP
	foundOffer := false
	deadline := time.After(15 * time.Second)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	sendDiscover := func() {
		udp := (&lmac.UDP{SrcPort: 68, DstPort: 67,
			Payload: dhcp.Discover(staMAC)}).Encode(zeroIP, bcastIP)
		if err := sendIP(bcast, zeroIP, bcastIP, lmac.IPProtoUDP, udp); err != nil {
			fmt.Printf("  NET: discover send: %v\n", err)
		}
	}
	sendDiscover()
	for !foundOffer {
		select {
		case <-tick.C:
			sendDiscover()
		case e := <-netCh:
			o := dhcpOfferFrom(e, xid)
			if o != nil {
				offer = *o
				foundOffer = true
			}
		case <-deadline:
			fmt.Println("  NET: no DHCP offer within 15s")
			return 1
		case <-ctx.Done():
			return 1
		}
	}
	fmt.Printf("  NET: offer %v from %v (lease %ds)\n", ipStr(offer.YIAddr), ipStr(offer.ServerID), offer.Lease)

	// --- DHCP request/ack ---
	var ack lmac.DHCP
	deadline = time.After(15 * time.Second)
	sendRequest := func() {
		udp := (&lmac.UDP{SrcPort: 68, DstPort: 67,
			Payload: offer.Request(staMAC, offer.YIAddr)}).Encode(zeroIP, bcastIP)
		if err := sendIP(bcast, zeroIP, bcastIP, lmac.IPProtoUDP, udp); err != nil {
			fmt.Printf("  NET: request send: %v\n", err)
		}
	}
	sendRequest()
waitAck:
	for {
		select {
		case <-tick.C:
			sendRequest()
		case e := <-netCh:
			if a := dhcpAckFrom(e, xid); a != nil {
				ack = *a
				break waitAck
			}
		case <-deadline:
			fmt.Println("  NET: no DHCP ack within 15s")
			return 1
		case <-ctx.Done():
			return 1
		}
	}
	myIP := ack.YIAddr
	if myIP == zeroIP {
		myIP = offer.YIAddr
	}
	gw := ack.Router
	if gw == zeroIP {
		gw = offer.Router
	}
	fmt.Printf("  NET: IP=%v mask=%v gw=%v dns=%v\n", ipStr(myIP), ipStr(ack.Subnet), ipStr(gw), ipStr(ack.DNS))

	// --- ARP for the gateway ---
	fmt.Printf("  NET: ARP for %v ...\n", ipStr(gw))
	var gwMAC [6]byte
	arpDeadline := time.After(10 * time.Second)
	sendARP := func() {
		arp := (&lmac.ARP{Op: 1, SenderMAC: staMAC, SenderIP: myIP, TargetIP: gw}).Encode()
		tx := &lmac.TxData{DA: bcast, SA: staMAC, Ethertype: lmac.EtherTypeARP,
			VifIdx: vif, StaIdx: 0xFF, Payload: arp}
		if frame, err := tx.Encode(); err == nil {
			_ = s.sess.BulkOutData(ctx, frame)
		}
	}
	sendARP()
	arpTick := time.NewTicker(2 * time.Second)
	defer arpTick.Stop()
waitARP:
	for {
		select {
		case <-arpTick.C:
			sendARP()
		case e := <-netCh:
			if e.Ethertype != lmac.EtherTypeARP {
				continue
			}
			var a lmac.ARP
			if err := a.Decode(e.Payload); err != nil {
				continue
			}
			if a.Op == 2 && a.SenderIP == gw {
				gwMAC = a.SenderMAC
				break waitARP
			}
		case <-arpDeadline:
			fmt.Println("  NET: no ARP reply within 10s")
			return 1
		case <-ctx.Done():
			return 1
		}
	}
	fmt.Printf("  NET: gateway %v at %v\n", ipStr(gw), macStr(gwMAC))

	// --- ICMP ping ---
	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		log.Printf("rand ping id: %v", err)
		return 1
	}
	pingID := binary.BigEndian.Uint16(idBytes[:])
	rx := 0
	for seq := uint16(1); seq <= 4; seq++ {
		echo := (&lmac.ICMPEcho{ID: pingID, Seq: seq,
			Data: []byte(fmt.Sprintf("event-horizon %d", seq))}).Encode(true)
		t0 := time.Now()
		if err := sendIP(gwMAC, myIP, gw, lmac.IPProtoICMP, echo); err != nil {
			fmt.Printf("  NET: ping send: %v\n", err)
			continue
		}
		select {
		case e := <-netCh:
			if e.Ethertype != lmac.EtherTypeIP {
				seq--
				continue
			}
			var ip lmac.IPv4
			if err := ip.Decode(e.Payload); err != nil || ip.Proto != lmac.IPProtoICMP {
				seq--
				continue
			}
			var reply lmac.ICMPEcho
			if err := reply.Decode(ip.Payload); err != nil {
				seq--
				continue
			}
			if len(reply.Data) == 0 || reply.ID != pingID || reply.Seq != seq {
				seq--
				continue
			}
			// Echo reply has type 0; Decode accepts 0 or 8 — verify reply.
			if ip.Payload[0] != 0 {
				seq--
				continue
			}
			rx++
			fmt.Printf("  NET: ping %v seq=%d rtt=%v\n", ipStr(gw), seq, time.Since(t0).Round(time.Millisecond))
		case <-time.After(3 * time.Second):
			fmt.Printf("  NET: ping %v seq=%d timeout\n", ipStr(gw), seq)
		case <-ctx.Done():
			return 1
		}
		_ = apMAC
	}
	fmt.Printf("  NET: %d/4 ping replies\n", rx)
	if rx == 0 {
		return 1
	}
	return 0
}

// dhcpOfferFrom returns the DHCP offer in e (or nil).
func dhcpOfferFrom(e lmac.Ethernet, xid [4]byte) *lmac.DHCP {
	if e.Ethertype != lmac.EtherTypeIP {
		return nil
	}
	var ip lmac.IPv4
	if err := ip.Decode(e.Payload); err != nil || ip.Proto != lmac.IPProtoUDP {
		return nil
	}
	if len(ip.Payload) < 8 {
		return nil
	}
	udp := ip.Payload
	if binary.BigEndian.Uint16(udp[0:2]) != 67 || binary.BigEndian.Uint16(udp[2:4]) != 68 {
		return nil
	}
	d, err := lmac.ParseDHCP(udp[8:])
	if err != nil || d.XID != xid || d.MsgType != lmac.DHCPOffer {
		return nil
	}
	return &d
}

// dhcpAckFrom returns the DHCP ack in e (or nil).
func dhcpAckFrom(e lmac.Ethernet, xid [4]byte) *lmac.DHCP {
	if e.Ethertype != lmac.EtherTypeIP {
		return nil
	}
	var ip lmac.IPv4
	if err := ip.Decode(e.Payload); err != nil || ip.Proto != lmac.IPProtoUDP {
		return nil
	}
	if len(ip.Payload) < 8 {
		return nil
	}
	udp := ip.Payload
	if binary.BigEndian.Uint16(udp[0:2]) != 67 || binary.BigEndian.Uint16(udp[2:4]) != 68 {
		return nil
	}
	d, err := lmac.ParseDHCP(udp[8:])
	if err != nil || d.XID != xid || d.MsgType != lmac.DHCPAck {
		return nil
	}
	return &d
}

func ipStr(ip [4]byte) string {
	return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
}

func macStr(m [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}
