package tun

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
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
	IPv4  uint64
	IPv6  uint64
	Other uint64

	// IPv4 L4 protocol counts.
	ICMP    uint64
	TCP     uint64
	UDP     uint64
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

// SyntheticResponsesEnabled reports whether the pump may answer dish
// requests with canned data when no real terminal is behind the bridge.
//
// It is off unless EVENT_HORIZON_SYNTHETIC_DISH=1. The pump used to do
// this unconditionally: unmatched gRPC requests fell through to a
// hardcoded DishGetStatus, so a caller asking for GPS got dish status
// back and concluded GPS was disabled, rather than learning that no dish
// was reachable at all. Downstream consumers then copied the canned
// values into their own source as defaults.
//
// Synthetic mode remains available for UI work without hardware, but it
// has to be asked for, and the pump says so on startup.
func SyntheticResponsesEnabled() bool {
	v := os.Getenv("EVENT_HORIZON_SYNTHETIC_DISH")
	return v == "1" || strings.EqualFold(v, "true")
}

// grpcWebUnavailable is a well-formed gRPC-Web reply carrying UNAVAILABLE,
// so a client sees a real status code instead of a plausible-looking
// message it did not ask for.
func grpcWebUnavailable() []byte {
	const msg = "no Starlink terminal is reachable through this bridge; " +
		"the dongle is not associated with a terminal network"
	trailer := "grpc-status: 14\r\ngrpc-message: " + url.QueryEscape(msg) + "\r\n"

	// Trailers frame: flag 0x80, then a 4-byte big-endian length.
	body := make([]byte, 5+len(trailer))
	body[0] = 0x80
	binary.BigEndian.PutUint32(body[1:5], uint32(len(trailer)))
	copy(body[5:], trailer)

	hdr := "HTTP/1.1 200 OK\r\n" +
		"Content-Type: application/grpc-web+proto\r\n" +
		"Connection: close\r\n\r\n"
	return append([]byte(hdr), body...)
}

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
		p.handleTCPPacket(pkt, ihl, srcIP, dstIP)
	case 17:
		atomic.AddUint64(&p.stats.UDP, 1)
		p.handleUDPPacket(pkt, ihl, srcIP, dstIP)
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
		BytesIn:      atomic.LoadUint64(&p.stats.BytesIn),
		BytesOut:     atomic.LoadUint64(&p.stats.BytesOut),
		PacketsIn:    atomic.LoadUint64(&p.stats.PacketsIn),
		PacketsOut:   atomic.LoadUint64(&p.stats.PacketsOut),
		IPv4:         atomic.LoadUint64(&p.stats.IPv4),
		IPv6:         atomic.LoadUint64(&p.stats.IPv6),
		Other:        atomic.LoadUint64(&p.stats.Other),
		ICMP:         atomic.LoadUint64(&p.stats.ICMP),
		TCP:          atomic.LoadUint64(&p.stats.TCP),
		UDP:          atomic.LoadUint64(&p.stats.UDP),
		OtherL4:      atomic.LoadUint64(&p.stats.OtherL4),
		TCPSYNToDish: atomic.LoadUint64(&p.stats.TCPSYNToDish),
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

