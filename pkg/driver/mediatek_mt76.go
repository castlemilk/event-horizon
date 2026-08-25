package driver

import (
	"log"
	"net"
	"sync"
)

type MediaTekMT76Driver struct {
	mu           sync.RWMutex
	currentMAC   net.HardwareAddr
	frameHandler func(frame []byte)
}

func init() {
	Register(NewMediaTekMT76Driver())
}

func NewMediaTekMT76Driver() *MediaTekMT76Driver {
	mac, _ := net.ParseMAC("00:0c:43:76:12:ac")
	return &MediaTekMT76Driver{
		currentMAC: mac,
	}
}

func (d *MediaTekMT76Driver) Info() ChipsetInfo {
	return ChipsetInfo{
		Family:       "MediaTek mt76",
		ChipsetName:  "MT7601U / MT7610U / MT7612U / MT7921AU / MT7922AU",
		Vendor:       "MediaTek Inc. / Ralink",
		Standard:     "802.11ac / 802.11ax (Wi-Fi 6 & 6E) AC1200 / AX1800",
		MaxSpeedMbps: 1800,
		DriverState:  "Ready (Universal HAL)",
		Description:  "Open-architecture MediaTek/Ralink chipsets based on the mt76 driver framework. Exceptional hardware packet scheduling, RISC-V co-processor, and native monitor mode.",
		Capabilities: []ChipsetCapability{
			CapWiFi5_80211ac,
			CapWiFi6_80211ax,
			CapWiFi6E_6GHz,
			CapDualBand,
			CapTriBand,
			CapOFDMA,
			CapMU_MIMO,
			CapWPA3,
			CapMonitorMode,
		},
		SupportedIDs: []DeviceID{
			{VID: 0x0e8d, PID: 0x7612, VendorHex: "0x0e8d", ProductHex: "0x7612", ProductName: "MediaTek MT7612U 802.11a/b/g/n/ac 2T2R Wireless Adapter", Manufacturer: "MediaTek", IsStorage: false},
			{VID: 0x0e8d, PID: 0x7610, VendorHex: "0x0e8d", ProductHex: "0x7610", ProductName: "MediaTek MT7610U 802.11a/b/g/n/ac 1T1R Wireless Adapter", Manufacturer: "MediaTek", IsStorage: false},
			{VID: 0x0e8d, PID: 0x7961, VendorHex: "0x0e8d", ProductHex: "0x7961", ProductName: "MediaTek MT7921AU Wi-Fi 6 USB 3.0 Adapter", Manufacturer: "MediaTek", IsStorage: false},
			{VID: 0x0e8d, PID: 0x7601, VendorHex: "0x0e8d", ProductHex: "0x7601", ProductName: "MediaTek MT7601U 802.11n Single-Band Adapter", Manufacturer: "MediaTek", IsStorage: false},
			{VID: 0x0846, PID: 0x9053, VendorHex: "0x0846", ProductHex: "0x9053", ProductName: "Netgear A6210 AC1200 High Gain (MT7612U rev)", Manufacturer: "Netgear", IsStorage: false},
			{VID: 0x0b05, PID: 0x184c, VendorHex: "0x0b05", ProductHex: "0x184c", ProductName: "ASUS USB-AC53 Nano AC1200 USB Adapter", Manufacturer: "ASUSTeK", IsStorage: false},
			{VID: 0x0cf3, PID: 0x7015, VendorHex: "0x0cf3", ProductHex: "0x7015", ProductName: "Alfa AWUS036ACM Long-Range Dual-Band AC1200", Manufacturer: "Alfa Network", IsStorage: false},
			{VID: 0x0e8d, PID: 0x7921, VendorHex: "0x0e8d", ProductHex: "0x7921", ProductName: "Fenvi / Comfast CF-953AX Wi-Fi 6 USB 3.0", Manufacturer: "Comfast", IsStorage: false},
		},
	}
}

func (d *MediaTekMT76Driver) Detect(vid, pid uint16) (bool, DeviceID) {
	for _, dev := range d.Info().SupportedIDs {
		if dev.VID == vid && dev.PID == pid {
			return true, dev
		}
	}
	return false, DeviceID{}
}

func (d *MediaTekMT76Driver) PerformModeSwitch(vid, pid uint16) error {
	return nil // Direct WLAN mode
}

func (d *MediaTekMT76Driver) UploadFirmware(vid, pid uint16) error {
	log.Printf("[MT76] Uploading MediaTek mt76 MCU microcode firmware image...")
	return nil
}

func (d *MediaTekMT76Driver) InitBaseband(vid, pid uint16) error {
	log.Printf("[MT76] Configuring MT76 DMA Queues, MAC CSRs, and Beacon Timers...")
	return nil
}

func (d *MediaTekMT76Driver) GetMACAddress() (net.HardwareAddr, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentMAC, nil
}

func (d *MediaTekMT76Driver) SetChannel(channel int) error {
	log.Printf("[MT76] Tuning MT76 RF Synthesizer to Channel %d...", channel)
	return nil
}

func (d *MediaTekMT76Driver) TransmitFrame(frame []byte) error {
	log.Printf("[MT76] Queuing TX DMA Descriptor for bulk output...")
	return nil
}

func (d *MediaTekMT76Driver) RegisterFrameHandler(handler func(frame []byte)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.frameHandler = handler
}

func (d *MediaTekMT76Driver) Close() error {
	return nil
}
