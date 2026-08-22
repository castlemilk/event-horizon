package protocol

/*
#cgo CFLAGS: -I/opt/homebrew/include -I/usr/local/include
#cgo LDFLAGS: -L/opt/homebrew/lib -L/usr/local/lib -lusb-1.0
#include <libusb-1.0/libusb.h>
#include <stdlib.h>
#include <string.h>

static libusb_device* dev_at(libusb_device **list, ssize_t i) {
	return list[i];
}

static libusb_device_handle* open_vid_pid(libusb_context *ctx, uint16_t vid, uint16_t pid) {
	return libusb_open_device_with_vid_pid(ctx, vid, pid);
}

static int detach_kernel_driver(libusb_device_handle *h, int iface) {
	return libusb_detach_kernel_driver(h, iface);
}

static int claim_interface(libusb_device_handle *h, int iface) {
	return libusb_claim_interface(h, iface);
}

static int release_interface(libusb_device_handle *h, int iface) {
	return libusb_release_interface(h, iface);
}

static int bulk_transfer(libusb_device_handle *h,
                         unsigned char endpoint,
                         unsigned char *data,
                         int length,
                         int *transferred,
                         unsigned int timeout_ms) {
	return libusb_bulk_transfer(h, endpoint, data, length, transferred, timeout_ms);
}

static int ctrl_transfer(libusb_device_handle *h,
                         uint8_t request_type,
                         uint8_t request,
                         uint16_t value,
                         uint16_t index,
                         unsigned char *data,
                         uint16_t length,
                         unsigned int timeout_ms) {
	return libusb_control_transfer(h, request_type, request, value, index,
	                               data, length, timeout_ms);
}

static int get_device_descriptor(libusb_device *dev,
                                 struct libusb_device_descriptor *out) {
	return libusb_get_device_descriptor(dev, out);
}

static uint8_t get_bus_number(libusb_device *dev) {
	return libusb_get_bus_number(dev);
}

static uint8_t get_device_address(libusb_device *dev) {
	return libusb_get_device_address(dev);
}

static int open_device(libusb_device *dev, libusb_device_handle **out) {
	return libusb_open(dev, out);
}

// reset_device issues a USB port reset. Revives a wedged device when the
// boot ROM is still running; a fully crashed ROM needs a power cycle.
static int reset_device(libusb_device_handle *h) {
	return libusb_reset_device(h);
}

static int get_active_config(libusb_device *dev, struct libusb_config_descriptor **cfg) {
	return libusb_get_active_config_descriptor(dev, cfg);
}

static void free_config(struct libusb_config_descriptor *cfg) {
	libusb_free_config_descriptor(cfg);
}

// Walk endpoints of interface 0 / alt 0 and return the bulk IN + OUT
// addresses. The first of each direction lands in in/out; the second
// (the dedicated command "msg" endpoints used by the fdrv driver) in
// in2/out2. 0 means none found.
static void find_bulk_endpoints(struct libusb_config_descriptor *cfg,
                                uint8_t *in, uint8_t *out,
                                uint8_t *in2, uint8_t *out2) {
    *in = 0;
    *out = 0;
    *in2 = 0;
    *out2 = 0;
    if (cfg->bNumInterfaces < 1) return;
    struct libusb_interface *iface = (struct libusb_interface *)&cfg->interface[0];
    if (iface->num_altsetting < 1) return;
    struct libusb_interface_descriptor *alt = (struct libusb_interface_descriptor *)&iface->altsetting[0];
    for (int i = 0; i < alt->bNumEndpoints; i++) {
        struct libusb_endpoint_descriptor *ep = (struct libusb_endpoint_descriptor *)&alt->endpoint[i];
        // bmAttributes low 2 bits are transfer type; 2 = bulk
        if ((ep->bmAttributes & 0x03) != 2) continue;
        uint8_t addr = ep->bEndpointAddress;
        if (addr & 0x80) {
            addr &= 0x7f;
            if (*in == 0) *in = addr;
            else if (*in2 == 0) *in2 = addr;
        } else {
            if (*out == 0) *out = addr;
            else if (*out2 == 0) *out2 = addr;
        }
    }
}
// dump_config writes a compact summary of every interface altsetting's
// endpoints into buf. Returns the number of bytes written.
static int dump_config(struct libusb_config_descriptor *cfg, char *buf, int cap) {
    int off = 0;
    for (int i = 0; i < cfg->bNumInterfaces; i++) {
        struct libusb_interface *iface = (struct libusb_interface *)&cfg->interface[i];
        for (int a = 0; a < iface->num_altsetting; a++) {
            struct libusb_interface_descriptor *alt = (struct libusb_interface_descriptor *)&iface->altsetting[a];
            off += snprintf(buf + off, cap - off, "if%d alt%d cls=0x%02x sub=0x%02x proto=0x%02x eps=%d:",
                i, a, alt->bInterfaceClass, alt->bInterfaceSubClass,
                alt->bInterfaceProtocol, alt->bNumEndpoints);
            for (int e = 0; e < alt->bNumEndpoints; e++) {
                struct libusb_endpoint_descriptor *ep = (struct libusb_endpoint_descriptor *)&alt->endpoint[e];
                const char *t = (ep->bmAttributes & 0x03) == 2 ? "bulk" :
                                (ep->bmAttributes & 0x03) == 3 ? "int" : "ctrl";
                off += snprintf(buf + off, cap - off, " [0x%02x %s %d]", ep->bEndpointAddress, t, ep->wMaxPacketSize);
            }
            off += snprintf(buf + off, cap - off, "\n");
        }
    }
    return off;
}
*/