// handleTCPPacket bridges TCP connections targeting 192.168.100.1 or local endpoints
// across the user-space utun interface.
func (p *PacketPump) handleTCPPacket(pkt []byte, ihl int, srcIP, dstIP net.IP) {
	if len(pkt) < ihl+20 {
		return
	}
	tcpHdr := pkt[ihl:]
	srcPort := binary.BigEndian.Uint16(tcpHdr[0:2])
	dstPort := binary.BigEndian.Uint16(tcpHdr[2:4])
	clientSeq := binary.BigEndian.Uint32(tcpHdr[4:8])
	flags := tcpHdr[13]

	dish := net.ParseIP("192.168.100.1").To4()
	dongle := net.ParseIP("192.168.100.2").To4()

	// Only process connections targeting the dish or local dongle IP
	if !dstIP.Equal(dish) && !dstIP.Equal(dongle) {
		return
	}

	tcpDataOffset := int(tcpHdr[12]>>4) * 4
	payload := []byte{}
	if len(tcpHdr) > tcpDataOffset {
		payload = tcpHdr[tcpDataOffset:]
	}

	// 1. TCP SYN: Reply with SYN+ACK
	if flags&0x02 != 0 && flags&0x10 == 0 {
		p.sendTCPSYNACK(srcIP, dstIP, srcPort, dstPort, clientSeq)
		return
	}

	// 2. TCP Data (PSH+ACK or ACK with payload): Reply with HTTP / Dish payload + FIN
	if len(payload) > 0 {
		// For dish gRPC (9200), try to proxy to the real dish via the host's
		// routing table (direct, not via utun). If the host is on Starlink
		// (en0 associated), this will succeed and return real dish data.
		// Otherwise, fall back to synthetic gRPC-Web handling.
		if dstPort == 9200 {
			// Always try the real terminal first. Only when nothing
			// answers does the synthetic path come into play, and that
			// now requires EVENT_HORIZON_SYNTHETIC_DISH=1.
			if resp := p.tryProxyToRealDish(payload); resp != nil {
				ackNum := clientSeq + uint32(len(payload))
				p.sendTCPData(srcIP, dstIP, srcPort, dstPort, 2000001, ackNum, resp)
				return
			}
			if resp := p.tryHandleGRPCWeb(payload); resp != nil {
				ackNum := clientSeq + uint32(len(payload))
				p.sendTCPData(srcIP, dstIP, srcPort, dstPort, 2000001, ackNum, resp)
				return
			}
			// Fallback for non-gRPC probes (curl, etc.). This used to
			// report ONLINE with a full set of invented figures.
			responseBody := []byte("HTTP/1.1 503 Service Unavailable\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n{\"error\":\"no Starlink terminal is reachable through this bridge\",\"bridge\":\"active\",\"dish\":\"unreachable\"}\r\n")
			if SyntheticResponsesEnabled() {
				responseBody = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n{\"synthetic\":true,\"note\":\"EVENT_HORIZON_SYNTHETIC_DISH=1\"}\r\n")
			}
			ackNum := clientSeq + uint32(len(payload))
			p.sendTCPData(srcIP, dstIP, srcPort, dstPort, 2000001, ackNum, responseBody)
			return
		}
		// Dish web portal. The bridge is genuinely active; whether a
		// terminal is behind it is a separate question, so do not claim
		// ONLINE on the terminal's behalf.
		responseBody := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n{\"utun_bridge\":\"active\",\"dish\":\"unknown\"}\r\n")
		ackNum := clientSeq + uint32(len(payload))
		p.sendTCPData(srcIP, dstIP, srcPort, dstPort, 2000001, ackNum, responseBody)
		return
	}

	// 3. TCP FIN: Reply with ACK
	if flags&0x01 != 0 {
		p.sendTCPACK(srcIP, dstIP, srcPort, dstPort, 2000001+uint32(len(payload)), clientSeq+1)
	}
}

func (p *PacketPump) tryProxyToRealDish(payload []byte) []byte {
	// Try to forward the raw HTTP payload to the real dish via the host's
	// routing table (not via utun). This only works when the host's en0
	// is actually on the Starlink network. Use a short timeout so we fail
	// fast and fall back to synthetic.
	conn, err := net.DialTimeout("tcp", "192.168.100.1:9200", 800*time.Millisecond)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	if _, err := conn.Write(payload); err != nil {
		return nil
	}
	// Read response with a deadline; dish typically responds in <100ms.
	resp := make([]byte, 8192)
	n, err := conn.Read(resp)
	if err != nil || n == 0 {
		return nil
	}
	return resp[:n]
}

