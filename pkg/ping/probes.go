package ping

import (
	"context"
	"crypto/tls"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
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
 Error string `json:"error,omitempty"`
}

// DNSProbeResult represents the timing and record resolution details for a domain query.
type DNSProbeResult struct {
	Domain        string   `json:"domain"`
	ResolveTimeMs int64    `json:"resolve_time_ms"`
	IPs           []string `json:"ips"`
	IsSuccess     bool     `json:"is_success"`
	Server        string   `json:"server"`
 Error string `json:"error,omitempty"`
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
 Error string `json:"error,omitempty"`
}

// ProbeHTTP executes an HTTP/HTTPS trace measuring DNS, TCP, TLS, TTFB, and Total latency.
func (t *Tester) ProbeHTTP(targetName, targetURL, ifaceName string) HTTPProbeResult {
 return t.ProbeHTTPContext(context.Background(), targetName, targetURL, ifaceName)
}

func (t *Tester) ProbeHTTPContext(ctx context.Context, targetName, targetURL, ifaceName string) HTTPProbeResult {
 binding, bindingErr := resolveInterface(ifaceName)
 if bindingErr != nil { return HTTPProbeResult{Target: targetName, URL: targetURL, Error: bindingErr.Error()} }
 var traceMu sync.Mutex
 record := func(target *time.Time) { traceMu.Lock(); *target = time.Now(); traceMu.Unlock() }
	var dnsStart, dnsDone time.Time
	var connStart, connDone time.Time
	var tlsStart, tlsDone time.Time
	var ttfbDone time.Time

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return HTTPProbeResult{
			Target:    targetName,
			URL:       targetURL,
			IsSuccess: false,
		}
	}

	trace := &httptrace.ClientTrace{
		DNSStart:             func(_ httptrace.DNSStartInfo) { record(&dnsStart) },
		DNSDone:              func(_ httptrace.DNSDoneInfo) { record(&dnsDone) },
		ConnectStart:         func(_, _ string) { record(&connStart) },
		ConnectDone:          func(_, _ string, _ error) { record(&connDone) },
		TLSHandshakeStart:    func() { record(&tlsStart) },
		TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { record(&tlsDone) },
		GotFirstResponseByte: func() { record(&ttfbDone) },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))


	transport := &http.Transport{
		DialContext:         binding.dialContext,
		DisableKeepAlives:   true,
		MaxIdleConns:        1,
		IdleConnTimeout:     3 * time.Second,
		TLSHandshakeTimeout: 3 * time.Second,
	}

 defer transport.CloseIdleConnections()
	client := &http.Client{
 CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
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

 traceMu.Lock()
 defer traceMu.Unlock()
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
		IsSuccess:      resp.StatusCode >= 200 && resp.StatusCode < 300,
		Protocol:       proto,
	}
}

// ProbeDNSOnInterface sends the DNS query to the declared server using the selected interface.
func (t *Tester) ProbeDNSOnInterface(ctx context.Context, domain, server, ifaceName string) DNSProbeResult {
 result := DNSProbeResult{Domain: domain, Server: server, IPs: []string{}}
 binding, err := resolveInterface(ifaceName)
 if err != nil { result.Error = err.Error(); return result }
 if net.ParseIP(server) == nil { result.Error = "DNS server must be an IP address"; return result }
 ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
 defer cancel()
 start := time.Now()
 ips, err := binding.resolver(server).LookupIP(ctx, "ip4", domain)
 result.ResolveTimeMs = time.Since(start).Milliseconds()
 if err != nil { result.Error = err.Error(); return result }
 for _, ip := range ips { result.IPs = append(result.IPs, ip.String()) }
 result.IsSuccess = len(result.IPs) > 0
 return result
}

func (t *Tester) RunDiagnosticSuite(ifaceName string) DiagnosticSuiteReport {
 return t.RunDiagnosticSuiteContext(context.Background(), ifaceName)
}

func (t *Tester) RunDiagnosticSuiteContext(ctx context.Context, ifaceName string) DiagnosticSuiteReport {
 binding, err := resolveInterface(ifaceName)
 if err != nil { return DiagnosticSuiteReport{Interface: ifaceName, Pings: []PingResult{}, HTTPProbes: []HTTPProbeResult{}, DNSProbes: []DNSProbeResult{}, JitterMs: -1, AvgLatencyMs: -1, MinLatencyMs: -1, MaxLatencyMs: -1, PacketLossPercent: 100, QualityGrade: "Unavailable", Timestamp: time.Now(), Error: err.Error()} }
 localIP := binding.ip.String()
 ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
 defer cancel()
 // No guessed gateway: interface subnets do not establish the router address.
 gatewayIP := ""
 var pings []PingResult
 httpTargets := []struct{name, url string}{{"Cloudflare Edge", "https://1.1.1.1"}, {"Google", "https://www.google.com"}}
 httpProbes := make([]HTTPProbeResult, len(httpTargets))
 dnsDomains := []string{"cloudflare.com", "google.com", "apple.com"}
 dnsProbes := make([]DNSProbeResult, len(dnsDomains))
 var wg sync.WaitGroup
 wg.Add(1)
 go func() { defer wg.Done(); pings = t.RunDiagnosticsOnInterfaceContext(ctx, ifaceName) }()
 for i, target := range httpTargets {
  wg.Add(1)
  go func(i int, name, url string) { defer wg.Done(); httpProbes[i] = t.ProbeHTTPContext(ctx, name, url, ifaceName) }(i, target.name, target.url)
 }
 for i, domain := range dnsDomains {
  wg.Add(1)
  go func(i int, domain string) { defer wg.Done(); dnsProbes[i] = t.ProbeDNSOnInterface(ctx, domain, "1.1.1.1", ifaceName) }(i, domain)
 }
 wg.Wait()

	// 4. Calculate Analytics (Jitter, Min/Avg/Max, Quality Score & Grade)
	
	var minLat int64 = math.MaxInt64
	var maxLat int64 = 0
	var totalLat float64
	var successCount int
	var totalPackets = len(pings)

	for _, p := range pings {
		if p.IsReachable && p.RTTMs >= 0 {
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
		minLat = -1
 maxLat = -1
	}

	avgLat := -1.0
	if successCount > 0 {
		avgLat = totalLat / float64(successCount)
	}

 // Different remote hosts are not consecutive latency samples from one path.
 jitter := -1.0
 var lossPercent float64
 for _, p := range pings { lossPercent += p.PacketLossPercent }
 if totalPackets > 0 { lossPercent /= float64(totalPackets) }

	// Composite Quality Score (0 to 100)
	// Factors: Loss (-50 max), Latency (-30 max for >100ms), Jitter (-20 max for >30ms)
	score := 100.0 - (lossPercent * 0.5)
	if avgLat > 20 {
		score -= math.Min(30, (avgLat-20)*0.4)
	}
	if jitter > 5 {
		score -= math.Min(20, (jitter-5)*0.5)
	}
 if successCount == 0 { score = 0 }
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
