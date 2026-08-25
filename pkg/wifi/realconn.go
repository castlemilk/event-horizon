package wifi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// coreWLANAssociateScript performs a real Wi-Fi association through CoreWLAN.
// It must run via the `swift` interpreter so CoreWLAN inherits the invoking
// shell's TCC/Location permission (compiled binaries get empty scans).
const coreWLANAssociateScript = `import CoreWLAN
import Foundation
let args = CommandLine.arguments
guard args.count >= 3 else { exit(2) }
let ssid = args[1]
let password = args[2]
let client = CWWiFiClient.shared()
guard let iface = client.interface() else { exit(3) }
do {
    let scan = try iface.scanForNetworks(withName: nil)
    guard let net = scan.first(where: { $0.ssid == ssid }) else { exit(4) }
    try iface.associate(to: net, password: password)
    Thread.sleep(forTimeInterval: 4)
    if iface.ssid() == ssid {
        print("CONNECTED " + ssid)
        exit(0)
    }
    exit(5)
} catch {
    exit(6)
}
`

// FindWiFiInterface returns the BSD interface name (e.g. "en0") of the active
// macOS Wi-Fi hardware port, or an error if no Wi-Fi port exists.
func FindWiFiInterface() (string, error) {
	out, err := exec.Command("networksetup", "-listallhardwareports").Output()
	if err != nil {
		return "", fmt.Errorf("networksetup -listallhardwareports: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "Hardware Port: Wi-Fi") {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			next := lines[j]
			if strings.HasPrefix(next, "Hardware Port") {
				break
			}
			if strings.HasPrefix(next, "Device:") {
				return strings.TrimSpace(strings.TrimPrefix(next, "Device:")), nil
			}
		}
	}
	return "", fmt.Errorf("no macOS Wi-Fi interface found")
}

// CurrentSSID returns the SSID the given interface is currently associated
// with. Returns an error when the interface is not associated with any network.
func CurrentSSID(iface string) (string, error) {
	// 1. Try ipconfig getsummary
	out, err := exec.Command("ipconfig", "getsummary", iface).Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "SSID :") {
				ssid := strings.TrimSpace(strings.TrimPrefix(line, "SSID :"))
				if ssid != "" && ssid != "<redacted>" && ssid != "<hidden>" {
					return ssid, nil
				}
			}
		}
	}

	// 2. Try networksetup -getairportnetwork
	out, err = exec.Command("networksetup", "-getairportnetwork", iface).Output()
	if err == nil {
		raw := strings.TrimSpace(string(out))
		const prefix = "Current Wi-Fi Network: "
		if strings.HasPrefix(raw, prefix) {
			ssid := strings.TrimPrefix(raw, prefix)
			if ssid != "" && ssid != "<redacted>" && ssid != "<hidden>" {
				return ssid, nil
			}
		}
	}

	// 3. Fallback: Preferred networks list
	prefOut, prefErr := exec.Command("networksetup", "-listpreferredwirelessnetworks", iface).Output()
	if prefErr == nil {
		for _, l := range strings.Split(string(prefOut), "\n") {
			candidate := strings.TrimSpace(l)
			if candidate == "" || strings.HasPrefix(candidate, "Preferred networks") {
				continue
			}
			return candidate, nil
		}
	}

	return "", fmt.Errorf("interface %s not associated", iface)
}

// AssociateToNetwork performs a real macOS Wi-Fi association to the given
// SSID on the specified interface using `networksetup -setairportnetwork`.
// It verifies the association took effect by reading back the current SSID.
func AssociateToNetwork(iface, ssid, passphrase string) error {
	args := []string{"-setairportnetwork", iface, ssid}
	if passphrase != "" {
		args = append(args, passphrase)
	}

	out, err := exec.Command("networksetup", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("association to %q failed: %v: %s",
			ssid, err, strings.TrimSpace(string(out)))
	}

	actual, verr := CurrentSSID(iface)
	if verr != nil {
		return fmt.Errorf("association to %q could not be confirmed: %v", ssid, verr)
	}
	if actual != ssid {
		return fmt.Errorf("association to %q not confirmed (currently on %q)", ssid, actual)
	}
	return nil
}

// AssociateViaCoreWLAN joins the given network through the CoreWLAN framework
// (swift interpreter). This is the reliable path for networks that
// `networksetup -setairportnetwork` rejects with keychain error -3925.
// Exit codes: 0 connected, 4 not found, 5 unconfirmed, else error.
// It must run as the console user, not as root, because CoreWLAN's
// CWWiFiClient.shared().interface() returns nil for root.
func AssociateViaCoreWLAN(ssid, passphrase string) error {
	script, err := writeTempScript("eh-corewlan-assoc", coreWLANAssociateScript)
	if err != nil {
		return fmt.Errorf("write assoc script: %w", err)
	}
	defer os.Remove(script)

	cmd := execCommandAsConsoleUser("swift", script, ssid, passphrase)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			switch ee.ExitCode() {
			case 4:
				return fmt.Errorf("network %q not found in scan", ssid)
			case 5:
				return fmt.Errorf("association to %q could not be confirmed", ssid)
			default:
				return fmt.Errorf("corewlan association failed (%d): %s", ee.ExitCode(), strings.TrimSpace(string(ee.Stderr)))
			}
		}
		return fmt.Errorf("corewlan association error: %w", err)
	}

	if strings.Contains(string(out), "CONNECTED "+ssid) {
		return nil
	}
	return fmt.Errorf("association to %q not confirmed: %s", ssid, strings.TrimSpace(string(out)))
}

// DisconnectViaCoreWLAN disconnects the Wi-Fi radio from its current network.
func DisconnectViaCoreWLAN() error {
	script := `import CoreWLAN
import Foundation
let client = CWWiFiClient.shared()
guard let iface = client.interface() else { exit(3) }
iface.disconnect()
exit(0)
`
	path, err := writeTempScript("eh-corewlan-disc", script)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	return execCommandAsConsoleUser("swift", path).Run()
}

func writeTempScript(prefix, body string) (string, error) {
	// Use /tmp directly — os.TempDir() may point to a per-user cache volume
	// that the root daemon cannot write to (e.g. /Volumes/.../tmp).
	candidates := []string{"/tmp", os.TempDir()}
	var lastErr error
	for _, dir := range candidates {
		path := filepath.Join(dir, fmt.Sprintf("%s-%d.swift", prefix, time.Now().UnixNano()))
		if err := os.WriteFile(path, []byte(body), 0o600); err == nil {
			return path, nil
		} else {
			lastErr = err
		}
	}
	return "", lastErr
}

func execCommandAsConsoleUser(name string, args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		user := os.Getenv("SUDO_USER")
		if user == "" {
			if out, err := exec.Command("stat", "-f", "%Su", "/dev/console").Output(); err == nil {
				user = strings.TrimSpace(string(out))
			}
		}
		if user != "" && user != "root" {
			allArgs := append([]string{"-u", user, name}, args...)
			return exec.Command("sudo", allArgs...)
		}
	}
	return exec.Command(name, args...)
}
