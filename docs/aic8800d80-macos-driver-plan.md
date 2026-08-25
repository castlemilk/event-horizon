# AIC8800D80 macOS Driver — Full Engineering Plan

Bring the UGREEN AX900 WiFi 6 (AIC8800D80) USB dongle to a fully functional
`enX` network interface on macOS, end-to-end on Intel Mac. The dongle currently
sits at boot-ROM stage (`0a69c:0d80`) and has no BSD interface because macOS
ships no driver for the AICSEMI chipset.

This plan spans three subsystems that must all work for the dongle to carry
traffic:

1. **Firmware loader** — user-space libusb tool that uploads the AIC8800D80
   firmware blobs so the device re-enumerates as `0a69c:0d81` (WiFi+BT) or
   `0a69c:0d83` (WiFi only).
2. **DriverKit IO80211 driver** — kernel-extension-resident driver that claims
   the operational device, presents it as a Wi-Fi interface, and handles
   802.11 management (scan/auth/assoc) using a DriverKit-compatible
   `IO80211Controller` subclass.
3. **Firmware sourcing** — fetch the proprietary firmware blobs from the
   Android kernel.org tree (`aic8800_fw`), verify SHA-256, and unpack them
   into a path the loader can read.

The full execution is 8–12 weeks of focused engineering. The plan is staged so
each phase is independently testable and the loader (no kernel driver
required) ships first.

---

## 0. Background — Why a Plan Is Required

### The chipset boot stages

The dongle enumerates in three stages on the USB bus:

| Stage | VID:PID    | Mode                                                        |
|-------|------------|-------------------------------------------------------------|
| 0     | `1111:1111` | ZeroCD — fake CD-ROM with Windows driver payload           |
| 1     | `a69c:8d80` | Boot ROM — accepts firmware upload over vendor SCSI cmd    |
| 2     | `a69c:8d81` | Operational WiFi+BT (combo)                                |
| 2'    | `a69c:8d83` | Operational WiFi only                                      |

The user is stuck at Stage 1 on macOS. Linux works because the DKMS driver
performs the Stage 1 → Stage 2 transition on attach.

### What macOS gives us

| Subsystem            | Status on macOS                                            |
|----------------------|------------------------------------------------------------|
| USB stack            | `IOUSBHost` accessible to DriverKit                        |
| 802.11 framework     | `IO80211Family` private framework (linked by Apple drivers) |
| Driver SDK           | DriverKit SDK ships with Xcode (no entitlements by default) |
| AIC8800D80 driver    | **None** — no Apple/3rd-party driver on the system         |
| Firmware blobs       | **None** — must be sourced from Android kernel.org         |

### What does *not* work

- App Store distribution: an `IO80211Controller` subclass uses the *private*
  `IO80211Family` framework. Third-party drivers built on it cannot be
  notarized for App Store distribution. They ship via Developer ID +
  `spctl`/`systemextensionsctl` (i.e. user must approve loading).
- DriverKit driver matching on USB devices with proprietary protocols: we
  write the match dictionary by hand against the operational VID/PID.
- Reverse-engineering Apple IO80211Family: the public SDK does not expose
  `IO80211Controller`. We use the public Apple open-source Darwin headers
  (`IO80211Controller.h`, `IO80211Interface.h`) from the opensource.apple.com
  tarballs. These are the same headers Apple ships to its own Wi-Fi driver
  vendors (Atheros/Broadcom).

The plan accepts all three constraints as the cost of doing this on macOS.

---

## 1. Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                         Event Horizon (Swift)                          │
└────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│           usbwifi daemon (Go, hypervisor / launchd)                   │
│   ┌──────────────────────┐    ┌──────────────────────┐                │
│   │  AIC Loader Daemon   │    │  USB Topology Probe  │  ← existing    │
│   │  (App, this plan)    │    │  (existing)          │                │
│   └──────────┬───────────┘    └──────────────────────┘                │
└──────────────┼─────────────────────────────────────────────────────────┘
               │ libusb (user-space)
               ▼
