# Loading the AIC8800D80 dext on macOS

The driver source compiles and links into a valid, signed `.dext`
(`AIC8800D80/build/AIC8800D80Driver.dext`). Getting macOS to actually **load**
it is gated by two OS mechanisms that are independent of the code:

1. **Entitlements.** A dext needs `com.apple.developer.driverkit`,
   `com.apple.developer.driverkit.transport.usb`, and
   `com.apple.developer.driverkit.family.networking` in its code signature.
   These are *restricted* entitlements. Measured fact: `codesign` **silently
   drops all entitlements** from a DriverKit-platform binary (Mach-O
   `platform 10`) unless a **provisioning profile that authorizes them** is
   present — this is true for both a Developer ID cert and an ad-hoc signature,
   and even for a non-restricted test entitlement. No profile on this machine
   grants DriverKit, and team `WFTX6CN23F` is not currently approved for it.

2. **System policy.** With SIP enabled, only a properly-provisioned, notarized
   dext delivered inside an app will load.

There is **no CLI** to activate a dext (`systemextensionsctl` has only
`developer`, `list`, `reset`, `uninstall`, `gc`). Activation must come from the
host app: `EventHorizonActivator.app` (built by `Activator/Makefile`), which
embeds the dext and calls `OSSystemExtensionRequest`.

Pick one path.

---

## Path A — Apple-signed, SIP stays on

Two tracks. **A1 (development)** is the fast on-ramp to a working radio on *this*
machine; **A2 (distribution)** is what ships to other Macs later. Both keep SIP
enabled — Apple's guidance is explicit that DriverKit development does **not**
require disabling SIP when you sign with an Apple Development identity.

### A1 — development signing (fast, this machine, no Apple wait)

Development signing of the USB DriverKit family is **self-serve** — it does not
need the multi-week restricted-entitlement approval that distribution does, and
it sidesteps the fact that VID `0xa69c` belongs to AICSEMI (a distribution-only
snag). App Store Connect API credentials are already on disk (key `3FM8M7Y8DG`,
issuer `f4c22181-…`, team `WFTX6CN23F`), so the whole cert→App ID→profile chain
can be scripted, or done in Xcode with automatic signing.

1. Ensure an **Apple Development** certificate exists for team `WFTX6CN23F`
   (currently only present for the personal team `Q5C5NG4JNU`). Create via Xcode
   → Settings → Accounts → Manage Certificates, or the ASC API.
2. Register App IDs `com.eventhorizon.driver` and the dext child
   `com.eventhorizon.driver.AIC8800D80` with the DriverKit + networking + USB
   transport capabilities; generate a **development** provisioning profile for
   each.
3. Build signed with the dev identity + the dev entitlements variant:
   ```
   make -C AIC8800D80 IDENTITY="Apple Development: … (WFTX6CN23F)" \
                      ENTITLEMENTS=AIC8800D80Driver.dev.entitlements
   make -C Activator  IDENTITY="Apple Development: … (WFTX6CN23F)" \
                      PROFILE=/path/to/AIC8800D80_dev.provisionprofile
   ```
4. Turn on developer mode and activate (SIP stays on):
   ```
   systemextensionsctl developer on
   make -C Activator activate            # approve in System Settings
   log stream --predicate 'sender == "AIC8800D80Driver"' --level debug
   ```

If the generated profile grants a wildcard VID instead of the pinned pair, read
its exact entitlement with `security cms -D -i <profile>` and mirror it into
`AIC8800D80Driver.dev.entitlements`.

### A2 — distribution signing (ships to any Mac, multi-week)

The route for shipping to end users' Macs unchanged. Needs Apple to grant the
restricted `com.apple.developer.driverkit.transport.usb` entitlement for a
**specific** VID.

1. Submit the request — see `DRIVERKIT-ENTITLEMENT-REQUEST.md` for the exact
   text (it addresses the VID-ownership question, since `0xa69c` is AICSEMI's).
   Portal → Account → Additional Resources → request the entitlement. Approval
   is manual, typically weeks.
2. After approval, add the granted VID capability to the App ID's *Additional
   Capabilities* and **re-generate** the provisioning profiles.
3. Rebuild with the Developer ID identity + the approved (specific-VID)
   entitlements, notarize `EventHorizonActivator.app`, staple, move to
   `/Applications`:
   ```
   make -C AIC8800D80 IDENTITY="Developer ID Application: … (WFTX6CN23F)"
   make -C Activator  IDENTITY="Developer ID Application: … (WFTX6CN23F)" \
                      PROFILE=/path/to/AIC8800D80_dist.provisionprofile
   xcrun notarytool submit EventHorizonActivator.app.zip --keychain-profile … --wait
   xcrun stapler staple EventHorizonActivator.app
   ```
4. `open EventHorizonActivator.app` and approve under **System Settings →
   General → Login Items & Extensions → Driver Extensions**. No developer mode,
   no SIP change.

## Path B — local development (disable SIP + AMFI, fast, this machine only)

No Apple approval needed. AMFI-disabled means the kernel stops enforcing the
missing entitlements, so the ad-hoc-signed dext loads. **Lowers the machine's
security posture** and needs a Recovery reboot.

1. Reboot to Recovery (hold power on Apple silicon) → Terminal:
   ```
   csrutil disable
   ```
2. Back in macOS, allow unrestricted entitlements and turn on developer mode:
   ```
   sudo nvram boot-args="amfi_get_out_of_my_way=0x1"
   sudo systemextensionsctl developer on
   sudo reboot
   ```
3. Build + activate (ad-hoc is the default identity):
   ```
   make -C AIC8800D80
   make -C Activator activate
   ```
4. Confirm binding once the dongle is in operational mode (VID `0xa69c`,
   PID `0x8d81`/`0x8d83`):
   ```
   systemextensionsctl list
   log stream --predicate 'sender == "AIC8800D80Driver"' --level debug
   ```
   The driver logs its LMAC bring-up (`stack_start` → `ClearStall` → scan) and
   prints each beacon BSSID it sees.

To undo Path B: `csrutil enable` in Recovery, `sudo nvram -d boot-args`,
`systemextensionsctl developer off`.

---

## What the driver does once loaded

`AIC8800D80Driver.cpp` runs the in-kernel LMAC bring-up that user-space libusb
could not: after `MM_SET_STACK_START` halts the bulk pipes, it calls
`IOUSBHostPipe::ClearStall` to recover them (the operation libusb cannot perform
on macOS), then runs RF calibration, TX-power, MAC init, and an active scan,
logging beacons. It does **not** yet expose an `enX` interface to macOS — that
requires the IO80211 family binding (see `aic8800d80-macos-driver-plan.md`),
which is the next milestone after radio bring-up is confirmed in-kernel.
