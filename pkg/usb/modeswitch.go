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
	"time"
)

// Target USB Device IDs for UGREEN / Realtek / AIC Wi-Fi dongles
const (
	// Storage Mode (ZeroCD) IDs
	VendorUgreenStorage  = 0x5964
	ProductUgreenStorage = 0x9315

	VendorAicStorage  = 0xa69c
	ProductAicStorage = 0x5723

	// Wi-Fi Mode (WLAN) IDs
	VendorAicWlan  = 0xa69c
	ProductAicWlan = 0x8d80

	VendorRealtekWlan  = 0x0bda
	ProductRealtekWlan = 0x8811
)

type DeviceInfo struct {
	VendorID  uint16
	ProductID uint16
	Name      string
	IsWlan    bool
}

// CheckAndSwitchDevices scans USB devices and mode-switches any ZeroCD storage Wi-Fi dongles
func CheckAndSwitchDevices() (*DeviceInfo, error) {
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

	var foundDevice *DeviceInfo

	// Iterate through attached USB devices
	for i := C.ssize_t(0); i < count; i++ {
		dev := C.get_dev(list, C.int(i))
		var desc C.struct_libusb_device_descriptor
		if C.libusb_get_device_descriptor(dev, &desc) < 0 {
			continue
		}

		vid := uint16(desc.idVendor)
		pid := uint16(desc.idProduct)

		// 1. Check if device is already in WLAN mode
		if (vid == VendorAicWlan && pid == ProductAicWlan) || vid == VendorRealtekWlan {
			log.Printf("[USB] Found active WLAN device: VID 0x%04x, PID 0x%04x", vid, pid)
			return &DeviceInfo{
				VendorID:  vid,
				ProductID: pid,
				Name:      "AIC/Realtek WLAN Adapter",
				IsWlan:    true,
			}, nil
		}

		// 2. Check if device is in Storage (ZeroCD) mode and needs mode-switching
		if (vid == VendorUgreenStorage && pid == ProductUgreenStorage) || (vid == VendorAicStorage && pid == ProductAicStorage) {
			log.Printf("[USB] Detected Wi-Fi dongle in Storage Mode (VID 0x%04x, PID 0x%04x). Initiating ModeSwitch...", vid, pid)

			var handle *C.libusb_device_handle
			if res := C.libusb_open(dev, &handle); res == 0 && handle != nil {
				C.libusb_claim_interface(handle, 0)
				resEject := C.send_scsi_eject(handle)
				C.libusb_release_interface(handle, 0)
				C.libusb_close(handle)

				if resEject == 0 {
					log.Printf("[USB] ModeSwitch Eject command sent successfully. Waiting for re-enumeration...")
					time.Sleep(2 * time.Second)
					foundDevice = &DeviceInfo{
						VendorID:  VendorAicWlan,
						ProductID: ProductAicWlan,
						Name:      "AIC Wlan Adapter (Post-Modeswitch)",
						IsWlan:    true,
					}
				}
			}
		}
	}

	if foundDevice != nil {
		return foundDevice, nil
	}

	return nil, fmt.Errorf("no supported USB Wi-Fi dongle found")
}
