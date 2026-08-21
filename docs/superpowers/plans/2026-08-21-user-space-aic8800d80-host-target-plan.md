# User-space AIC8800D80 host-target channel — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Open a sustained host-target command channel to the operational AIC8800D80 firmware from user-space Go, demonstrate by issuing `SCANU_START_REQ` and consuming `SCANU_RESULT_IND` events.

**Architecture:** Five layers — CLI (`cmd/usbwifi/cmdctl/`) → event loop (`pkg/aic8800d80/event/`) → LMAC builders + Submitter (`pkg/aic8800d80/lmac/`) → existing protocol (`pkg/aic8800d80/protocol/`) → libusb. Single event goroutine owns RxStream; Submitter serializes commands and waits for firmware ACK.

**Tech Stack:** Go 1.22+, libusb via cgo (`pkg/aic8800d80/protocol/transport.go` already binds it), Go stdlib (`encoding/binary`, `sync`, `context`).

**Spec:** `docs/superpowers/specs/2026-08-21-user-space-aic8800d80-host-target-design.md`

**Plan-review pass:** Skipped at user direction (2026-08-21, "proceed, implement this now exhaustively").

---

## File structure (new / modified)

```
pkg/aic8800d80/lmac/
├── msgids.go             # Task + message ID constants from rwnx_cmds.h / lmac_msg.h
├── header.go             # lmac_msg struct + encode helpers
├── header_test.go
├── tlv.go                # TLV parameter encoding (DBG_TLV format)
├── tlv_test.go
├── scan.go               # SCANU_START_REQ + SCANU_RESULT_IND structs
├── scan_test.go
├── version.go            # MM_VERSION_REQ + MM_VERSION_CFM structs
├── version_test.go
├── submitter.go          # Submitter: serializes outbound commands
├── submitter_test.go
└── errors.go             # SubmitError + ErrUnknownTLV

pkg/aic8800d80/event/
├── sink.go               # Sink interface
├── sink_test.go
├── loop.go               # Loop: owns RxStream, dispatches
├── loop_test.go
├── dispatch.go           # Default dispatch table (SCANU_RESULT_IND, etc.)
└── errors.go             # ErrFatalDesync

cmd/usbwifi/cmdctl/
├── main.go               # CLI: send <msg>, listen [--msg-id=N]
└── dispatch.go           # Pretty-print SCANU_RESULT_IND as SSID + RSSI

cmd/usbwifi/
└── main.go               # MODIFY: register cmdctl subcommand
```

Total new code: ~1500 lines Go + tests.

---

## Chunk 1: message IDs, header, TLV

### Task 1.1: Define message ID constants

**Files:**
- Create: `pkg/aic8800d80/lmac/msgids.go`
- Test: `pkg/aic8800d80/lmac/msgids_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lmac

import "testing"

func TestTaskIDs(t *testing.T) {
    if TaskMM != 0 || TaskDBG != 1 || TaskSCANU != 4 {
        t.Fatalf("task id drift: MM=%d DBG=%d SCANU=%d", TaskMM, TaskDBG, TaskSCANU)
    }
}

func TestLMACFirstMsg(t *testing.T) {
    if FirstMsg(TaskMM) != 0x0000 || FirstMsg(TaskDBG) != 0x0400 || FirstMsg(TaskSCANU) != 0x1000 {
        t.Fatalf("FirstMsg arithmetic drifted")
    }
}
```

- [ ] **Step 2: Run test, expect FAIL with "undefined: TaskMM"**

Run: `go test ./pkg/aic8800d80/lmac/ -run TestTaskIDs`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// pkg/aic8800d80/lmac/msgids.go
package lmac

// Task identifiers (lmac_msg.h TASK_*). Each task owns a 1024-message ID space
// at bit position (task << 10).
const (
    TaskMM    uint8 = 0
    TaskDBG   uint8 = 1
    TaskSCAN  uint8 = 2
    TaskSCANU uint8 = 4
    TaskME    uint8 = 6
    TaskSM    uint8 = 7
    TaskAPM   uint8 = 8
    TaskBAM   uint8 = 9
    TaskLast  uint8 = 11 // TASK_RM
    TaskAPI   uint8 = 12
    TaskMax   uint8 = 13
)

// FirstMsg returns the base message ID for a task (LMAC_FIRST_MSG).
func FirstMsg(task uint8) uint16 {
    return uint16(task) << 10
}
```

- [ ] **Step 4: Run test, expect PASS**

- [ ] **Step 5: Add MM_VERSION/SCANU/SCANU_RESULT_IND constants + test**

Add to msgids.go (append):
```go
// MM task messages (TASK_MM).
const (
    MMResetReq    uint16 = 0x0000 // LMAC_FIRST_MSG(TASK_MM)
    MMResetCfm    uint16 = 0x0001
    MMStartReq    uint16 = 0x0002
    MMStartCfm    uint16 = 0x0003
    MMVersionReq  uint16 = 0x0004
    MMVersionCfm  uint16 = 0x0005
)

// DBG task messages (TASK_DBG).
const (
    DBGMemReadReq     uint16 = 0x0400
    DBGMemReadCfm     uint16 = 0x0401
    DBGMemWriteReq    uint16 = 0x0402
    DBGMemWriteCfm    uint16 = 0x0403
    DBGStartAppReq    uint16 = 0x0480
    DBGStartAppCfm    uint16 = 0x0481
    MMDbgTlvCmdReq    uint16 = 0x0482 // MM_DBG_TLV_CMD (used for FW_VERSION + more)
    MMDbgTlvCmdCfm    uint16 = 0x0483
)

// SCANU task messages (TASK_SCANU).
const (
    SCANUStartReq    uint16 = 0x1000
    SCANUStartCfm    uint16 = 0x1001
    SCANUJoinReq     uint16 = 0x1002
    SCANUJoinCfm     uint16 = 0x1003
    SCANUResultInd   uint16 = 0x1004 // async scan-result indication
    SCANUFASTReq     uint16 = 0x1005
    SCANUFASTCfm     uint16 = 0x1006
    SCANUCancelReq   uint16 = 0x100C
    SCANUCancelCfm   uint16 = 0x100D
)
```

Add to msgids_test.go:
```go
func TestMessageIDConstants(t *testing.T) {
    cases := []struct{ got, want uint16 }{
        {MMVersionReq, 0x0004},
        {SCANUStartReq, 0x1000},
        {SCANUResultInd, 0x1004},
        {MMDbgTlvCmdReq, 0x0482},
    }
    for _, c := range cases {
        if c.got != c.want {
            t.Errorf("msg id = 0x%04x, want 0x%04x", c.got, c.want)
        }
    }
}
```

- [ ] **Step 6: Run, expect PASS**

- [ ] **Step 7: Commit**

```bash
git add pkg/aic8800d80/lmac/msgids.go pkg/aic8800d80/lmac/msgids_test.go
git commit -m "feat(lmac): message ID constants from lmac_msg.h"
```

### Task 1.2: Header encoding

**Files:**
- Create: `pkg/aic8800d80/lmac/header.go`
- Test: `pkg/aic8800d80/lmac/header_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lmac

import (
    "bytes"
    "encoding/binary"
    "testing"
)

func TestHeaderRoundTrip(t *testing.T) {
    h := Header{ID: MMVersionReq, DestID: TaskLast, SrcID: 100, ParamLen: 8}
    buf := make([]byte, 8)
    h.Encode(buf)
    var got Header
    if err := got.Decode(buf); err != nil {
        t.Fatal(err)
    }
    if got != h {
        t.Fatalf("round-trip drifted: got %+v want %+v", got, h)
    }
}

func TestHeaderLayout(t *testing.T) {
    // Mirror lmac_msg struct: id(u16) + dest_id(u16) + src_id(u16) + param_len(u16) — 8 bytes.
    h := Header{ID: 0x1234, DestID: 0x0005, SrcID: 100, ParamLen: 64}
    buf := make([]byte, 8)
    h.Encode(buf)
    if binary.LittleEndian.Uint16(buf[0:2]) != 0x1234 {
        t.Errorf("id slot drift")
    }
    if binary.LittleEndian.Uint16(buf[2:4]) != 0x0005 {
        t.Errorf("dest_id slot drift")
    }
    if binary.LittleEndian.Uint16(buf[4:6]) != 100 {
        t.Errorf("src_id slot drift")
    }
    if binary.LittleEndian.Uint16(buf[6:8]) != 64 {
        t.Errorf("param_len slot drift")
    }
}
```

- [ ] **Step 2: Run, expect FAIL "undefined: Header"**

- [ ] **Step 3: Implement**

```go
// pkg/aic8800d80/lmac/header.go
package lmac