import "C"
import (
	"fmt"
	"unsafe"
)

// USBDevice is a thin Go wrapper around libusb_device + libusb_device_handle.
type USBDevice struct {
	handle  *C.libusb_device_handle
	dev     *C.libusb_device
	bulkIn  uint8
	bulkOut uint8
	// msgIn/msgOut are the second bulk endpoints of each direction — the
	// dedicated command pipes (fdrv msg_in_pipe / msg_out_pipe). 0 if the
	// device exposes only one endpoint per direction.
	msgIn  uint8
	msgOut uint8
	// rx reassembles length-prefixed frames from the bulk IN stream —
	// the device aggregates multiple frames per transfer.
	rx RxStream
}

// libusbContext is an alias for the cgo libusb_context type so other
// files in this package can reference it without importing cgo.
type libusbContext = C.libusb_context

// Init creates a libusb context. Must be paired with Deinit.
func Init() (*C.libusb_context, error) {
	var ctx *C.libusb_context
	rc := C.libusb_init(&ctx)
	if rc < 0 {
		return nil, fmt.Errorf("libusb_init failed: %s", libusbErrname(rc))
	}
	return ctx, nil
}

// Deinit frees the libusb context.
func Deinit(ctx *C.libusb_context) {
	C.libusb_exit(ctx)
}

// OpenByVIDPID opens the first device matching vid:pid and probes its
// bulk endpoints. Errors if absent or no bulk endpoints.
func OpenByVIDPID(ctx *C.libusb_context, vid, pid uint16) (*USBDevice, error) {
	h := C.open_vid_pid(ctx, C.uint16_t(vid), C.uint16_t(pid))
	if h == nil {
		return nil, fmt.Errorf("libusb open %04x:%04x: not found", vid, pid)
	}
	// We need the device pointer to walk the config descriptor. We
	// can get it from a temporary list call.
	dev, err := deviceMatch(ctx, vid, pid)
	if err != nil {
		C.libusb_close(h)
		return nil, fmt.Errorf("find device for %04x:%04x: %w", vid, pid, err)
	}
	bulkIn, bulkOut, msgIn, msgOut, err := findBulkEndpoints(dev)
	if err != nil {
		C.libusb_close(h)
		return nil, fmt.Errorf("find bulk endpoints for %04x:%04x: %w", vid, pid, err)
	}
	return &USBDevice{handle: h, dev: dev, bulkIn: bulkIn, bulkOut: bulkOut, msgIn: msgIn, msgOut: msgOut}, nil
}

// deviceMatch returns the libusb_device pointer for the first match of
// vid:pid. Caller does NOT own this pointer but it lives until the
// list is freed.
func deviceMatch(ctx *C.libusb_context, vid, pid uint16) (*C.libusb_device, error) {
	var list **C.libusb_device
	count := C.libusb_get_device_list(ctx, &list)
	if count < 0 {
		return nil, fmt.Errorf("get device list: %s", libusbErrname(C.int(count)))
	}
	defer C.libusb_free_device_list(list, 1)
	for i := C.ssize_t(0); i < count; i++ {
		d := C.dev_at(list, i)
		var desc C.struct_libusb_device_descriptor
		if C.get_device_descriptor(d, &desc) < 0 {
			continue
		}
		if uint16(desc.idVendor) == vid && uint16(desc.idProduct) == pid {
			return d, nil
		}
	}
	return nil, fmt.Errorf("device %04x:%04x not enumerated", vid, pid)
}

