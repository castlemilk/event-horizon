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
	TestSSID      = ""
	TestPassword  = ""
	TestInterface = ""
)

func TestE2E(t *testing.T) {
	if os.Getenv("EH_HARDWARE_E2E") != "1" {
		t.Skip("Hardware association tests require EH_HARDWARE_E2E=1, WIFI_SSID and WIFI_INTERFACE; they change the live connection")
	}
	if os.Getenv("WIFI_SSID") == "" || os.Getenv("WIFI_INTERFACE") == "" {
		t.Fatal("Set WIFI_SSID and WIFI_INTERFACE explicitly so this test cannot use the wrong radio")
	}
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