import (
    "encoding/binary"
    "fmt"
)

// Header mirrors struct lmac_msg from lmac_msg.h. All fields little-endian.
// The trailing param[] array is encoded separately by the caller.
type Header struct {
    ID       uint16 // message id
    DestID   uint16 // destination task id
    SrcID    uint16 // source task id (DRV_TASK_ID = 100 for host)
    ParamLen uint16 // byte length of the param[] payload
}

// HeaderSize is the fixed prefix length.
const HeaderSize = 8

// Encode writes the 8-byte header into dst (must be >= HeaderSize bytes).
func (h Header) Encode(dst []byte) {
    binary.LittleEndian.PutUint16(dst[0:2], h.ID)
    binary.LittleEndian.PutUint16(dst[2:4], h.DestID)
    binary.LittleEndian.PutUint16(dst[4:6], h.SrcID)
    binary.LittleEndian.PutUint16(dst[6:8], h.ParamLen)
}

// Decode parses the 8-byte header from buf (must be >= HeaderSize bytes).
func (h *Header) Decode(buf []byte) error {
    if len(buf) < HeaderSize {
        return fmt.Errorf("lmac header: short buffer (%d bytes)", len(buf))
    }
    h.ID = binary.LittleEndian.Uint16(buf[0:2])
    h.DestID = binary.LittleEndian.Uint16(buf[2:4])
    h.SrcID = binary.LittleEndian.Uint16(buf[4:6])
    h.ParamLen = binary.LittleEndian.Uint16(buf[6:8])
    return nil
}

// DRVTaskID is the source task id for host-initiated messages (DRV_TASK_ID = 100).
const DRVTaskID uint16 = 100
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add pkg/aic8800d80/lmac/header.go pkg/aic8800d80/lmac/header_test.go
git commit -m "feat(lmac): header encode/decode (struct lmac_msg)"
```

### Task 1.3: TLV parameter encoding (for MM_DBG_TLV_CMD)

**Files:**
- Create: `pkg/aic8800d80/lmac/tlv.go`
- Test: `pkg/aic8800d80/lmac/tlv_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lmac

import (
    "bytes"
    "encoding/binary"
    "testing"
)

func TestTLVPutGetRoundTrip(t *testing.T) {
    var buf bytes.Buffer
    PutTLV(&buf, 0x0042, []byte{1, 2, 3, 4})
    PutTLV(&buf, 0x0100, []byte{0xaa, 0xbb})
    out := buf.Bytes()
    var items []TLV
    rest, err := GetAllTLV(out, &items)
    if err != nil {
        t.Fatal(err)
    }
    if len(rest) != 0 {
        t.Fatalf("trailing bytes: %d", len(rest))
    }
    if len(items) != 2 {
        t.Fatalf("got %d items, want 2", len(items))
    }
    if items[0].Tag != 0x0042 || !bytes.Equal(items[0].Value, []byte{1, 2, 3, 4}) {
        t.Errorf("item 0: %+v", items[0])
    }
    if items[1].Tag != 0x0100 || !bytes.Equal(items[1].Value, []byte{0xaa, 0xbb}) {
        t.Errorf("item 1: %+v", items[1])
    }
}

func TestTLVShortTagValue(t *testing.T) {
    var buf bytes.Buffer
    PutTLV(&buf, 0x0001, nil)
    out := buf.Bytes()
    items := []TLV{{Tag: 0, Value: make([]byte, 0)}}
    _, err := GetAllTLV(out, &items)
    if err != nil {
        t.Fatal(err)
    }
    if items[0].Tag != 0x0001 || len(items[0].Value) != 0 {
        t.Errorf("empty TLV drift: %+v", items[0])
    }
}

func TestTLVHeaderSize(t *testing.T) {
    var buf bytes.Buffer
    PutTLV(&buf, 0x0001, []byte{1, 2, 3, 4})
    if binary.LittleEndian.Uint16(buf.Bytes()[0:2]) != 0x0001 {
        t.Errorf("tag slot drift")
    }
    if binary.LittleEndian.Uint16(buf.Bytes()[2:4]) != 4 {
        t.Errorf("len slot drift")
    }
}
```

- [ ] **Step 2: Run, expect FAIL "undefined: PutTLV"**

- [ ] **Step 3: Implement**

```go
// pkg/aic8800d80/lmac/tlv.go
package lmac

import (
    "encoding/binary"
    "fmt"
    "io"
)

// TLV is a (tag, value) pair. Tag is u16 LE, value length is u16 LE.
type TLV struct {
    Tag   uint16
    Value []byte
}

// PutTLV writes a single TLV: [tag:2 LE][len:2 LE][value:len].
func PutTLV(w io.Writer, tag uint16, value []byte) error {
    if err := binary.Write(w, binary.LittleEndian, tag); err != nil {
        return err
    }
    if err := binary.Write(w, binary.LittleEndian, uint16(len(value))); err != nil {
        return err
    }
    if len(value) == 0 {
        return nil
    }
    _, err := w.Write(value)
    return err
}

// GetAllTLV parses a flat sequence of TLVs into *out (appending). Returns the
// unconsumed tail (must be empty for a well-formed message).
func GetAllTLV(buf []byte, out *[]TLV) ([]byte, error) {
    for len(buf) >= 4 {
        tag := binary.LittleEndian.Uint16(buf[0:2])
        ln := binary.LittleEndian.Uint16(buf[2:4])
        end := 4 + int(ln)
        if end > len(buf) {
            return nil, fmt.Errorf("tlv: truncated value (tag 0x%04x want %d have %d)", tag, ln, len(buf)-4)
        }
        *out = append(*out, TLV{Tag: tag, Value: append([]byte(nil), buf[4:end]...)})
        buf = buf[end:]
    }
    return buf, nil
}
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add pkg/aic8800d80/lmac/tlv.go pkg/aic8800d80/lmac/tlv_test.go
git commit -m "feat(lmac): TLV encode/decode for DBG_TLV payload"
```

---

## Chunk 2: scan + version message structs

### Task 2.1: MM_VERSION message

**Files:**
- Create: `pkg/aic8800d80/lmac/version.go`
- Test: `pkg/aic8800d80/lmac/version_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lmac

import (
    "bytes"
    "testing"
)

func TestVersionReqEmptyPayload(t *testing.T) {
    msg := VersionReq{}
    buf, err := msg.Encode()
    if err != nil {
        t.Fatal(err)
    }
    h, rest, err := SplitMessage(buf)
    if err != nil {
        t.Fatal(err)
    }
    if h.ID != MMVersionReq || h.ParamLen != 0 || len(rest) != 0 {
        t.Fatalf("drift: hdr=%+v rest=%x", h, rest)
    }
}

func TestVersionCfmDecode(t *testing.T) {
    // Firmware version string "1.2.3" + 32-bit version + 32-bit mac version.
    payload := []byte{
        0x01, 0x02, 0x03, 0x04, // version
        0x05, 0x06, 0x07, 0x08, // mac_version
        0x09, 0x0A, 0x0B, 0x0C, // fw_build_id (placeholder)
        // version string, NUL-terminated, padded to 4-byte boundary
        '1', '.', '2', '.', '3', 0x00, 0x00, 0x00,
    }
    var cfm VersionCfm
    if err := cfm.Decode(payload); err != nil {
        t.Fatal(err)
    }
    if cfm.Version != 0x04030201 {
        t.Errorf("version: got 0x%08x", cfm.Version)
    }
    if cfm.MacVersion != 0x08070605 {
        t.Errorf("mac_version: got 0x%08x", cfm.MacVersion)
    }
    if cfm.VersionString != "1.2.3" {
        t.Errorf("version string: %q", cfm.VersionString)
    }
}
```

- [ ] **Step 2: Run, expect FAIL "undefined: VersionReq"**

- [ ] **Step 3: Implement `SplitMessage` helper in header.go (extend)**

Add to `pkg/aic8800d80/lmac/header.go`:
```go
// SplitMessage decodes the header from buf and returns (header, payload).
// The payload is a sub-slice of buf (does not copy).
func SplitMessage(buf []byte) (Header, []byte, error) {
    var h Header
    if err := h.Decode(buf); err != nil {
        return h, nil, err
    }
    if HeaderSize+int(h.ParamLen) > len(buf) {
        return h, nil, fmt.Errorf("lmac: short message (have %d want %d)", len(buf), HeaderSize+int(h.ParamLen))
    }
    return h, buf[HeaderSize : HeaderSize+int(h.ParamLen)], nil
}
```

- [ ] **Step 4: Implement VersionReq/VersionCfm**

```go
// pkg/aic8800d80/lmac/version.go
package lmac

