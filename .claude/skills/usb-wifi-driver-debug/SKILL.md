---
name: usb-wifi-driver-debug
description: Use when debugging, building, or extending the event-horizon USB Wi-Fi driver stack (AIC8800D80 / LMAC firmware over libusb+IOKit on macOS) — firmware bootstrap, LMAC message plumbing, scanning, association, or any "radio does not respond / receives nothing" symptom.
---

# Debugging & building event-horizon USB Wi-Fi drivers

Hard-won rules for the AIC8800D80 (UGREEN AX900) stack. Most of these were
learned by losing hours to a wrong assumption — trust them over intuition.

## 0. Orient before touching anything

Read the auto-memory `event-horizon-lmac-radio` first. It carries the current
verified state: message IDs, struct layouts, the working firmware set, and a
list of **negative results that must not be retried**.

## 1. The vendor driver is the source of truth — extract it

The dongle ships its own Windows driver on a ZeroCD volume. Do not guess at
protocol details when you can read them:

```bash
scripts/aic-zerocd-capture.sh                     # copies the 3.7MB UGREEN volume
brew install innoextract && innoextract -e Setup.exe
# aicloadfw.Sys = boot-ROM loader (firmware is embedded in .data)
# aicusbwifi.Sys = the Wi-Fi driver
llvm-objdump -d --no-show-raw-insn --x86-asm-syntax=intel aicusbwifi.Sys > /tmp/wifi.asm
```

Find a message builder by its id: `grep -nE "mov\s+dx, 0x<id>$" /tmp/wifi.asm`,
then read the surrounding function for the payload. The Linux reference
(`radxa-pkg/aic8800`) gives readable C for the same structs — prefer it for
layouts, use the Windows disassembly for **this** firmware's message IDs and
call order, which differ.

## 2. Message IDs are firmware-specific — verify, never assume

Ordinals shift between builds. On this firmware: `0x77` = txpwr **level**,
`0x79` = txpwr **offset**, `0x7b` = stack_start. Sending a level table under
`0x79` left the PHY uncalibrated and the receiver stone deaf — the single
costliest bug in the project, and it looked exactly like "broken RF hardware".

**If a whole subsystem seems dead, suspect a message ID before suspecting the
silicon.**

## 3. Framing differs by stage

- Boot ROM: raw LMAC message (`BuildLmacMessage`), no prefix.
- Operational firmware: **must** be wrapped (`lmac.WrapCommand`).

`protocol.MemWrite/MemRead` use boot-ROM framing and are silently ignored by
the running firmware. Use `lmac.DbgMemWriteReq` / `DbgMemReadReq` post-boot.

## 4. Session and reset discipline (device-destroying if ignored)

- Every command after `stack_start` **in the same session** gets no CFM (they
  are still delivered). `--stack` therefore exits immediately after it.
- A **second `MM_RESET`** on a running stack permanently kills RX until
  re-flash. One reset per firmware instance.
- Only the **first session after `stack_start`** has a reliable radio. Later
  sessions degrade; a run showing `stack_start: NO CFM` is already dead — its
  results are meaningless, so re-flash before trusting anything.
- The device can only be re-flashed from ZeroCD, and this firmware never
  crashes back there ⇒ **every clean test costs a physical replug**. Budget
  them; batch flash + stack + the real test into one command.

## 5. RX stream rules

- A record stride must be bounded (`maxRecord = 4096`). An unbounded stride
  from a mis-aligned header makes the parser wait forever for bytes that never
  come — the stream stalls permanently and *all* RX dies. This presented as
  "the radio is deaf".
- On a corrupt record, drop the whole buffer and resync on the next USB
  transfer. Byte-wise resync is measurably worse (it mis-frames good records).
- Keep a bulk-IN read **permanently outstanding** (dedicated goroutine).
  Reading only between dispatches drops frames arriving during processing and
  silently cuts scan yield to 1-3 BSSes.

## 6. Scanning

- Send `scan_start` **once** per session — re-issuing it restarts the scan so it
  never completes (0 results).
- `ssid_cnt > 0` acts as a **filter**, not a wildcard: a directed scan returns
  nothing and leaves the BSS list empty, which then fails association.