func (p *PacketPump) tryHandleGRPCWeb(payload []byte) []byte {
	payloadStr := string(payload)
	if !contains(payloadStr, "application/grpc-web") {
		return nil
	}
	// Without a real dish behind the bridge there is no honest answer to
	// give. Returning canned protobuf makes every RPC look implemented,
	// which is worse than an error: callers cannot tell a terminal with a
	// feature switched off from no terminal at all.
	if !SyntheticResponsesEnabled() {
		return grpcWebUnavailable()
	}
	// Find header/body split
	hdrEnd := bytes.Index(payload, []byte("\r\n\r\n"))
	if hdrEnd < 0 {
		return nil
	}

	body := payload[hdrEnd+4:]
	var protoReq []byte
	if len(body) >= 5 && body[0] == 0x00 {
		msgLen := int(binary.BigEndian.Uint32(body[1:5]))
		if len(body) >= 5+msgLen {
			protoReq = body[5 : 5+msgLen]
		} else {
			protoReq = body[5:]
		}
	}

	log.Printf("[TUN-GRPC] Received payload len=%d, hdrEnd=%d, protoReq=%x", len(payload), hdrEnd, protoReq)

	var protoResp []byte
	switch {
	case bytes.Contains(protoReq, []byte{0xca, 0x3f}): // GetLocation (1017)
		protoResp, _ = hex.DecodeString("ca3f280a1b09e561a1d634ef40c011b1e1e995b2e66240190000000000004d40180421333333333333f33f")
	case bytes.Contains(protoReq, []byte{0xd2, 0x7d}): // DishSetConfig (2010)
		protoResp, _ = hex.DecodeString("d27d070a051001887d01")
	case bytes.Contains(protoReq, []byte{0xda, 0x7d}): // DishGetConfig (2011)
		protoResp, _ = hex.DecodeString("da7d040a021001")
	case bytes.Contains(protoReq, []byte{0x82, 0x3f}): // GetDeviceInfo (1008)
		protoResp, _ = hex.DecodeString("e23e470a450a1a35313730386431322d34393831373830612d3961316362373064120b726576335f70726f746f321a16323032362e30382e31302e6d72343130302d70726f6422024155")
	case bytes.Contains(protoReq, []byte{0x9a, 0x7d}): // DishGetContext (2003)
		protoResp, _ = hex.DecodeString("9a7d1120c0c407282a4095064865b501ae47613f")
	case bytes.Contains(protoReq, []byte{0xca, 0x7d}): // DishGetEmc (2009)
		protoResp, _ = hex.DecodeString("ca7d1e8101e561a1d634ef40c08901b1e1e995b2e662409101000000000000f83f")
	case bytes.Contains(protoReq, []byte{0xc2, 0x40}): // GetGnssMeasurement (1032)
		protoResp, _ = hex.DecodeString("c2b50300")
	default: // GetStatus (1004) / DishGetStatus (2004)
		protoResp, _ = hex.DecodeString("a27d6e0a450a1a35313730386431322d34393831373830612d3961316362373064120b726576335f70726f746f321a16323032362e30382e31302e6d72343130302d70726f6422024155120408c0c407fd3e046e304d853fc0d8a74b8d3f0000e0419d3f00008041a53f00008042d03f01")
	}

	dataFrame := make([]byte, 5+len(protoResp))
	dataFrame[0] = 0x00
	binary.BigEndian.PutUint32(dataFrame[1:5], uint32(len(protoResp)))
	copy(dataFrame[5:], protoResp)

	trailers := []byte("grpc-status: 0\r\n")
	trailerFrame := make([]byte, 5+len(trailers))
	trailerFrame[0] = 0x80
	binary.BigEndian.PutUint32(trailerFrame[1:5], uint32(len(trailers)))
	copy(trailerFrame[5:], trailers)

	bodyBytes := append(dataFrame, trailerFrame...)
	headers := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/grpc-web+proto\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(bodyBytes))
	return append([]byte(headers), bodyBytes...)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func (p *PacketPump) sendTCPSYNACK(clientIP, serverIP net.IP, clientPort, serverPort uint16, clientSeq uint32) {
	ipHeaderLen := 20
	tcpHeaderLen := 20
	totalLen := ipHeaderLen + tcpHeaderLen

	pkt := make([]byte, totalLen)

	// IPv4 Header
	pkt[0] = 0x45
	pkt[1] = 0x00
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0x1234)
	pkt[6] = 0x40 // Don't fragment
	pkt[7] = 0x00
	pkt[8] = 64 // TTL
	pkt[9] = 6  // TCP
	copy(pkt[12:16], serverIP.To4())
	copy(pkt[16:20], clientIP.To4())
	ipChk := checksum(pkt[:ipHeaderLen])
	binary.BigEndian.PutUint16(pkt[10:12], ipChk)

	// TCP Header
	tcp := pkt[ipHeaderLen:]
	binary.BigEndian.PutUint16(tcp[0:2], serverPort)
	binary.BigEndian.PutUint16(tcp[2:4], clientPort)
	binary.BigEndian.PutUint32(tcp[4:8], 2000000)      // Server Seq
	binary.BigEndian.PutUint32(tcp[8:12], clientSeq+1) // Server Ack
	tcp[12] = 0x50                                     // Data offset: 5 * 4 = 20 bytes
	tcp[13] = 0x12                                     // Flags: SYN | ACK
	binary.BigEndian.PutUint16(tcp[14:16], 65535)      // Window size

	tcpChk := tcpChecksum(serverIP, clientIP, tcp)
	binary.BigEndian.PutUint16(tcp[16:18], tcpChk)

	p.writeUtunFrame(pkt)
}

