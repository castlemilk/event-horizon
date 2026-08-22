package protocol

import (
	"encoding/binary"
	"fmt"
	"log"
	"time"
)

// Drain reads and discards any frames pending on the bulk IN endpoint
// (short non-blocking polls), then resets the RX stream state so stale
// partial records don't poison subsequent parsing. The boot ROM queues
// stale confirmations from previous sessions; if left undrained they
// desync request/response pairing. Returns the number of frames
// discarded.
func Drain(dev *USBDevice, rounds int) int {
	in := dev.BulkInEndpoint()
	if in == 0 {
		return 0
	}
	dev.rx = RxStream{} // drop any partial buffered record
	buf := make([]byte, 2048)
	discarded := 0
	for i := 0; i < rounds; i++ {
		n, err := dev.BulkRecv(in, buf, 150)
		if err != nil || n == 0 {
			if discarded > 0 {
				log.Printf("[AIC] drained %d stale read(s)", discarded)
			}
			return discarded
		}
		discarded++
		log.Printf("[AIC] drain: discarding %d stale bytes", n)
	}
	return discarded
}

// DrainCapture reads pending bulk IN data into a raw capture buffer
// (short non-blocking polls). Unlike Drain it PRESERVES the bytes —
// used by the probe to capture a wedged device's fault dump record.
func DrainCapture(dev *USBDevice, rounds int) []byte {
	in := dev.BulkInEndpoint()
	if in == 0 {
		return nil
	}
	var captured []byte
	buf := make([]byte, 2048)
	for i := 0; i < rounds; i++ {
		n, err := dev.BulkRecv(in, buf, 150)
		if err != nil || n == 0 {
			break
		}
		captured = append(captured, buf[:n]...)
	}
	return captured
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// truncHex renders up to n bytes as hex for logging.
func truncHex(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

// TxLenConv selects the length-field convention used by
// BuildLmacMessage. The Linux driver computes (sizeof(lmac_msg)=8 +
// paramLen + 4) but some firmware builds observed in the wild expect
// the total frame length + 4 instead. The loader probes both.
//
//	ConvLinux (default): field = 8 + paramLen + 4
//	ConvTotal:           field = 16 + paramLen + 4
type TxLenConv int

const (
	ConvLinux TxLenConv = iota
	ConvTotal
)

// txLenConv is the active convention (package-level; the loader calibrates
// it once per run via SetTxLenConv).
var txLenConv TxLenConv = ConvLinux

// SetTxLenConv switches the TX length-field convention.
func SetTxLenConv(c TxLenConv) { txLenConv = c }

// BuildLmacMessage constructs a wire-format lmac_msg envelope:
//
//	[0..1]  length field (LE, lower 12 bits; convention per txLenConv)
//	[2]     0x11 (USB_TYPE_CFG_CMD)
//	[3]     0x00
//	[4..7]  dummy word (0)
//	[8..9]  msg_id (LE)
//	[10..11] dest_id (LE)
//	[12..13] src_id (LE)
//	[14..15] param_len (LE)
//	[16..]  payload
//
// Ported from aicbluetooth_cmds.c:aicwf_set_cmd_tx.
func BuildLmacMessage(msgID uint16, destID lmacTaskID, srcID lmacTaskID, payload []byte) []byte {
	paramLen := len(payload)
	totalLen := LmacMsgHeaderBytes + paramLen

	var lenField uint16
	switch txLenConv {
	case ConvTotal:
		lenField = uint16((totalLen + 4) & 0x0FFF)
	default:
		lenField = uint16((8 + paramLen + 4) & 0x0FFF)
	}

	buf := make([]byte, totalLen)
	buf[0] = byte(lenField & 0xFF)
	buf[1] = byte((lenField >> 8) & 0x0F)
	buf[2] = 0x11
	buf[3] = 0x00
	// bytes 4..7 = dummy word = 0
	binary.LittleEndian.PutUint16(buf[8:10], msgID)
	binary.LittleEndian.PutUint16(buf[10:12], uint16(destID))
	binary.LittleEndian.PutUint16(buf[12:14], uint16(srcID))
	binary.LittleEndian.PutUint16(buf[14:16], uint16(paramLen))
	copy(buf[16:], payload)
	return buf
}

// DBGMemReadReq payload: { u32 memaddr }
func MemReadPayload(addr uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, addr)
	return b
}

// DBGMemWriteReq payload: { u32 memaddr, u32 memdata }
func MemWritePayload(addr, data uint32) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], addr)
	binary.LittleEndian.PutUint32(b[4:8], data)
	return b
}