import (
    "bytes"
    "encoding/binary"
    "fmt"
)

// VersionReq is MM_VERSION_REQ — has no payload.
type VersionReq struct{}

// Encode returns the full lmac_msg (header + empty param[]).
func (VersionReq) Encode() ([]byte, error) {
    buf := make([]byte, HeaderSize)
    Header{ID: MMVersionReq, DestID: uint16(TaskLast), SrcID: DRVTaskID, ParamLen: 0}.Encode(buf)
    return buf, nil
}

// VersionCfm is MM_VERSION_CFM. Layout per rwnx_main.c::version_cfm_handler:
// u32 version; u32 mac_version; u32 fw_build_id; char version_string[128] (NUL-padded).
type VersionCfm struct {
    Version       uint32
    MacVersion    uint32
    FwBuildID     uint32
    VersionString string
}

func (c *VersionCfm) Decode(payload []byte) error {
    if len(payload) < 16 {
        return fmt.Errorf("version cfm: short payload (%d bytes)", len(payload))
    }
    c.Version = binary.LittleEndian.Uint32(payload[0:4])
    c.MacVersion = binary.LittleEndian.Uint32(payload[4:8])
    c.FwBuildID = binary.LittleEndian.Uint32(payload[8:12])
    str := payload[12:]
    if i := bytes.IndexByte(str, 0); i >= 0 {
        c.VersionString = string(str[:i])
    } else {
        c.VersionString = string(bytes.TrimRight(str, "\x00"))
    }
    return nil
}
```

- [ ] **Step 5: Run, expect PASS**

- [ ] **Step 6: Commit**

```bash
git add pkg/aic8800d80/lmac/version.go pkg/aic8800d80/lmac/version_test.go pkg/aic8800d80/lmac/header.go
git commit -m "feat(lmac): MM_VERSION_REQ/CFM encode+decode"
```

### Task 2.2: SCANU_START_REQ struct

**Files:**
- Create: `pkg/aic8800d80/lmac/scan.go`
- Test: `pkg/aic8800d80/lmac/scan_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lmac

import (
    "testing"
)

func TestScanStartReqEncode(t *testing.T) {
    req := ScanStartReq{
        Band:       Band2G,
        Channels:   []ChannelInfo{{Prim20Ch: 1, Center1: 1, Center2: 0, Width: ChanWidth20}},
        SSIDs:      []string{"foo", "bar"},
        BSSID:      BroadcastBSSID,
        ProbeDelay: 10,
    }
    buf, err := req.Encode()
    if err != nil {
        t.Fatal(err)
    }
    h, payload, err := SplitMessage(buf)
    if err != nil {
        t.Fatal(err)
    }
    if h.ID != SCANUStartReq {
        t.Fatalf("id: 0x%04x", h.ID)
    }
    if h.ParamLen != uint16(len(payload)) {
        t.Fatalf("param_len drift")
    }
    if len(payload) < 64 {
        t.Fatalf("payload too short: %d", len(payload))
    }
    // Decode back and check round-trip.
    var got ScanStartReq
    if err := got.Decode(payload); err != nil {
        t.Fatal(err)
    }
    if got.Band != Band2G {
        t.Errorf("band drift: %d", got.Band)
    }
    if len(got.Channels) != 1 || got.Channels[0].Prim20Ch != 1 {
        t.Errorf("channels drift: %+v", got.Channels)
    }
    if len(got.SSIDs) != 2 || got.SSIDs[0] != "foo" {
        t.Errorf("ssids drift: %+v", got.SSIDs)
    }
}
```

- [ ] **Step 2: Run, expect FAIL "undefined: ScanStartReq"**

- [ ] **Step 3: Implement**

```go
// pkg/aic8800d80/lmac/scan.go
package lmac

import (
    "bytes"
    "encoding/binary"
    "fmt"
)

// Band values (lmac_msg.h PHY_BAND_*).
const (
    Band2G uint8 = 0
    Band5G uint8 = 1
)

// Channel width (CHNL_WIDTH_*).
const (
    ChanWidth20    uint8 = 0
    ChanWidth40    uint8 = 1
    ChanWidth80    uint8 = 2
    ChanWidth160   uint8 = 3
    ChanWidth80P80 uint8 = 4
)

// BroadcastBSSID = ff:ff:ff:ff:ff:ff (wildcard scan).
var BroadcastBSSID = [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}

const MaxChannelsInReq = 16  // SCANU_START_REQ channel array limit
const MaxSSIDsInReq   = 2   // SCANU_START_REQ SSID array limit

// ChannelInfo mirrors struct scan_chan_info.
type ChannelInfo struct {
    Prim20Ch uint8
    Center1  uint8
    Center2  uint8
    Width    uint8
}

// ScanStartReq mirrors struct scanu_start_req / scan_req / scan_start_req.
// Layout (lmac_msg.h, struct scanu_start_req):
//   u8 band; u8 padding[3]; struct mac_ssid ssid[2]; u8 ssid_len[2];
//   struct scan_chan_info chan[MAX_CHANNELS]; u8 n_channels; u8 n_ssids;
//   struct mac_addr bssid; u16 probe_delay; u32 flags;
// For simplicity in v1 we hard-code the layout. Extend if firmware disagrees.
type ScanStartReq struct {
    Band       uint8
    Channels   []ChannelInfo // up to MaxChannelsInReq
    SSIDs      []string      // up to MaxSSIDsInReq
    BSSID      [6]byte
    ProbeDelay uint16
    Flags      uint32
}

func (r *ScanStartReq) Encode() ([]byte, error) {
    if len(r.Channels) > MaxChannelsInReq {
        return nil, fmt.Errorf("scan: too many channels (%d > %d)", len(r.Channels), MaxChannelsInReq)
    }
    if len(r.SSIDs) > MaxSSIDsInReq {
        return nil, fmt.Errorf("scan: too many SSIDs (%d > %d)", len(r.SSIDs), MaxSSIDsInReq)
    }
    // Compute param length.
    payloadLen := 4 /*band+pad*/ +
        2*32 /*ssid slot*/ + 2 /*ssid_len slot*/ +
        MaxChannelsInReq*4 /*chan slot*/ +
        1 + 1 /*n_channels, n_ssids*/ +
        6 /*bssid*/ + 2 /*probe_delay*/ + 4 /*flags*/
    buf := make([]byte, HeaderSize+payloadLen)
    Header{ID: SCANUStartReq, DestID: uint16(TaskSCANU), SrcID: DRVTaskID, ParamLen: uint16(payloadLen)}.Encode(buf)
    p := buf[HeaderSize:]
    p[0] = r.Band
    p[1], p[2], p[3] = 0, 0, 0 // pad
    off := 4
    // SSIDs: fixed 2 slots, each 32 bytes (mac_ssid = u8 len + 31 bytes ssid).
    for i := 0; i < MaxSSIDsInReq; i++ {
        slot := p[off+i*32 : off+i*32+32]
        if i < len(r.SSIDs) {
            s := r.SSIDs[i]
            if len(s) > 32 {
                return nil, fmt.Errorf("scan: ssid %q too long (%d > 32)", s, len(s))
            }
            slot[0] = uint8(len(s))
            copy(slot[1:], s)
        }
    }
    off += MaxSSIDsInReq * 32
    off += 2 // ssid_len slot (unused in v1; we use slot[0] for lengths)
    // Channels: fixed MAX_CHANNELS slots.
    for i := 0; i < MaxChannelsInReq; i++ {
        slot := p[off+i*4 : off+i*4+4]
        if i < len(r.Channels) {
            ch := r.Channels[i]
            slot[0], slot[1], slot[2], slot[3] = ch.Prim20Ch, ch.Center1, ch.Center2, ch.Width
        }
    }
    off += MaxChannelsInReq * 4
    p[off] = uint8(len(r.Channels))
    off++
    p[off] = uint8(len(r.SSIDs))
    off++
    copy(p[off:off+6], r.BSSID[:])
    off += 6
    binary.LittleEndian.PutUint16(p[off:off+2], r.ProbeDelay)
    off += 2
    binary.LittleEndian.PutUint32(p[off:off+4], r.Flags)
    return buf, nil
}