┌────────────────────────────────────────────────────────────────────────┐
│                  AIC8800D80 USB Dongle (Stage 1)                       │
│              VID 0a69c PID 8d80 — accepts fw upload                    │
└────────────────────────────────────────────────────────────────────────┘
               │ (after firmware upload)
               ▼
┌────────────────────────────────────────────────────────────────────────┐
│             Operating system — IO80211Family DriverKit                  │
│      ┌──────────────────────────────────────────────────┐           │
│      │  com.eventhorizon.driver.AIC8800D80.dext          │           │
│      │  (IO80211Controller subclass via IOUSBHost)       │           │
│      └──────────────────────────────────────────────────┘           │
└────────────────────────────────────────────────────────────────────────┘
               │
               ▼
         en0 | enX — BSD network interface
```

The loader is a separate work-stream from the DriverKit driver. The loader is
useful on its own (it gets the dongle to operational); the driver is required
for traffic.

---

## 2. Reference Material — Sources to Clone

Clone these on the Intel Mac build host. All are public, redistributable.

### 2.1 Linux driver (the protocol reference)

The Linux DKMS driver is the only working AIC8800D80 implementation. We read
it for the firmware upload protocol, the 802.11 state machine, and the
command catalogue.

```bash
mkdir -p ~/projects/aic8800d80/refs
git clone https://github.com/olamellberg/AIC8800D80.git   ~/projects/aic8800d80/refs/linux-dkms
```

Key files inside the clone:

| Path                                                                 | Purpose                          |
|----------------------------------------------------------------------|----------------------------------|
| `aic_load_fw/aic_bluetooth_main.c`                                   | Stage 0→1 firmware upload        |
| `aic_load_fw/aic_main.c`                                             | Loader entrypoint + mode switch  |
| `aic8800_fdrv/aic8800_drv.h`                                         | Opcode table, register maps      |
| `aic8800_fdrv/aic8800_usb.c`                                         | USB transport (bulk, control)    |
| `aic8800_fdrv/aic8800_fw.c`                                          | FW placement + boot sequence     |
| `aic8800_fdrv/aic8800_txrx.c`                                        | Tx/Rx ring management            |
| `wlan_src/aic8800_fmac_rx.c`                                         | Rx frame handling                |
| `wlan_src/aic8800_fmac_main.c`                                       | mac80211 ops (state machine)     |
| `wlan_src/aic8800_fmac_cmd.c`                                        | Host-target cmd ring             |
| `include/rwnx_cmds.h`                                                | Command catalogue (register map) |

### 2.2 Apple Darwin SDK (the IO80211 protocol reference)

`IO80211Family` is a private framework. The headers are available in the
Apple open-source Darwin tarballs. We use them to know which
`IO80211Controller` virtual methods to override.

```bash
mkdir -p ~/projects/aic8800d80/refs/darwin
curl -L https://opensource.apple.com/tarballs/IO80211Family/IO80211Family-1600.5.tar.gz \
  -o ~/projects/aic8800d80/refs/darwin/IO80211Family.tar.gz
tar -xzf ~/projects/aic8800d80/refs/darwin/IO80211Family.tar.gz \
        -C ~/projects/aic8800d80/refs/darwin/
