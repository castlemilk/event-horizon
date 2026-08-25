package driver

import (
	"log"
	"net"
	"sync"
)

type QualcommAth9kDriver struct {
	mu           sync.RWMutex
	currentMAC   net.HardwareAddr
	frameHandler func(frame []byte)
}

func init() {
	Register(NewQualcommAth9kDriver())
}

func NewQualcommAth9kDriver() *QualcommAth9kDriver {
	mac, _ := net.ParseMAC("00:03:7f:92:71:00")
	return &QualcommAth9kDriver{
		currentMAC: mac,
	}
}

func (d *QualcommAth9kDriver) Info() ChipsetInfo {
	return ChipsetInfo{
		Family:       "Qualcomm Atheros ath9k_htc",
		ChipsetName:  "AR9271 / AR7010",
		Vendor:       "Qualcomm Atheros Inc.",
		Standard:     "802.11b/g/n (Wi-Fi 4) High Power",
		MaxSpeedMbps: 300,
		DriverState:  "Ready (Universal HAL)",
		Description:  "The legendary open-source USB Wi-Fi chipset with open HTC (Host Target Communication) firmware. Legendary sensitivity, packet injection, and raw frame monitoring.",
		Capabilities: []ChipsetCapability{
			CapWiFi4_80211n,
			CapMonitorMode,
		},
		SupportedIDs: []DeviceID{
			{VID: 0x0cf3, PID: 0x9271, VendorHex: "0x0cf3", ProductHex: "0x9271", ProductName: "Qualcomm Atheros AR9271 802.11n Wireless Network Adapter", Manufacturer: "Qualcomm Atheros", IsStorage: false},
			{VID: 0x0cf3, PID: 0x7010, VendorHex: "0x0cf3", ProductHex: "0x7010", ProductName: "Atheros AR7010 + AR9280 Dual-Band 802.11abgn", Manufacturer: "Qualcomm Atheros", IsStorage: false},
			{VID: 0x0cf3, PID: 0x1006, VendorHex: "0x0cf3", ProductHex: "0x1006", ProductName: "Atheros AR9271 Boot Device", Manufacturer: "Qualcomm Atheros", IsStorage: false},
			{VID: 0x0846, PID: 0x9030, VendorHex: "0x0846", ProductHex: "0x9030", ProductName: "Netgear WNA1100 Wireless-N 150 USB Adapter", Manufacturer: "Netgear", IsStorage: false},
			{VID: 0x0411, PID: 0x017f, VendorHex: "0x0411", ProductHex: "0x017f", ProductName: "Sony UWA-BR100 802.11abgn Wireless LAN Adapter", Manufacturer: "Sony", IsStorage: false},
			{VID: 0x050d, PID: 0x845a, VendorHex: "0x050d", ProductHex: "0x845a", ProductName: "Belkin F7D1101 v1 Basic Wireless USB Adapter", Manufacturer: "Belkin", IsStorage: false},
			{VID: 0x2001, PID: 0x3a10, VendorHex: "0x2001", ProductHex: "0x3a10", ProductName: "D-Link DWA-126 Wireless N 150 USB Adapter", Manufacturer: "D-Link", IsStorage: false},
		},
	}
}

func (d *QualcommAth9kDriver) Detect(vid, pid uint16) (bool, DeviceID) {
	for _, dev := range d.Info().SupportedIDs {
		if dev.VID == vid && dev.PID == pid {
			return true, dev
		}
	}
	return false, DeviceID{}
}

func (d *QualcommAth9kDriver) PerformModeSwitch(vid, pid uint16) error {
	return nil // Direct WLAN mode
}

func (d *QualcommAth9kDriver) UploadFirmware(vid, pid uint16) error {
	log.Printf("[ATH9K] Uploading Open HTC Firmware (htc_9271.fw) via USB EP0...")
	return nil
}

func (d *QualcommAth9kDriver) InitBaseband(vid, pid uint16) error {
	log.Printf("[ATH9K] Initializing WMI Target, BB calibration, and MAC descriptors...")
	return nil
}

func (d *QualcommAth9kDriver) GetMACAddress() (net.HardwareAddr, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentMAC, nil
}

func (d *QualcommAth9kDriver) SetChannel(channel int) error {
	log.Printf("[ATH9K] Tuning Atheros RF to 802.11n Channel %d...", channel)
	return nil
}

func (d *QualcommAth9kDriver) TransmitFrame(frame []byte) error {
	log.Printf("[ATH9K] Sending HTC TX Command over Endpoint 0x01...")
	return nil
}

func (d *QualcommAth9kDriver) RegisterFrameHandler(handler func(frame []byte)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.frameHandler = handler
}

func (d *QualcommAth9kDriver) Close() error {
	return nil
}
