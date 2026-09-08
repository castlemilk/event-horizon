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
// kernel driver and claims the WLAN interface (e.g. interface 2 on WiFi+BT).
// The caller must Close the returned session.
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
		dev, err = open(VID_AIC8800D80_OpWin, PID_AIC8800D80_OpWin)
	}
	if err != nil {
		Deinit(c)
		return nil, fmt.Errorf("open operational device: %w", err)
	}
	s.dev = dev
	iface := int(dev.InterfaceNumber())
	if err := dev.DetachKernelDriver(iface); err != nil {
		dev.Close()
		Deinit(c)
		return nil, fmt.Errorf("detach kernel driver iface %d: %w", iface, err)
	}
	if err := dev.ClaimInterface(iface); err != nil {
		dev.Close()
		Deinit(c)
		return nil, fmt.Errorf("claim interface %d: %w", iface, err)
	}
	return s, nil
}

// Device returns the underlying USB device.
func (s *Session) Device() *USBDevice { return s.dev }

// BulkOut writes one command frame to the dedicated msg OUT endpoint
// (lmac.BulkWriter shape). Falls back to the data OUT endpoint when the
// device exposes only one bulk OUT.
func (s *Session) BulkOut(_ context.Context, frame []byte) error {
	if s == nil || s.dev == nil {
		return fmt.Errorf("session closed")
	}
	_, err := s.dev.BulkSend(s.dev.MsgOutEndpoint(), frame, 1000)
	return err
}

// BulkOutData writes one TX data record (TxData.Encode, type 0x01) to the
// first bulk OUT endpoint — the data pipe per the reference driver.
func (s *Session) BulkOutData(_ context.Context, frame []byte) error {
	if s == nil || s.dev == nil {
		return fmt.Errorf("session closed")
	}
	_, err := s.dev.BulkSend(s.dev.BulkOutEndpoint(), frame, 1000)
	return err
}

// BulkOutDataMsg writes one TX data record to the second (command) OUT
// endpoint instead. Normally wrong per the reference — but if the data pipe
// is dead on this firmware while commands flow, this is the experiment that
// proves where TX really goes.
func (s *Session) BulkOutDataMsg(_ context.Context, frame []byte) error {
	if s == nil || s.dev == nil {
		return fmt.Errorf("session closed")
	}
	_, err := s.dev.BulkSend(s.dev.MsgOutEndpoint(), frame, 1000)
	return err
}

// BulkIn reads one chunk from the bulk IN endpoint into buf. Returns the
// number of bytes received.
func (s *Session) BulkIn(buf []byte, timeoutMs int) (int, error) {
	if s == nil || s.dev == nil {
		return 0, fmt.Errorf("session closed")
	}
	return s.dev.BulkRecv(s.dev.BulkInEndpoint(), buf, timeoutMs)
}

// HasMsgIn reports whether the device exposes a second (command) IN endpoint.
func (s *Session) HasMsgIn() bool {
	return s != nil && s.dev != nil && s.dev.HasMsgIn()
}

// MsgIn reads one chunk from the dedicated command IN endpoint. The firmware
// routes some responses there (notably after MM_SET_STACK_START), so it is
// drained alongside BulkIn.
func (s *Session) MsgIn(buf []byte, timeoutMs int) (int, error) {
	if s == nil || s.dev == nil {
		return 0, fmt.Errorf("session closed")
	}
	return s.dev.MsgIn(buf, timeoutMs)
}

// Close releases the interface, closes the handle and deinitialises the
// libusb context. Safe to call more than once.
func (s *Session) Close() {
	if s == nil {
		return
	}
	if s.dev != nil {
		s.dev.ReleaseInterface(int(s.dev.InterfaceNumber()))
		s.dev.Close()
		s.dev = nil
	}
	if s.ctx != nil {
		Deinit(s.ctx)
		s.ctx = nil
	}
}