```

Key headers we will need:

```text
IO80211Family/IO80211Family/IO80211Controller.h
IO80211Family/IO80211Family/IO80211Interface.h
IO80211Family/IO80211Family/IO80211WorkSource.h
IO80211Family/IO80211Family/IO80211PowerManager.h
```

### 2.3 DriverKit SDK + sample

DriverKit ships with Xcode. Sample that we extend:

```text
/Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk/System/Library/Frameworks/System.framework
/Applications/Xcode.app/Contents/Developer/Library/DriverKit/Samples/
```

In particular, `dext-to-user-app` and `IOUSBHost` samples show the boilerplate
we will need.

### 2.4 Firmware blobs (Android kernel.org)

The proprietary firmware lives in `aic8800_fw/` inside the Android common
kernel tree. We fetch and verify; we do **not** vendor the blobs in this
repo because they're GPL-tainted.

```bash
cd ~/projects/aic8800d80/refs
git clone --depth 1 https://android.googlesource.com/kernel/common.git aic8800-and-kernel
find aic8800-and-kernel -path '*aic8800_fw*' -type d
```

Expected paths under the clone:

```text
drivers/net/wireless/aic8800/aic8800_fw/
├── aic8800D80/
│   ├── fw_patch_table.bin
│   ├── fmacfw.bin
│   ├── lmacfw_r.bin
│   ├── lmacfw.bin
│   └── …
```

---

## 3. Phase 1 — Firmware Loader (libusb, user-space)

The loader is the smallest workstream and the highest-leverage. It does
not require a custom driver; it just gets the device to operational.

### 3.1 Layout under this repo

```text
pkg/aic8800d80/
├── loader.go                  # public entry: LoadFirmware(ctx, vid, pid)
├── loader_capi.go             # // #cgo bindings — libusb
├── protocol/
│   ├── opcodes.go             # vendor command catalogue (from aic8800_drv.h)
│   ├── stages.go              # Stage 0/1/2 transitions
│   └── firmware.go            # patch/fmac/lmac loader
├── firmware/
│   └── embed.go               # //go:embed for binaries (resolved at runtime)
└── loader_test.go
```

### 3.2 Boot ROM protocol — concrete sequence

Reference: `aic_load_fw/aic_main.c` `aic_load_normal_firmware_cb`.

```c
// 1. Open device by VID/PID, claim interface 0x00
rc = libusb_open(dev_with_pid, &handle);
rc = libusb_claim_interface(handle, 0);

// 2. Stage 0: ZeroCD mode-switch (only if at 1111:1111)
if (found_zerocd) {
    send_scsi_mode_switch(handle);   // proprietary 16-byte CDB FD..F2
    wait_for_replug(2000);
}

// 3. Stage 1: Boot ROM firmware upload
for (bin in {fw_patch_table, lmacfw, lmacfw_r, fmacfw}) {
    send_firmware_block(handle, bin, offset);
}
send_boot_complete(handle);   // forces re-enumeration

// 4. Device re-enumerates as a69c:8d81 (or 8d83)
```

The Go-side equivalent (`pkg/aic8800d80/protocol/firmware.go`):

```go
// pseudo-code (full implementation in plan)
func (l *loader) loadFirmwareBlocks(handle *libusb, blob []byte) error {
    // 1. Vendor control transfer: write address
    // 2. Bulk OUT: write chunk (4 KiB aligned)
    // 3. Vendor control transfer: checksum verify
    // 4. Repeat until end-of-blob
}
```

### 3.3 CLI entrypoint

Add a subcommand to `cmd/usbwifi`:

```bash
cmd/usbwifi/aicloader/
├── main.go        # cobra-style flag: --vid 0a69c --pid 8d80 --firmware-dir ./firmware
└── README.md
```

Usage:

```bash
sudo ./bin/usbwifi aicloader --stage 1 --firmware-dir ~/.event-horizon/firmware/aic8800D80
sudo ./bin/usbwifi aicloader --stage 0    # mode-switch from ZeroCD
```

The loader requires `sudo` because libusb open() needs root on macOS for
non-Apple drivers.

### 3.4 Tests

`pkg/aic8800d80/loader_test.go` mocks the libusb handle and exercises:

- Block upload chunking at 4 KiB boundary
- Checksum verification
- Re-enumeration polling (mocked `IOKit` iterator)
- ZeroCD → Boot ROM mode-switch

Reference the Linux driver tests for fixtures:

```bash
~/projects/aic8800d80/refs/linux-dkms/aic_load_fw/aic_main_test.c
```

---

## 4. Phase 2 — DriverKit IO80211 Driver

This is the largest workstream. DriverKit for an `IO80211Controller` subclass
is the only sanctioned way to expose a Wi-Fi interface on modern macOS.

### 4.1 Project layout

```text
DriverKit/AIC8800D80/
├── AIC8800D80Driver.iig                 # DriverKit C++ interface definitions
├── AIC8800D80Controller.iig             # IO80211Controller subclass decl
├── AIC8800D80Controller.cpp             # implementations
├── AIC8800D80Channel.h                  # local constants
├── AIC8800D80UsbTransport.cpp           # IOUSBHost bulk + control transfers
├── AIC8800D80Firmware.cpp               # flash-conf / fw upload (operational stage)
├── AIC8800D80Scan.cpp                   # scan state machine
├── AIC8800D80Assoc.cpp                  # auth/assoc state machine
├── AIC8800D80Tx.cpp                     # Tx path (host → target ring)
├── AIC8800D80Rx.cpp                     # Rx path (target → host ring)
├── Info.plist                           # IOProviderClass, match dictionary
└── Makefile                             # dext build
```

### 4.2 Match dictionary (`Info.plist`)

```xml
<key>IOKitPersonalities</key>
<dict>
  <key>AIC8800D80</key>
  <dict>
    <key>CFBundleIdentifier</key>
    <string>com.eventhorizon.driver.AIC8800D80</string>
    <key>IOClass</key>
    <string>com_eventhorizon_driver_AIC8800D80</string>
    <key>IOProviderClass</key>
    <string>IOUSBHostDevice</string>
    <key>idVendor</key><integer>42652</integer>  <!-- 0xa69c -->
    <key>idProduct</key><integer>36225</integer> <!-- 0x8d81 -->
    <key>bConfigurationValue</key><integer>1</integer>
    <key>bInterfaceNumber</key><integer>0</integer>
    <key>IOUserClientClass</key>
    <string>IOUserUSBHost</string>
  </dict>