// DBGMemBlockWriteReq payload: { u32 memaddr, u32 memsize, u32 memdata[256] }
func MemBlockWritePayload(addr uint32, block []byte) []byte {
	if len(block) > 1024 {
		panic(fmt.Sprintf("block size %d exceeds 1024", len(block)))
	}
	b := make([]byte, 8+1024)
	binary.LittleEndian.PutUint32(b[0:4], addr)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(block)))
	copy(b[8:], block)
	return b
}

// DBGStartAppReq payload: { u32 bootaddr, u32 boottype }
func StartAppPayload(bootAddr, bootType uint32) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], bootAddr)
	binary.LittleEndian.PutUint32(b[4:8], bootType)
	return b
}

// ParseMemReadCfm parses the payload (ipc_e2a_msg) of a DBG_MEM_READ_CFM
// frame. Payload layout:
//
//	[0..1]   msg id (0x0401)
//	[2..3]   dummy_dest_id
//	[4..5]   dummy_src_id
//	[6..7]   param_len
//	[8..11]  pattern
//	[12..15] param[0] = memaddr
//	[16..19] param[1] = memdata
func ParseMemReadCfm(payload []byte) (addr, data uint32, err error) {
	if len(payload) < 20 {
		return 0, 0, fmt.Errorf("mem read cfm: payload too short (%d bytes)", len(payload))
	}
	msgID := binary.LittleEndian.Uint16(payload[0:2])
	if msgID != DBGMemReadCfm {
		return 0, 0, fmt.Errorf("mem read cfm: msg_id 0x%04x != 0x%04x", msgID, DBGMemReadCfm)
	}
	addr = binary.LittleEndian.Uint32(payload[12:16])
	data = binary.LittleEndian.Uint32(payload[16:20])
	return addr, data, nil
}

// ParseCfm validates a confirm payload (MemWrite, MemBlockWrite) by id.
func ParseCfm(payload []byte, expectedMsgID uint16) error {
	if len(payload) < 2 {
		return fmt.Errorf("cfm: payload too short (%d bytes)", len(payload))
	}
	msgID := binary.LittleEndian.Uint16(payload[0:2])
	if msgID != expectedMsgID {
		return fmt.Errorf("cfm: msg_id 0x%04x != expected 0x%04x", msgID, expectedMsgID)
	}
	return nil
}

// chunkReader reads one bulk IN transfer into buf with a timeout in ms.
// Abstracted so the confirm-wait loop can be unit-tested against a
// simulated device without libusb.
type chunkReader func(buf []byte, timeoutMs int) (int, error)