func (p *PacketPump) sendTCPData(clientIP, serverIP net.IP, clientPort, serverPort uint16, serverSeq, clientAck uint32, payload []byte) {
	ipHeaderLen := 20
	tcpHeaderLen := 20
	totalLen := ipHeaderLen + tcpHeaderLen + len(payload)

	pkt := make([]byte, totalLen)

	// IPv4 Header
	pkt[0] = 0x45
	pkt[1] = 0x00
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0x1235)
	pkt[6] = 0x40
	pkt[7] = 0x00
	pkt[8] = 64
	pkt[9] = 6 // TCP
	copy(pkt[12:16], serverIP.To4())
	copy(pkt[16:20], clientIP.To4())
	ipChk := checksum(pkt[:ipHeaderLen])
	binary.BigEndian.PutUint16(pkt[10:12], ipChk)

	// TCP Header
	tcp := pkt[ipHeaderLen:]
	binary.BigEndian.PutUint16(tcp[0:2], serverPort)
	binary.BigEndian.PutUint16(tcp[2:4], clientPort)
	binary.BigEndian.PutUint32(tcp[4:8], serverSeq)
	binary.BigEndian.PutUint32(tcp[8:12], clientAck)
	tcp[12] = 0x50 // Data offset: 5 * 4 = 20 bytes
	tcp[13] = 0x18 // Flags: PSH | ACK
	binary.BigEndian.PutUint16(tcp[14:16], 65535)

	// Copy Payload
	copy(tcp[tcpHeaderLen:], payload)

	tcpChk := tcpChecksum(serverIP, clientIP, tcp)
	binary.BigEndian.PutUint16(tcp[16:18], tcpChk)

	p.writeUtunFrame(pkt)

	// Send FIN to gracefully close the stream for Connection: close
	p.sendTCPFIN(clientIP, serverIP, clientPort, serverPort, serverSeq+uint32(len(payload)), clientAck)
}

func (p *PacketPump) sendTCPFIN(clientIP, serverIP net.IP, clientPort, serverPort uint16, serverSeq, clientAck uint32) {
	ipHeaderLen := 20
	tcpHeaderLen := 20
	totalLen := ipHeaderLen + tcpHeaderLen

	pkt := make([]byte, totalLen)

	pkt[0] = 0x45
	pkt[1] = 0x00
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0x1237)
	pkt[6] = 0x40
	pkt[7] = 0x00
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], serverIP.To4())
	copy(pkt[16:20], clientIP.To4())
	ipChk := checksum(pkt[:ipHeaderLen])
	binary.BigEndian.PutUint16(pkt[10:12], ipChk)

	tcp := pkt[ipHeaderLen:]
	binary.BigEndian.PutUint16(tcp[0:2], serverPort)
	binary.BigEndian.PutUint16(tcp[2:4], clientPort)
	binary.BigEndian.PutUint32(tcp[4:8], serverSeq)
	binary.BigEndian.PutUint32(tcp[8:12], clientAck)
	tcp[12] = 0x50 // Data offset: 20 bytes
	tcp[13] = 0x11 // Flags: FIN | ACK
	binary.BigEndian.PutUint16(tcp[14:16], 65535)

	tcpChk := tcpChecksum(serverIP, clientIP, tcp)
	binary.BigEndian.PutUint16(tcp[16:18], tcpChk)

	p.writeUtunFrame(pkt)
}

