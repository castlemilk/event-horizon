package usb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stubTopologyCommands(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	commands := map[string]string{
		"networksetup": `#!/bin/sh
case "$1" in
-listallhardwareports)
cat <<'PORTS'
Hardware Port: Wi-Fi
Device: en0
Ethernet Address: aa:bb:cc:dd:ee:ff

Hardware Port: USB Wi-Fi
Device: en9
Ethernet Address: 11:22:33:44:55:66
PORTS
;;
-getairportnetwork) echo 'You are not associated with an AirPort network.' ;;
-listpreferredwirelessnetworks) echo 'Preferred networks on en0:'; echo '    Saved But Disconnected' ;;
esac
`,
		"ipconfig": "#!/bin/sh\necho 'SSID : <redacted>'\n",
		"netstat":  "#!/bin/sh\nexit 0\n",
		"ifconfig": "#!/bin/sh\nexit 0\n",
	}
	for name, content := range commands {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestWiFiSSIDNeverUsesPreferredNetworkAsAssociation(t *testing.T) {
	stubTopologyCommands(t)
	if got := wifiSSID("en0"); got != "" {
		t.Fatalf("redacted/disconnected Wi-Fi reported saved network %q as connected", got)
	}
}

func TestHardwarePortsPreserveMACAndIdentifyUSBWiFi(t *testing.T) {
	stubTopologyCommands(t)
	ports := enumerateHardwarePorts()
	if ports["en0"].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("lost hardware MAC: %+v", ports["en0"])
	}
	if !ports["en9"].IsUSB || !ports["en9"].IsWiFi {
		t.Errorf("USB Wi-Fi misclassified: %+v", ports["en9"])
	}
}

func TestDongleTopologyDoesNotInventConnectivity(t *testing.T) {
	stubTopologyCommands(t)
	t.Setenv("HOME", t.TempDir())
	InvalidateLinkStateCache()
	resetDongleCache()
	dongleCacheMu.Lock()
	dongleCacheDevs = []DeviceInfo{
		{VendorID: VendorAicWlan, ProductID: ProductAicOperational, Name: "test dongle", Serial: "NOTAMAC1234", IsWlan: true},
	}
	dongleCacheAt, dongleCacheOK = time.Now(), true
	dongleCacheMu.Unlock()
	t.Cleanup(resetDongleCache)
	SetDongleConnected("stale simulated SSID")
	t.Cleanup(func() { SetDongleConnected("") })
	found := false
	for _, node := range GetHardwareTopology() {
		if node.VendorID != "0xa69c" {
			continue
		}
		found = true
		if node.BSDInterface != "" || node.IPAddress != "" || node.Gateway != "" || node.MACAddress != "" || node.NetworkTarget != "" {
			t.Errorf("unassociated dongle invented live addressing: %+v", node)
		}
		if strings.Contains(node.Status, "Connected") {
			t.Errorf("unassociated dongle reports connected: %s", node.Status)
		}
	}
	if !found {
		t.Fatal("dongle missing from topology")
	}
}
