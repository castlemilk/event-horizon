package driver

import (
	"fmt"
	"net"
	"sync"
)

// ChipsetCapability defines hardware features supported by a Wi-Fi driver.
type ChipsetCapability string

const (
	CapWiFi4_80211n  ChipsetCapability = "802.11n (Wi-Fi 4)"
	CapWiFi5_80211ac ChipsetCapability = "802.11ac (Wi-Fi 5)"
	CapWiFi6_80211ax ChipsetCapability = "802.11ax (Wi-Fi 6)"
	CapWiFi6E_6GHz   ChipsetCapability = "802.11ax 6GHz (Wi-Fi 6E)"
	CapDualBand      ChipsetCapability = "2.4GHz + 5GHz Dual-Band"
	CapTriBand       ChipsetCapability = "2.4GHz + 5GHz + 6GHz Tri-Band"
	CapMU_MIMO       ChipsetCapability = "MU-MIMO"
	CapOFDMA         ChipsetCapability = "OFDMA"
	CapWPA3          ChipsetCapability = "WPA3-SAE Hardware Crypto"
	CapZeroCD        ChipsetCapability = "ZeroCD Virtual CD ModeSwitch"
	CapMonitorMode   ChipsetCapability = "Raw Packet Monitor Mode"
)

// ChipsetInfo describes a supported USB Wi-Fi chipset family and model list.
type ChipsetInfo struct {
	Family       string              `json:"family"`
	ChipsetName  string              `json:"chipset_name"`
	Vendor       string              `json:"vendor"`
	Standard     string              `json:"standard"`
	MaxSpeedMbps int                 `json:"max_speed_mbps"`
	SupportedIDs []DeviceID          `json:"supported_ids"`
	Capabilities []ChipsetCapability `json:"capabilities"`
	DriverState  string              `json:"driver_state"` // "Active", "Operational", "Ready", "Community Supported"
	Description  string              `json:"description"`
}

// DeviceID maps a USB Vendor ID and Product ID pair to a commercial product name.
type DeviceID struct {
	VID         uint16 `json:"vid"`
	PID         uint16 `json:"pid"`
	VendorHex   string `json:"vendor_hex"`
	ProductHex  string `json:"product_hex"`
	ProductName string `json:"product_name"`
	Manufacturer string `json:"manufacturer"`
	IsStorage   bool   `json:"is_storage"`
}

// WiFiChipsetDriver is the universal interface implemented by all chipset adapters.
type WiFiChipsetDriver interface {
	Info() ChipsetInfo
	Detect(vid, pid uint16) (bool, DeviceID)
	PerformModeSwitch(vid, pid uint16) error
	UploadFirmware(vid, pid uint16) error
	InitBaseband(vid, pid uint16) error
	GetMACAddress() (net.HardwareAddr, error)
	SetChannel(channel int) error
	TransmitFrame(frame []byte) error
	RegisterFrameHandler(handler func(frame []byte))
	Close() error
}

// Registry manages the global collection of hardware chipset drivers.
type Registry struct {
	mu      sync.RWMutex
	drivers []WiFiChipsetDriver
}

var globalRegistry = &Registry{}

// Register adds a driver implementation to the global hardware registry.
func Register(d WiFiChipsetDriver) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.drivers = append(globalRegistry.drivers, d)
}

// GetRegistry returns the global registry instance.
func GetRegistry() *Registry {
	return globalRegistry
}

// FindDriverForDevice locates the appropriate driver for a given USB VID:PID.
func (r *Registry) FindDriverForDevice(vid, pid uint16) (WiFiChipsetDriver, DeviceID, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, d := range r.drivers {
		if matched, devID := d.Detect(vid, pid); matched {
			return d, devID, true
		}
	}
	return nil, DeviceID{}, false
}

// ListAllChipsets returns metadata for all supported chipset families.
func (r *Registry) ListAllChipsets() []ChipsetInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []ChipsetInfo
	for _, d := range r.drivers {
		list = append(list, d.Info())
	}
	return list
}

// FormatHex converts a 16-bit int to "0x0000" hex string.
func FormatHex(val uint16) string {
	return fmt.Sprintf("0x%04x", val)
}