// findBulkEndpoints walks the device's active config and returns the
// first and second bulk IN and OUT endpoint addresses (interface 0 /
// alt 0). Second endpoints are 0 when absent.
func findBulkEndpoints(dev *C.libusb_device) (in, out, msgIn, msgOut uint8, err error) {
	var cfg *C.struct_libusb_config_descriptor
	rc := C.get_active_config(dev, &cfg)
	if rc < 0 {
		return 0, 0, 0, 0, fmt.Errorf("get active config: %s", libusbErrname(rc))
	}
	defer C.free_config(cfg)
	var inAddr, outAddr, in2Addr, out2Addr C.uint8_t
	C.find_bulk_endpoints(cfg, &inAddr, &outAddr, &in2Addr, &out2Addr)
	return uint8(inAddr), uint8(outAddr), uint8(in2Addr), uint8(out2Addr), nil
}

// BulkInEndpoint returns the discovered bulk IN endpoint (0x80 bit
// cleared). Returns 0 if not yet known.
func (d *USBDevice) BulkInEndpoint() uint8 { return d.bulkIn }

// BulkOutEndpoint returns the discovered bulk OUT endpoint. Returns 0
// if not yet known.
func (d *USBDevice) BulkOutEndpoint() uint8 { return d.bulkOut }

// MsgOutEndpoint returns the dedicated command OUT endpoint (second
// bulk OUT), falling back to the first when only one exists. LMAC
// host-target commands go here per fdrv msg_out_pipe handling.
func (d *USBDevice) MsgOutEndpoint() uint8 {
	if d.msgOut != 0 {
		return d.msgOut
	}
	return d.bulkOut
}

// MsgInEndpoint returns the dedicated command IN endpoint (second bulk
// IN), or 0 when the device exposes only one.
func (d *USBDevice) MsgInEndpoint() uint8 { return d.msgIn }

// DumpConfig returns a human-readable summary of every interface and
// endpoint in the device's active configuration.
func (d *USBDevice) DumpConfig() (string, error) {
	dev := C.libusb_get_device(d.handle)
	if dev == nil {
		return "", fmt.Errorf("no device")
	}
	var cfg *C.struct_libusb_config_descriptor
	rc := C.get_active_config(dev, &cfg)
	if rc < 0 {
		return "", fmt.Errorf("get active config: %s", libusbErrname(rc))
	}
	defer C.free_config(cfg)
	buf := make([]byte, 4096)
	n := C.dump_config(cfg, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	return string(buf[:n]), nil
}

// Location returns the bus number and device address of the open device.
// Used to detect true re-enumeration (address changes on every reset).
func (d *USBDevice) Location() DeviceLocation {
	if d == nil || d.handle == nil {
		return DeviceLocation{}
	}
	dev := C.libusb_get_device(d.handle)
	if dev == nil {
		return DeviceLocation{}
	}
	return DeviceLocation{
		Bus:     uint8(C.get_bus_number(dev)),
		Address: uint8(C.get_device_address(dev)),
	}
}

// ResetDevice issues a USB port reset. Returns an error if the reset
// failed. Note: after a successful reset the device re-enumerates and
// the handle must be reopened — callers should close and reopen.
func (d *USBDevice) ResetDevice() error {
	rc := C.reset_device(d.handle)
	if rc < 0 {
		return fmt.Errorf("libusb_reset_device: %s", libusbErrname(rc))
	}
	return nil
}

// PersistentCaches opaquely retains the device pointer so we can later
// call GetBusNumber/GetAddress without a second open.
func (d *USBDevice) DetachKernelDriver(iface int) error {
	rc := C.detach_kernel_driver(d.handle, C.int(iface))
	if rc < 0 && rc != C.LIBUSB_ERROR_NOT_FOUND && rc != C.LIBUSB_ERROR_NOT_SUPPORTED {
		return fmt.Errorf("detach kernel driver iface %d: %s", iface, libusbErrname(rc))
	}
	return nil
}

// ClaimInterface claims an interface for userspace use.
func (d *USBDevice) ClaimInterface(iface int) error {
	rc := C.claim_interface(d.handle, C.int(iface))
	if rc < 0 {
		return fmt.Errorf("claim interface %d: %s", iface, libusbErrname(rc))
	}
	return nil
}

// ReleaseInterface releases a previously claimed interface.
func (d *USBDevice) ReleaseInterface(iface int) {
	C.release_interface(d.handle, C.int(iface))
}

// Close releases the device handle.
func (d *USBDevice) Close() {
	if d != nil && d.handle != nil {
		C.libusb_close(d.handle)
		d.handle = nil
	}
}

// BulkSend synchronously writes `data` to the bulk OUT endpoint. Returns
// the number of bytes actually transmitted.
func (d *USBDevice) BulkSend(endpoint uint8, data []byte, timeoutMs int) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	var transferred C.int
	rc := C.bulk_transfer(d.handle,
		C.uchar(endpoint),
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.int(len(data)),
		&transferred,
		C.uint(timeoutMs),
	)
	if rc < 0 {
		return int(transferred), fmt.Errorf("bulk OUT 0x%02x: %s", endpoint, libusbErrname(rc))
	}
	return int(transferred), nil
}

