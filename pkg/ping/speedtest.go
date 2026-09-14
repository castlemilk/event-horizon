package ping

import (
 "bytes"
 "context"
 "crypto/rand"
 "errors"
 "fmt"
 "io"
 "math"
 "net/http"
 "sync"
 "sync/atomic"
 "time"
)

type SpeedTestResult struct {
 Phase string `json:"phase"`
 ProgressPercent float64 `json:"progress_percent"`
 DownloadMbps float64 `json:"download_mbps"`
 UploadMbps float64 `json:"upload_mbps"`
 PingMs int64 `json:"ping_ms"`
 JitterMs float64 `json:"jitter_ms"`
 BytesReceived int64 `json:"bytes_received"`
 BytesSent int64 `json:"bytes_sent"`
 Interface string `json:"interface"`
 Server string `json:"server"`
 Timestamp time.Time `json:"timestamp"`
 IsRunning bool `json:"is_running"`
 Error string `json:"error,omitempty"`
 SourceIP string `json:"source_ip,omitempty"`
}

type speedTestConfig struct { baseURL, latencyTarget string; phaseDuration time.Duration }
type SpeedTester struct {
 mu sync.RWMutex
 current SpeedTestResult
 cancelFunc context.CancelFunc
 config speedTestConfig
}

var ErrTestRunning = errors.New("a speedtest is already running")
var globalSpeedTester = &SpeedTester{current: SpeedTestResult{Phase: "idle", Server: "Cloudflare Edge", PingMs: -1, JitterMs: -1, Timestamp: time.Now()}}
func GetSpeedTester() *SpeedTester { return globalSpeedTester }

func (s *SpeedTester) GetStatus() SpeedTestResult {
 s.mu.RLock(); defer s.mu.RUnlock()
 return s.current
}

func (s *SpeedTester) StartTest(ifaceName string) error {
 s.mu.Lock(); defer s.mu.Unlock()
 if s.current.IsRunning { return ErrTestRunning }
 s.current = SpeedTestResult{Phase: "error", Interface: ifaceName, Server: "Cloudflare Edge", PingMs: -1, JitterMs: -1, Timestamp: time.Now()}
 binding, err := resolveInterface(ifaceName)
 if err != nil { s.current.Error = err.Error(); return err }
 config := s.config
 if config.baseURL == "" { config.baseURL = "https://speed.cloudflare.com" }
 if config.latencyTarget == "" { config.latencyTarget = "1.1.1.1" }
 if config.phaseDuration <= 0 { config.phaseDuration = 5*time.Second }
 ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
 s.cancelFunc = cancel
 s.current.Phase = "ping"
 s.current.SourceIP = binding.ip.String()
 s.current.IsRunning = true
 go s.runPipeline(ctx, cancel, binding, config)
 return nil
}

func (s *SpeedTester) Cancel() {
 s.mu.Lock(); defer s.mu.Unlock()
 if s.cancelFunc != nil { s.cancelFunc() }
}

func (s *SpeedTester) runPipeline(ctx context.Context, cancel context.CancelFunc, binding interfaceBinding, config speedTestConfig) {
 var pipelineErr error
 defer func() {
  s.mu.Lock(); defer s.mu.Unlock()
  switch {
  case errors.Is(ctx.Err(), context.Canceled): s.current.Phase = "cancelled"; s.current.Error = "Speed test cancelled"
  case ctx.Err() != nil: s.current.Phase = "error"; s.current.Error = "Speed test timed out"
  case pipelineErr != nil: s.current.Phase = "error"; s.current.Error = pipelineErr.Error()
  default: s.current.Phase = "complete"; s.current.ProgressPercent = 100
  }
  s.current.IsRunning = false
  s.current.Timestamp = time.Now()
  s.cancelFunc = nil
  cancel()
 }()

 var samples []int64
 for i := 0; i < 3 && ctx.Err() == nil; i++ {
  result := NewTester().PingTargetOnInterfaceContext(ctx, binding.iface.Name, config.latencyTarget, 53)
  if result.IsReachable && result.RTTMs >= 0 { samples = append(samples, result.RTTMs) }
 }
 if ctx.Err() != nil { return }
 avg, jitter := latencyStatistics(samples)
 s.mu.Lock(); s.current.PingMs = avg; s.current.JitterMs = jitter; s.mu.Unlock()

 transport := &http.Transport{DialContext: binding.dialContext, MaxIdleConnsPerHost: 3, DisableCompression: true, TLSHandshakeTimeout: 3*time.Second, ResponseHeaderTimeout: 5*time.Second}
 defer transport.CloseIdleConnections()
 client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
 if pipelineErr = s.transferPhase(ctx, client, config, false); pipelineErr != nil || ctx.Err() != nil { return }
 pipelineErr = s.transferPhase(ctx, client, config, true)
}

