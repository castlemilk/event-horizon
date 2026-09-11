# Event Horizon

**A USB Wi-Fi driver for macOS that never touches the kernel.**

Event Horizon drives a UGREEN AX900 (AICSEMI AIC8800D80) USB Wi-Fi adapter
entirely from user space: firmware bootstrap, the LMAC control plane, RF
calibration, scanning, association, the WPA2 4-way handshake, DHCP and an IP
bridge into a `utun`. System Integrity Protection stays on. There is no kernel
extension, no DriverKit extension and no third-party driver anywhere.

Around that driver sits a small macOS app, an HTTP daemon and an MCP server so
an AI agent can scan, connect and diagnose the link itself.

> The full story of how this was built, including the month of wrong turns,
> is written up at
> **[benebsworth.com/blog/usb-wifi-in-userspace](https://benebsworth.com/blog/usb-wifi-in-userspace/)**.
> Landing page: **[castlemilk.github.io/event-horizon](https://castlemilk.github.io/event-horizon/)** (GitHub Pages, deployed by `.github/workflows/pages.yml` on every push to `master` touching `web/`; mirrored at [event-horizon-amber.vercel.app](https://event-horizon-amber.vercel.app/)).

```
CONNECTED to "Uncle Rad-Guest": bssid=d2:e8:f0:50:f8:32 band=0 freq=2412
EAPOL: msg3 MIC ok / GTK unwrapped (16 bytes, idx 1)
mm_key_add PTK: CFM ok / mm_key_add GTK: CFM ok / control port open
NET: offer 192.168.2.243 from 192.168.2.1 (lease 3600s)
NET: gateway 192.168.2.1 at 74:24:9f:44:28:1f
NET: ping 192.168.2.1 seq=1 rtt=3ms ... 4/4 ping replies
```

## Why

I ordered a standard USB Wi-Fi dongle, the kind that works on any Windows
laptop the moment you plug it in. It does not work on Apple hardware: macOS
mounts a small disk image with a Windows installer on it and stops there. So
we made it work.

The original goal was reaching a Starlink terminal. The dish serves a gRPC API
on `192.168.100.1:9200`, and the host's `en0` sat on a different subnet with
100% packet loss to it. A second radio joined to the Starlink router's network,
bridged into a `utun` with one host route, turns the dish into an ordinary
socket. No client code changes.

The dongle is not the point. The point is that "there is no macOS driver for
this" turned out not to be a stopping condition.

## What it does

| Layer | Implemented in `pkg/` | Normally provided by |
|---|---|---|
| USB transport, record framing, resync | `aic8800d80/protocol` | kernel USB stack |
| Firmware bootstrap (ZeroCD → boot ROM → operational) | `aic8800d80/protocol`, `aic8800d80/firmware` | vendor kext |
| LMAC control plane (MM/ME/SM messages, TLV config, sequence-ID ACKs) | `aic8800d80/lmac` | vendor kext |
| RF / PHY calibration | `aic8800d80/lmac` | vendor kext |
| MAC bring-up, scan, association | `aic8800d80/lmac`, `aic8800d80/event` | 802.11 stack / CoreWLAN |
| WPA2 4-way handshake (EAPOL-Key, PTK/GTK, RFC 3394 unwrap) | `wifi`, `cmd/usbwifi` | `wpa_supplicant` |
| 802.11 MPDU → Ethernet, DHCP, ARP | `cmd/usbwifi`, `tun` | 802.11 stack, network stack |
| `utun` bridge and host route | `tun`, `routing` | the kernel |

The only privilege required is `root`, for two ordinary reasons: libusb must
claim the USB interface exclusively, and creating a `utun` requires it.

On top of the driver:

- **`usbwifi` daemon** (`cmd/usbwifi`): the driver, a link state machine, and
  an HTTP API on `127.0.0.1:8990` (scan, connect, telemetry, topology,
  diagnostics, speedtest, uptime).
- **`usbwifi-mcp`** (`cmd/usbwifi-mcp`): a Model Context Protocol server over
  stdio exposing the same capabilities as tools, so Claude, Codex or any
  MCP-capable agent can drive the dongle.
- **Event Horizon.app** (`Sources/`): a SwiftUI dashboard showing the USB →
  BSD interface → endpoint topology, live telemetry and the link state. It
  bundles and supervises the daemon.
- **Supporting packages**: RF spectrum / channel occupancy scoring (`wifi`),
  multi-WAN policy routing and a failover watchdog (`routing`), ping and
  multi-stream speedtest probes (`ping`), an OpenTelemetry exporter (`otel`),
  and mode-switch detection for other vendors' dongles (`usb`, `driver`).

## Install

**Requirements:** macOS 14 or newer on Apple Silicon, a UGREEN AX900 /
AIC8800D80 dongle, and `innoextract` (`brew install innoextract`) for the
one-time firmware step.

1. Download the DMG from the
   [latest release](https://github.com/castlemilk/event-horizon/releases/latest)
   and drag **Event Horizon.app** to Applications. It is signed with Developer
   ID and notarised by Apple, so it opens without any Gatekeeper workaround.

2. Produce the firmware. The app ships none: three of the four blobs are
   public and the fourth is on the dongle itself.

   ```bash
   EH="/Applications/Event Horizon.app/Contents/Resources"
   "$EH/usbwifi" firmware fetch     # three public blobs, SHA-256 verified
   # plug the dongle in fresh: macOS mounts it as a small disk named UGREEN
   "$EH/usbwifi" firmware carve     # carves the fourth from the dongle's own driver
   ```

   `carve` unpacks the Windows installer on the dongle's ZeroCD volume, finds
   the boot-ROM loader, locates the firmware image by its signature and
   writes the complete set to `~/.event-horizon/firmware/aic8800D80-hybrid/`.

3. Launch Event Horizon. It asks for administrator privileges once, to claim
   the USB device and create the `utun`, then flashes, associates and bridges
   from the dashboard. The same thing from the CLI:

   ```bash
   sudo "$EH/usbwifi" cmdctl link --ssid "<network>" --pass '<passphrase>' \
     --channel <n> --bssid <aa:bb:cc:dd:ee:ff> --route 192.168.100.1
   ```

Verify the traffic really went over the dongle. If the host can already reach
the target on its own, a passing test proves nothing:

```bash
route -n get 192.168.100.1 | grep interface   # expect: utun<N>, not en0
ping -c 4 192.168.100.1
grpcurl -plaintext 192.168.100.1:9200 list    # expect: SpaceX.API.Device.Device
```

### MCP server

`usbwifi-mcp` is in the same `Resources` directory. Point Claude Desktop,
Codex or any MCP-capable agent at it and it can scan, connect and diagnose the
link:

```json
{ "mcpServers": { "usbwifi": {
    "command": "/Applications/Event Horizon.app/Contents/Resources/usbwifi-mcp",
    "env": { "DAEMON_URL": "http://127.0.0.1:8990" } } } }
```

### Build from source

Go 1.22+, Swift 6, [Task](https://taskfile.dev) and libusb (`brew install libusb`).

```bash
git clone https://github.com/castlemilk/event-horizon
cd event-horizon
task build              # signed app bundle + DMG + MCP server into build/
task test               # Go + Swift unit tests (task test:hardware needs a dongle)
```

`task --list` shows everything; the `aic:*` tasks wrap the individual driver
stages for debugging, and `docs/RELEASING.md` covers signing, notarisation
and publishing.

## Firmware

The boot ROM wants four blobs and only one is chip-specific. Nothing
proprietary ships with this repo.

| Blob | Size | Source |
|---|---|---|
| `fmacfw_8800d80_u02_ipc.bin` | 324,848 B | carved from the vendor's Windows driver on the dongle's ZeroCD volume |
| `fw_adid_8800d80_u02.bin` | 1,708 B | [radxa-pkg/aic8800](https://github.com/radxa-pkg/aic8800) |
| `fw_patch_8800d80_u02.bin` | 32,700 B | radxa-pkg/aic8800 |
| `fw_patch_table_8800d80_u02.bin` | 1,384 B | radxa-pkg/aic8800 |

The carve: unpack the `Setup.exe` on the ZeroCD volume with `innoextract`,
and in `win10_x64/aicloadfw.Sys` find the single occurrence of the two-word
signature `0x001A0000 0x001201A5` (initial SP, reset vector). The u32 at
`+0x454` is the end address, so the image is `end - 0x120000` = 324,848 bytes
from there. Pin the `win10_x64` variant: `win7_x64` is the same length and
differs in 11 bytes.

`usbwifi firmware carve` does all of that; the recipe is here so it can be
checked. **Do not use radxa's `fmacfw_8800d80_u02.bin` (358,072 B).** It is for
different silicon. Flashing it is what produced the week-long "0x170000 write
wall" red herring described in the write-up.

## Hardware rules

These come from `docs/HANDOVER-aic8800d80.md` and will cost you a day each if
ignored.

1. **One `MM_RESET` per firmware instance.** A second reset on a running stack
   permanently kills RX until re-flash.
2. **Only the first session after `stack_start` has a reliable radio.** A run
   printing `stack_start: NO CFM` is already dead.
3. **Re-flashing requires ZeroCD, and this firmware never crashes back to it.**
   Every clean test costs a physical unplug and replug. Batch flash, stack and
   the real test into one command (`cmdctl link` does this and refuses to run
   on a spent instance).
4. **`sudo` strips the environment.** Debug switches are CLI flags
   (`--dump`, `--net-target`), never env vars.

## Repository layout

```
cmd/usbwifi/         driver CLI + daemon (bringup, link, aicloader, firmware, api)
cmd/usbwifi-mcp/     MCP server over stdio
pkg/aic8800d80/      the driver: protocol/ (USB, loader), lmac/ (messages), event/, firmware/
pkg/{tun,routing,wifi,ping,usb,driver,supervisor,otel,api}
Sources/             SwiftUI app (EventHorizonApp, EventHorizonCore)
DriverKit/           the abandoned kernel-side plan, kept for reference
docs/                HANDOVER-aic8800d80.md is the operational source of truth
web/                 landing page (Next.js static export → GitHub Pages via pages.yml)
.claude/skills/      usb-wifi-driver-debug: the distilled debugging rules
```

## Status

Verified on hardware (2026-09-10): firmware bootstrap, control plane, RF
calibration, scan, association, WPA2 handshake, DHCP, and routed IP traffic
through a `utun` to a Starlink terminal. Bring-up from a cold replug takes
about 32 s, dominated by the 26 s firmware upload.

Not done: exposing the link as a real `enX` interface (the `utun` bridge is
sufficient), and support for chips other than the AIC8800D80 (the `driver/`
package detects Realtek and MediaTek dongles but does not drive them).

## References

- `docs/HANDOVER-aic8800d80.md`: status, firmware facts, operating rules,
  reproduce steps and the failure table.
- `.claude/skills/usb-wifi-driver-debug/SKILL.md`: the debugging rules,
  especially §9, *never validate a wire format against your own encoder*.
- [radxa-pkg/aic8800](https://github.com/radxa-pkg/aic8800): the Linux USB
  fullmac reference driver. Use it for struct layouts; use the Windows
  disassembly for this firmware's message IDs and call order.
- [The write-up](https://benebsworth.com/blog/usb-wifi-in-userspace/).
