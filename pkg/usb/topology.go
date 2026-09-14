package usb

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/castlemilk/event-horizon/pkg/driver"
)

type HardwareTopology struct {
	USBDriver     string `json:"usb_driver"`
	VendorID      string `json:"vendor_id"`
	ProductID     string `json:"product_id"`
	SerialNumber  string `json:"serial_number"`
	Speed         string `json:"speed"`
	BusPath       string `json:"bus_path,omitempty"`
	BSDInterface  string `json:"bsd_interface"`
	NetworkTarget string `json:"network_target"`
	IPAddress     string `json:"ip_address"`
	SubnetMask    string `json:"subnet_mask"`
	Gateway       string `json:"gateway"`
	MACAddress    string `json:"mac_address"`
	Status        string `json:"status"`
	DriverType    string `json:"driver_type"`
}

// hardwarePort describes a macOS hardware port from networksetup.
type hardwarePort struct {
	Name   string
	Device string
	MAC    string
	IsWiFi bool
	IsUSB  bool
}

// enumerateHardwarePorts reads `networksetup -listallhardwareports` to map BSD
// interfaces to their real hardware port names and types.
func enumerateHardwarePorts() map[string]hardwarePort {
	ports := make(map[string]hardwarePort)
	out, err := topologyCommand("networksetup", "-listallhardwareports")
	if err != nil {
		return ports
	}

	var current *hardwarePort
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Hardware Port:"):
			name := strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))
			current = &hardwarePort{Name: name, IsWiFi: strings.Contains(name, "Wi-Fi")}
			if strings.Contains(name, "USB") {
				current.IsUSB = true
			}
		case strings.HasPrefix(line, "Device:") && current != nil:
			dev := strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
			current.Device = dev
			ports[dev] = *current
		case strings.HasPrefix(line, "Ethernet Address:") && current != nil:
			current.MAC = strings.TrimSpace(strings.TrimPrefix(line, "Ethernet Address:"))
			if current.Device != "" {
				ports[current.Device] = *current
			}
		case line == "":
			current = nil
		}
	}
	return ports
}

// defaultGateways returns a map of interface -> gateway from the live routing
// table (default routes only).
func defaultGateways() map[string]string {
	routes := make(map[string]string)
	out, err := topologyCommand("netstat", "-rn", "-f", "inet")
	if err != nil {
		return routes
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "default" {
			continue
		}
		gateway := strings.TrimSuffix(fields[1], ".*")
		routes[fields[3]] = gateway
	}
	return routes
}

// ifaceIPv4 returns the interface's first IPv4 address and netmask (dotted).
func ifaceIPv4(iface net.Interface) (ip, mask string) {
	addrs, err := iface.Addrs()
	if err != nil {
		return "", ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.To4() == nil || ipNet.IP.IsLoopback() {
			continue
		}
		ones, bits := ipNet.Mask.Size()
		if ones > 0 && bits == 32 {
			return ipNet.IP.String(), net.IP(net.CIDRMask(ones, 32)).String()
		}
	}
	return "", ""
}

// wifiSSID returns the SSID the given interface is associated with, or "".
func wifiSSID(iface string) string {
	// 1. Try ipconfig getsummary
	out, err := topologyCommand("ipconfig", "getsummary", iface)
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "SSID :") {
				raw := strings.TrimSpace(strings.TrimPrefix(line, "SSID :"))
				if raw != "" && raw != "<redacted>" && raw != "<hidden>" {
					return raw
				}
			}
		}
	}

	// 2. Try networksetup -getairportnetwork
	out, err = topologyCommand("networksetup", "-getairportnetwork", iface)
	if err == nil {
		raw := strings.TrimSpace(string(out))
		const prefix = "Current Wi-Fi Network: "
		if strings.HasPrefix(raw, prefix) {
			val := strings.TrimPrefix(raw, prefix)
			if val != "" && val != "<redacted>" && val != "<hidden>" {
				return val
			}
		}
	}

	return ""
}

