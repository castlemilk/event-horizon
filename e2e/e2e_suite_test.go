package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	DaemonBaseURL = "http://127.0.0.1:8990"
	TestSSID      = "Starlink"
	TestPassword  = "lemon123"
	TestInterface = "en0"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Event Horizon End-to-End Test Suite")
}

var _ = BeforeSuite(func() {
	if envURL := os.Getenv("DAEMON_URL"); envURL != "" {
		DaemonBaseURL = envURL
	}
	if envSSID := os.Getenv("WIFI_SSID"); envSSID != "" {
		TestSSID = envSSID
	}
	if envPass := os.Getenv("WIFI_PASSWORD"); envPass != "" {
		TestPassword = envPass
	}
	if envIface := os.Getenv("WIFI_INTERFACE"); envIface != "" {
		TestInterface = envIface
	}

	// Verify daemon reachability
	Eventually(func() error {
		resp, err := http.Get(DaemonBaseURL + "/api/status")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
		return nil
	}, 10*time.Second, 1*time.Second).Should(Succeed(), "USB Wi-Fi daemon must be running at "+DaemonBaseURL)
})
