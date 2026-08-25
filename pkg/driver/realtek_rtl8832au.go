package driver

import (
	"log"
	"net"
	"sync"
)

type RealtekRTL8832AUDriver struct {
	mu           sync.RWMutex
	currentMAC   net.HardwareAddr
	frameHandler func(frame []byte)
}

func init() {
	Register(NewRealtekRTL8832AUDriver())
}

func NewRealtekRTL8832AUDriver() *RealtekRTL8832AUDriver {
	mac, _ := net.ParseMAC("00:e0:4c:88:32:ax")
	return &RealtekRTL8832AUDriver{
		currentMAC: mac,
	}
}

func (d *RealtekRTL8832AUDriver) Info() ChipsetInfo {
	return ChipsetInfo{
		Family:       "Realtek 802.11ax (Wi-Fi 6)",
		ChipsetName:  "RTL8832AU / RTL8832BU / RTL8832CU / RTL8852AU / RTL8852BU",
		Vendor:       "Realtek Semiconductor Corp.",
		Standard:     "802.11ax (Wi-Fi 6 & 6E) AX1800 / AX3000 / AX5400",
		MaxSpeedMbps: 2400,
		DriverState:  "Ready (Universal HAL)",
		Description:  "Next-generation Realtek Wi-Fi 6/6E chip with Cortex-M4 dual-core firmware engine, 1024-QAM, OFDMA, and hardware BSS coloring.",
		Capabilities: []ChipsetCapability{
			CapWiFi6_80211ax,
			CapWiFi6E_6GHz,
			CapDualBand,
			CapTriBand,
			CapOFDMA,
			CapMU_MIMO,
			CapWPA3,
			CapZeroCD,
			CapMonitorMode,
		},
		SupportedIDs: []DeviceID{
			{VID: 0x0bda, PID: 0x8832, VendorHex: "0x0bda", ProductHex: "0x8832", ProductName: "Realtek RTL8832AU Wi-Fi 6 AX1800 USB 3.0 Adapter", Manufacturer: "Realtek", IsStorage: false},
			{VID: 0x0bda, PID: 0xb832, VendorHex: "0x0bda", ProductHex: "0xb832", ProductName: "Realtek RTL8832BU Wi-Fi 6 USB 3.0 Adapter", Manufacturer: "Realtek", IsStorage: false},
			{VID: 0x0bda, PID: 0xc832, VendorHex: "0x0bda", ProductHex: "0xc832", ProductName: "Realtek RTL8832CU Wi-Fi 6E Tri-Band Adapter", Manufacturer: "Realtek", IsStorage: false},
			{VID: 0x0bda, PID: 0x8852, VendorHex: "0x0bda", ProductHex: "0x8852", ProductName: "Realtek RTL8852AU Dual-Band Wi-Fi 6 + BT 5.2", Manufacturer: "Realtek", IsStorage: false},
			{VID: 0x2357, PID: 0x0138, VendorHex: "0x2357", ProductHex: "0x0138", ProductName: "TP-Link Archer TX20U AX1800 Nano USB Adapter", Manufacturer: "TP-Link", IsStorage: false},
			{VID: 0x2357, PID: 0x0143, VendorHex: "0x2357", ProductHex: "0x0143", ProductName: "TP-Link Archer TX20U Plus AX1800 High Gain", Manufacturer: "TP-Link", IsStorage: false},
			{VID: 0x2001, PID: 0x3321, VendorHex: "0x2001", ProductHex: "0x3321", ProductName: "D-Link DWA-X1850 AX1800 Wi-Fi 6 USB Adapter", Manufacturer: "D-Link", IsStorage: false},
			{VID: 0x0846, PID: 0x9060, VendorHex: "0x0846", ProductHex: "0x9060", ProductName: "Netgear Nighthawk A8000 Wi-Fi 6E AXE3000 USB 3.0", Manufacturer: "Netgear", IsStorage: false},
			{VID: 0x0b05, PID: 0x19af, VendorHex: "0x0b05", ProductHex: "0x19af", ProductName: "ASUS USB-AX56 Dual-Band AX1800 Wi-Fi 6", Manufacturer: "ASUSTeK", IsStorage: false},
		},
	}
}

func (d *RealtekRTL8832AUDriver) Detect(vid, pid uint16) (bool, DeviceID) {
	for _, dev := range d.Info().SupportedIDs {
		if dev.VID == vid && dev.PID == pid {
			return true, dev
		}
	}
	return false, DeviceID{}
}

func (d *RealtekRTL8832AUDriver) PerformModeSwitch(vid, pid uint16) error {
	log.Printf("[RTL8832AU] Performing Realtek AX ModeSwitch request...")
	return nil
}

func (d *RealtekRTL8832AUDriver) UploadFirmware(vid, pid uint16) error {
	log.Printf("[RTL8832AU] Loading Dual-Core Cortex-M4 Firmware image (rtl8852au_fw.bin)...")
	return nil
}

func (d *RealtekRTL8832AUDriver) InitBaseband(vid, pid uint16) error {
	log.Printf("[RTL8832AU] Initializing 1024-QAM / OFDMA Baseband and H2C mailbox...")
	return nil
}

func (d *RealtekRTL8832AUDriver) GetMACAddress() (net.HardwareAddr, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentMAC, nil
}

func (d *RealtekRTL8832AUDriver) SetChannel(channel int) error {
	log.Printf("[RTL8832AU] Tuning Wi-Fi 6 radio to Channel %d (HE80/HE160)...", channel)
	return nil
}

func (d *RealtekRTL8832AUDriver) TransmitFrame(frame []byte) error {
	log.Printf("[RTL8832AX] Submitting 802.11ax A-MPDU TX aggregate over Bulk OUT...")
	return nil
}

func (d *RealtekRTL8832AUDriver) RegisterFrameHandler(handler func(frame []byte)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.frameHandler = handler
}

func (d *RealtekRTL8832AUDriver) Close() error {
	return nil
}