func latencyStatistics(samples []int64) (int64, float64) {
 if len(samples) == 0 { return -1, -1 }
 var sum int64
 var difference float64
 for i, sample := range samples { sum += sample; if i > 0 { difference += math.Abs(float64(sample-samples[i-1])) } }
 jitter := -1.0
 if len(samples) > 1 { jitter = difference / float64(len(samples)-1) }
 return sum/int64(len(samples)), jitter
}

func (s *SpeedTester) transferPhase(ctx context.Context, client *http.Client, config speedTestConfig, upload bool) error {
 phase, initial, span := "download", 10.0, 45.0
 if upload { phase, initial, span = "upload", 55, 44 }
 s.mu.Lock(); s.current.Phase = phase; s.current.ProgressPercent = initial; s.mu.Unlock()
 phaseCtx, cancel := context.WithTimeout(ctx, config.phaseDuration)
 defer cancel()
 start := time.Now()
 var transferred atomic.Int64
 var wg sync.WaitGroup
 workerErrors := make(chan error, 3)
 for i := 0; i < 3; i++ {
  wg.Add(1)
  go func() {
   defer wg.Done()
   var payload []byte
   if upload {
    payload = make([]byte, 1024*1024)
    if _, err := rand.Read(payload); err != nil { workerErrors <- err; return }
   }
   buffer := make([]byte, 64*1024)
   for phaseCtx.Err() == nil {
    method, url := http.MethodGet, config.baseURL+"/__down?bytes=10000000"
    var body io.Reader
    if upload { method, url, body = http.MethodPost, config.baseURL+"/__up", bytes.NewReader(payload) }
    req, err := http.NewRequestWithContext(phaseCtx, method, url, body)
    if err != nil { workerErrors <- err; return }
    req.Header.Set("Cache-Control", "no-store")
    req.Header.Set("Content-Type", "application/octet-stream")
    resp, err := client.Do(req)
    if err != nil { if phaseCtx.Err() == nil { workerErrors <- err }; return }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
     resp.Body.Close(); workerErrors <- fmt.Errorf("%s server returned HTTP %d", phase, resp.StatusCode); return
    }
    if upload {
     _, err = io.Copy(io.Discard, resp.Body)
     // Count only payloads acknowledged by a successful HTTP response.
     if err == nil { transferred.Add(int64(len(payload))) }
    } else {
     for {
      var n int
      n, err = resp.Body.Read(buffer)
      transferred.Add(int64(n))
      if err != nil { break }
     }
    }
    resp.Body.Close()
    if err != nil && err != io.EOF { if phaseCtx.Err() == nil { workerErrors <- err }; return }
   }
  }()
 }
 done := make(chan struct{})
 go func() { wg.Wait(); close(done) }()
 ticker := time.NewTicker(200*time.Millisecond)
 defer ticker.Stop()
 update := func() {
  elapsed := time.Since(start).Seconds()
  count := transferred.Load()
  mbps := float64(count)*8 / math.Max(elapsed, 0.000001) / 1e6
  s.mu.Lock(); defer s.mu.Unlock()
  if upload { s.current.UploadMbps = mbps; s.current.BytesSent = count } else { s.current.DownloadMbps = mbps; s.current.BytesReceived = count }
  s.current.ProgressPercent = initial + math.Min(1, elapsed/config.phaseDuration.Seconds())*span
 }
 var phaseErr error
 running := true
 for running {
  select {
  case <-phaseCtx.Done(): running = false
  case err := <-workerErrors: phaseErr = err; cancel(); running = false
  case <-done: running = false
  case <-ticker.C: update()
  }
 }
 cancel()
 <-done
 update()
 if ctx.Err() != nil { return ctx.Err() }
 if phaseErr == nil { select { case phaseErr = <-workerErrors: default: } }
 if phaseErr != nil { return fmt.Errorf("%s failed on %s: %w", phase, s.GetStatus().Interface, phaseErr) }
 if transferred.Load() == 0 { return fmt.Errorf("%s transferred no confirmed data on %s", phase, s.GetStatus().Interface) }
 return nil
}

// Kept for callers of the old API; results contain measured values only.
func RunSpeedTestOnInterface(iface string) SpeedTestResult {
 st := GetSpeedTester()
 _ = st.StartTest(iface)
 return st.GetStatus()
}