</dict>
```

`idVendor` / `idProduct` decimal: `0xa69c = 42652`, `0x8d81 = 36225`. Match
both `0x8d81` (WiFi+BT) and `0x8d83` (WiFi only) with two personalities.

### 4.3 Entitlements

```text
com.apple.developer.driverkit               = true
com.apple.developer.driverkit.transport.usb = true
com.apple.developer.driverkit.family.networking = true
com.apple.developer.driverkit.allow-any-userclient = false
```

The driver's code signature must include the DriverKit signing identity
(developer ID team). No Apple notarization is required for a system
extension, but the developer must be added to the user's `systemextensionsctl`
trust list.

### 4.4 IO80211Controller subclass

```cpp
// AIC8800D80Controller.iig
#include <IOUSBHost/IOUSBHostDevice.h>
#include <DriverKit/IOService.h>
#include <IO80211/IO80211Controller.h>   // private framework

class AIC8800D80Controller : public IO80211Controller {
public:
    virtual bool init() override;
    virtual kern_return_t Start(IOService* provider) override;
    virtual kern_return_t Stop(IOService* provider) override;

    // IO80211Controller overrides
    virtual kern_return_t enable(IO80211Interface* interface) override;
    virtual kern_return_t disable(IO80211Interface* interface) override;
    virtual kern_return_t requestScan(...) override;
    virtual kern_return_t associate(...) override;
    virtual kern_return_t disassociate() override;
    virtual kern_return_t setPowerState(unsigned long powerState) override;

protected:
    IOUSBHostDevice*      _device;
    IOUSBHostPipe*        _pipeBulkIn;
    IOUSBHostPipe*        _pipeBulkOut;
    Firmware              _firmware;
    ScanState             _scanState;
};
```

### 4.5 USB transport

DriverKit uses `IOUSBHost` for USB. The driver allocates two bulk pipes
(in/out) on the default interface and exchanges frames via bulk transfer
queues. The endpoint addresses are from the device descriptor:

| Pipe | Direction | Endpoint | Max packet |
|------|-----------|----------|------------|
| Bulk IN  | IN  | 0x84 | 512 |
| Bulk OUT | OUT | 0x04 | 512 |

### 4.6 802.11 scan / auth / assoc

The host-target cmd ring is documented in the Linux driver. We replicate
the relevant commands:

```cpp
// From aic8800_drv.h (vendor opcodes)
constexpr uint16_t SCAN_START_REQ     = 0x0001;
constexpr uint16_t SCAN_RESULT_EVT    = 0x8001;
constexpr uint16_t CONNECT_REQ        = 0x0010;
constexpr uint16_t CONNECT_RESULT_EVT = 0x8010;
constexpr uint16_t DISCONNECT_REQ     = 0x0011;
constexpr uint16_t TX_REQ             = 0x0020;
constexpr uint16_t RX_EVT             = 0x8020;
```

Each command is a fixed-size struct written to the bulk OUT endpoint. The
target replies asynchronously via bulk IN.

### 4.7 Tests

```text
DriverKit/AIC8800D80/AIC8800D80Tests/
├── transport_test.cpp        # mock IOUSBHostPipe, exercise request/response
├── scan_test.cpp             # feed canned SCAN_RESULT_EVT frames
├── assoc_test.cpp            # drive CONNECT_REQ → CONNECT_RESULT_EVT
└── firmware_test.cpp         # verify flash-conf upload against fixture
```

Tests are unit tests on the macOS host using DriverKit's `IOUserClientTest`
machinery. No real USB required.

---

## 5. Phase 3 — Firmware Fetcher

The firmware blobs are GPL-tainted and not vendored in this repo. The
fetcher pulls them on demand from Android kernel.org.

### 5.1 CLI entrypoint

```bash
cmd/usbwifi/firmware/
├── main.go
└── firmware.go
```

Usage:

```bash
./bin/usbwifi firmware fetch --target aic8800D80 --out ~/.event-horizon/firmware
./bin/usbwifi firmware verify --target aic8800D80 --in ~/.event-horizon/firmware
./bin/usbwifi firmware list         # available targets
```

### 5.2 Manifest of expected blobs

```go
// pkg/aic8800d80/firmware/manifest.go
var AIC8800D80Blobs = []Blob{
    {Name: "fw_patch_table.bin", Size: 4096,   SHA256: "<from kernel.org tree>"},
    {Name: "fmacfw.bin",         Size: 198000, SHA256: "<from kernel.org tree>"},
    {Name: "lmacfw.bin",         Size: 220000, SHA256: "<from kernel.org tree>"},
    {Name: "lmacfw_r.bin",       Size: 64000,  SHA256: "<from kernel.org tree>"},
}
```

SHA-256 hashes are populated once at first fetch from a checked-in
`manifest.lock.json` that records the upstream commit hash.

### 5.3 Fetch flow

```bash
# 1. Clone fixed commit of Android common kernel
git clone --depth 1 <commit> refs/aic8800-and-kernel

