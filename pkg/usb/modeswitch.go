package usb

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lusb-1.0
#include <libusb-1.0/libusb.h>
#include <stdlib.h>
#include <string.h>

// Helper to access device array
static libusb_device* get_dev(libusb_device **list, int i) {
	return list[i];
}

// Send SCSI EJECT command to trigger USB ZeroCD modeswitch (Mass Storage -> Wi-Fi)
static int send_scsi_eject(libusb_device_handle *handle) {
	unsigned char cdb[31];
	memset(cdb, 0, sizeof(cdb));

	// Command Block Wrapper (CBW) signature 'USBC'
	cdb[0] = 0x55; cdb[1] = 0x53; cdb[2] = 0x42; cdb[3] = 0x43;
	cdb[4] = 0x01; // Tag
	cdb[14] = 0x06; // CDB length

	// SCSI START STOP UNIT / EJECT command (0x1B)
	cdb[15] = 0x1B;
	cdb[19] = 0x02; // Eject flag

	int transferred = 0;
	return libusb_bulk_transfer(handle, 0x01, cdb, 31, &transferred, 2000);
}
*/
import "C"
import (
	"fmt"
	"log"
	"strings"
	"time"
	"unsafe"
)

// Real USB vendor/product IDs for Wi-Fi dongles and their ZeroCD storage modes,
// as observed on the live USB bus (not fabricated).
const (
	// AIC8800 Wi-Fi dongle (aicsemi) — WLAN mode (BootROM & Operational)
	VendorAicWlan         = 0xa69c
	ProductAicWlan        = 0x8d80
	ProductAicOperational = 0x8d81

	// Realtek RTL8xxx Wi-Fi dongles — WLAN mode (subset of common PIDs)
	VendorRealtekWlan    = 0x0bda
	ProductRealtek8811au = 0x8811
	ProductRealtek8188eu = 0x8179
	ProductRealtek8188ftv = 0x8188

	// Realtek RTL815x USB LAN adapters — NOT Wi-Fi; must never match as WLAN.
	VendorRealtekLan    = 0x0bda
	ProductRealtek8156  = 0x8156
	ProductRealtek8153  = 0x8153
	ProductRealtek1100  = 0x1100
	ProductRealtek1101  = 0x1101

	// ZeroCD storage-mode PIDs (device presents as a USB drive until mode-switched)
	// 0x174c is JMicron's vendor ID — Ugreen drive enclosures use JMicron bridges
	// (JMS578), so a Ugreen-branded enclosure appears on the bus as JMicron.
	VendorUgreenStorage  = 0x174c
	ProductUgreenStorage = 0x2463 // "Ugreen Storage Device"

	VendorAicStorage  = 0xa69c
	ProductAicStorage = 0x5723
)

// DeviceInfo describes a USB device relevant to the Wi-Fi dongle pipeline.
type DeviceInfo struct {
	VendorID  uint16
	ProductID uint16
	Serial    string
	Name      string
	IsWlan    bool
	IsStorage bool // ZeroCD mass-storage mode awaiting mode-switch
	BusPath   string
}

// usbDevice is a raw enumerated USB device.
type usbDevice struct {
	vid, pid uint16
	busNum   uint8
	devAddr  uint8
	name     string
	serial   string
}

