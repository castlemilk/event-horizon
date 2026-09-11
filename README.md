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

## Quick start

Requirements: macOS 14+ on Apple Silicon, Go 1.22+, [Task](https://taskfile.dev),
libusb (`brew install libusb`), and a UGREEN AX900 / AIC8800D80 dongle. Swift 6
is only needed for the app bundle.

```bash
git clone https://github.com/castlemilk/event-horizon
cd event-horizon
go build -o bin/usbwifi ./cmd/usbwifi

# 1. Fetch the three public firmware blobs (SHA-256 verified)
./bin/usbwifi firmware fetch  --target=aic8800D80 --out=~/.event-horizon/firmware
./bin/usbwifi firmware verify --target=aic8800D80 --in=~/.event-horizon/firmware

# 2. Carve the fourth blob from the dongle's own ZeroCD volume (see below)

# 3. Unplug the dongle, wait ~10 s, plug it back in. Then ONE command does everything:
#    flash -> stack_start -> associate -> WPA2 4-way -> DHCP -> utun bridge
sudo bin/usbwifi cmdctl link \
  --ssid "<network>" --pass '<passphrase>' \
  --channel <n> --bssid <aa:bb:cc:dd:ee:ff> \
  --route 192.168.100.1
```

Verify the traffic really went over the dongle. If the host can already reach
the target on its own, a passing test proves nothing:

```bash
route -n get 192.168.100.1 | grep interface   # expect: utun<N>, not en0
ping -c 4 192.168.100.1
grpcurl -plaintext 192.168.100.1:9200 list    # expect: SpaceX.API.Device.Device
```

For the app and MCP server:

```bash
task build              # app bundle + DMG + MCP server
open "build/Event Horizon.app"

go build -o bin/usbwifi-mcp ./cmd/usbwifi-mcp
# .agents/mcp_config.json / claude_desktop_config.json:
# { "mcpServers": { "usbwifi": { "command": "bin/usbwifi-mcp",
#     "env": { "DAEMON_URL": "http://127.0.0.1:8990" } } } }
```

`task --list` shows every command; the `aic:*` tasks wrap the individual driver
stages for debugging.

## Firmware

The boot ROM wants four blobs and only one is chip-specific. Nothing
proprietary ships with this repo.

| Blob | Size | Source |
|---|---|---|
| `fmacfw_8800d80_u02_ipc.bin` | 324,848 B | carved from the vendor's Windows driver on the dongle's ZeroCD volume |
| `fw_adid_8800d80_u02.bin` | 1,708 B | [radxa-pkg/aic8800](https://github.com/radxa-pkg/aic8800) |
| `fw_patch_8800d80_u02.bin` | 32,700 B | radxa-pkg/aic8800 |
| `fw_patch_table_8800d80_u02.bin` | 1,384 B | radxa-pkg/aic8800 |

To carve the main image: capture the ZeroCD volume with
`scripts/aic-zerocd-capture.sh`, unpack `Setup.exe` with `innoextract`, and in
`win10_x64/aicloadfw.Sys` find the single occurrence of the two-word signature
`0x001A0000 0x001201A5` (initial SP, reset vector). The image is 324,848 bytes
from there. Pin the `win10_x64` variant: `win7_x64` is the same length and
differs in 11 bytes.

**Do not use radxa's `fmacfw_8800d80_u02.bin` (358,072 B).** It is for
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
