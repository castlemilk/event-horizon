package ping

import (
 "context"
 "fmt"
 "net"
 "os"
 "os/exec"
 "strconv"
 "strings"
 "sync"
 "time"
)

type PingResult struct {
 Interface string `json:"interface"`
 Target string `json:"target"`
 IsReachable bool `json:"is_reachable"`
 RTTMs int64 `json:"rtt_ms"`
 PacketLossPercent float64 `json:"packet_loss_percent"`
 LastChecked time.Time `json:"last_checked"`
 SourceIP string `json:"source_ip,omitempty"`
 Method string `json:"method"`
 Error string `json:"error,omitempty"`
}

type Tester struct{}
func NewTester() *Tester { return &Tester{} }

func (t *Tester) PingTargetOnInterface(ifaceName, target string, port int) PingResult {
 return t.PingTargetOnInterfaceContext(context.Background(), ifaceName, target, port)
}

// A TCP handshake is not an ICMP ping: preserve ICMP loss and failure rather
// than replacing a failed ping with a successful TCP connection.
func (t *Tester) PingTargetOnInterfaceContext(ctx context.Context, ifaceName, target string, port int) PingResult {
 result := PingResult{Interface: ifaceName, Target: target, RTTMs: -1, PacketLossPercent: 100, LastChecked: time.Now(), Method: "icmp"}
 binding, err := resolveInterface(ifaceName)
 if err != nil { result.Error = err.Error(); return result }
 result.SourceIP = binding.ip.String()
 ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
 defer cancel()
 if target == "" || strings.HasPrefix(target, "-") || strings.ContainsAny(target, " \t\r\n") { result.Error = "enter an IPv4 address or hostname"; return result }
 ip := net.ParseIP(target)
 if ip == nil {
  ips, lookupErr := binding.resolver("1.1.1.1").LookupIP(ctx, "ip4", target)
  if lookupErr != nil || len(ips) == 0 { result.Error = fmt.Sprintf("resolve %s on %s: %v", target, ifaceName, lookupErr); return result }
  ip = ips[0]
 }
 if ip.To4() == nil { result.Error = "IPv6 ping is not supported by this test"; return result }
 args := pingArguments(binding, ip.String())
 if len(args) == 0 { result.Error = "interface-bound ping is unsupported on this platform"; return result }
 cmd := exec.CommandContext(ctx, "ping", args...)
 cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
 out, err := cmd.CombinedOutput()
 result.LastChecked = time.Now()
 result.RTTMs = parseRTT(string(out))
 result.PacketLossPercent = parseLoss(string(out))
 if ctx.Err() != nil { result.Error = ctx.Err().Error(); return result }
 result.IsReachable = result.RTTMs >= 0 && result.PacketLossPercent < 100
 if !result.IsReachable {
  result.Error = "no ICMP replies received on " + ifaceName
  if err != nil { result.Error += ": " + err.Error() }
 }
 return result
}

func (t *Tester) RunDiagnosticsOnInterface(ifaceName string) []PingResult {
 return t.RunDiagnosticsOnInterfaceContext(context.Background(), ifaceName)
}

func (t *Tester) RunDiagnosticsOnInterfaceContext(ctx context.Context, ifaceName string) []PingResult {
 targets := []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}
 results := make([]PingResult, len(targets))
 var wg sync.WaitGroup
 for i, target := range targets {
  wg.Add(1)
  go func(i int, target string) { defer wg.Done(); results[i] = t.PingTargetOnInterfaceContext(ctx, ifaceName, target, 53) }(i, target)
 }
 wg.Wait()
 return results
}

func (t *Tester) RunDiagnostics() []PingResult { return t.RunDiagnosticsOnInterface("en0") }

func parseRTT(output string) int64 {
 for _, line := range strings.Split(output, "\n") {
  if strings.Contains(line, "min/avg/max") {
   parts := strings.SplitN(line, "=", 2)
   if len(parts) == 2 {
    values := strings.Split(strings.TrimSpace(parts[1]), "/")
    if len(values) > 1 { if f, err := strconv.ParseFloat(values[1], 64); err == nil && f >= 0 { return int64(f) } }
   }
  }
 }
 return -1
}

func parseLoss(output string) float64 {
 if idx := strings.Index(output, "% packet loss"); idx != -1 {
  fields := strings.Fields(output[:idx])
  if len(fields) > 0 { if f, err := strconv.ParseFloat(fields[len(fields)-1], 64); err == nil && f >= 0 && f <= 100 { return f } }
 }
 return 100
}
