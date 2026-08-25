package ping

import (
	"context"
	"crypto/tls"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
)

// HTTPProbeResult represents detailed timing metrics for an HTTP/HTTPS endpoint probe.
type HTTPProbeResult struct {
	Target         string `json:"target"`
	URL            string `json:"url"`
	StatusCode     int    `json:"status_code"`
	DNSLookupMs    int64  `json:"dns_lookup_ms"`
	TCPHandshakeMs int64  `json:"tcp_handshake_ms"`
	TLSHandshakeMs int64  `json:"tls_handshake_ms"`
	TTFBMs         int64  `json:"ttfb_ms"`
	TotalMs        int64  `json:"total_ms"`
	IsSuccess      bool   `json:"is_success"`
	Protocol       string `json:"protocol"`
}

// DNSProbeResult represents the timing and record resolution details for a domain query.
type DNSProbeResult struct {
	Domain        string   `json:"domain"`
	ResolveTimeMs int64    `json:"resolve_time_ms"`
	IPs           []string `json:"ips"`
	IsSuccess     bool     `json:"is_success"`
	Server        string   `json:"server"`
}

// DiagnosticSuiteReport aggregates ping, HTTP, DNS, jitter, and link score telemetry.
type DiagnosticSuiteReport struct {
	Interface         string            `json:"interface"`
	LocalIP           string            `json:"local_ip"`
	Gateway           string            `json:"gateway"`
	Pings             []PingResult      `json:"pings"`
	HTTPProbes        []HTTPProbeResult `json:"http_probes"`
	DNSProbes         []DNSProbeResult  `json:"dns_probes"`
	JitterMs          float64           `json:"jitter_ms"`
	AvgLatencyMs      float64           `json:"avg_latency_ms"`
	MinLatencyMs      int64             `json:"min_latency_ms"`
	MaxLatencyMs      int64             `json:"max_latency_ms"`
	PacketLossPercent float64           `json:"packet_loss_percent"`
	QualityScore      float64           `json:"quality_score"`
	QualityGrade      string            `json:"quality_grade"`
	Timestamp         time.Time         `json:"timestamp"`
}

// ProbeHTTP executes an HTTP/HTTPS trace measuring DNS, TCP, TLS, TTFB, and Total latency.
func (t *Tester) ProbeHTTP(targetName, targetURL, ifaceName string) HTTPProbeResult {
	var dnsStart, dnsDone time.Time
	var connStart, connDone time.Time
	var tlsStart, tlsDone time.Time
	var ttfbDone time.Time

	req, err := http.NewRequestWithContext(context.Background(), "GET", targetURL, nil)
	if err != nil {
		return HTTPProbeResult{
			Target:    targetName,
			URL:       targetURL,
			IsSuccess: false,
		}
	}

	trace := &httptrace.ClientTrace{
		DNSStart:             func(_ httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:              func(_ httptrace.DNSDoneInfo) { dnsDone = time.Now() },
		ConnectStart:         func(_, _ string) { connStart = time.Now() },
		ConnectDone:          func(_, _ string, _ error) { connDone = time.Now() },
		TLSHandshakeStart:    func() { tlsStart = time.Now() },
		TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { tlsDone = time.Now() },
		GotFirstResponseByte: func() { ttfbDone = time.Now() },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	// Bind to interface if available
	var localIP string
	if ifaceName != "" {
		if iface, err := net.InterfaceByName(ifaceName); err == nil {
			if addrs, err := iface.Addrs(); err == nil {
				for _, a := range addrs {
					if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
						localIP = ipnet.IP.String()
						break
					}
				}
			}
		}
	}

	dialer := &net.Dialer{
		Timeout: 3 * time.Second,
	}
	if localIP != "" {
		dialer.LocalAddr = &net.TCPAddr{IP: net.ParseIP(localIP)}
	}

	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, // Allow local dish self-signed
		DisableKeepAlives:   true,
		MaxIdleConns:        1,
		IdleConnTimeout:     3 * time.Second,
		TLSHandshakeTimeout: 3 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   4 * time.Second,
	}

	totalStart := time.Now()
	resp, err := client.Do(req)
	totalMs := time.Since(totalStart).Milliseconds()

	if err != nil {
		return HTTPProbeResult{
			Target:    targetName,
			URL:       targetURL,
			TotalMs:   totalMs,
			IsSuccess: false,
		}
	}
	defer resp.Body.Close()

	var dnsMs, tcpMs, tlsMs, ttfbMs int64
	if !dnsStart.IsZero() && !dnsDone.IsZero() {
		dnsMs = dnsDone.Sub(dnsStart).Milliseconds()
	}
	if !connStart.IsZero() && !connDone.IsZero() {
		tcpMs = connDone.Sub(connStart).Milliseconds()
	}
	if !tlsStart.IsZero() && !tlsDone.IsZero() {
		tlsMs = tlsDone.Sub(tlsStart).Milliseconds()
	}
	if !connDone.IsZero() && !ttfbDone.IsZero() {
		ttfbMs = ttfbDone.Sub(connDone).Milliseconds()
	}

	proto := resp.Proto
	if proto == "" {
		proto = "HTTP/1.1"
	}

	return HTTPProbeResult{
		Target:         targetName,
		URL:            targetURL,
		StatusCode:     resp.StatusCode,
		DNSLookupMs:    dnsMs,
		TCPHandshakeMs: tcpMs,
		TLSHandshakeMs: tlsMs,
		TTFBMs:         ttfbMs,
		TotalMs:        totalMs,
		IsSuccess:      resp.StatusCode >= 200 && resp.StatusCode < 400,
		Protocol:       proto,
	}
}

