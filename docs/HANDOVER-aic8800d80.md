# Handover — AIC8800D80 USB Wi-Fi on macOS (event-horizon)

Status as of 2026-09-08. Goal: drive a UGREEN AX900 (AICSEMI AIC8800D80) USB
Wi-Fi adapter entirely from **user space on macOS** — no kernel driver, SIP
enabled — far enough to associate to an AP and pass IP traffic.

---

## 1. Where things stand

**Working and verified on hardware:**

| Capability | State |
|---|---|
| Firmware bootstrap (ZeroCD → boot ROM → operational) | works, repeatable |
| LMAC control plane (all MM/ME config messages) | works |
| RF / PHY calibration | works |
| MAC bring-up (reset → me_config → chan_config → start → coex → add_if) | works, vif=0 |
| Scanning | works — real SSIDs, real RSSI, multiple BSSes |
| `SM_CONNECT_REQ` accepted (`SM_CONNECT_CFM status=0`) | works |
| Bidirectional RF link | **proven** — AP sends probe responses to our MAC |
| Association completes (`SM_CONNECT_IND status=0`) | works, every run |
| EAPOL msg1 received from the AP, with live ANonces | works |

**Where the frontier is now (2026-09-08).** Association is no longer the
blocker: the RX record-stride fix (commit `4c2ecf0`) took scanning from 0–1
garbage BSS to 9 real networks, and association has completed on every run
since. The handshake now runs to msg1.

The last observed wall was "the AP ignores our msg2 and retransmits msg1",
which was diagnosed as dead host-descriptor data TX. **That verdict was
wrong.** msg2 was *malformed*: the EAPOL-Key descriptor is 95 fixed bytes,
not 91 — IEEE 802.11-2016 §12.7.2 fig 12-33 has an 8-byte Reserved field
between the Key RSC and the Key MIC. Every field from the MIC onward sat 8
bytes early, so a standards-compliant AP read `KeyDataLength` = 44036 against
18 bytes available and dropped the frame with `key_data overflow` before ever
checking the MIC. Fixed in `4eba628`; msg2 is now 121 bytes and verifies
against an independent standards parser with an independently derived KCK.

**Untested on hardware:** whether msg3 now arrives. That is the one thing the
next physical run should establish (§8).

**Not started:** IP/ARP/DHCP/ICMP end to end, `enX`-style interface exposure.

---

## 2. Hardware & firmware facts

- Chip: `chip_id=0x07`, `chip_mcu_id=1` (legacy MCU).
- USB identities: `a69c:5723` ZeroCD (mass storage) → `a69c:8d80` boot ROM →
  **`368b:8d85` operational** (this is the real product identity — the Windows
  INF binds `USB\VID_368b&PID_8d85`).
- Endpoints when operational: bulk **OUT ep1**, bulk **IN ep1**, bulk **OUT ep2**.
  Commands go out on ep2; everything comes back on the single IN.

### The firmware set that works

`~/.event-horizon/firmware/aic8800D80-hybrid/` — a hybrid:

- `fmacfw_8800d80_u02_ipc.bin` — **324,848 B, carved from the vendor's own
  Windows driver** (`aicloadfw.Sys`, chip_id=7 branch, loads at `0x120000`,
  ends `0x16f4f0`).
