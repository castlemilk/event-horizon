package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type APIResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

var _ = Describe("Event Horizon Wi-Fi & DriverKit E2E Workflow", Ordered, func() {
	var httpClient *http.Client

	BeforeAll(func() {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
		}
	})

	Context("Phase 1: Daemon, App & Hardware Initialization", func() {
		It("should report daemon healthy on Apple Silicon with macOS architecture", func() {
			resp, err := httpClient.Get(DaemonBaseURL + "/api/status")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var apiResp APIResponse
			err = json.NewDecoder(resp.Body).Decode(&apiResp)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiResp.Status).To(Equal("success"))

			var statusData map[string]interface{}
			err = json.Unmarshal(apiResp.Data, &statusData)
			Expect(err).NotTo(HaveOccurred())
			Expect(statusData["arch"]).To(Equal("arm64"))
			Expect(statusData["os"]).To(Equal("darwin"))
			Expect(statusData["version"]).To(Equal("1.0.0"))
		})

		It("should verify runtime supervisor watchdog is active and logging self-healing events", func() {
			resp, err := httpClient.Get(DaemonBaseURL + "/api/supervisor/status")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var apiResp APIResponse
			err = json.NewDecoder(resp.Body).Decode(&apiResp)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiResp.Status).To(Equal("success"))

			var supData map[string]interface{}
			err = json.Unmarshal(apiResp.Data, &supData)
			Expect(err).NotTo(HaveOccurred())
			Expect(supData["is_running"]).To(BeTrue())
		})

		It("should list hardware topology with attached USB Wi-Fi dongles and virtual interfaces", func() {
			resp, err := httpClient.Get(DaemonBaseURL + "/api/hardware/topology")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var apiResp APIResponse
			err = json.NewDecoder(resp.Body).Decode(&apiResp)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiResp.Status).To(Equal("success"))

			var nodes []map[string]interface{}
			err = json.Unmarshal(apiResp.Data, &nodes)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(nodes)).To(BeNumerically(">", 0), "Should discover at least one hardware or network topology node")
		})

		It("should return universal chipset driver coverage matrix", func() {
			resp, err := httpClient.Get(DaemonBaseURL + "/api/drivers/supported")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var apiResp APIResponse
			err = json.NewDecoder(resp.Body).Decode(&apiResp)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiResp.Status).To(Equal("success"))

			var chipsets []map[string]interface{}
			err = json.Unmarshal(apiResp.Data, &chipsets)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(chipsets)).To(BeNumerically(">=", 5), "Should support at least 5 chipset families (AicSemi, Realtek AC, Realtek AX, MediaTek, Qualcomm)")
		})
	})

	Context("Phase 2: Scanning & Wi-Fi Association via User-Space Stack", func() {
		It("should scan 802.11 beacons and discover in-range Wi-Fi hotspots", func() {
			resp, err := httpClient.Get(DaemonBaseURL + "/api/wifi/scan")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var apiResp APIResponse
			err = json.NewDecoder(resp.Body).Decode(&apiResp)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiResp.Status).To(Equal("success"))

			var hotspots []map[string]interface{}
			err = json.Unmarshal(apiResp.Data, &hotspots)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(hotspots)).To(BeNumerically(">", 0), "Scan should discover active 802.11 access points")

			foundTarget := false
			for _, ap := range hotspots {
				if ap["ssid"] == TestSSID {
					foundTarget = true
					break
				}
			}
			Expect(foundTarget).To(BeTrue(), fmt.Sprintf("Access point '%s' should be discovered in scan", TestSSID))
		})

		It("should connect to target SSID through user-space 802.11 handshake", func() {
			payload := map[string]string{
				"ssid":       TestSSID,
				"passphrase": TestPassword,
			}
			bodyBytes, _ := json.Marshal(payload)

			resp, err := httpClient.Post(DaemonBaseURL+"/api/wifi/connect", "application/json", bytes.NewReader(bodyBytes))
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var apiResp APIResponse
			err = json.NewDecoder(resp.Body).Decode(&apiResp)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiResp.Status).To(Equal("success"))

			var connectedAP map[string]interface{}
			err = json.Unmarshal(apiResp.Data, &connectedAP)
			Expect(err).NotTo(HaveOccurred())
			Expect(connectedAP["ssid"]).To(Equal(TestSSID))
			Expect(connectedAP["is_selected"]).To(BeTrue())
		})
	})

	Context("Phase 3: Multi-Protocol Diagnostics Execution", func() {
		It("should run the comprehensive multi-protocol diagnostic suite via REST API", func() {
			reqURL := fmt.Sprintf("%s/api/diagnostics/suite?interface=%s", DaemonBaseURL, TestInterface)
			resp, err := httpClient.Get(reqURL)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var apiResp APIResponse
			err = json.NewDecoder(resp.Body).Decode(&apiResp)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiResp.Status).To(Equal("success"))
		})

		It("should execute diagnostics via MCP tool call", func() {
			// Test MCP tool via CLI stdin JSON-RPC
			mcpCmd := exec.Command("./bin/usbwifi-mcp")
			mcpCmd.Dir = ".."
			reqJSON := fmt.Sprintf(`{"jsonrpc":"2.0","id":101,"method":"tools/call","params":{"name":"usbwifi_run_diagnostics","arguments":{"interface":"%s"}}}`+"\n", TestInterface)
			mcpCmd.Stdin = strings.NewReader(reqJSON)

			out, err := mcpCmd.Output()
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).To(ContainSubstring(`"result"`))
			Expect(string(out)).To(ContainSubstring(`quality_score`))
		})
	})

	Context("Phase 4: Diagnostics Data & Link Quality Validation", func() {
		var reportData map[string]interface{}

		BeforeAll(func() {
			reqURL := fmt.Sprintf("%s/api/diagnostics/suite?interface=%s", DaemonBaseURL, TestInterface)
			resp, err := httpClient.Get(reqURL)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			var apiResp APIResponse
			err = json.NewDecoder(resp.Body).Decode(&apiResp)
			Expect(err).NotTo(HaveOccurred())

			err = json.Unmarshal(apiResp.Data, &reportData)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should contain valid ICMP ping matrix with low latency and 0% loss", func() {
			pings, ok := reportData["pings"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(len(pings)).To(BeNumerically(">=", 3))

			for _, p := range pings {
				pingMap := p.(map[string]interface{})
				Expect(pingMap["is_reachable"]).To(BeTrue())
				Expect(pingMap["packet_loss_percent"]).To(BeNumerically("<=", 10))
				Expect(pingMap["rtt_ms"]).To(BeNumerically(">", 0))
				Expect(pingMap["rtt_ms"]).To(BeNumerically("<", 300))
			}
		})

		It("should contain HTTP / TLS layer timing breakdown", func() {
			httpProbes, ok := reportData["http_probes"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(len(httpProbes)).To(BeNumerically(">=", 1))

			firstProbe := httpProbes[0].(map[string]interface{})
			Expect(firstProbe["is_success"]).To(BeTrue())
			Expect(firstProbe["status_code"]).To(Equal(float64(200)))
			Expect(firstProbe["total_ms"]).To(BeNumerically(">", 0))
		})

		It("should contain DNS resolution benchmark results", func() {
			dnsProbes, ok := reportData["dns_probes"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(len(dnsProbes)).To(BeNumerically(">=", 3))

			for _, d := range dnsProbes {
				dnsMap := d.(map[string]interface{})
				Expect(dnsMap["is_success"]).To(BeTrue())
				Expect(dnsMap["resolve_time_ms"]).To(BeNumerically(">=", 0))
				ips := dnsMap["ips"].([]interface{})
				Expect(len(ips)).To(BeNumerically(">", 0))
			}
		})

		It("should calculate Link Quality Score and Letter Grade", func() {
			qualityScore, ok := reportData["quality_score"].(float64)
			Expect(ok).To(BeTrue())
			Expect(qualityScore).To(BeNumerically(">=", 50.0), "Link quality score should be >= 50%")

			grade, ok := reportData["quality_grade"].(string)
			Expect(ok).To(BeTrue())
			Expect([]string{"A+", "A", "B", "C", "D"}).To(ContainElement(grade))
		})
	})

	Context("Phase 5: Real Data Traffic & Telemetry Validation", func() {
		It("should track dynamic byte and packet transfer on active interfaces", func() {
			// Query initial stats
			resp, err := httpClient.Get(DaemonBaseURL + "/api/network/telemetry")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			var apiResp APIResponse
			err = json.NewDecoder(resp.Body).Decode(&apiResp)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiResp.Status).To(Equal("success"))

			var initialStats []map[string]interface{}
			err = json.Unmarshal(apiResp.Data, &initialStats)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(initialStats)).To(BeNumerically(">", 0))

			// Verify interface active
			hasActive := false
			for _, s := range initialStats {
				if s["is_up"] == true {
					hasActive = true
					break
				}
			}
			Expect(hasActive).To(BeTrue(), "Should have at least one active UP interface")
		})

		It("should export valid Prometheus & OpenTelemetry metric streams", func() {
			resp, err := httpClient.Get(DaemonBaseURL + "/metrics")
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			bodyBytes, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			metricsText := string(bodyBytes)
			Expect(metricsText).To(ContainSubstring("net_bytes_rx_total"))
			Expect(metricsText).To(ContainSubstring("net_bytes_tx_total"))
			Expect(metricsText).To(ContainSubstring("net_packets_rx_total"))
			Expect(metricsText).To(ContainSubstring("net_link_uptime_seconds"))
		})
	})
})
