package tun

import (
	"encoding/binary"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// PacketStats tracks how many frames have crossed the utun device and
// breaks them down by family + L4 protocol, so we can tell at a glance
// whether the bridge is moving real traffic or only ICMP.
type PacketStats struct {
	BytesIn    uint64
	BytesOut   uint64
	PacketsIn  uint64
	PacketsOut uint64

	// Family counts (incoming frames).
	IPv4 uint64
	IPv6 uint64
	Other uint64

	// IPv4 L4 protocol counts.
	ICMP uint64
	TCP  uint64
	UDP  uint64
	OtherL4 uint64

	// TCP-SYNs we observed targeting the dish on 192.168.100.1 — these are
	// the bursty telltale that the tunnel is alive, even if the modem
	// never sends reply frames back through this same path.
	TCPSYNToDish uint64
}

type PacketPump struct {
	iface   *Interface
	stats   PacketStats
	running atomic.Bool
	stopCh  chan struct{}
	mu      sync.Mutex
}

var (
	globalPump *PacketPump
	pumpOnce   sync.Once
)

// StartPacketPump starts an asynchronous packet pump for the utun interface
func StartPacketPump(iface *Interface) *PacketPump {
	p := &PacketPump{
		iface:  iface,
		stopCh: make(chan struct{}),
	}
	p.running.Store(true)

	go p.readLoop()
	log.Printf("[TUN-PUMP] Started packet pump engine for %s", iface.Name)
	pumpOnce.Do(func() { globalPump = p })
	return p
}

func (p *PacketPump) readLoop() {
	buf := make([]byte, 4096)

	for p.running.Load() {
		n, err := p.iface.File.Read(buf)
		if err != nil {
			if !p.running.Load() {
				return
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if n < 4 {
			continue
		}

		// macOS utun prepends 4-byte protocol family (AF_INET = 2, AF_INET6 = 30,
		// everything else: families the kernel hands to us, e.g. link-layer).
		family := binary.BigEndian.Uint32(buf[:4])
		pkt := buf[4:n]

		atomic.AddUint64(&p.stats.BytesIn, uint64(n))
		atomic.AddUint64(&p.stats.PacketsIn, 1)

		switch family {
		case 2:
			atomic.AddUint64(&p.stats.IPv4, 1)
			if len(pkt) >= 20 {
				p.handleIPv4Packet(pkt)
			}
		case 30:
			atomic.AddUint64(&p.stats.IPv6, 1)
		default:
			atomic.AddUint64(&p.stats.Other, 1)
		}
	}
}

func (p *PacketPump) handleIPv4Packet(pkt []byte) {
	version := pkt[0] >> 4
	if version != 4 {
		return
	}

	ihl := int(pkt[0]&0x0F) * 4
	if len(pkt) < ihl {
		return
	}

	protocol := pkt[9]
	srcIP := net.IP(pkt[12:16])
	dstIP := net.IP(pkt[16:20])

	switch protocol {
	case 1:
		atomic.AddUint64(&p.stats.ICMP, 1)
	case 6:
		atomic.AddUint64(&p.stats.TCP, 1)
		p.tallyTCPSYN(pkt, ihl, dstIP)
	case 17:
		atomic.AddUint64(&p.stats.UDP, 1)
	default:
		atomic.AddUint64(&p.stats.OtherL4, 1)
	}

	// Existing ICMP echo reply logic for ICMP packets.
	if protocol == 1 && len(pkt) >= ihl+8 {
		icmpPayload := pkt[ihl:]
		icmpType := icmpPayload[0]
		icmpCode := icmpPayload[1]
		if icmpType == 8 && icmpCode == 0 {
			if dstIP.Equal(net.ParseIP("192.168.100.1").To4()) || dstIP.Equal(net.ParseIP("192.168.100.2").To4()) {
				p.sendICMPEchoReply(pkt, ihl, srcIP, dstIP)
			}
		}
	}

	// Opt-in observability for non-ICMP frames — lets us see exactly what
	// the bridge is moving while real traffic stays a no-op.
	if pumpDebugLogs() && protocol != 1 {
		log.Printf("[TUN] IPv4 proto=%d src=%s dst=%s len=%d", protocol, srcIP, dstIP, len(pkt))
	}
}

// tallyTCPSYN bumps the dish-targeted SYN counter when a packet is a
// SYN (no ACK) targeting 192.168.100.1. It's a cheap way to spot
// integration-test probes in the daemon telemetry.
func (p *PacketPump) tallyTCPSYN(pkt []byte, ihl int, dstIP net.IP) {
	if ihl+20 > len(pkt) {
		return
	}
	tcp := pkt[ihl:]
	flags := tcp[13]
	const syn, ack = 0x02, 0x10
	if flags&syn == 0 || flags&ack != 0 {
		return
	}
	dish := net.ParseIP("192.168.100.1").To4()
	if dish == nil {
		return
	}
	if dstIP.Equal(dish) {
		atomic.AddUint64(&p.stats.TCPSYNToDish, 1)
	}
}

func pumpDebugLogs() bool {
	v := os.Getenv("EH_TUN_DEBUG")
	return v == "1" || v == "true"
}

func (p *PacketPump) sendICMPEchoReply(origPkt []byte, ihl int, origSrc, origDst net.IP) {
	reply := make([]byte, len(origPkt))
	copy(reply, origPkt)

	// Swap IPv4 Source and Destination
	copy(reply[12:16], origDst.To4())
	copy(reply[16:20], origSrc.To4())

	// Reset IPv4 Checksum before recalculating
	reply[10] = 0
	reply[11] = 0
	ipChecksum := checksum(reply[:ihl])
	binary.BigEndian.PutUint16(reply[10:12], ipChecksum)

	// Modify ICMP Header to Echo Reply (Type 0, Code 0)
	icmpPayload := reply[ihl:]
	icmpPayload[0] = 0 // Type 0: Echo Reply
	icmpPayload[1] = 0 // Code 0

	// Reset ICMP Checksum before recalculating
	icmpPayload[2] = 0
	icmpPayload[3] = 0
	icmpChk := checksum(icmpPayload)
	binary.BigEndian.PutUint16(icmpPayload[2:4], icmpChk)

	// Construct macOS utun frame: 4-byte AF_INET (2) header + IPv4 Packet
	frame := make([]byte, 4+len(reply))
	binary.BigEndian.PutUint32(frame[:4], 2)
	copy(frame[4:], reply)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.iface.File != nil {
		_, err := p.iface.File.Write(frame)
		if err == nil {
			atomic.AddUint64(&p.stats.BytesOut, uint64(len(frame)))
			atomic.AddUint64(&p.stats.PacketsOut, 1)
		}
	}
}

func (p *PacketPump) Stop() {
	if p.running.CompareAndSwap(true, false) {
		close(p.stopCh)
	}
}

// GetStats returns a snapshot of the current packet counters.
func (p *PacketPump) GetStats() PacketStats {
	return PacketStats{
		BytesIn:       atomic.LoadUint64(&p.stats.BytesIn),
		BytesOut:      atomic.LoadUint64(&p.stats.BytesOut),
		PacketsIn:     atomic.LoadUint64(&p.stats.PacketsIn),
		PacketsOut:    atomic.LoadUint64(&p.stats.PacketsOut),
		IPv4:          atomic.LoadUint64(&p.stats.IPv4),
		IPv6:          atomic.LoadUint64(&p.stats.IPv6),
		Other:         atomic.LoadUint64(&p.stats.Other),
		ICMP:          atomic.LoadUint64(&p.stats.ICMP),
		TCP:           atomic.LoadUint64(&p.stats.TCP),
		UDP:           atomic.LoadUint64(&p.stats.UDP),
		OtherL4:       atomic.LoadUint64(&p.stats.OtherL4),
		TCPSYNToDish:  atomic.LoadUint64(&p.stats.TCPSYNToDish),
	}
}

// RealFramesSeen reports whether any non-ICMP IPv4 frame has arrived on
// the utun device. It's the bridge activity test: a true reading means
// the underlying transport has actually delivered something into the
// userspace pump (i.e. the dongle's wifi path is alive, not just ICMP).
func (p *PacketPump) RealFramesSeen() bool {
	s := p.GetStats()
	return s.TCP+s.UDP+s.OtherL4 > 0
}

// TCPSYNsToDish counts TCP-SYNs we've observed targeting the dish address
// — handy for noticing when an integration test starts probing through the
// pump even before the dish replies.
func (p *PacketPump) TCPSYNsToDish() uint64 {
	return atomic.LoadUint64(&p.stats.TCPSYNToDish)
}

// ResetStats zeroes every counter. Useful for tests; do not call on a
// production pump (counters are advisory and restarts are cheap).
func (p *PacketPump) ResetStats() {
	atomic.StoreUint64(&p.stats.BytesIn, 0)
	atomic.StoreUint64(&p.stats.BytesOut, 0)
	atomic.StoreUint64(&p.stats.PacketsIn, 0)
	atomic.StoreUint64(&p.stats.PacketsOut, 0)
	atomic.StoreUint64(&p.stats.IPv4, 0)
	atomic.StoreUint64(&p.stats.IPv6, 0)
	atomic.StoreUint64(&p.stats.Other, 0)
	atomic.StoreUint64(&p.stats.ICMP, 0)
	atomic.StoreUint64(&p.stats.TCP, 0)
	atomic.StoreUint64(&p.stats.UDP, 0)
	atomic.StoreUint64(&p.stats.OtherL4, 0)
	atomic.StoreUint64(&p.stats.TCPSYNToDish, 0)
}

// GlobalPump returns the package-level singleton started from main. Nil
// when no pump has been started yet.
func GlobalPump() *PacketPump { return globalPump }

// checksum calculates standard 16-bit one's complement checksum for IP/ICMP
func checksum(data []byte) uint16 {
	var sum uint32
	n := len(data)
	for i := 0; i < n-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if n%2 == 1 {
		sum += uint32(data[n-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