// ProbeDNS measures name resolution speed and record count for a domain.
func (t *Tester) ProbeDNS(domain, server string) DNSProbeResult {
	start := time.Now()
	ips, err := net.LookupHost(domain)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		return DNSProbeResult{
			Domain:        domain,
			ResolveTimeMs: elapsed,
			IPs:           []string{},
			IsSuccess:     false,
			Server:        server,
		}
	}

	return DNSProbeResult{
		Domain:        domain,
		ResolveTimeMs: elapsed,
		IPs:           ips,
		IsSuccess:     len(ips) > 0,
		Server:        server,
	}
}

// RunDiagnosticSuite performs complete ICMP, HTTP, and DNS diagnostics with link analytics.
func (t *Tester) RunDiagnosticSuite(ifaceName string) DiagnosticSuiteReport {
	if ifaceName == "" {
		ifaceName = "en0"
	}

	var localIP string
	if iface, err := net.InterfaceByName(ifaceName); err == nil {
		if addrs, err := iface.Addrs(); err == nil {
			for _, a := range addrs {
				if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
					localIP = ipnet.IP.String()
					break
				}
			}
		}
	}

	// 1. ICMP Ping Targets
	pings := t.RunDiagnosticsOnInterface(ifaceName)

	// Add Gateway / Dish ping if localIP is available
	gatewayIP := "192.168.0.1"
	if strings.HasPrefix(localIP, "192.168.4.") {
		gatewayIP = "192.168.4.1"
	} else if strings.HasPrefix(localIP, "192.168.100.") {
		gatewayIP = "192.168.100.1"
	}
	pings = append([]PingResult{t.PingTargetOnInterface(ifaceName, gatewayIP, 53)}, pings...)

	// 2. HTTP Probes
	httpTargets := []struct {
		name string
		url  string
	}{
		{"Cloudflare Edge", "https://1.1.1.1"},
		{"Google Web Index", "https://www.google.com"},
		{"Starlink Dish Core", "http://192.168.100.1"},
	}
	var httpProbes []HTTPProbeResult
	for _, target := range httpTargets {
		httpProbes = append(httpProbes, t.ProbeHTTP(target.name, target.url, ifaceName))
	}

	// 3. DNS Resolution Probes
	dnsDomains := []string{"cloudflare.com", "starlink.com", "google.com", "apple.com"}
	var dnsProbes []DNSProbeResult
	for _, domain := range dnsDomains {
		dnsProbes = append(dnsProbes, t.ProbeDNS(domain, "System DNS"))
	}

	// 4. Calculate Analytics (Jitter, Min/Avg/Max, Quality Score & Grade)
	var latencies []float64
	var minLat int64 = math.MaxInt64
	var maxLat int64 = 0
	var totalLat float64
	var successCount int
	var totalPackets = len(pings)

	for _, p := range pings {
		if p.IsReachable && p.RTTMs >= 0 {
			latencies = append(latencies, float64(p.RTTMs))
			totalLat += float64(p.RTTMs)
			if p.RTTMs < minLat {
				minLat = p.RTTMs
			}
			if p.RTTMs > maxLat {
				maxLat = p.RTTMs
			}
			successCount++
		}
	}

	if minLat == math.MaxInt64 {
		minLat = 0
	}

	var avgLat float64
	if successCount > 0 {
		avgLat = totalLat / float64(successCount)
	}

	// Calculate Jitter (Mean Absolute Difference between consecutive samples)
	var jitter float64
	if len(latencies) > 1 {
		var diffSum float64
		for i := 1; i < len(latencies); i++ {
			diffSum += math.Abs(latencies[i] - latencies[i-1])
		}
		jitter = diffSum / float64(len(latencies)-1)
	}

	var lossPercent float64
	if totalPackets > 0 {
		lossPercent = float64(totalPackets-successCount) / float64(totalPackets) * 100.0
	}

	// Composite Quality Score (0 to 100)
	// Factors: Loss (-50 max), Latency (-30 max for >100ms), Jitter (-20 max for >30ms)
	score := 100.0 - (lossPercent * 0.5)
	if avgLat > 20 {
		score -= math.Min(30, (avgLat-20)*0.4)
	}
	if jitter > 5 {
		score -= math.Min(20, (jitter-5)*0.5)
	}
	if score < 0 {
		score = 0
	}
	score = math.Round(score*10) / 10

	var grade string
	switch {
	case score >= 95:
		grade = "A+"
	case score >= 85:
		grade = "A"
	case score >= 75:
		grade = "B"
	case score >= 60:
		grade = "C"
	case score >= 40:
		grade = "D"
	default:
		grade = "F"
	}

	return DiagnosticSuiteReport{
		Interface:         ifaceName,
		LocalIP:           localIP,
		Gateway:           gatewayIP,
		Pings:             pings,
		HTTPProbes:        httpProbes,
		DNSProbes:         dnsProbes,
		JitterMs:          math.Round(jitter*10) / 10,
		AvgLatencyMs:      math.Round(avgLat*10) / 10,
		MinLatencyMs:      minLat,
		MaxLatencyMs:      maxLat,
		PacketLossPercent: lossPercent,
		QualityScore:      score,
		QualityGrade:      grade,
		Timestamp:         time.Now(),
	}
}