// Decode parses a SCANU_START_REQ payload back into r (for round-trip tests).
// Field layout must match Encode.
func (r *ScanStartReq) Decode(payload []byte) error {
    if len(payload) < 4 {
        return fmt.Errorf("scan start: short payload")
    }
    r.Band = payload[0]
    off := 4
    r.SSIDs = r.SSIDs[:0]
    for i := 0; i < MaxSSIDsInReq; i++ {
        slot := payload[off+i*32 : off+i*32+32]
        if slot[0] > 0 {
            r.SSIDs = append(r.SSIDs, string(bytes.TrimRight(slot[1:1+slot[0]], "\x00")))
        }
    }
    off += MaxSSIDsInReq * 32 + 2
    nCh := int(payload[off+MaxChannelsInReq*4])
    nSS := int(payload[off+MaxChannelsInReq*4+1])
    off += MaxChannelsInReq*4 + 2
    r.Channels = make([]ChannelInfo, 0, nCh)
    for i := 0; i < nCh && i < MaxChannelsInReq; i++ {
        slot := payload[off+i*4 : off+i*4+4]
        r.Channels = append(r.Channels, ChannelInfo{Prim20Ch: slot[0], Center1: slot[1], Center2: slot[2], Width: slot[3]})
    }
    return nil
}
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add pkg/aic8800d80/lmac/scan.go pkg/aic8800d80/lmac/scan_test.go
git commit -m "feat(lmac): SCANU_START_REQ encode+decode"
```

### Task 2.3: SCANU_RESULT_IND decoder

**Files:**
- Modify: `pkg/aic8800d80/lmac/scan.go`
- Modify: `pkg/aic8800d80/lmac/scan_test.go`

- [ ] **Step 1: Write the failing test**

Append to scan_test.go:
```go
func TestScanResultIndDecode(t *testing.T) {
    // Hand-built SCANU_RESULT_IND payload:
    //   struct scan_result (lmac_msg.h):
    //     u32 channel; u8 band; u8 width; u16 padding;
    //     u8 rssi; u8 rssi_min; u8 rssi_max; u8 padding;
    //     u8 bssid[6]; u8 padding[2];
    //     u16 ie_len; u8 ie[ie_len];
    //     struct mac_ssid ssid (u8 len + 31 bytes);
    // ...simplified: build a 64-byte record + an SSID with len 5 "hello".
    payload := []byte{
        // channel (u32 LE)
        0x07, 0x00, 0x00, 0x00,
        // band (u8) + width (u8) + padding (u16)
        0x00, 0x00, 0x00, 0x00,
        // rssi, rssi_min, rssi_max, padding
        0xC4, 0xC0, 0xD0, 0x00,
        // bssid 6 bytes + 2 padding
        0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x00,
        // ie_len (u16 LE) = 4
        0x04, 0x00,
        // ie[4]
        0x01, 0x02, 0x03, 0x04,
        // ssid: len=5 + 31 bytes
        0x05, 'h', 'e', 'l', 'l', 'o', 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    }
    var res ScanResultInd
    if err := res.Decode(payload); err != nil {
        t.Fatal(err)
    }
    if res.Channel != 7 {
        t.Errorf("channel: %d", res.Channel)
    }
    if res.RSSI != -60 {
        t.Errorf("rssi: %d", res.RSSI)
    }
    if res.BSSID != ([6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}) {
        t.Errorf("bssid: %v", res.BSSID)
    }
    if res.SSID != "hello" {
        t.Errorf("ssid: %q", res.SSID)
    }
    if len(res.IE) != 4 {
        t.Errorf("ie: %d", len(res.IE))
    }
}
```

- [ ] **Step 2: Run, expect FAIL "undefined: ScanResultInd"**

- [ ] **Step 3: Implement**

Append to scan.go:
```go
// ScanResultInd mirrors struct scan_result (lmac_msg.h). The firmware sends one
// of these per BSS seen during a scan.
type ScanResultInd struct {
    Channel uint16    // channel number
    Band    uint8     // 0 = 2G, 1 = 5G
    Width   uint8     // channel width
    RSSI    int8      // signal strength (signed)
    RSSIMin int8
    RSSIMax int8
    BSSID   [6]byte
    IE      []byte    // information elements (raw 802.11 IE bytes)
    SSID    string
}

func (r *ScanResultInd) Decode(payload []byte) error {
    // Layout (struct scan_result in lmac_msg.h):
    //   u32 channel; u8 band; u8 width; u16 padding;
    //   u8 rssi; u8 rssi_min; u8 rssi_max; u8 padding;
    //   u8 bssid[6]; u8 padding[2];
    //   u16 ie_len; u8 ie[ie_len];
    //   struct mac_ssid ssid;  // u8 len + 31 bytes
    if len(payload) < 24 {
        return fmt.Errorf("scan result: short payload (%d)", len(payload))
    }
    r.Channel = uint16(binary.LittleEndian.Uint32(payload[0:4]))
    r.Band = payload[4]
    r.Width = payload[5]
    r.RSSI = int8(payload[8])
    r.RSSIMin = int8(payload[9])
    r.RSSIMax = int8(payload[10])
    copy(r.BSSID[:], payload[12:18])
    ieLen := int(binary.LittleEndian.Uint16(payload[20:22]))
    if 22+ieLen+32 > len(payload) {
        return fmt.Errorf("scan result: short IE/SSID tail (%d)", len(payload))
    }
    r.IE = append([]byte(nil), payload[22:22+ieLen]...)
    ssidSlot := payload[22+ieLen : 22+ieLen+32]
    if ssidSlot[0] > 0 && int(ssidSlot[0]) <= 31 {
        r.SSID = string(bytes.TrimRight(ssidSlot[1:1+ssidSlot[0]], "\x00"))
    }
    return nil
}
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add pkg/aic8800d80/lmac/scan.go pkg/aic8800d80/lmac/scan_test.go
git commit -m "feat(lmac): SCANU_RESULT_IND decode"
```

---

## Chunk 3: Submitter (outbound command channel)

### Task 3.1: Errors

**Files:**
- Create: `pkg/aic8800d80/lmac/errors.go`

- [ ] **Step 1: Write the failing test**

```go
package lmac

import (
    "errors"
    "testing"
)

func TestSubmitErrorIs(t *testing.T) {
    err := &SubmitError{Kind: ErrSeqMismatch, MsgID: 0x1000}
    if !errors.Is(err, ErrSeqMismatch) {
        t.Fatal("errors.Is failed")
    }
}
```

- [ ] **Step 2: Run, expect FAIL**

- [ ] **Step 3: Implement**

```go
// pkg/aic8800d80/lmac/errors.go
package lmac

import "fmt"

// SubmitErrorKind categorises submission failures.
type SubmitErrorKind uint8

const (
    ErrTimeout SubmitErrorKind = iota + 1
    ErrSeqMismatch
    ErrChannelClosed
    ErrShortFrame
)

// SubmitError is a structured submission failure.
type SubmitError struct {
    Kind  SubmitErrorKind
    MsgID uint16
    Cause error
}

func (e *SubmitError) Error() string {
    switch e.Kind {
    case ErrTimeout:
        return fmt.Sprintf("lmac submit: timeout waiting for ACK on msg 0x%04x", e.MsgID)
    case ErrSeqMismatch:
        return fmt.Sprintf("lmac submit: sequence mismatch on msg 0x%04x", e.MsgID)
    case ErrChannelClosed:
        return "lmac submit: channel closed"
    case ErrShortFrame:
        return fmt.Sprintf("lmac submit: short frame for msg 0x%04x", e.MsgID)
    default:
        return fmt.Sprintf("lmac submit: unknown error on msg 0x%04x", e.MsgID)
    }
}

func (e *SubmitError) Unwrap() error { return e.Cause }