- Set a per-channel `Duration` (~120 TU); 0 means a minimal dwell.
- Results arrive as `SCANU_RESULT_IND (0x1004)` **config** frames carrying the
  full beacon; also decode every beacon candidate per frame, not just the first.

## 7. Association

- `SM_CONNECT_CFM/IND` are not reliably ACKed — send fire-and-forget and wait
  for the async `SM_CONNECT_IND (0x1802)`.
- `status_code=1` **with an all-zero BSSID** means the firmware never found the
  BSS — almost always the **wrong channel**, not bad credentials. Confirm the
  target's real channel and BSSID from a scan before blaming the passphrase.
- WPA2 is host-driven (`CONTROL_PORT_HOST`): association first, then the EAPOL
  4-way handshake and `MM_KEY_ADD`. This works end to end — association, the
  full 4-way, key install, DHCP, and IP traffic through a utun bridge to a
  real Starlink terminal. `SM_DISCONNECT_IND` (0x1805) carries the 802.11
  reason code and is the firmware telling you exactly why a link dropped;
  reason 15 is a 4-way timeout, i.e. the AP never accepted your msg2/msg4.

## 8. The data path (TX and RX)

- The USB TX record header is **4 bytes** — `[len_lo, len_hi&0x0f, 0x01, 0x00]`
  then a 28-byte hostdesc — from `aicwf_usb_bus_txdata()`. The **8**-byte
  aggregated header from `aicwf_usb_aggr()` is compiled only under
  `CONFIG_USB_TX_AGGR`, which the reference Makefile sets to `n`. Send the
  8-byte form and the firmware reads byte 2 as the record type, sees a length
  byte instead of `0x01`, and silently discards every data frame — while
  libusb reports success. This looked exactly like dead TX hardware.
- **Never alternate TX pipes.** Data frames go out the bulk data pipe. A
  leftover "which pipe works?" probe toggled per send, so msg4 went out the
  command pipe and vanished after msg2 had already succeeded.
- `need_cfm` is derived from the **ethertype**, not chosen by the caller: the
  reference sets it for EAPOL/WAPI and never for ordinary data. A field whose
  zero value means "request a confirm on slot 0" is a trap — DHCP inherited it
  by not setting it and wedged the bulk OUT endpoint.
- **RX delivers 802.11 MPDUs, not Ethernet.** A record is a 56-byte `hw_rxhdr`
  (whose first 4 bytes *are* the USB record header) + 4 pad, then the MPDU at
  offset 60. Convert: 24-byte header (+2 QoS, +4 HT Control, +6 4-addr), skip
  the cipher header the hardware leaves in place (8 for CCMP), skip LLC/SNAP,
  then for AP→STA **DA = addr1, SA = addr3** — addr2 is the AP, not the
  originator.
- `ME_TX_CREDITS_UPDATE_IND` (0x140b) is **advisory**. Every credit mutation in
  the reference is inside `#if 0`. Do not implement credit tracking.
- The EAPOL-Key descriptor is **95** fixed bytes: there is an 8-byte Reserved
  field between the RSC and the MIC. Omit it and every frame you send is 8
  bytes short and every MIC you check is wrong.

## 9. Never validate a wire format against your own encoder

Every wall in this project — the EAPOL descriptor, the TX record header, the
pipe alternation, the RX layout, and a client that spoke gRPC-Web to a mock —
had a **passing test** behind it. A self-consistent wrong layout round-trips
perfectly, and a checker written from the same wrong assumption agrees with it.
An "independent" verification in another language proves nothing if it reuses
your offsets.

Pin formats to something you did not write: a hand-built frame laid out from
the standard, a byte string from the reference driver, or real captured bytes.
When a whole subsystem looks dead, suspect that a passing test is asserting the
wrong thing before you suspect the silicon.

## 10. Method

- Instrument before theorising. `--dump` plus `protocol.SetRxDebug` (record
  boundaries) found in one run what days of reasoning missed. Note that `sudo`
  strips the environment, so wire debug switches to CLI flags, not env vars.
- Verify a struct layout against the reference header before "fixing" values;
  most layouts here were already correct.
- When a fix makes things worse, revert it and record it as a negative result
  in the code comment *and* in memory. Several dead ends were re-attempted
  because they were only remembered vaguely.