func skipInterface(name string) bool {
	for _, prefix := range []string{"lo", "utun", "bridge", "ap", "awdl", "llw", "gif", "stf", "fw", "p2p", "vlan", "ipsec", "tun", "tap", "anpi"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

var (
	dongleConnectedSSID string
	dongleConnectedMu   sync.RWMutex
)

// SetDongleConnected updates the topology node for the active USB dongle connection.
func SetDongleConnected(ssid string) {
	dongleConnectedMu.Lock()
	defer dongleConnectedMu.Unlock()
	dongleConnectedSSID = ssid
}

// GetDongleConnected returns the currently connected SSID for the dongle.
func GetDongleConnected() string {
	dongleConnectedMu.RLock()
	defer dongleConnectedMu.RUnlock()
	return dongleConnectedSSID
}

// GetHardwareTopology returns real network interfaces and USB Wi-Fi dongles
// discovered on the live system. No fabricated devices or SSIDs.
func GetHardwareTopology() []HardwareTopology {
	ports := enumerateHardwarePorts()
	gateways := defaultGateways()

	var nodes []HardwareTopology

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			name := iface.Name
			if skipInterface(name) || len(iface.HardwareAddr) != 6 {
				continue
			}

			port, known := ports[name]
			isWiFi := known && port.IsWiFi

			ip, mask := ifaceIPv4(iface)
			_, hasDefaultRoute := gateways[name]
			status := "Down"
			switch {
			case iface.Flags&net.FlagUp == 0:
				status = "Down"
			case hasDefaultRoute && ip != "":
				status = "Active (Default Route)"
			case ip != "":
				status = "Up"
			case isWiFi:
				status = "Up (No Wi-Fi Network)"
			}

			node := HardwareTopology{
				USBDriver:    port.Name,
				BSDInterface: name,
				MACAddress:   iface.HardwareAddr.String(),
				IPAddress:    ip,
				SubnetMask:   mask,
				Gateway:      gateways[name],
				Status:       status,
			}

			if isWiFi {
				if iface.Flags&net.FlagUp != 0 {
					node.NetworkTarget = wifiSSID(name)
				}
				if port.IsUSB {
					node.DriverType = "USB Wi-Fi (macOS network interface)"
				} else {
					node.DriverType = "Apple Built-in Wi-Fi"
					node.USBDriver = "Built-in Wi-Fi (" + port.Name + ")"
				}
			} else if port.IsUSB {
				node.DriverType = "USB Ethernet (DriverKit)"
			} else {
				node.DriverType = "Ethernet"
			}
			nodes = append(nodes, node)
		}
	}

	// USB Wi-Fi dongles discovered on the live bus (the product device).
	//
	// Served from the shared TTL cache, not a fresh enumeration: this runs
	// on the topology poll path and a synchronous libusb pass has wedged
	// API responses against a half-enumerated ZeroCD dongle. A cold cache
	// simply contributes no dongle nodes until its first pass lands.
	for _, d := range cachedDonglesForTopology() {
		status := "Present — No verified network connection"
		driverName := "USB Wi-Fi Dongle (libusb)"
		productName := d.Name
		if drv, devID, matched := driver.GetRegistry().FindDriverForDevice(d.VendorID, d.ProductID); matched {
			if devID.ProductName != "" {
				productName = devID.ProductName
			}
			driverName = fmt.Sprintf("%s (%s)", drv.Info().Family, drv.Info().Standard)
		}
		if d.IsStorage {
			status = "Storage (ZeroCD) — ModeSwitch Required"
			driverName = "USB Wi-Fi Dongle (ZeroCD Storage)"
		}
		// USB descriptors prove presence, not association. Firmware product IDs,
		// saved SSIDs and installation progress cannot identify a live bridge.
		nodes = append(nodes, HardwareTopology{
			USBDriver:    productName,
			VendorID:     fmt.Sprintf("0x%04x", d.VendorID),
			ProductID:    fmt.Sprintf("0x%04x", d.ProductID),
			SerialNumber: d.Serial,
			Speed:        d.Speed,
			BusPath:      d.BusPath,
			Status:       status,
			DriverType:   driverName,
		})
	}

	// Deterministic order: active default route first, then by interface name.
	sort.SliceStable(nodes, func(i, j int) bool {
		di := nodes[i].Status == "Active (Default Route)"
		dj := nodes[j].Status == "Active (Default Route)"
		if di != dj {
			return di
		}
		return nodes[i].BSDInterface < nodes[j].BSDInterface
	})

	return nodes
}

// Bound system utilities too: a slow hardware query must not indefinitely hold
// the topology request open while the application is starting or a device leaves.
func topologyCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}
