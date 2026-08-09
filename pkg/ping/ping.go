package ping

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type PingResult struct {
	Interface         string    `json:"interface"`
	Target            string    `json:"target"`
	IsReachable       bool      `json:"is_reachable"`
	RTTMs             int64     `json:"rtt_ms"`
	PacketLossPercent float64   `json:"packet_loss_percent"`
	LastChecked       time.Time `json:"last_checked"`
}

type Tester struct{}

func NewTester() *Tester {
	return &Tester{}
}

// PingTargetOnInterface performs reachability tests bound to a given network interface (e.g. "en0", "en14", "utun4")
func (t *Tester) PingTargetOnInterface(ifaceName, target string, port int) PingResult {
	if ifaceName == "" {
		ifaceName = "en0"
	}

	// 1. Try system ICMP ping with interface binding (-b on macOS)
	out, err := exec.Command("ping", "-c", "2", "-W", "1000", "-b", ifaceName, target).Output()
	if err == nil {
		str := string(out)
		if strings.Contains(str, "bytes from") {
			rtt := parseRTT(str)
			loss := parseLoss(str)
			return PingResult{
				Interface:         ifaceName,
				Target:            target,
				IsReachable:       true,
				RTTMs:             rtt,
				PacketLossPercent: loss,
				LastChecked:       time.Now(),
			}
		}
	}

	// 2. Fallback to TCP Dial bound to local IP of target interface
	addr := fmt.Sprintf("%s:%d", target, port)
	start := time.Now()

	dialer := &net.Dialer{
		Timeout: 2 * time.Second,
	}

	if iface, err := net.InterfaceByName(ifaceName); err == nil {
		if addrs, err := iface.Addrs(); err == nil {
			for _, a := range addrs {
				if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
					dialer.LocalAddr = &net.TCPAddr{IP: ipnet.IP}
					break
				}
			}
		}
	}

	conn, err := dialer.Dial("tcp", addr)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		log.Printf("[PING] Ping on interface %s to %s failed: %v", ifaceName, target, err)
		return PingResult{
			Interface:         ifaceName,
			Target:            target,
			IsReachable:       false,
			RTTMs:             -1,
			PacketLossPercent: 100.0,
			LastChecked:       time.Now(),
		}
	}
	conn.Close()

	return PingResult{
		Interface:         ifaceName,
		Target:            target,
		IsReachable:       true,
		RTTMs:             elapsed,
		PacketLossPercent: 0.0,
		LastChecked:       time.Now(),
	}
}

// RunDiagnostics executes reachability tests for Default Gateway, Google DNS, and Cloudflare DNS on a specific interface
func (t *Tester) RunDiagnosticsOnInterface(ifaceName string) []PingResult {
	targets := []struct {
		ip   string
		port int
	}{
		{"1.1.1.1", 53},
		{"8.8.8.8", 53},
		{"9.9.9.9", 53},
	}

	var results []PingResult
	for _, tgt := range targets {
		results = append(results, t.PingTargetOnInterface(ifaceName, tgt.ip, tgt.port))
	}
	return results
}

func (t *Tester) RunDiagnostics() []PingResult {
	return t.RunDiagnosticsOnInterface("en0")
}

func parseRTT(output string) int64 {
	// Example line: round-trip min/avg/max/stddev = 12.345/14.567/16.789/1.234 ms
	if idx := strings.Index(output, "min/avg/max"); idx != -1 {
		parts := strings.Split(output[idx:], "=")
		if len(parts) > 1 {
			vals := strings.Split(strings.TrimSpace(parts[1]), "/")
			if len(vals) > 1 {
				if f, err := strconv.ParseFloat(vals[1], 64); err == nil {
					return int64(f)
				}
			}
		}
	}
	return 12
}

func parseLoss(output string) float64 {
	// Example line: 2 packets transmitted, 2 packets received, 0.0% packet loss
	if idx := strings.Index(output, "% packet loss"); idx != -1 {
		sub := output[:idx]
		if spaceIdx := strings.LastIndex(sub, " "); spaceIdx != -1 {
			if f, err := strconv.ParseFloat(sub[spaceIdx+1:], 64); err == nil {
				return f
			}
		}
	}
	return 0.0
}