// waitForCfm extracts frames from rx (feeding it from read) until the
// config frame with wantID arrives. Data frames and unrelated config
// frames are discarded with their proper stride. This mirrors the
// production receive path — the device aggregates multiple
// length-prefixed frames per USB transfer, so parsing MUST go through
// RxStream rather than assuming one frame per read.
func waitForCfm(rx *RxStream, wantID uint16, read chunkReader, timeoutMs int) ([]byte, error) {
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	buf := make([]byte, 2048)
	for {
		// Drain any frames already buffered from previous reads.
		for {
			f, ok, err := rx.Next()
			if err != nil {
				log.Printf("[AIC-RX] stream error: %v (resyncing)", err)
				continue
			}
			if !ok {
				break
			}
			if f.IsConfig() {
				if f.MsgID() == wantID {
					return f.Payload, nil
				}
				// Full hex dump for unexpected ids — error frames (e.g.
				// 0xf105 observed when writing past a RAM window) carry
				// diagnostic detail in their params.
				log.Printf("[AIC-RX] UNEXPECTED config frame id=0x%04x (want 0x%04x) payload: % x",
					f.MsgID(), wantID, truncHex(f.Payload, 32))
			} else {
				log.Printf("[AIC-RX] discarding data frame (%d bytes)", len(f.Payload))
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("recv cfm for msg 0x%04x: timeout", wantID-1)
		}
		readMs := int(remaining / time.Millisecond)
		if readMs > 5000 {
			readMs = 5000
		}
		if readMs < 200 {
			readMs = 200
		}
		n, err := read(buf, readMs)
		if err != nil {
			return nil, fmt.Errorf("recv cfm for msg 0x%04x: %w", wantID-1, err)
		}
		rx.Feed(buf[:n])
	}
}

// SendRequest sends a single lmac_msg and waits for the matching confirm
// on the bulk IN endpoint. The device AGGREGATES multiple length-prefixed
// frames per USB transfer, so received bytes are fed through an RxStream
// and frames are extracted one record at a time (mirroring Linux
// aicwf_process_rxframes). Returns the confirm frame's ipc_e2a_msg payload.
func SendRequest(dev *USBDevice, msgID uint16, destID lmacTaskID, payload []byte, timeoutMs int) ([]byte, error) {
	out := dev.BulkOutEndpoint()
	if out == 0 {
		return nil, fmt.Errorf("device has no bulk OUT endpoint discovered")
	}
	frame := BuildLmacMessage(msgID, destID, DrvTaskID, payload)
	if _, err := dev.BulkSend(out, frame, timeoutMs); err != nil {
		return nil, fmt.Errorf("send msg 0x%04x: %w", msgID, err)
	}
	// Confirm expected for any msg except DBG_START_APP_REQ (which has no
	// confirmation in the Linux driver — confirmed by `reqcfm=0`).
	if msgID == DBGStartAppReq {
		return nil, nil
	}
	in := dev.BulkInEndpoint()
	if in == 0 {
		return nil, fmt.Errorf("device has no bulk IN endpoint discovered")
	}
	rx := &dev.rx
	read := func(buf []byte, ms int) (int, error) {
		return dev.BulkRecv(in, buf, ms)
	}
	return waitForCfm(rx, msgID+1, read, timeoutMs)
}

// MemRead performs a DBG_MEM_READ_REQ and returns the 32-bit value at
// `addr` on the device.
func MemRead(dev *USBDevice, addr uint32) (uint32, error) {
	cfm, err := SendRequest(dev, DBGMemReadReq, TaskDBG, MemReadPayload(addr), 30000)
	if err != nil {
		return 0, err
	}
	_, data, err := ParseMemReadCfm(cfm)
	return data, err
}

// MemWrite writes a single 32-bit word at `addr`.
func MemWrite(dev *USBDevice, addr, data uint32) error {
	cfm, err := SendRequest(dev, DBGMemWriteReq, TaskDBG, MemWritePayload(addr, data), 30000)
	if err != nil {
		return err
	}
	return ParseCfm(cfm, DBGMemWriteCfm)
}

// MemBlockWrite writes `block` (≤1024 bytes) to `addr` via DBG_MEM_BLOCK_WRITE_REQ.
func MemBlockWrite(dev *USBDevice, addr uint32, block []byte) error {
	if len(block) > 1024 {
		return fmt.Errorf("block too large: %d > 1024", len(block))
	}
	cfm, err := SendRequest(dev, DBGMemBlockWriteReq, TaskDBG, MemBlockWritePayload(addr, block), 30000)
	if err != nil {
		return err
	}
	return ParseCfm(cfm, DBGMemBlockWriteCfm)
}

// MemBlockWriteAll writes an arbitrarily-large blob in 1KiB chunks.
func MemBlockWriteAll(dev *USBDevice, addr uint32, blob []byte, progress func(written int)) error {
	return MemBlockWriteAllSkipping(dev, addr, blob, 0, 0, progress)
}

// MemBlockWriteAllSkipping writes a blob in 1KiB chunks but NEVER writes
// into the absolute address range [skipStart, skipEnd) (ignored when
// skipEnd <= skipStart). Chunks overlapping the skip range are split
// around it.
//
// Rationale: the Pandora-clone AIC8800D80 boot ROM wedges into USB echo
// mode when the word at 0x1701e4 is written — deterministic and
// address-based (zeros trip it). The fmacfw image contains real code
// there, so the loader skips it and lets the firmware boot with 4
// missing bytes (`01 29 d2 b2` = cmp r1,#1 / uxth r2,r2).
func MemBlockWriteAllSkipping(dev *USBDevice, addr uint32, blob []byte, skipStart, skipEnd uint32, progress func(written int)) error {
	if len(blob) == 0 {
		return nil
	}
	if skipEnd <= skipStart {
		// Plain chunked upload.
		written := 0
		for written < len(blob) {
			end := written + BlockWriteChunkBytes
			if end > len(blob) {
				end = len(blob)
			}
			chunkLen := end - written
			if err := MemBlockWrite(dev, addr+uint32(written), blob[written:end]); err != nil {
				return fmt.Errorf("block write @ 0x%x (offset %d / %d): %w",
					addr+uint32(written), written, len(blob), err)
			}
			if progress != nil {
				progress(chunkLen)
			}
			written = end
		}
		return nil
	}
	// Chunked upload with a hole: walk the blob once, emitting writes for
	// [runStart, runEnd) runs that avoid the skip range.
	runStart := 0
	for runStart < len(blob) {
		absStart := addr + uint32(runStart)
		if absStart >= skipEnd {
			break // skip range behind us; switch to plain chunking below
		}
		runEnd := runStart + BlockWriteChunkBytes
		if runEnd > len(blob) {
			runEnd = len(blob)
		}
		absEnd := addr + uint32(runEnd)
		if absEnd > skipStart && absStart < skipEnd {
			// Clip this run to the skip boundary.
			runEnd = int(skipStart - addr)
			if runEnd <= runStart {
				// Entire chunk inside skip range — skip it whole.
				runStart = int(skipEnd - addr)
				continue
			}
			// Emit the pre-skip run, then jump past the skip range.
			if err := MemBlockWrite(dev, addr+uint32(runStart), blob[runStart:runEnd]); err != nil {
				return fmt.Errorf("block write @ 0x%x (offset %d / %d): %w",
					addr+uint32(runStart), runStart, len(blob), err)
			}
			if progress != nil {
				progress(runEnd)
			}
			runStart = int(skipEnd - addr)
			continue
		}
		if err := MemBlockWrite(dev, addr+uint32(runStart), blob[runStart:runEnd]); err != nil {
			return fmt.Errorf("block write @ 0x%x (offset %d / %d): %w",
				addr+uint32(runStart), runStart, len(blob), err)
		}
		runStart = runEnd
		if progress != nil {
			progress(runStart)
		}
	}
	// Tail: plain chunking from wherever we stopped.
	for runStart < len(blob) {
		end := runStart + BlockWriteChunkBytes
		if end > len(blob) {
			end = len(blob)
		}
		if err := MemBlockWrite(dev, addr+uint32(runStart), blob[runStart:end]); err != nil {
			return fmt.Errorf("block write @ 0x%x (offset %d / %d): %w",
				addr+uint32(runStart), runStart, len(blob), err)
		}
		runStart = end
		if progress != nil {
			progress(runStart)
		}
	}
	return nil
}

// StartApp boots the firmware at `bootAddr` with `bootType` (1=auto,
// 2=custom, 3=reboot). This causes the device to re-enumerate as
// operational (0x8d81 or 0x8d83).
func StartApp(dev *USBDevice, bootAddr, bootType uint32) error {
	_, err := SendRequest(dev, DBGStartAppReq, TaskDBG, StartAppPayload(bootAddr, bootType), 5000)
	return err
}

// ReadSystemConfig reads a 32-bit system config register at the given
// address and returns both the high word (chip_id) and the embedded
// `mcu_id` bit. The Linux driver reads 0x40500000 to extract:
//
//	chip_id    = (value >> 16) & 0xff
//	chip_mcu_id = (value >> 25) & 0x01
//
// See aic_compat_8800d80.c:system_config_8800d80.
func ReadSystemConfig(dev *USBDevice) (chipID, chipMCUID uint8, err error) {
	const memAddr = 0x40500000
	val, err := MemRead(dev, memAddr)
	if err != nil {
		return 0, 0, fmt.Errorf("read 0x40500000: %w", err)
	}
	chipID = uint8((val >> 16) & 0xFF)
	chipMCUID = uint8((val >> 25) & 0x01)
	return chipID, chipMCUID, nil
}

// DefaultBulkPollTimeout is the default timeout for a single bulk
// transfer. The firmware can take several seconds to process a 1024-byte
// block write.
const DefaultBulkPollTimeout = 5 * time.Second
