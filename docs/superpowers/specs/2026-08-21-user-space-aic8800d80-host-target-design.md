# User-space AIC8800D80 host-target channel — Sub-project A

Date: 2026-08-21
Author: brainstorming session
Status: approved (spec-review pass skipped at user direction)

## Goal

Get the operational AIC8800D80 firmware (Stage 2) responding to host-target
LMAC commands and emitting firmware events over its bulk IN/OUT endpoints,
entirely from user-space Go. End state: `cmd/usbwifi cmdctl` issues
`SCAN_START_REQ` and prints SSIDs from `SCAN_RESULT_EVT` events to stdout.

Out of scope: connection state machine, EAPOL, crypto, TX/RX data path,
utun integration. Those are sub-projects B and C.

## Context

- The existing `pkg/aic8800d80/loader.go` (with the V3 firmware path +
  MCU1 cache fix) successfully boots the dongle to Stage 2 (`0xa69c:0x8d81`).
- `pkg/aic8800d80/protocol/transport.go` provides bulk + control transfer
  via libusb cgo.
- `pkg/aic8800d80/protocol/rxstream.go` parses the length-prefixed
  USB frame stream into `RxFrame{Type, Payload}`, classifying config vs
  data frames via the `0x10` bit.
- User has rejected any SIP disable / DriverKit / IO80211 binding approach.
- Linux reference for the message structures:
  `~/projects/aic8800d80/refs/radxa-linux/src/USB/driver_fw/drivers/aic8800/aic8800_fdrv/`.

## Architecture

Five layers, top-down:

1. **CLI** (`cmd/usbwifi/cmdctl/`) — `send <msg>`, `listen [--msg-id=N]`.
2. **Event loop** (`pkg/aic8800d80/event/`) — single goroutine owns the
   `RxStream`, classifies config frames, dispatches by `MsgID`.
3. **LMAC** (`pkg/aic8800d80/lmac/`) — message structs (mirroring
   `rwnx_cmds.h`), `Encoder` builders, `Submitter` with sequence-id
   handshake against firmware ACK events.
4. **Protocol** (`pkg/aic8800d80/protocol/`, existing) — USB transport +
   RxStream.
5. **libusb** → AIC8800D80 firmware.

## Components

### `pkg/aic8800d80/lmac/`

| File | Purpose |
|------|---------|
| `msgids.go` | Constants for all LMAC message IDs (mirroring `rwnx_cmds.h`). |
| `header.go` | `Header{Type, Request, SequenceNumber, ...}` and encoding. |
| `tlv.go` | TLV (type-length-value) parameter encoding/decoding (the firmware's parameter model). |
| `scan.go` | `MM_SCAN_START_REQ`, `MM_SCAN_RESULT_EVT`, `MM_SCAN_COMPLETE_EVT` structs + encoders/decoders. |
| `version.go` | `MM_DBG_TLV_CMD` + `DBG_TLV_ID_GET_FW_VERSION` for firmware self-introspection. |
| `submitter.go` | `Submitter` — serializes submissions via a channel, handles the host-target sequence-id handshake against `MSG_IEEE80211_ACK` / `MM_SET_VENDOR_READY` style ACKs, returns typed errors. |
| `errors.go` | `SubmitError` (timeout, sequence mismatch, channel closed), `ErrUnknownTLV`. |

### `pkg/aic8800d80/event/`

| File | Purpose |
|------|---------|
| `sink.go` | `Sink` interface — handlers register typed callbacks per `MsgID`. |
| `loop.go` | `Loop` — owns the RxStream, runs the dispatch goroutine, classifies frames, fans out to the registered handler. |
| `dispatch.go` | Per-MsgID dispatch table (the v1 table covers: SCAN_RESULT_EVT, SCAN_COMPLETE_EVT, DBG_TLV_RESP, and any unknown id is logged as warning). |
| `errors.go` | `ErrFatalDesync`, `ErrHandlerError`. |

### `cmd/usbwifi/cmdctl/`

| File | Purpose |
|------|---------|
| `main.go` | CLI entry — `send <msg>` and `listen [--msg-id=N]`. |
| `dispatch.go` | Print SSIDs (sorted by RSSI) from SCAN_RESULT_EVT events. |

## Public API surface

```go
// lmac.Submitter
type Submitter struct { /* ... */ }
func NewSubmitter(dev *protocol.USBDevice) *Submitter
func (s *Submitter) Submit(ctx context.Context, msg Builder) (Ack, error)
func (s *Submitter) Close() error

// event.Loop
type Loop struct { /* ... */ }
func NewLoop(dev *protocol.USBDevice, sink Sink) *Loop
func (l *Loop) Run(ctx context.Context) error
func (l *Loop) Close() error

// event.Sink
type Sink interface {
    Handle(ctx context.Context, msgID uint16, payload []byte) error
}
```

## Concurrency model

- One event-dispatch goroutine, owns `RxStream`.
- `Submitter` is thread-safe; submissions queue through a buffered channel
  and serialize at the firmware. The sequence-id handshake ensures
  ordering against firmware ACKs.
- Handlers run on the event goroutine, NOT in spawned goroutines — the
  firmware expects in-order processing and we honor that.

## Error handling

Three tiers:

1. **Fatal** (loop stops, process exits): USB claim lost, firmware
   crash-back, corrupt frame header that desyncs the stream.
2. **Recoverable** (logged + retry, bounded): single command timeout,
   transient bulk IN stall. Bounded to 3 retries, then escalate to fatal.
3. **Handler error** (logged + loop continues): a registered callback
   returns an error — never blocks the loop.

## Testing

- **Unit**: every LMAC message builder has a round-trip test
  (encode → decode → structural equality). Coverage target: ≥90% of
  `pkg/aic8800d80/lmac/`.
- **Protocol simulator**: extend existing `simulator_test.go` to feed
  canned `RxStream` bytes and assert dispatch + handler invocation.
- **Integration** (skipped without device): real hardware, run
  `SCAN_START_REQ`, assert ≥1 `SCAN_RESULT_EVT` within 10s.
- **CLI smoke**: `cmd/usbwifi cmdctl send mm_scan_start_req` exits 0;
  `cmd/usbwifi cmdctl listen --msg-id=MM_SCAN_RESULT_EVT` stays running
  until killed.

## Risks

- **LMAC command structure drift** — the V3 firmware loaded on the
  dongle (chip_id=7, mcu=1) may use different `rwnx_cmds.h` than the
  radxa Linux driver. Mitigation: issue `MM_DBG_TLV_CMD` with
  `DBG_TLV_ID_GET_FW_VERSION` first (already in `applyPatchConfig`'s
  read path), and gate the v1 command set on the firmware's response.
- **Event ordering** — Linux assumes strict in-order; firmware honors
  this. We mirror the assumption explicitly and document it.
- **Bulk IN stalls** — the dongle's USB controller may stall under
  sustained event load. Mitigation: 5s read timeout with bounded
  retry; if it persists, fall back to firmware reset (out of scope for
  this spec).

## Spec-review pass

Skipped at user direction (2026-08-21, "proceed, implement this now
exhaustively"). User has read this document and approved it.
