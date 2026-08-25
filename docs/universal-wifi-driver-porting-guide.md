# Universal macOS Wi-Fi Driver Porting & Reverse Engineering Guide

This guide documents the engineering principles, reverse engineering methodology, and universal hardware coverage plan developed during the implementation of the UGREEN AIC8800D80 Wi-Fi 6 driver for macOS on Apple Silicon.

---

## 1. Executive Summary & Architecture

Modern macOS (macOS Sonoma, Sequoia, and beyond) imposes strict kernel boundaries:
1. Traditional KEXTs (Kernel Extensions) are deprecated.
2. DriverKit (`.dext`) runs in user-space with restricted micro-kernel message passing.
3. Third-party USB Wi-Fi dongles (UGREEN, Realtek, MediaTek) do not have native Apple Silicon drivers.

To deliver high-performance, rock-solid connectivity, Event Horizon implements a **Hybrid User-Space + DriverKit Architecture**:

```
+-------------------------------------------------------------------------------+
|                           Event Horizon macOS App                             |
|              (SwiftUI 6, Observation, Swift Concurrency, Metrics UI)           |
+-------------------------------------------------------------------------------+
        |                                                   |
        | HTTP/JSON API (:8990)                             | UNIX Domain Socket
        v                                                   v
+-------------------------------------------------------------------------------+
|                            usbwifi Daemon / Core                              |
|   - ZeroCD ModeSwitch (SCSI Eject)                                            |
|   - BootROM Firmware Upload & Memory Patching                                 |
|   - 802.11 Beacon Decoder & WPA2/WPA3 4-Way Handshake Engine                  |
|   - Virtual Network Tunnel (/dev/utun)                                        |
|   - Multi-Protocol Diagnostics (ICMP, HTTP/TLS, DNS, Jitter)                  |
|   - OpenTelemetry (OTel) Prometheus Exporter                                  |
+-------------------------------------------------------------------------------+
        |                                                   |
        | libusb-1.0 (User-Space)                           | DriverKit Dext (Kernel)
        v                                                   v
+------------------------------------+             +----------------------------+
| AIC8800D80 Wi-Fi 6 Hardware Dongle |             | BSD Network Interface (enX)|
| (Bulk OUT 0x02, Bulk IN 0x81)      |             | (IOUserNetworkEthernet)    |
+------------------------------------+             +----------------------------+
```

---

## 2. The 6-Stage End-to-End Checklist for Driver Porting

### Phase 1: USB Bus Identification & ZeroCD ModeSwitch
- [x] **Enumerate Bus Devices**: Scan for Target VID:PID (`0xa69c:0x5723` for ZeroCD, `0xa69c:0x8d81` for WLAN).
- [x] **Safe SCSI Eject**: Target exclusively USB CD-ROM / Flash Storage modes without affecting connected external hard drives.
- [x] **Bus Re-enumeration Polling**: Wait for USB disconnect and re-attachment in Stage 1/2 mode.

### Phase 2: BootROM & Firmware Upload Protocol
- [x] **Clock PLL & Bus Calibration**: Prepare device register zones for high-speed RAM transfers.
- [x] **Register Protection & Skip Zones**: Preserve BootROM jump vectors and avoid writing to protected addresses.
- [x] **Chunked Block Uploads**: Send 512-byte payload segments with status verification.
- [x] **Execution Vector Jump**: Issue entrypoint trigger to transition from Stage 1 (BootROM) to Stage 2 (Operational).

### Phase 3: Baseband & Radio Stream Initialization
- [x] **Bulk IN/OUT Endpoint Mapping**: Bind `0x02` for TX commands and `0x81` for continuous RX streams.
- [x] **Hardware Identity & MAC Readout**: Extract factory-calibrated MAC address.
- [x] **Continuous Frame Ingestion**: Stream raw 802.11 beacon, probe response, and data frames.

### Phase 4: User-Space 802.11 & Virtual Network Interface
- [x] **802.11 Management Frame Decoder**: Parse SSIDs, BSSIDs, Channels, Capabilities, and RSSI.
- [x] **WPA2/WPA3 Handshake State Machine**: PMK derivation via PBKDF2, PTK calculation via PRF-512, EAPOL-Key exchanges.
- [x] **Virtual utun Tunnel**: Create and configure macOS `/dev/utun` network interface with routing rules.

### Phase 5: Production DriverKit Dext Kernel Integration
- [x] **Subclass IOUserNetworkEthernet**: Implement native Ethernet frame translation.
- [x] **Apple Silicon Signing**: Apply DriverKit networking and USB transport entitlements.

### Phase 6: Multi-Protocol Diagnostics & Observability
- [x] **ICMP Reachability & Jitter Engine**: Native packet loss and RTT benchmarking.
- [x] **HTTP & TLS Layer Probes**: Trace DNS lookup, TCP handshake, TLS negotiation, and TTFB.
- [x] **DNS Benchmark**: Domain query latency and IP count validation.
- [x] **OTel Prometheus Exporter**: Real-time Prometheus metrics scraping at `/metrics`.

---

## 3. Retrospective Analysis: How to Accelerate Reverse Engineering

1. **Linux `usbmon` Differential Captures**:
   - Capture USB packets during boot and association using Wireshark on Linux.
   - Filter `usb.transfer_type == 0x02` (Bulk) and `usb.transfer_type == 0x00` (Control).
   - Diff the sequence of vendor control requests against the decompiled open-source driver.

2. **Ghidra Decompilation of Out-of-Tree GPL Drivers**:
   - Decompile Linux kernel modules (`aic8800_fdrv.ko`, `r8188eu.ko`, `mt7601u.ko`).
   - Extract raw register definitions and firmware loading structures.

3. **Symbol Extraction & String Tables**:
   - Analyze firmware blobs for ASCII error strings and memory map layouts.

---

## 4. Universal Coverage Roadmap for All Wi-Fi Dongles

| Chipset Family | Popular Dongles | Protocol Pattern | Porting Priority |
| :--- | :--- | :--- | :--- |
| **AicSemi AIC8800** | UGREEN AX900, UGREEN AX1800 | BootROM RAM upload (`0x00100000`) | **COMPLETE** |
| **Realtek RTL8811AU / RTL8812AU** | TP-Link Archer T2U/T4U, Netgear A6210 | 8051 MCU Firmware Download + Page Regs | High |
| **Realtek RTL8832AU / RTL8832BU** | TP-Link Archer TX20U, D-Link DWA-X1850 | Wi-Fi 6 Cortex-M4 Dual-Core Firmware | High |
| **MediaTek MT7612U / MT7921AU** | Alfa AWUS036ACM, Comfast CF-953AX | RISC-V `mt76` architecture | High |
| **Qualcomm Atheros AR9271** | Alfa AWUS036NHA | Open Source `ath9k_htc` firmware | Medium |

By plugging into the Event Horizon **Chipset Driver Interface (`WiFiChipsetDriver`)**, each new chipset family shares 100% of the WPA handshake engine, diagnostic test suite, Prometheus OTel exporter, and SwiftUI user interface.