// BulkRecv synchronously reads from the bulk IN endpoint into `buf`. Returns
// the number of bytes actually received.
func (d *USBDevice) BulkRecv(endpoint uint8, buf []byte, timeoutMs int) (int, error) {
	if len(buf) == 0 {
		return 0, fmt.Errorf("zero-length recv buffer")
	}
	var transferred C.int
	rc := C.bulk_transfer(d.handle,
		C.uchar(endpoint|0x80),
		(*C.uchar)(unsafe.Pointer(&buf[0])),
		C.int(len(buf)),
		&transferred,
		C.uint(timeoutMs),
	)
	if rc < 0 {
		return int(transferred), fmt.Errorf("bulk IN 0x%02x: %s", endpoint, libusbErrname(rc))
	}
	return int(transferred), nil
}

// ControlSend performs a control OUT transfer.
func (d *USBDevice) ControlSend(requestType, request uint8, value, index uint16, data []byte, timeoutMs int) (int, error) {
	var ptr *C.uchar
	if len(data) > 0 {
		ptr = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	rc := C.ctrl_transfer(d.handle,
		C.uint8_t(requestType),
		C.uint8_t(request),
		C.uint16_t(value),
		C.uint16_t(index),
		ptr,
		C.uint16_t(len(data)),
		C.uint(timeoutMs),
	)
	if rc < 0 {
		return int(rc), fmt.Errorf("control OUT: %s", libusbErrname(rc))
	}
	return int(rc), nil
}

// libusbErrname formats a libusb error code into a human-readable string.
func libusbErrname(rc C.int) string {
	cStr := C.libusb_error_name(rc)
	if cStr == nil {
		return fmt.Sprintf("libusb error %d", int(rc))
	}
	return C.GoString(cStr)
}

// ListByVIDPID enumerates all devices matching the given VID/PID and
// returns their bus number + device address. Useful for re-enumeration
// polling after a stage transition.
func ListByVIDPID(ctx *C.libusb_context, vid, pid uint16) ([]DeviceLocation, error) {
	var list **C.libusb_device
	count := C.libusb_get_device_list(ctx, &list)
	if count < 0 {
		return nil, fmt.Errorf("get device list: %s", libusbErrname(C.int(count)))
	}
	defer C.libusb_free_device_list(list, 1)

	var out []DeviceLocation
	for i := C.ssize_t(0); i < count; i++ {
		dev := C.dev_at(list, i)
		var desc C.struct_libusb_device_descriptor
		if C.get_device_descriptor(dev, &desc) < 0 {
			continue
		}
		if uint16(desc.idVendor) != vid || uint16(desc.idProduct) != pid {
			continue
		}
		out = append(out, DeviceLocation{
			Bus:     uint8(C.get_bus_number(dev)),
			Address: uint8(C.get_device_address(dev)),
		})
	}
	return out, nil
}

// DeviceLocation uniquely identifies a USB device on the bus.
type DeviceLocation struct {
	Bus     uint8
	Address uint8
}

// AIC8800D80 fixed bulk endpoint addresses. From the Linux driver:
//
//   aic_load_fw/aicwf_usb.c:
//     bulk_in_pipe      = 0x84 (IN)
//     bulk_out_pipe     = 0x04 (OUT)
//     msg_out_pipe      = 0x06 (OUT, command)
//
// Bulk OUT (0x04) carries data and the 8-byte-prefixed command messages.
// Bulk IN  (0x84) carries command confirms.
//
// These are constants for the AIC8800D80 family; calling
// libusb_get_active_config_descriptor confirmed them on multiple devices.
const (
	BulkOUTEndpoint uint8 = 0x04
	BulkINEndpoint  uint8 = 0x84
)