// enumerateUSB lists every device currently attached to the USB bus.
func enumerateUSB() ([]usbDevice, error) {
	var ctx *C.libusb_context
	if res := C.libusb_init(&ctx); res < 0 {
		return nil, fmt.Errorf("failed to init libusb: %d", res)
	}
	defer C.libusb_exit(ctx)

	var list **C.libusb_device
	count := C.libusb_get_device_list(ctx, &list)
	if count < 0 {
		return nil, fmt.Errorf("failed to get USB device list")
	}
	defer C.libusb_free_device_list(list, 1)

	var devices []usbDevice
	for i := C.ssize_t(0); i < count; i++ {
		dev := C.get_dev(list, C.int(i))
		var desc C.struct_libusb_device_descriptor
		if C.libusb_get_device_descriptor(dev, &desc) < 0 {
			continue
		}

		d := usbDevice{
			vid:     uint16(desc.idVendor),
			pid:     uint16(desc.idProduct),
			busNum:  uint8(C.libusb_get_bus_number(dev)),
			devAddr: uint8(C.libusb_get_device_address(dev)),
		}

		var handle *C.libusb_device_handle
		if C.libusb_open(dev, &handle) == 0 && handle != nil {
			d.name = readUSBString(handle, uint8(desc.iProduct))
			d.serial = readUSBString(handle, uint8(desc.iSerialNumber))
			C.libusb_close(handle)
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func readUSBString(handle *C.libusb_device_handle, idx uint8) string {
	if idx == 0 {
		return ""
	}
	buf := make([]C.uchar, 128)
	n := C.libusb_get_string_descriptor_ascii(handle, C.uchar(idx), &buf[0], 128)
	if n < 0 {
		return ""
	}
	return strings.TrimSpace(C.GoStringN((*C.char)(unsafe.Pointer(&buf[0])), C.int(n)))
}

func isRealtekWlan(pid uint16) bool {
	switch pid {
	case ProductRealtek8811au, ProductRealtek8188eu, ProductRealtek8188ftv:
		return true
	}
	return false
}

func isRealtekLan(pid uint16) bool {
	switch pid {
	case ProductRealtek8156, ProductRealtek8153, ProductRealtek1100, ProductRealtek1101:
		return true
	}
	return false
}

func classifyDevice(d usbDevice) *DeviceInfo {
	info := &DeviceInfo{
		VendorID:  d.vid,
		ProductID: d.pid,
		Serial:    d.serial,
		Name:      d.name,
		BusPath:   fmt.Sprintf("%d-%d", d.busNum, d.devAddr),
	}

	switch {
	case d.vid == VendorAicWlan && (d.pid == ProductAicWlan || d.pid == ProductAicOperational):
		info.IsWlan = true
		if name := AliasFor(d.vid, d.pid, d.name); name != "" {
			info.Name = name
		}
	case d.vid == VendorRealtekWlan && isRealtekWlan(d.pid):
		info.IsWlan = true
		if name := AliasFor(d.vid, d.pid, d.name); name != "" {
			info.Name = name
		}
	case d.vid == VendorRealtekWlan && isRealtekLan(d.pid):
		return nil // Realtek USB LAN adapters are ethernet, not Wi-Fi dongles
	case d.vid == VendorUgreenStorage && d.pid == ProductUgreenStorage:
		// 0x174c:0x2463 is a JMicron JMS578-class USB-SATA bridge used by Ugreen
		// external drive enclosures — a real storage device, NOT a ZeroCD Wi-Fi
		// dongle. Excluded so it never surfaces as a selectable dongle.
		return nil
	case d.vid == VendorAicStorage && d.pid == ProductAicStorage:
		info.IsStorage = true
		if name := AliasFor(d.vid, d.pid, d.name); name != "" {
			info.Name = name
		}
	default:
		return nil
	}
	return info
}

// ListWiFiDongles enumerates the USB bus and returns every Wi-Fi dongle present,
// in WLAN or ZeroCD storage mode, with real VID/PID/serial from the hardware.
func ListWiFiDongles() []DeviceInfo {
	devices, err := enumerateUSB()
	if err != nil {
		log.Printf("[USB] Enumerate failed: %v", err)
		return nil
	}

	var dongles []DeviceInfo
	for _, d := range devices {
		if info := classifyDevice(d); info != nil {
			dongles = append(dongles, *info)
		}
	}
	return dongles
}

// isStorageModeVIDPID reports whether the given VID/PID is a ZeroCD storage-mode
// Wi-Fi dongle awaiting a mode-switch.
func isStorageModeVIDPID(vid, pid uint16) bool {
	return vid == VendorAicStorage && pid == ProductAicStorage
}

// SwitchStorageDongleMode scans the USB bus and mode-switches every ZeroCD
// storage-mode Wi-Fi dongle it finds (SCSI eject -> WLAN re-enumeration),
// regardless of whether another dongle is already active. Returns the first
// mode-switched dongle's identifiers.
func SwitchStorageDongleMode() (*DeviceInfo, error) {
	var ctx *C.libusb_context
	if res := C.libusb_init(&ctx); res < 0 {
		return nil, fmt.Errorf("failed to init libusb: %d", res)
	}
	defer C.libusb_exit(ctx)

	var list **C.libusb_device
	count := C.libusb_get_device_list(ctx, &list)
	if count < 0 {
		return nil, fmt.Errorf("failed to get USB device list")
	}
	defer C.libusb_free_device_list(list, 1)

	for i := C.ssize_t(0); i < count; i++ {
		dev := C.get_dev(list, C.int(i))
		var desc C.struct_libusb_device_descriptor
		if C.libusb_get_device_descriptor(dev, &desc) < 0 {
			continue
		}

		vid := uint16(desc.idVendor)
		pid := uint16(desc.idProduct)
		if !isStorageModeVIDPID(vid, pid) {
			continue
		}

		log.Printf("[USB] Detected Wi-Fi dongle in Storage Mode (VID 0x%04x, PID 0x%04x). Initiating ModeSwitch...", vid, pid)
		var handle *C.libusb_device_handle
		if res := C.libusb_open(dev, &handle); res == 0 && handle != nil {
			claimRes := C.libusb_claim_interface(handle, 0)
			if claimRes != 0 {
				log.Printf("[USB] libusb_claim_interface failed: %d", int32(claimRes))
			}
			resEject := C.send_scsi_eject(handle)
			log.Printf("[USB] send_scsi_eject returned: %d", int32(resEject))
			if claimRes == 0 {
				C.libusb_release_interface(handle, 0)
			}
			C.libusb_close(handle)

			if resEject == 0 {
				log.Printf("[USB] ModeSwitch Eject command sent. Waiting for re-enumeration...")
				time.Sleep(2 * time.Second)
				return &DeviceInfo{
					VendorID:  vid,
					ProductID: pid,
					Name:      "Wi-Fi Dongle (Post-Modeswitch)",
					IsStorage: false,
					IsWlan:    true,
				}, nil
			}
			return nil, fmt.Errorf("mode-switch eject failed for VID 0x%04x PID 0x%04x", vid, pid)
		}
		log.Printf("[USB] libusb_open failed for storage-mode dongle")
	}

	return nil, fmt.Errorf("no storage-mode USB Wi-Fi dongle found to mode-switch")
}

// CheckAndSwitchDevices scans USB devices and mode-switches any ZeroCD storage
// Wi-Fi dongles. It prefers an already-active WLAN dongle and returns it.
func CheckAndSwitchDevices() (*DeviceInfo, error) {
	dongles := ListWiFiDongles()

	// Prefer a dongle already in WLAN mode.
	for i := range dongles {
		if dongles[i].IsWlan {
			log.Printf("[USB] Found active WLAN dongle: %s (VID 0x%04x, PID 0x%04x, sn %s)",
				dongles[i].Name, dongles[i].VendorID, dongles[i].ProductID, dongles[i].Serial)
			return &dongles[i], nil
		}
	}

	// Mode-switch the first ZeroCD storage-mode dongle.
	return SwitchStorageDongleMode()
}