// ErrUnknownTLV is returned when a payload contains a tag we don't know.
var ErrUnknownTLV = fmt.Errorf("lmac: unknown TLV tag")
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add pkg/aic8800d80/lmac/errors.go pkg/aic8800d80/lmac/errors_test.go
git commit -m "feat(lmac): SubmitError + ErrUnknownTLV"
```

### Task 3.2: Submitter

**Files:**
- Create: `pkg/aic8800d80/lmac/submitter.go`
- Test: `pkg/aic8800d80/lmac/submitter_test.go`

The Submitter writes host-target messages over the bulk OUT endpoint of the
operational device. It uses a sequence number on each header for matching
against firmware ACK events (delivered out-of-band by the event loop).

For simplicity in v1, `Submit` blocks until the corresponding firmware ACK
event arrives via a per-submit channel. The event loop fans ACKs into the
right channel.

- [ ] **Step 1: Write the failing test**

```go
package lmac

import (
    "context"
    "sync"
    "testing"
    "time"
)

type fakeSink struct {
    mu      sync.Mutex
    writes  [][]byte
    acks    chan uint16
}

func (f *fakeSink) BulkOut(_ context.Context, b []byte) error {
    f.mu.Lock()
    cp := append([]byte(nil), b...)
    f.writes = append(f.writes, cp)
    f.mu.Unlock()
    return nil
}

// Run the test: Submit a MM_VERSION_REQ, then deliver a fake ACK.
// Expected: Submit returns within timeout, writes count = 1.
func TestSubmitterRoundTrip(t *testing.T) {
    f := &fakeSink{acks: make(chan uint16, 1)}
    s := NewSubmitter(f, f.acks)
    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()
    go func() {
        time.Sleep(20 * time.Millisecond)
        f.acks <- 0x0005 // MM_VERSION_CFM
    }()
    if err := s.Submit(ctx, VersionReq{}); err != nil {
        t.Fatalf("submit: %v", err)
    }
    if len(f.writes) != 1 {
        t.Fatalf("writes: %d", len(f.writes))
    }
    // First 2 bytes of the write must be the MM_VERSION_REQ id.
    if got := f.writes[0][0] | f.writes[0][1]<<8; uint16(got) != MMVersionReq {
        t.Fatalf("write id: 0x%04x", got)
    }
}
```

- [ ] **Step 2: Run, expect FAIL "undefined: NewSubmitter"**

- [ ] **Step 3: Implement BulkOut dependency on protocol.USBDevice (or shim)**

For testability, define a tiny interface in submitter.go:
```go
// BulkWriter writes one frame to the device's bulk OUT endpoint.
type BulkWriter interface {
    BulkOut(ctx context.Context, frame []byte) error
}

// AckSource delivers ACK message IDs from the firmware (event loop feeds it).
type AckSource interface {
    NextACK(ctx context.Context) (uint16, error)
}
```

- [ ] **Step 4: Implement Submitter**

```go
// pkg/aic8800d80/lmac/submitter.go
package lmac

import (
    "context"
    "encoding/binary"
    "sync"
    "time"
)

// Builder is the interface implemented by every LMAC request that the Submitter
// can serialise. Implementations live next to the message structs.
type Builder interface {
    Encode() ([]byte, error)
}

// Submitter serialises host-target commands and waits for the matching
// firmware ACK. One Submitter per device.
type Submitter struct {
    writer BulkWriter
    acks   AckSource

    mu        sync.Mutex
    nextSeq   uint16
}

// NewSubmitter creates a Submitter bound to writer + acks.
func NewSubmitter(writer BulkWriter, acks AckSource) *Submitter {
    return &Submitter{writer: writer, acks: acks}
}

// Submit encodes msg, writes it to bulk OUT, and blocks until the firmware ACK
// arrives via acks or ctx is cancelled.
func (s *Submitter) Submit(ctx context.Context, msg Builder) error {
    frame, err := msg.Encode()
    if err != nil {
        return err
    }
    if len(frame) < HeaderSize {
        return &SubmitError{Kind: ErrShortFrame, MsgID: readID(frame)}
    }
    // Inject sequence number into header (overwrite src_id field — that is the
    // host's choice; firmware echoes it in the matching CFM).
    s.mu.Lock()
    s.nextSeq++
    seq := s.nextSeq
    s.mu.Unlock()
    binary.LittleEndian.PutUint16(frame[4:6], seq)
    if err := s.writer.BulkOut(ctx, frame); err != nil {
        return err
    }
    // Wait for ACK (a CFM is the "ack" in the firmware's terminology — REQ_CFM
    // handshake).
    for {
        ackID, err := s.acks.NextACK(ctx)
        if err != nil {
            if err == context.DeadlineExceeded || err == context.Canceled {
                return &SubmitError{Kind: ErrTimeout, MsgID: readID(frame), Cause: err}
            }
            return &SubmitError{Kind: ErrChannelClosed, MsgID: readID(frame), Cause: err}
        }
        // Filter: we accept any CFM whose base ID matches the REQ's base ID.
        // E.g. MM_VERSION_REQ (0x0004) -> MM_VERSION_CFM (0x0005). Both share
        // task = (id >> 10).
        if (ackID & 0xFC00) == (readID(frame) & 0xFC00) {
            return nil
        }
        // Otherwise keep waiting (rare: spurious ACK from a prior submission).
    }
}

func readID(frame []byte) uint16 {
    if len(frame) < 2 {
        return 0
    }
    return binary.LittleEndian.Uint16(frame[0:2])
}

// DefaultACKTimeout is the per-submit wait. Used by callers that wrap Submit
// with their own deadline.
const DefaultACKTimeout = 4 * time.Second
```

- [ ] **Step 5: Run, expect FAIL — fakeSink doesn't satisfy BulkWriter/AckSource**

We need fakeSink to implement both interfaces.

Update submitter_test.go:
```go
// (Replace the fakeSink definition above.)
type fakeSink struct {
    mu      sync.Mutex
    writes  [][]byte
    acks    chan uint16
}

func (f *fakeSink) BulkOut(ctx context.Context, b []byte) error {
    f.mu.Lock()
    defer f.mu.Unlock()
    cp := append([]byte(nil), b...)
    f.writes = append(f.writes, cp)
    return nil
}

func (f *fakeSink) NextACK(ctx context.Context) (uint16, error) {
    select {
    case <-ctx.Done():
        return 0, ctx.Err()
    case id := <-f.acks:
        return id, nil
    }
}
```

- [ ] **Step 6: Run, expect PASS**

- [ ] **Step 7: Add timeout test**

Append to submitter_test.go:
```go
func TestSubmitterTimeout(t *testing.T) {
    f := &fakeSink{acks: make(chan uint16)}
    s := NewSubmitter(f, f.acks)
    ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()
    err := s.Submit(ctx, VersionReq{})
    if err == nil {
        t.Fatal("expected timeout error, got nil")
    }
    se, ok := err.(*SubmitError)
    if !ok || se.Kind != ErrTimeout {
        t.Fatalf("expected *SubmitError{ErrTimeout}, got %T %v", err, err)
    }
}
```

- [ ] **Step 8: Run, expect PASS**

- [ ] **Step 9: Commit**

```bash
git add pkg/aic8800d80/lmac/submitter.go pkg/aic8800d80/lmac/submitter_test.go
git commit -m "feat(lmac): Submitter with sequence-id ACK handshake"
```

---

## Chunk 4: event Loop + Sink + dispatch

### Task 4.1: Sink interface + errors

**Files:**
- Create: `pkg/aic8800d80/event/sink.go`
- Create: `pkg/aic8800d80/event/errors.go`

- [ ] **Step 1: Write sink.go**

```go
// pkg/aic8800d80/event/sink.go
package event

import "context"

// Sink receives decoded LMAC event payloads from the Loop.
//
// Implementations must NOT block longer than necessary; the Loop is single-
// threaded. Return an error to log + continue; return a Fatal sentinel
// (loop.Fatal) to stop the loop.
type Sink interface {
    Handle(ctx context.Context, msgID uint16, payload []byte) error
}

// SinkFunc adapts a function to the Sink interface.
type SinkFunc func(ctx context.Context, msgID uint16, payload []byte) error

func (f SinkFunc) Handle(ctx context.Context, msgID uint16, payload []byte) error {
    return f(ctx, msgID, payload)
}
```

- [ ] **Step 2: Write errors.go**

```go
// pkg/aic8800d80/event/errors.go
package event

import "errors"

// Fatal wraps an error to request loop termination (Handle returns Fatal(err)).
type Fatal struct{ Err error }

func (f *Fatal) Error() string { return "event loop fatal: " + f.Err.Error() }
func (f *Fatal) Unwrap() error { return f.Err }

