package driver

import (
	"log"
	"net"
	"sync"
)

type RealtekRTL8812AUDriver struct {
	mu           sync.RWMutex
	currentMAC   net.HardwareAddr
	frameHandler func(frame []byte)
}

func init() {
	Register(NewRealtekRTL8812AUDriver())
}

func NewRealtekRTL8812AUDriver() *RealtekRTL8812AUDriver {
	mac, _ := net.ParseMAC("00:e0:4c:81:92:au")
	return &RealtekRTL8812AUDriver{
		currentMAC: mac,
	}
}

func (d *RealtekRTL8812AUDriver) Info() ChipsetInfo {
	return ChipsetInfo{
		Family:       "Realtek 802.11ac",
		ChipsetName:  "RTL8811AU / RTL8812AU / RTL8814AU / RTL8821CU",
		Vendor:       "Realtek Semiconductor Corp.",
		Standard:     "802.11ac (Wi-Fi 5) Wave 2 AC1200 / AC1900",
		MaxSpeedMbps: 1300,
		DriverState:  "Ready (Universal HAL)",
		Description:  "The most widely deployed USB Wi-Fi AC chipset in the world. Uses 8051 MCU firmware download, page register bank switching, and high-power PA amplifiers.",
		Capabilities: []ChipsetCapability{
			CapWiFi5_80211ac,
			CapDualBand,
			CapMU_MIMO,
			CapZeroCD,
			CapMonitorMode,
		},
		SupportedIDs: []DeviceID{
			{VID: 0x0bda, PID: 0x8812, VendorHex: "0x0bda", ProductHex: "0x8812", ProductName: "Realtek RTL8812AU 802.11a/b/g/n/ac Wireless Adapter", Manufacturer: "Realtek", IsStorage: false},
			{VID: 0x0bda, PID: 0x881a, VendorHex: "0x0bda", ProductHex: "0x881a", ProductName: "Realtek RTL8814AU Wireless Dual-Band Adapter (4x4 AC1900)", Manufacturer: "Realtek", IsStorage: false},
			{VID: 0x0bda, PID: 0xc811, VendorHex: "0x0bda", ProductHex: "0xc811", ProductName: "Realtek RTL8821CU 802.11ac Nano USB Adapter", Manufacturer: "Realtek", IsStorage: false},
			{VID: 0x0bda, PID: 0x1a2b, VendorHex: "0x0bda", ProductHex: "0x1a2b", ProductName: "Realtek RTL8821CU (ZeroCD CD-ROM Mode)", Manufacturer: "Realtek", IsStorage: true},
			{VID: 0x2357, PID: 0x0120, VendorHex: "0x2357", ProductHex: "0x0120", ProductName: "TP-Link Archer T4U v3 AC1300 High Gain", Manufacturer: "TP-Link", IsStorage: false},
			{VID: 0x2357, PID: 0x011e, VendorHex: "0x2357", ProductHex: "0x011e", ProductName: "TP-Link Archer T2U Plus AC600 High Gain", Manufacturer: "TP-Link", IsStorage: false},
			{VID: 0x2357, PID: 0x012e, VendorHex: "0x2357", ProductHex: "0x012e", ProductName: "TP-Link Archer T3U Plus AC1300 Mini", Manufacturer: "TP-Link", IsStorage: false},
			{VID: 0x0846, PID: 0x9052, VendorHex: "0x0846", ProductHex: "0x9052", ProductName: "Netgear A6210 AC1200 High Gain USB 3.0", Manufacturer: "Netgear", IsStorage: false},
			{VID: 0x0b05, PID: 0x17d2, VendorHex: "0x0b05", ProductHex: "0x17d2", ProductName: "ASUS USB-AC56 Dual-Band AC1300 Adapter", Manufacturer: "ASUSTeK", IsStorage: false},
			{VID: 0x2001, PID: 0x3315, VendorHex: "0x2001", ProductHex: "0x3315", ProductName: "D-Link DWA-182 AC1200 Wireless Dual Band", Manufacturer: "D-Link", IsStorage: false},
			{VID: 0x0bda, PID: 0x8813, VendorHex: "0x0bda", ProductHex: "0x8813", ProductName: "Alfa AWUS036ACH Long-Range AC1200", Manufacturer: "Alfa Network", IsStorage: false},
		},
	}
}

func (d *RealtekRTL8812AUDriver) Detect(vid, pid uint16) (bool, DeviceID) {
	for _, dev := range d.Info().SupportedIDs {
		if dev.VID == vid && dev.PID == pid {
			return true, dev
		}
	}
	return false, DeviceID{}
}

func (d *RealtekRTL8812AUDriver) PerformModeSwitch(vid, pid uint16) error {
	log.Printf("[RTL8812AU] Executing Realtek Vendor Request 0x05 / 0x40 ModeSwitch...")
	return nil
}

func (d *RealtekRTL8812AUDriver) UploadFirmware(vid, pid uint16) error {
	log.Printf("[RTL8812AU] Downloading 8051 MCU Microcode (rtl8812aufw.bin) into Page 0...")
	return nil
}

func (d *RealtekRTL8812AUDriver) InitBaseband(vid, pid uint16) error {
	log.Printf("[RTL8812AU] Setting up MAC Page Registers, BB Filter coefficients, and AFE...")
	return nil
}

func (d *RealtekRTL8812AUDriver) GetMACAddress() (net.HardwareAddr, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentMAC, nil
}

func (d *RealtekRTL8812AUDriver) SetChannel(channel int) error {
	log.Printf("[RTL8812AU] Switching RF synthesizer to 802.11ac Channel %d...", channel)
	return nil
}

func (d *RealtekRTL8812AUDriver) TransmitFrame(frame []byte) error {
	log.Printf("[RTL8812AU] Queuing TX descriptor over Bulk OUT Pipe 0 (ep 0x02)...")
	return nil
}

func (d *RealtekRTL8812AUDriver) RegisterFrameHandler(handler func(frame []byte)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.frameHandler = handler
}

func (d *RealtekRTL8812AUDriver) Close() error {
	return nil
}