func (p *PacketPump) sendTCPACK(clientIP, serverIP net.IP, clientPort, serverPort uint16, serverSeq, clientAck uint32) {
	ipHeaderLen := 20
	tcpHeaderLen := 20
	totalLen := ipHeaderLen + tcpHeaderLen

	pkt := make([]byte, totalLen)

	pkt[0] = 0x45
	pkt[1] = 0x00
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0x1236)
	pkt[6] = 0x40
	pkt[7] = 0x00
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], serverIP.To4())
	copy(pkt[16:20], clientIP.To4())
	ipChk := checksum(pkt[:ipHeaderLen])
	binary.BigEndian.PutUint16(pkt[10:12], ipChk)

	tcp := pkt[ipHeaderLen:]
	binary.BigEndian.PutUint16(tcp[0:2], serverPort)
	binary.BigEndian.PutUint16(tcp[2:4], clientPort)
	binary.BigEndian.PutUint32(tcp[4:8], serverSeq)
	binary.BigEndian.PutUint32(tcp[8:12], clientAck)
	tcp[12] = 0x50 // Data offset: 20 bytes
	tcp[13] = 0x10 // Flags: ACK
	binary.BigEndian.PutUint16(tcp[14:16], 65535)

	tcpChk := tcpChecksum(serverIP, clientIP, tcp)
	binary.BigEndian.PutUint16(tcp[16:18], tcpChk)

	p.writeUtunFrame(pkt)
}

func (p *PacketPump) writeUtunFrame(ipv4Pkt []byte) {
	frame := make([]byte, 4+len(ipv4Pkt))
	binary.BigEndian.PutUint32(frame[:4], 2) // AF_INET = 2
	copy(frame[4:], ipv4Pkt)

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

// handleUDPPacket bridges UDP datagrams across the utun interface,
// handling DNS resolution (port 53), NTP (port 123), and probe datagrams.
func (p *PacketPump) handleUDPPacket(pkt []byte, ihl int, srcIP, dstIP net.IP) {
	if len(pkt) < ihl+8 {
		return
	}
	udpHdr := pkt[ihl:]
	srcPort := binary.BigEndian.Uint16(udpHdr[0:2])
	dstPort := binary.BigEndian.Uint16(udpHdr[2:4])
	udpLen := int(binary.BigEndian.Uint16(udpHdr[4:6]))
	if len(udpHdr) < udpLen || udpLen < 8 {
		return
	}
	payload := udpHdr[8:udpLen]

	dish := net.ParseIP("192.168.100.1").To4()
	dongle := net.ParseIP("192.168.100.2").To4()

	// 1. DNS Query (Port 53)
	if dstPort == 53 && len(payload) >= 12 {
		p.sendUDPDNSResponse(srcIP, dstIP, srcPort, dstPort, payload)
		return
	}

	// 2. NTP Time Request (Port 123)
	if dstPort == 123 && len(payload) >= 48 {
		p.sendUDPNTPResponse(srcIP, dstIP, srcPort, dstPort, payload)
		return
	}

	// 3. General UDP Probes to Dish/Dongle
	if dstIP.Equal(dish) || dstIP.Equal(dongle) {
		// Echo payload back to client
		p.sendUDPResponse(srcIP, dstIP, srcPort, dstPort, payload)
	}
}

func (p *PacketPump) sendUDPDNSResponse(clientIP, serverIP net.IP, clientPort, serverPort uint16, query []byte) {
	txID := binary.BigEndian.Uint16(query[0:2])

	// Minimal compliant DNS Response:
	// Header: 12 bytes + Question section + Answer section (A record -> 192.168.100.1)
	resp := make([]byte, 0, len(query)+16)
	resp = append(resp, query...) // Include original query (header + question)

	// Flags: 0x8180 (Standard query response, No error)
	resp[2] = 0x81
	resp[3] = 0x80
	// Answer count: 1
	resp[6] = 0x00
	resp[7] = 0x01

	// Answer Record: Pointer to name (0xC00C), Type A (0x0001), Class IN (0x0001), TTL 60 (0x0000003C), DataLen 4 (0x0004), IP 192.168.100.1
	answer := []byte{
		0xc0, 0x0c, // Name pointer -> offset 12
		0x00, 0x01, // Type A
		0x00, 0x01, // Class IN
		0x00, 0x00, 0x00, 0x3c, // TTL: 60s
		0x00, 0x04, // RDLENGTH: 4 bytes
		192, 168, 100, 1, // RDATA: 192.168.100.1
	}
	_ = txID
	resp = append(resp, answer...)

	p.sendUDPResponse(clientIP, serverIP, clientPort, serverPort, resp)
}

func (p *PacketPump) sendUDPNTPResponse(clientIP, serverIP net.IP, clientPort, serverPort uint16, req []byte) {
	resp := make([]byte, 48)
	// LI=0, VN=4, Mode=4 (Server response): 0x24
	resp[0] = 0x24
	resp[1] = 2    // Stratum 2
	resp[2] = 6    // Poll 6
	resp[3] = 0xEC // Precision

	// Copy client transmit timestamp to originate timestamp (bytes 24-31)
	if len(req) >= 48 {
		copy(resp[24:32], req[40:48])
	}

	// Set current NTP timestamp in transmit timestamp (bytes 40-47)
	now := time.Now()
	secs := uint32(now.Unix() + 2208988800) // NTP epoch (1900)
	frac := uint32(uint64(now.Nanosecond()) * (1 << 32) / 1000000000)
	binary.BigEndian.PutUint32(resp[40:44], secs)
	binary.BigEndian.PutUint32(resp[44:48], frac)

	p.sendUDPResponse(clientIP, serverIP, clientPort, serverPort, resp)
}

func (p *PacketPump) sendUDPResponse(clientIP, serverIP net.IP, clientPort, serverPort uint16, payload []byte) {
	ipHeaderLen := 20
	udpHeaderLen := 8
	totalLen := ipHeaderLen + udpHeaderLen + len(payload)

	pkt := make([]byte, totalLen)

	// IPv4 Header
	pkt[0] = 0x45
	pkt[1] = 0x00
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0x1237)
	pkt[6] = 0x40
	pkt[7] = 0x00
	pkt[8] = 64 // TTL
	pkt[9] = 17 // UDP
	copy(pkt[12:16], serverIP.To4())
	copy(pkt[16:20], clientIP.To4())
	ipChk := checksum(pkt[:ipHeaderLen])
	binary.BigEndian.PutUint16(pkt[10:12], ipChk)

	// UDP Header
	udp := pkt[ipHeaderLen:]
	binary.BigEndian.PutUint16(udp[0:2], serverPort)
	binary.BigEndian.PutUint16(udp[2:4], clientPort)
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpHeaderLen+len(payload)))

	// Copy Payload
	copy(udp[udpHeaderLen:], payload)

	udpChk := udpChecksum(serverIP, clientIP, udp)
	binary.BigEndian.PutUint16(udp[6:8], udpChk)

	p.writeUtunFrame(pkt)
}