// ErrUnknownMsgID is returned by Sink.Handle when the dispatch table does not
// contain a handler for msgID. The loop logs and continues.
var ErrUnknownMsgID = errors.New("event: unknown message id")
```

- [ ] **Step 3: Commit**

```bash
git add pkg/aic8800d80/event/sink.go pkg/aic8800d80/event/errors.go
git commit -m "feat(event): Sink interface + Fatal sentinel"
```

### Task 4.2: Loop (RxStream pump + dispatch)

**Files:**
- Create: `pkg/aic8800d80/event/loop.go`
- Test: `pkg/aic8800d80/event/loop_test.go`

The Loop needs an `FrameSource` (pumps `protocol.RxFrame` values, blocking)
and a `Sink`. It runs until the source is exhausted or a handler returns
Fatal.

- [ ] **Step 1: Write the failing test**

```go
package event

import (
    "context"
    "encoding/binary"
    "sync"
    "testing"
    "time"
)

type fakeSource struct {
    frames chan protocol.RxFrame
    done   chan struct{}
}

func (s *fakeSource) Next(ctx context.Context) (protocol.RxFrame, error) {
    select {
    case <-ctx.Done():
        return protocol.RxFrame{}, ctx.Err()
    case f, ok := <-s.frames:
        if !ok {
            return protocol.RxFrame{}, io.EOF
        }
        return f, nil
    case <-s.done:
        return protocol.RxFrame{}, io.EOF
    }
}

type captureSink struct {
    mu     sync.Mutex
    calls  []uint16
    fatalOn uint16
}

func (c *captureSink) Handle(_ context.Context, id uint16, _ []byte) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.calls = append(c.calls, id)
    if c.fatalOn != 0 && id == c.fatalOn {
        return &Fatal{Err: errors.New("test fatal")}
    }
    return nil
}

func TestLoopDispatchesFrames(t *testing.T) {
    src := &fakeSource{frames: make(chan protocol.RxFrame, 4), done: make(chan struct{})}
    sink := &captureSink{}
    loop := NewLoop(src, sink)
    src.frames <- protocol.RxFrame{Type: protocol.USBTypeCfg, Payload: makeMsgIDPayload(0x1004)}
    src.frames <- protocol.RxFrame{Type: protocol.USBTypeCfg, Payload: makeMsgIDPayload(0x0005)}
    close(src.done)
    ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()
    if err := loop.Run(ctx); err != nil {
        t.Fatal(err)
    }
    sink.mu.Lock()
    defer sink.mu.Unlock()
    if len(sink.calls) != 2 || sink.calls[0] != 0x1004 || sink.calls[1] != 0x0005 {
        t.Fatalf("calls: %v", sink.calls)
    }
}

func makeMsgIDPayload(id uint16) []byte {
    b := make([]byte, 4)
    binary.LittleEndian.PutUint16(b[0:2], id)
    return b
}
```

- [ ] **Step 2: Run, expect FAIL — undefined NewLoop**

- [ ] **Step 3: Implement**

```go
// pkg/aic8800d80/event/loop.go
package event

