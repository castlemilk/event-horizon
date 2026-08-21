package protocol

import (
	"context"
	"fmt"
)

// Session is an opened, claimed connection to the dongle at Operational
// stage. It owns the libusb context and device handle; Close releases
// everything in reverse order.
type Session struct {
	ctx *libusbContext
	dev *USBDevice
}

// OpenOperational finds the dongle running operational firmware
// (a69c:8d81 WiFi+BT or a69c:8d83 WiFi-only), opens it, detaches any
// kernel driver and claims interface 0. The caller must Close the
// returned session.
func OpenOperational(ctx context.Context) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := Init()
	if err != nil {
		return nil, err
	}
	s := &Session{ctx: c}
	open := func(vid, pid uint16) (*USBDevice, error) {
		return OpenByVIDPID(c, vid, pid)
	}
	dev, err := open(VID_AIC8800D80_OpWiFiBT, PID_AIC8800D80_OpWiFiBT)
	if err != nil {
		dev, err = open(VID_AIC8800D80_OpWiFi, PID_AIC8800D80_OpWiFi)
	}
	if err != nil {
		Deinit(c)
		return nil, fmt.Errorf("open operational device: %w", err)
	}
	s.dev = dev
	if err := dev.DetachKernelDriver(0); err != nil {
		dev.Close()
		Deinit(c)
		return nil, fmt.Errorf("detach kernel driver: %w", err)
	}
	if err := dev.ClaimInterface(0); err != nil {
		dev.Close()
		Deinit(c)
		return nil, fmt.Errorf("claim interface 0: %w", err)
	}
	return s, nil
}

// Device returns the underlying USB device.
func (s *Session) Device() *USBDevice { return s.dev }

// BulkOut writes one frame to the bulk OUT endpoint (lmac.BulkWriter shape).
func (s *Session) BulkOut(_ context.Context, frame []byte) error {
	_, err := s.dev.BulkSend(BulkOUTEndpoint, frame, 1000)
	return err
}

// BulkIn reads one chunk from the bulk IN endpoint into buf. Returns the
// number of bytes received.
func (s *Session) BulkIn(buf []byte, timeoutMs int) (int, error) {
	return s.dev.BulkRecv(BulkINEndpoint, buf, timeoutMs)
}

// Close releases the interface, closes the handle and deinitialises the
// libusb context. Safe to call more than once.
func (s *Session) Close() {
	if s == nil {
		return
	}
	if s.dev != nil {
		s.dev.ReleaseInterface(0)
		s.dev.Close()
		s.dev = nil
	}
	if s.ctx != nil {
		Deinit(s.ctx)
		s.ctx = nil
	}
}
