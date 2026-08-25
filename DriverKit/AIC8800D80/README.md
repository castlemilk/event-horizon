# AIC8800D80 DriverKit Driver

This directory contains the DriverKit driver for the AICSEMI AIC8800D80
USB Wi-Fi 6 chipset. It is a skeleton: it matches the operational
VID:PID but does not yet bind to the macOS 802.11 subsystem.

## Status

**Skeleton.** The driver compiles and matches the device. It does NOT
yet:

- Bind to the IO80211 private framework
- Implement scan/auth/assoc state machine
- Implement Tx/Rx ring management

The user-space firmware loader (Phase 1) is at `pkg/aic8800d80/`. Run
`sudo ./bin/usbwifi aicloader --firmware-dir=<dir>` to get the dongle
to the operational stage (a69c:8d81 or a69c:8d83). Once the device
re-enumerates, this driver will match it.

## Build

```
cd DriverKit/AIC8800D80
make
```

The Makefile produces `build/AIC8800D80Driver.dext`.

**Note:** DriverKit drivers are normally built via Xcode. The
`Makefile` here is a documented manual flow that requires the iig
compiler to be run ahead of time:

```bash
mkdir -p build
iig --def AIC8800D80Driver.iig --header build/AIC8800D80Driver.h \
    --impl build/AIC8800D80Driver_impl.h --framework-name DriverKit \
    --deployment-target 13.0 -- \
    -isysroot $(xcrun --sdk driverkit --show-sdk-path) \
    -I$(xcrun --sdk driverkit --show-sdk-path)/System/DriverKit/System/Library/Frameworks/DriverKit.framework/Headers \
    -I$(xcrun --sdk driverkit --show-sdk-path)/System/DriverKit/System/Library/Frameworks/USBDriverKit.framework/Headers
```

Or create an Xcode project (recommended):

```
File > New > Project > macOS > DriverKit > Driver
```

Then add the AIC8800D80Driver.cpp / .iig / .h files.

## Signing & Loading

```bash
# 1. Sign with Developer ID Application cert (already set as IDENTITY
#    in the Makefile, or use Xcode's automatic signing).
make IDENTITY="Developer ID Application: Your Name"

# 2. Approve the developer mode for system extensions.
sudo systemextensionsctl developer on

# 3. Copy the bundle to /Library/SystemExtensions.
sudo cp -R build/AIC8800D80Driver.dext /Library/SystemExtensions/

# 4. Load it.
sudo systemextensionsctl load com.eventhorizon.driver.AIC8800D80

# 5. Verify.
systemextensionsctl list | grep -i aic
```

## Architecture

```
DriverKit/AIC8800D80/
├── AIC8800D80Driver.iig           # DriverKit interface definition
├── AIC8800D80Driver.h             # (generated from .iig)
├── AIC8800D80Driver.cpp           # Skeleton implementation
├── IO80211Controller.h            # ⚠️ REVERSED-ENGINEERED MOCK
├── Info.plist                     # match dictionary
├── AIC8800D80Driver.entitlements  # DriverKit entitlements
├── Makefile                       # Build
└── README.md                      # This file
```

## Why A Mock IO80211Controller.h?

`IO80211Controller` is part of the private `IO80211Family` framework.
The public Swift and Objective-C SDKs do not include it. To write a
real DriverKit driver that binds to the macOS Wi-Fi stack, you need
the private headers. They are leaked in the open-source Darwin tree
but not in the public SDK.

The mock `IO80211Controller.h` here provides just enough surface to
compile the skeleton. Replace it with the real header when integrating.

## References

Linux driver (protocol source of truth):
https://github.com/radxa-pkg/aic8800

Apple DriverKit documentation:
https://developer.apple.com/documentation/driverkit

Apple IO80211Family open source:
https://github.com/apple-oss-distributions (when available)

Plan document:
`docs/aic8800d80-macos-driver-plan.md`