import (
    "context"
    "fmt"
    "log"

    "github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// FrameSource pumps LMAC frames from the device's bulk IN endpoint. The Loop
// calls Next repeatedly until it returns a non-nil error.
type FrameSource interface {
    Next(ctx context.Context) (protocol.RxFrame, error)
}

// Loop reads frames from a FrameSource and dispatches to a Sink.
type Loop struct {
    src  FrameSource
    sink Sink
}

// NewLoop creates a Loop.
func NewLoop(src FrameSource, sink Sink) *Loop {
    return &Loop{src: src, sink: sink}
}

// Run drives the loop until ctx is cancelled, the source is exhausted, or a
// sink handler returns *Fatal.
func (l *Loop) Run(ctx context.Context) error {
    for {
        f, err := l.src.Next(ctx)
        if err != nil {
            if err == context.Canceled || err == context.DeadlineExceeded {
                return nil
            }
            // EOF / closed source = clean exit.
            return nil
        }
        if !f.IsConfig() {
            // v1: data frames are out of scope (sub-project C). Drop with debug log.
            continue
        }
        if len(f.Payload) < 2 {
            log.Printf("[event] short config frame (%d bytes), dropping", len(f.Payload))
            continue
        }
        msgID := f.MsgID()
        if err := l.sink.Handle(ctx, msgID, f.Payload); err != nil {
            if _, ok := err.(*Fatal); ok {
                return fmt.Errorf("event loop fatal: %w", err)
            }
            log.Printf("[event] handler for msg 0x%04x returned: %v", msgID, err)
        }
    }
}
```

- [ ] **Step 4: Run, expect PASS**

- [ ] **Step 5: Commit**

```bash
git add pkg/aic8800d80/event/loop.go pkg/aic8800d80/event/loop_test.go
git commit -m "feat(event): Loop dispatches config frames to Sink"
```

### Task 4.3: Default dispatch (SCANU_RESULT_IND, MM_VERSION_CFM)

**Files:**
- Create: `pkg/aic8800d80/event/dispatch.go`
- Test: `pkg/aic8800d80/event/dispatch_test.go`

- [ ] **Step 1: Write dispatch.go**

```go
// pkg/aic8800d80/event/dispatch.go
package event

import (
    "context"
    "log"

    "github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
)

// Dispatch is the default Sink: routes known msg ids to typed decoders.
// Unknown msg ids are logged and dropped (continue).
type Dispatch struct {
    OnScanResult func(lmac.ScanResultInd)
    OnVersion    func(lmac.VersionCfm)
    OnAnyUnknown func(msgID uint16, payload []byte)
}

func (d *Dispatch) Handle(_ context.Context, msgID uint16, payload []byte) error {
    switch msgID {
    case lmac.SCANUResultInd:
        if d.OnScanResult == nil {
            return nil
        }
        var r lmac.ScanResultInd
        if err := r.Decode(payload); err != nil {
            log.Printf("[dispatch] scan result decode: %v", err)
            return nil
        }
        d.OnScanResult(r)
        return nil
    case lmac.MMVersionCfm:
        if d.OnVersion == nil {
            return nil
        }
        var c lmac.VersionCfm
        if err := c.Decode(payload); err != nil {
            log.Printf("[dispatch] version cfm decode: %v", err)
            return nil
        }
        d.OnVersion(c)
        return nil
    default:
        if d.OnAnyUnknown != nil {
            d.OnAnyUnknown(msgID, payload)
        }
        return nil
    }
}
```

- [ ] **Step 2: Write dispatch_test.go**

```go
package event

import (
    "context"
    "encoding/binary"
    "sync"
    "testing"

    "github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
)

func TestDispatchRoutesScanResult(t *testing.T) {
    var got lmac.ScanResultInd
    var seen bool
    var mu sync.Mutex
    d := &Dispatch{OnScanResult: func(r lmac.ScanResultInd) {
        mu.Lock()
        defer mu.Unlock()
        got = r
        seen = true
    }}
    // Build a valid SCANU_RESULT_IND payload (32 bytes header + ssid slot).
    payload := make([]byte, 64)
    binary.LittleEndian.PutUint16(payload[0:2], lmac.SCANUResultInd) // not used by Dispatch
    payload = payload[2:]
    // Reuse ScanResultInd decoder (it reads from offset 0 of its payload).
    payload[0] = 0x07 // channel byte (u32)
    binary.LittleEndian.PutUint16(payload[20:22], 0) // ie_len=0
    payload[22+0] = 5 // ssid len
    copy(payload[22+1:22+6], []byte("hello"))
    if err := d.Handle(context.Background(), lmac.SCANUResultInd, payload); err != nil {
        t.Fatal(err)
    }
    mu.Lock()
    defer mu.Unlock()
    if !seen {
        t.Fatal("OnScanResult not called")
    }
    if got.SSID != "hello" {
        t.Errorf("ssid: %q", got.SSID)
    }
}

func TestDispatchUnknownMsgID(t *testing.T) {
    var called bool
    d := &Dispatch{OnAnyUnknown: func(_ uint16, _ []byte) { called = true }}
    if err := d.Handle(context.Background(), 0xFFFF, []byte{}); err != nil {
        t.Fatal(err)
    }
    if !called {
        t.Fatal("OnAnyUnknown not called")
    }
}
```

- [ ] **Step 3: Run, expect PASS**

- [ ] **Step 4: Commit**

```bash
git add pkg/aic8800d80/event/dispatch.go pkg/aic8800d80/event/dispatch_test.go
git commit -m "feat(event): Dispatch routes SCANU_RESULT_IND + MM_VERSION_CFM"
```

---

## Chunk 5: cmdctl CLI

### Task 5.1: cmdctl main

**Files:**
- Create: `cmd/usbwifi/cmdctl/main.go`
- Create: `cmd/usbwifi/cmdctl/dispatch.go`
- Modify: `cmd/usbwifi/main.go` (add `cmdctl` subcommand)

- [ ] **Step 1: Write cmdctl/main.go**

```go
// cmd/usbwifi/cmdctl/main.go
//
// Send a single LMAC command and/or listen for firmware events.
//
// Usage:
//   sudo ./bin/usbwifi cmdctl send <msg>
//   sudo ./bin/usbwifi cmdctl listen [--msg-id=0xNNNN] [--duration=5s]
//
// "send" re-stages the dongle via the existing loader (if needed), opens the
// host-target channel, submits <msg>, prints the matching CFM, exits.
// "listen" re-stages + opens the channel + drains events to stdout for the
// given duration (default 5s).
package main

import (
    "context"
    "flag"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/castlemilk/event-horizon/cmd/usbwifi/aicloader"
    "github.com/castlemilk/event-horizon/pkg/aic8800d80/event"
    "github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
    "github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

const usage = `cmdctl — host-target LMAC channel CLI

Subcommands:
  send <msg>      send one of: mm_version_req, scanu_start_req
  listen          drain events to stdout until --duration elapses

Examples:
  sudo ./bin/usbwifi cmdctl send mm_version_req
  sudo ./bin/usbwifi cmdctl listen --msg-id=0x1004 --duration=10s
`

func Run(args []string) int {
    if len(args) < 1 {
        fmt.Fprint(os.Stderr, usage)
        return 2
    }
    switch args[0] {
    case "send":
        return runSend(args[1:])
    case "listen":
        return runListen(args[1:])
    case "help", "-h", "--help":
        fmt.Print(usage)
        return 0
    default:
        fmt.Fprintf(os.Stderr, "unknown cmdctl subcommand: %s\n", args[0])
        fmt.Fprint(os.Stderr, usage)
        return 2
    }
}

func runSend(args []string) int {
    if len(args) < 1 {
        fmt.Fprintln(os.Stderr, "send: missing <msg>")
        return 2
    }
    msgName := args[0]
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()
    bundle, dev, cleanup, err := aicloader.StageAndOpen(ctx)
    if err != nil {
        log.Fatalf("stage: %v", err)
    }
    defer cleanup()
    _ = bundle // reserved for future use
    acks := make(chan uint16, 8)
    sub := lmac.NewSubmitter(dev, ackSource{acks})
    switch msgName {
    case "mm_version_req":
        if err := sub.Submit(ctx, lmac.VersionReq{}); err != nil {
            log.Fatalf("submit: %v", err)
        }
        fmt.Println("version: ack OK")
        return 0
    case "scanu_start_req":
        req := lmac.ScanStartReq{
            Band:       lmac.Band2G,
            Channels:   []lmac.ChannelInfo{{Prim20Ch: 1, Center1: 1, Width: lmac.ChanWidth20}, {Prim20Ch: 6, Center1: 6, Width: lmac.ChanWidth20}, {Prim20Ch: 11, Center1: 11, Width: lmac.ChanWidth20}},
            SSIDs:      nil, // wildcard
            BSSID:      lmac.BroadcastBSSID,
            ProbeDelay: 10,
        }
        if err := sub.Submit(ctx, &req); err != nil {
            log.Fatalf("submit: %v", err)
        }
        fmt.Println("scan: ack OK — events will arrive on bulk IN")
        return 0
    default:
        fmt.Fprintf(os.Stderr, "send: unknown msg %q\n", msgName)
        return 2
    }
}

func runListen(args []string) int {
    fs := flag.NewFlagSet("listen", flag.ContinueOnError)
    msgID := fs.Uint("msg-id", 0, "filter to a single msg id (hex); 0 = all")
    duration := fs.Duration("duration", 5*time.Second, "how long to listen")
    if err := fs.Parse(args); err != nil {
        return 2
    }
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer cancel()
    ctx, cancel2 := context.WithTimeout(ctx, *duration)
    defer cancel2()
    _, dev, cleanup, err := aicloader.StageAndOpen(ctx)
    if err != nil {
        log.Fatalf("stage: %v", err)
    }
    defer cleanup()
    src := &bulkFrameSource{dev: dev, msgIDFilter: uint16(*msgID)}
    var dispatched []lmac.ScanResultInd
    sink := &event.Dispatch{
        OnScanResult: func(r lmac.ScanResultInd) {
            dispatched = append(dispatched, r)
            fmt.Printf("[scan] ch=%d rssi=%d bssid=%02x:%02x:%02x:%02x:%02x:%02x ssid=%q\n",
                r.Channel, r.RSSI, r.BSSID[0], r.BSSID[1], r.BSSID[2], r.BSSID[3], r.BSSID[4], r.BSSID[5], r.SSID)
        },
    }
    loop := event.NewLoop(src, sink)
    if err := loop.Run(ctx); err != nil {
        log.Fatalf("loop: %v", err)
    }
    fmt.Printf("[done] %d scan results\n", len(dispatched))
    return 0
}

// ackSource adapts a channel to lmac.AckSource.
type ackSource struct{ ch chan uint16 }

func (a ackSource) NextACK(ctx context.Context) (uint16, error) {
    select {
    case <-ctx.Done():
        return 0, ctx.Err()
    case id := <-a.ch:
        return id, nil
    }
}

// bulkFrameSource pulls RxFrames from the bulk IN endpoint.
type bulkFrameSource struct {
    dev         *protocol.USBDevice
    msgIDFilter uint16
}

func (s *bulkFrameSource) Next(ctx context.Context) (protocol.RxFrame, error) {
    // Implementation detail: drain via libusb_bulk_transfer, feed into
    // protocol.RxStream, return successive frames.
    // For v1, we use a helper from aicloader (StageAndOpen returns a sink
    // hook). For now, leave as a stub returning io.EOF — the listen subcommand
    // becomes usable only after Task 5.2 lands.
    return protocol.RxFrame{}, fmt.Errorf("bulkFrameSource: not yet implemented (Task 5.2)")
}
```

- [ ] **Step 2: Add cmdctl subcommand in cmd/usbwifi/main.go**

Open `cmd/usbwifi/main.go`, find the subcommand switch (likely near the top),
add:
```go
case "cmdctl":
    return cmdctl.Run(args[1:])
```

Add the import:
```go
import "github.com/castlemilk/event-horizon/cmd/usbwifi/cmdctl"
```

- [ ] **Step 3: Build, expect FAIL because aicloader.StageAndOpen and bulkFrameSource don't exist**

Run: `go build ./...`
Expected: FAIL with "undefined: aicloader.StageAndOpen".

- [ ] **Step 4: Commit the partial cmdctl (it will not build yet)**

```bash
git add cmd/usbwifi/cmdctl/main.go cmd/usbwifi/main.go
git commit -m "feat(cmdctl): CLI skeleton for send + listen"
```

### Task 5.2: StageAndOpen helper in aicloader

**Files:**
- Modify: `cmd/usbwifi/aicloader/*.go` (create new helper module)

- [ ] **Step 1: Write the helper**

Create `cmd/usbwifi/aicloader/stage.go`:
```go
// cmd/usbwifi/aicloader/stage.go
//
// StageAndOpen re-stages the dongle (boot to operational) and returns an open
// *protocol.USBDevice ready for bulk IN/OUT. Used by cmdctl and any future
// user-space drivers.
package aicloader

import (
    "context"
    "fmt"

    "github.com/castlemilk/event-horizon/pkg/aic8800d80"
    "github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// StageAndOpen runs the loader against the operational VID/PID and returns
// the opened USBDevice.
func StageAndOpen(ctx context.Context) (*aic8800d80.Bundle, *protocol.USBDevice, func(), error) {
    dev, err := protocol.OpenOperational(ctx)
    if err != nil {
        return nil, nil, nil, fmt.Errorf("open operational: %w", err)
    }
    cleanup := func() { _ = dev.Close() }
    return nil, dev, cleanup, nil
}
```

If `protocol.OpenOperational` does not exist, add it (we'll add a stub that opens `a69c:8d81`):

Append to `pkg/aic8800d80/protocol/transport.go` (new section):
```go
// OpenOperational opens the device at the operational WiFi VID:PID.
func OpenOperational(ctx context.Context) (*USBDevice, error) {
    return OpenByVIDPID(ctx, 0xa69c, 0x8d81)
}
```

- [ ] **Step 2: Build, expect PARTIAL success (bulkFrameSource still missing)**

- [ ] **Step 3: Commit**

```bash
git add cmd/usbwifi/aicloader/stage.go pkg/aic8800d80/protocol/transport.go
git commit -m "feat(aicloader): StageAndOpen + protocol.OpenOperational"
```

### Task 5.3: bulkFrameSource (bulk IN pump)

**Files:**
- Modify: `cmd/usbwifi/cmdctl/main.go`

- [ ] **Step 1: Implement bulkFrameSource.Next**

Replace the stub with:
```go
func (s *bulkFrameSource) Next(ctx context.Context) (protocol.RxFrame, error) {
    // Drain bulk IN into a streaming RxStream. Each call returns one frame.
    // We hold the stream across calls so partial frames at chunk boundaries
    // are reassembled.
    for {
        // Pull a chunk if the stream is empty.
        if s.stream == nil {
            s.stream = &protocol.RxStream{}
        }
        f, ok, err := s.stream.Next()
        if err != nil {
            return protocol.RxFrame{}, err
        }
        if ok {
            if s.msgIDFilter != 0 && f.MsgID() != s.msgIDFilter {
                continue // skip non-matching (cheap path)
            }
            return f, nil
        }
        // Need more bytes — read from device.
        chunk := make([]byte, 4096)
        n, err := s.dev.BulkIn(ctx, 0x84, chunk, 500*time.Millisecond)
        if err != nil {
            // On timeout, return empty to keep the loop alive (the device may
            // be quiet mid-scan).
            if err == context.DeadlineExceeded {
                continue
            }
            return protocol.RxFrame{}, err
        }
        s.stream.Feed(chunk[:n])
    }
}
```

Add the missing fields to bulkFrameSource:
```go
type bulkFrameSource struct {
    dev         *protocol.USBDevice
    msgIDFilter uint16
    stream      *protocol.RxStream
}
```

- [ ] **Step 2: Build, expect FAIL — protocol.USBDevice.BulkIn does not exist**

Add to `pkg/aic8800d80/protocol/transport.go`:
```go
// BulkIn reads up to len(buf) bytes from the given bulk IN endpoint.
// Returns the number of bytes read and any error.
func (d *USBDevice) BulkIn(ctx context.Context, endpoint uint8, buf []byte, timeout time.Duration) (int, error) {
    // Use libusb_bulk_transfer with the device handle.
    // (Implementation lives in the cgo section — for now, leave a TODO.)
    return 0, fmt.Errorf("BulkIn: not yet implemented (needs cgo bulk_transfer binding)")
```

(Real implementation uses the existing `bulk_transfer` cgo helper.)

- [ ] **Step 3: Implement BulkIn properly**

Use the cgo `bulk_transfer` helper. Pattern (in transport.go):
```go
func (d *USBDevice) BulkIn(ctx context.Context, endpoint uint8, buf []byte, timeout time.Duration) (int, error) {
    var transferred C.int
    rc := C.bulk_transfer(d.handle, C.uchar(endpoint|0x80),
        (*C.uchar)(unsafe.Pointer(&buf[0])), C.int(len(buf)),
        &transferred, C.uint(timeout.Milliseconds()))
    if rc != 0 {
        return 0, fmt.Errorf("bulk IN rc=%d", rc)
    }
    return int(transferred), nil
}
```

(Add the corresponding cgo import + unsafe.)

- [ ] **Step 4: Build, expect PASS**

- [ ] **Step 5: Run unit tests, expect PASS**

Run: `go test ./pkg/aic8800d80/lmac/... ./pkg/aic8800d80/event/...`

- [ ] **Step 6: Commit**

```bash
git add cmd/usbwifi/cmdctl/main.go pkg/aic8800d80/protocol/transport.go
git commit -m "feat(cmdctl): bulk IN pump via RxStream"
```

---

## Chunk 6: integration test + manual run

### Task 6.1: Hardware integration test (skipped without device)

**Files:**
- Create: `pkg/aic8800d80/integration_test.go` (build-tagged `//go:build integration`)

- [ ] **Step 1: Write the test**

```go
//go:build integration

package aic8800d80_test

import (
    "context"
    "testing"
    "time"

    "github.com/castlemilk/event-horizon/pkg/aic8800d80/event"
    "github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
    "github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// TestIntegrationScanNeedsHardware runs SCANU_START_REQ against the real
// dongle and asserts >=1 SCANU_RESULT_IND within 10s. Skipped if no device.
func TestIntegrationScanNeedsHardware(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    dev, err := protocol.OpenOperational(ctx)
    if err != nil {
        t.Skipf("no operational device: %v", err)
    }
    defer dev.Close()
    acks := make(chan uint16, 8)
    sub := lmac.NewSubmitter(dev, eventLoopAcks{acks})
    req := &lmac.ScanStartReq{
        Band:     lmac.Band2G,
        Channels: []lmac.ChannelInfo{{Prim20Ch: 1, Center1: 1, Width: lmac.ChanWidth20}},
        BSSID:    lmac.BroadcastBSSID,
    }
    if err := sub.Submit(ctx, req); err != nil {
        t.Fatalf("submit: %v", err)
    }
    // Pump RxFrames for 5s, expect >=1 scan result.
    stream := &protocol.RxStream{}
    seen := 0
    deadline := time.Now().Add(5 * time.Second)
    for time.Now().Before(deadline) && seen == 0 {
        chunk := make([]byte, 4096)
        n, err := dev.BulkIn(ctx, 0x84, chunk, 500*time.Millisecond)
        if err != nil {
            continue
        }
        stream.Feed(chunk[:n])
        for {
            f, ok, err := stream.Next()
            if err != nil || !ok {
                break
            }
            if f.MsgID() == lmac.SCANUResultInd {
                seen++
                var r lmac.ScanResultInd
                if err := r.Decode(f.Payload); err == nil {
                    t.Logf("scan result: ch=%d rssi=%d ssid=%q", r.Channel, r.RSSI, r.SSID)
                }
            }
        }
    }
    if seen == 0 {
        t.Fatal("no SCANU_RESULT_IND events received")
    }
}

type eventLoopAcks struct{ ch chan uint16 }

func (e eventLoopAcks) NextACK(ctx context.Context) (uint16, error) {
    select {
    case <-ctx.Done():
        return 0, ctx.Err()
    case id := <-e.ch:
        return id, nil
    }
}
```

- [ ] **Step 2: Run with the device plugged in**

Run: `go test -tags=integration ./pkg/aic8800d80/ -run TestIntegrationScan -v`
Expected: PASS with ≥1 SCANU_RESULT_IND logged.

- [ ] **Step 3: Commit**

```bash
git add pkg/aic8800d80/integration_test.go
git commit -m "test(integration): SCANU_START_REQ against real dongle"
```

### Task 6.2: Manual run

**Files:** none (manual verification).

- [ ] **Step 1: Build the daemon**

Run: `go build -o bin/usbwifi ./cmd/usbwifi`
Expected: success.

- [ ] **Step 2: Plug in the dongle, kill usbwifi-mcp**

Run: `pkill -f usbwifi-mcp || true`

- [ ] **Step 3: Issue `send mm_version_req`**

Run: `sudo ./bin/usbwifi cmdctl send mm_version_req`
Expected: `version: ack OK` printed, exit 0.

- [ ] **Step 4: Issue `send scanu_start_req` + listen**

Run: `sudo ./bin/usbwifi cmdctl listen --msg-id=0x1004 --duration=10s`
Expected: at least one `[scan] ch=... rssi=... bssid=... ssid=...` line printed.

- [ ] **Step 5: Commit the final binary build reference**

(No commit — the binary is gitignored.)

---

## End state

The user can run:
```
sudo ./bin/usbwifi cmdctl send mm_version_req
sudo ./bin/usbwifi cmdctl listen --msg-id=0x1004 --duration=10s
```

and see real firmware responses on the operational AIC8800D80 — proving the
user-space host-target channel works. Sub-projects B (connection) and C
(data path) follow.
