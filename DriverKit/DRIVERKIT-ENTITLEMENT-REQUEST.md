# DriverKit distribution entitlement request (track A2)

Submit at the Apple Developer portal → **Account → Additional Resources →
[request the DriverKit entitlement / capability]** (the "System Extension /
DriverKit" capability request), signed in as team `WFTX6CN23F`. Development (A1)
does **not** need this; only distributing to other Macs unchanged does.

Copy the fields below.

---

**Team ID:** WFTX6CN23F

**Entitlements requested (distribution):**

- `com.apple.developer.driverkit`
- `com.apple.developer.driverkit.family.networking`
- `com.apple.developer.driverkit.transport.usb` — for USB **Vendor ID `0xA69C`
  (42652 dec)**, Product IDs `0x8D81` (36225) and `0x8D83` (36235)

**App IDs:**

- App: `com.eventhorizon.driver`
- Driver extension: `com.eventhorizon.driver.AIC8800D80`

**Describe your driver and why it needs these entitlements:**

> EventHorizon is a macOS host-side bridge that brings up USB Wi‑Fi adapters as
> additional network endpoints (the built‑in interface is intentionally left
> untouched; the product connects multiple Wi‑Fi endpoints simultaneously via
> USB radios). This DriverKit extension drives the AICSEMI **AIC8800D80** Wi‑Fi 6
> chipset — as shipped in the UGREEN AX900 USB adapter — over its LMAC
> host‑target protocol: firmware bring‑up, RF calibration, MAC initialisation,
> scanning, and station association.
>
> The extension is **strictly scoped to USB Vendor ID `0xA69C` and Product IDs
> `0x8D81` / `0x8D83`** (the adapter's operational descriptors). It contains no
> proprietary or third‑party code; the bring‑up is an independent
> reimplementation of the documented LMAC message protocol. It needs
> `transport.usb` to claim the device's bulk pipes and `family.networking`
> because it is a network‑class driver.
>
> **Vendor ID ownership note:** `0xA69C` is AICSEMI's vendor ID, not ours — we
> are enabling a commodity Wi‑Fi adapter on macOS for interoperability, not
> shipping AICSEMI's own driver. The entitlement would be pinned to the exact
> VID/PID triplet above so it cannot bind any other device. If Apple requires
> written authorization from the VID owner, please advise and we will pursue it;
> otherwise we ask that the request be evaluated on the exact‑VID/PID scoping.

**Distribution method:** Developer ID (outside the Mac App Store), notarized.

---

## After approval

1. Portal → Identifiers → `com.eventhorizon.driver.AIC8800D80` → **Additional
   Capabilities** → enable the granted `DriverKit USB Transport – VendorID`.
2. **Regenerate** the Developer ID provisioning profiles (the #1 cause of
   "unsatisfied entitlements" is forgetting this).
3. Verify the profile actually carries the VID:
   `security cms -D -i AIC8800D80_dist.provisionprofile | grep -A6 transport.usb`
   — if it came back with only one VID/PID, file a follow‑up to add the rest.
4. Build + notarize per `MACOS-LOADING.md` track A2.