- `fw_adid` / `fw_patch` / `fw_patch_table` — from the Amlogic set (the
  Windows BT patch overruns this chip's BT RAM at `0x210000`).

**Why this matters:** every other firmware we tried was wrong. The vendor
selects firmware *by chip id*, and for chip 7 the correct image lives entirely
below `0x170000` — which is why the long-running "0x170000 write wall" was a
red herring. It only ever appeared because we were flashing images never
intended for this silicon.

The Windows driver package is extracted at
`~/.event-horizon/zerocd-capture/extracted/app/win10_x64/` (`aicloadfw.Sys` =
boot-ROM loader with firmware embedded in `.data`; `aicusbwifi.Sys` = the Wi-Fi
driver). Disassemble with:

```bash
llvm-objdump -d --no-show-raw-insn --x86-asm-syntax=intel aicusbwifi.Sys > /tmp/wifi.asm
grep -nE "mov\s+dx, 0x<msgid>$" /tmp/wifi.asm   # find a message builder
```

---

## 3. Operating rules (violate these and results are meaningless)

1. **One `MM_RESET` per firmware instance.** A second reset on a running stack
   permanently kills RX until re-flash.
2. **`--stack` exits immediately after `stack_start`.** Every command sent
   after `stack_start` *in the same session* gets no CFM (they are still
   delivered) — so continuing would apply a reset the next session repeats.
3. **Only the first session after `stack_start` has a reliable radio.** A run
   showing `stack_start: NO CFM` is already dead — re-flash before trusting
   anything it prints.
4. **Re-flashing requires ZeroCD**, and this firmware never crashes back to it
   ⇒ **every clean test costs a physical unplug/replug.** Batch flash + stack +
   the real test into one command.
5. `sudo` strips the environment — wire debug switches to **CLI flags**, not
   env vars. (`NOPASSWD` is configured for `scripts/aic-zerocd-eject.sh` and
   `bin/usbwifi`.)

---

## 4. Reproduce the current state

```bash
cd ~/projects/event-horizon
go build -o bin/usbwifi ./cmd/usbwifi

# 1. replug the dongle, then flash
sudo -n scripts/aic-zerocd-eject.sh ~/.event-horizon/firmware/aic8800D80-hybrid

# 2. start the MAC stack (exits straight after stack_start)
sudo -n bin/usbwifi cmdctl bringup --stack

# 3a. scan
sudo -n bin/usbwifi cmdctl bringup --channels 1,6,11 --scan-duration 20s

# 3b. or associate
sudo -n bin/usbwifi cmdctl bringup \
  --connect "Uncle Rad-Guest" --connect-pass '<pw>' \
  --connect-channel 1 --connect-bssid d2:e8:f0:50:f8:32
```

Useful flags: `--dump` (hex-dump every frame **and** enable RX record-boundary
tracing), `--prescan`, `--connect-bssid`.

### The test network

`Uncle Rad-Guest` is on **channel 1**, BSSID **`d2:e8:f0:50:f8:32`** — on the
*same router as the Starlink terminal*:

| BSSID | SSID | Ch |
|---|---|---|
| `d2:e8:f0:20:f8:32` | `KIT407545886XSD-FieldOps` (Starlink terminal) | 1 |
| `d2:e8:f0:50:f8:32` | **`Uncle Rad-Guest`** | 1 |
| `d2:e8:f0:80:f8:32` | (hidden) | 1 |
| `d2:e8:f0:b0/e0:f8:32` | (hidden) | 11 |

Its beacon RSN IE: group **CCMP**, pairwise **CCMP**, AKM = **PSK *and* SAE**,
RSN caps `0x0080` = **MFPC** (PMF capable) — i.e. a WPA2/WPA3-transition BSS.
Passphrase was rotated several times during testing; get the current one from
the owner.

---

## 5. Root causes found and fixed

These were each worth days; the pattern is that *every* "the hardware is
broken" conclusion turned out to be a software bug.

1. **TX-power message ID** — on this firmware `0x77` = txpwr **level**,
   `0x79` = txpwr **offset**, `0x7b` = stack_start. We sent the 95-byte
   power-*level* table under `0x79`, so levels were never set and the offset
   table got garbage → PHY mis-calibrated → **receiver completely deaf**. This
   masqueraded as broken RF hardware for a long time.
   (`lmac/msgids.go`, `lmac/rf.go`)

2. **RX stream stall** — a mis-aligned record header produced a stride larger
   than the read buffer (`pktLen=55358 stride=55424`), so the parser waited
   forever for bytes that never arrive. One bad header killed **all** RX for
   the session. Fixed by bounding the stride (`maxRecord = 4096`) and dropping
   the buffer to resync. (`protocol/rxstream.go`)

3. **Reads only between dispatches** — frames arriving while a frame was being
   processed were dropped, cutting scan yield to 1–3 BSSes. Fixed with a
   dedicated reader goroutine keeping a bulk-IN read permanently outstanding.
   This is what finally revealed `Uncle Rad-Guest`. (`event/bulksource.go`)

4. **Wrong firmware** (see §2) — resolved by carving the chip-7 image out of
   the vendor's Windows driver.

5. **Sequence order** — RF config must precede `stack_start` (the vendor's
   order); we had it backwards.

6. **`MM_KEY_ADD` id** — request is `0x0024` (CFM `0x0025`). Sending `0x25` as
   the request is silently ignored. (`lmac/msgids.go`)

7. **RX record strides** — config records stride `4 + roundup(len,4)`, data
   records a flat `len + 60`. We had `4 + len` and `4 + roundup(len+60,4)`, so
   odd-length CFMs left stray bytes and every data record over-ate 4 bytes into
   the next header. Scanning went from 0–1 garbage BSS to 9 real networks.
   (`protocol/rxstream.go`, commit `4c2ecf0`)

8. **EAPOL-Key descriptor was 8 bytes short** — the 95-byte descriptor has an
   8-byte Reserved field between Key RSC and Key MIC (802.11-2016 §12.7.2 fig
   12-33; `u8 key_id[8]` in wpa_supplicant's `struct wpa_eapol_key`). We used
   91, so every field from the MIC on sat 8 bytes early and the AP read
   `KeyDataLength` = 44036 → `key_data overflow` → silent drop. This masqueraded
   as dead data TX for a full session of experiments. (`lmac/eapol.go`,
   commit `4eba628`)

Also fixed: scan per-channel `Duration` was 0 (minimal dwell) → now 120 TU;
the dispatcher decoded only the first beacon per frame → now decodes all;
msg3's Key Data is one AES-wrapped blob and must be unwrapped *before* its KDEs
are walked (`lmac.ParseKeyData`, commit `1450de5`).

**Method rule earned the hard way (see #8):** never validate a wire format
against your own encoder. A self-consistent wrong layout round-trips perfectly
— all five EAPOL tests passed, and an independent-looking Python MIC check
agreed, because both used our layout. Pin formats to a hand-built
standard-layout frame (`TestKeyFrameStandardLayout`).

---

## 6. Negative results — do **not** retry

- **Byte-wise resync** in `RxStream` on a corrupt record is *worse* than
  dropping the buffer (0 scan results vs 1) — it mis-frames the good records
  behind it.
- **Zero-length wildcard SSID** (`ssid_cnt=1`) to force an active scan returns
  **zero** results — this firmware treats the SSID array as a match **filter**.
  Keep `ssid_cnt=0`. A *directed* scan for a specific SSID likewise returns
  nothing and leaves the BSS list empty.
- **Re-issuing `scan_start`** during an in-flight scan restarts it, so it never
  completes → 0 results. Send it **once** per session.
- **DriverKit / SIP disable** — an entire branch (a signed `.dext`, host
  activator app, Apple provisioning) was built on the false premise that only a
  kernel driver could recover the pipes after `stack_start`. It is **not
  needed**; user space is sufficient. Kept in `DriverKit/` for reference only.
- `0x40100020` "MCU1 cache fix", the d80x2 perf-clock syscfg, and word-write
  window modes — none affect the `0x170000` behaviour.

---

## 7. Code map

| File | Change |
|---|---|
| `pkg/aic8800d80/lmac/msgids.go` | txpwr `0x77`/`0x79`, `MMKeyAdd 0x24/0x25`, operational IDs |
| `pkg/aic8800d80/lmac/rf.go` | `TxpwrLvlReq`→0x77, `RFConfigReq` (0x69), `DbgMemRead/WriteReq` (LMAC-framed) |
| `pkg/aic8800d80/protocol/rxstream.go` | stride bound + resync; `SetRxDebug` tracing |
| `pkg/aic8800d80/event/bulksource.go` | dedicated reader goroutine (continuous bulk-IN) |
| `pkg/aic8800d80/event/dispatch.go` | decode all beacons per frame; `OnDataFrame` hook (for EAPOL) |
| `pkg/aic8800d80/protocol/stages.go`, `session.go`, `loader.go` | `368b:8d85` as a 3rd operational identity |
| `pkg/aic8800d80/protocol/patchtable.go` | hardcoded U02 fallback when the table has no `PINF` |
| `cmd/usbwifi/bringup_scan.go` | vendor order, ACK-tolerant init, `--connect-bssid`, SM tracing, scan dwell |
| `cmd/usbwifi/aicloader.go` | `--write-mode vendor` (survives `sudo`) |
| `scripts/aic-zerocd-capture.sh` | captures the dongle's Windows driver volume |

**Framing rule:** boot ROM takes raw LMAC messages; the operational firmware
requires `lmac.WrapCommand`. `protocol.MemWrite/MemRead` use boot-ROM framing
and are **silently ignored** post-boot — use `lmac.DbgMemWrite/ReadReq`.

---

## 8. Next steps, in order

1. **One hardware run: does msg3 arrive?** This is the whole question, and it
   costs one replug:

   ```bash
   # replug the dongle, then:
   scripts/aic-zerocd-eject.sh ~/.event-horizon/firmware/aic8800D80-hybrid
   sudo -n bin/usbwifi cmdctl bringup --stack
   sudo -n bin/usbwifi cmdctl bringup --connect "Uncle Rad-Guest" \
     --connect-pass '<pw>' --connect-channel 1 \
     --connect-bssid d2:e8:f0:50:f8:32 --dump
   ```

   - **msg3 arrives** → data TX was never broken; the frame was garbage. Carry
     on to key install → control port → DHCP.
   - **AP still only retransmits msg1** → the dead-data-TX hypothesis is back,
     but note it has *never actually been tested*: until `4eba628` no
     well-formed msg2 had ever been transmitted, so all seven prior TX
     experiments were run against a frame the AP was always going to discard.

2. **Key install and control port.** `MM_KEY_ADD` (**0x0024**) for the PTK
   (`sta_idx` = `SM_CONNECT_IND.ap_idx`) and the GTK (`sta_idx` 0xFF), then
   `ME_SET_CONTROL_PORT` (0x1404). These now gate the success message on their
   CFMs, so a failure reports itself instead of printing "controlled port
   open".

3. **Validation / ping** — with no `enX`, do it manually over the data path:
   ARP for the gateway → static IP (simpler than DHCP for a first proof) →
   ICMP echo. Log at every layer (assoc → key install → TX accepted → RX seen →
   ARP reply → ICMP reply) so a failure is attributable.

4. **Still-open leads if msg3 does not come** (from the audit, not yet acted
   on): the 4s MM flush poke stops the moment the handshake starts, so the
   handshake waits ~50s sending nothing; `MM_SET_COEX` may be `0x0065` rather
   than our `0x0067` (both the Linux enum and the vendor driver say 0x65);
   and `ME_CONFIG` sets `ht_supp=1` with an all-zero `mac_htcapability`, i.e.
   "HT capable, supports no HT rate". Change one per run — each costs a replug.

---

## 9. References

- **Skill:** `.claude/skills/usb-wifi-driver-debug/SKILL.md` — the debugging
  rules distilled (read this first).
- **Auto-memory:** `event-horizon-lmac-radio` — the running lab notebook, with
  byte-level detail and every dead end.
- **Linux reference driver:** `radxa-pkg/aic8800`, USB fullmac at
  `src/USB/driver_fw/drivers/aic8800/aic8800_fdrv/` — authoritative C for
  struct layouts (`lmac_msg.h`, `rwnx_msg_tx.c`, `rwnx_rx.c`, `rwnx_tx.c`).
  Use it for *layouts*; use the Windows disassembly for *this* firmware's
  message IDs and call order, which differ.
- `docs/aic8800d80-macos-driver-plan.md` — the older DriverKit plan (superseded).

### Verified-correct — don't re-investigate

Task IDs (MM 0, DBG 1, SCAN 2, TDLS 3, SCANU 4, ME 5, SM 6, APM 7);
`scanu_start_req` (376 B: chan[42]×6, ssid[3]×33 @252, bssid @352, add_ies @360,
vif_idx @366, chan_cnt @367, ssid_cnt @368, no_cck @369, duration @372);
`sm_connect_req` (320 B); `me_chan_config_req` (254 B, counts at 252/253);
`me_config_req` (112 B, `ht_supp` @103); the RF/txpwr/calib sequence.