// udpChecksum calculates standard 16-bit one's complement UDP checksum with IPv4 pseudo-header
func udpChecksum(srcIP, dstIP net.IP, udpSegment []byte) uint16 {
	pseudoHeader := make([]byte, 12)
	copy(pseudoHeader[0:4], srcIP.To4())
	copy(pseudoHeader[4:8], dstIP.To4())
	pseudoHeader[8] = 0
	pseudoHeader[9] = 17 // UDP
	binary.BigEndian.PutUint16(pseudoHeader[10:12], uint16(len(udpSegment)))

	var sum uint32
	for i := 0; i < len(pseudoHeader); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pseudoHeader[i : i+2]))
	}

	udpCopy := make([]byte, len(udpSegment))
	copy(udpCopy, udpSegment)
	udpCopy[6] = 0
	udpCopy[7] = 0

	n := len(udpCopy)
	for i := 0; i < n-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(udpCopy[i : i+2]))
	}
	if n%2 == 1 {
		sum += uint32(udpCopy[n-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	res := ^uint16(sum)
	if res == 0 {
		return 0xffff
	}
	return res
}

// tcpChecksum calculates standard 16-bit one's complement TCP checksum with IPv4 pseudo-header
func tcpChecksum(srcIP, dstIP net.IP, tcpSegment []byte) uint16 {
	pseudoHeader := make([]byte, 12)
	copy(pseudoHeader[0:4], srcIP.To4())
	copy(pseudoHeader[4:8], dstIP.To4())
	pseudoHeader[8] = 0
	pseudoHeader[9] = 6 // TCP
	binary.BigEndian.PutUint16(pseudoHeader[10:12], uint16(len(tcpSegment)))

	var sum uint32
	for i := 0; i < len(pseudoHeader); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pseudoHeader[i : i+2]))
	}

	tcpCopy := make([]byte, len(tcpSegment))
	copy(tcpCopy, tcpSegment)
	tcpCopy[16] = 0
	tcpCopy[17] = 0

	n := len(tcpCopy)
	for i := 0; i < n-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(tcpCopy[i : i+2]))
	}
	if n%2 == 1 {
		sum += uint32(tcpCopy[n-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

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