# 2. Locate aic8800_fw blobs
find refs/aic8800-and-kernel -path '*aic8800_fw/aic8800D80/*'

# 3. Copy to output dir
cp blobs/* ~/.event-horizon/firmware/aic8800D80/

# 4. Verify SHA-256 against manifest
sha256sum -c manifest.lock.json
```

### 5.4 Licensing

The blobs are under GPL with redistribution rights. The fetcher emits a
license notice on each fetch:

```
This firmware is redistributed under GPLv2 from the Android common kernel.
Licensed recipients only. See ~/projects/aic8800d80/refs/linux-dkms/COPYING.
```

---

## 6. Build System

### 6.1 Makefile integration

Add to existing `Makefile`:

```makefile
AIC_DRIVER_DIR := DriverKit/AIC8800D80
AIC_DRIVER_APP := $(AIC_DRIVER_DIR)/build/AIC8800D80Driver.dext

.PHONY: aic-driver build-aic-loader build-aic-firmware

aic-driver: $(AIC_DRIVER_APP)
$(AIC_DRIVER_APP): $(shell find $(AIC_DRIVER_DIR) -name '*.iig' -o -name '*.cpp' -o -name '*.h')
    cd $(AIC_DRIVER_DIR) && make

build-aic-loader:
    go build -o bin/aicloader ./cmd/usbwifi/aicloader

build-aic-firmware:
    go build -o bin/usbwifi ./cmd/usbwifi
```

### 6.2 DriverKit build

```bash
cd DriverKit/AIC8800D80
export DRIVERKIT_FRAMEWORK_PATH=/Applications/Xcode.app/Contents/Developer/Library/Frameworks/DriverKit.framework
make    # produces AIC8800D80Driver.dext
```

### 6.3 Deployment

```bash
# 1. Copy dext to /Library/SystemExtensions
sudo cp -R AIC8800D80Driver.dext /Library/SystemExtensions/

# 2. Activate via systemextensionsctl
sudo systemextensionsctl developer on
sudo systemextensionsctl load com.eventhorizon.driver.AIC8800D80

# 3. Verify
systemextensionsctl list | grep AIC8800D80
```

### 6.4 Signing identity

DriverKit drivers require a Developer ID Application certificate. The build
uses `codesign --sign "Developer ID Application: <YOUR TEAM>"`.

In CI: developer ID stored in `~/.event-horizon/secrets/developer-id.p12`,
keychain reference `event-horizon-build`.

---

## 7. Testing Strategy

### 7.1 Unit tests (no hardware)

| Subsystem       | Test file                                      | Coverage                          |
|-----------------|------------------------------------------------|-----------------------------------|
| Loader          | `pkg/aic8800d80/loader_test.go`                | Chunking, checksum, re-enum       |
| DriverKit       | `DriverKit/AIC8800D80/AIC8800D80Tests/`        | Transport, scan, assoc            |
| Firmware        | `cmd/usbwifi/firmware/firmware_test.go`        | Hash verification                 |

### 7.2 Integration on Intel Mac

```bash
# Preconditions
- AIC8800D80 dongle plugged in (Stage 1, a69c:8d80)
- Event Horizon daemon running

# Step 1: Fetch firmware
./bin/usbwifi firmware fetch --target aic8800D80 --out ~/.event-horizon/firmware

# Step 2: Upload firmware (Stage 1 → Stage 2)
sudo ./bin/usbwifi aicloader --stage 1 --firmware-dir ~/.event-horizon/firmware/aic8800D80

# Step 3: Verify re-enumeration
ioreg -p IOUSB | grep -i aic

# Step 4: Install DriverKit driver
sudo cp -R DriverKit/AIC8800D80/build/AIC8800D80Driver.dext /Library/SystemExtensions/
sudo systemextensionsctl load com.eventhorizon.driver.AIC8800D80

# Step 5: Verify network interface
ifconfig | grep -i 'aic\|en[0-9]'

# Step 6: Test connectivity
networksetup -setairportnetwork enX "Starlink" "<passphrase>"
ping -c 4 1.1.1.1
```

### 7.3 Failure modes to instrument

| Failure                       | Expected log line                          |
|-------------------------------|--------------------------------------------|
| Firmware CRC mismatch         | `[AIC] checksum failed at offset 0xXXXX`   |
| DriverKit signature invalid   | `systemextensionsctl: blocked`             |
| IO80211 framework not linked  | dyld: Symbol not found: _IO80211Controller |
| Bulk pipe stalled             | `[AIC] bulk IN timeout 5000 ms`            |

---

## 8. Timeline (6 Milestones)

| Wk | Milestone                                                                  |
|----|----------------------------------------------------------------------------|
| 1  | M1: Firmware loader CLI working — Stage 1 → Stage 2 confirmed on Intel Mac |
| 2  | M2: DriverKit skeleton builds + installs (no 802.11 yet)                   |
| 3  | M3: IOUSBHost transport + bulk read/write functional                       |
| 4  | M4: Scan state machine — `networksetup -setairportnetwork enX` works      |
| 5  | M5: Auth/assoc state machine — actual connection to "Starlink" succeeds   |
| 6  | M6: Tx/Rx rate stable, packet loss < 0.5% under 10-min soak test           |
| 7–8| M7: Code-signed, deployed, CI integration                                  |
| 9–10| M8: Apple Silicon parity (arm64 build)                                   |
| 11–12| M9: open-source release + developer notes                               |

---

## 9. Risks and Mitigations

| Risk                                                | Mitigation                                              |
|-----------------------------------------------------|---------------------------------------------------------|
| IO80211 headers not public                          | Use opensource.apple.com Darwin tarball                 |
| Firmware blob licensing (GPL)                       | Fetcher-only, redistribute only at user opt-in          |
| DriverKit not accepting custom IOProviderClass     | Validate against `IOUSBHostDevice` reference in Xcode 15|
| Apple Silicon breakage (different USB pipe timing)  | Test on both Intel and Apple Silicon in M7             |
| DriverKit memory limits                             | Use `IOBufferMemoryDescriptor` for zero-copy DMA       |
| App Store distribution impossible                   | Distribute via Developer ID + GitHub releases            |
| User-mode `sudo` for loader                         | Use `setuid` bit on `aicloader` binary                  |

---

## 10. What Gets Cloned On The Intel Mac Build Host

```bash
mkdir -p ~/projects/aic8800d80
cd ~/projects/aic8800d80

# Reference Linux driver — protocol source of truth
git clone https://github.com/olamellberg/AIC8800D80.git        refs/linux-dkms

# Apple Darwin IO80211Family — headers for IO80211Controller subclass
curl -L https://opensource.apple.com/tarballs/IO80211Family/IO80211Family-1600.5.tar.gz \
        -o refs/darwin/IO80211Family.tar.gz

# Android common kernel — firmware blob source
git clone --depth 1 https://android.googlesource.com/kernel/common.git refs/aic8800-and-kernel

# This repo (the driver + loader code)
gh repo clone castlemilk/event-horizon app
```

Total disk: ~2.5 GB.

---

## 11. Open Questions Before Coding

These need decisions before Phase 1 starts:

1. **Driver signing identity.** Do we have a Developer ID Application cert
   ready, or do we build unsigned (`kextload` only) for local dev?
2. **App Store fork.** Do we ship a non-driver version of the app build for
   the App Store (no IO80211 private framework) and a Developer-ID version
   for the driver, or do we ship only the Developer-ID version?
3. **Firmware cache location.** `~/.event-horizon/firmware/` (user-owned) or
   `/Library/Application Support/EventHorizon/firmware/` (system-wide)?
4. **BT coexistence.** The chipset is `0x8d81` WiFi+BT. Do we ship a BT HCI
   driver too, or only Wi-Fi? HCI is a separate DriverKit workstream.
5. **Patent claims.** AICSEMI's firmware stack may have patent-encumbered
   elements. We redistribute unchanged; no patent review needed at this stage.

---

## 12. Appendix — Command Catalogue (Excerpt)

```c
// From aic8800_drv.h, opcode table
#define AIC_OP_SCAN_START_REQ     0x0001
#define AIC_OP_SCAN_RESULT_EVT    0x8001
#define AIC_OP_CONNECT_REQ        0x0010
#define AIC_OP_CONNECT_RESULT_EVT 0x8010
#define AIC_OP_DISCONNECT_REQ     0x0011
#define AIC_OP_TX_REQ             0x0020
#define AIC_OP_RX_EVT             0x8020
#define AIC_OP_FLASH_CONF         0x0090
#define AIC_OP_BOOT_READY         0x00A0
```

## 13. Appendix — File Tree Target

```text
pkg/aic8800d80/
├── loader.go
├── loader_capi.go
├── protocol/
│   ├── opcodes.go
│   ├── stages.go
│   └── firmware.go
├── firmware/
│   ├── manifest.go
│   └── embed.go
└── loader_test.go
cmd/usbwifi/
├── main.go                       # existing
├── aicloader/main.go             # new
└── firmware/main.go              # new
DriverKit/AIC8800D80/
├── AIC8800D80Driver.iig
├── AIC8800D80Controller.iig
├── AIC8800D80Controller.cpp
├── AIC8800D80Channel.h
├── AIC8800D80UsbTransport.cpp
├── AIC8800D80Firmware.cpp
├── AIC8800D80Scan.cpp
├── AIC8800D80Assoc.cpp
├── AIC8800D80Tx.cpp
├── AIC8800D80Rx.cpp
├── Info.plist
├── Makefile
└── AIC8800D80Tests/
    ├── transport_test.cpp
    ├── scan_test.cpp
    ├── assoc_test.cpp
    └── firmware_test.cpp
```
