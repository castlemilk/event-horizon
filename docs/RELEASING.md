# Releasing Event Horizon

One command does the whole thing once the machine is set up:

```bash
task release            # test → build + sign → notarise + staple → GitHub release
```

The stages are separate tasks so a failure can be resumed:

| Task | What it does | Needs |
|---|---|---|
| `task build` | `scripts/package_app.sh`: Go daemon + MCP server + Swift app into `build/Event Horizon.app`, signed with Developer ID + hardened runtime, wrapped in a signed DMG | the `Developer ID Application` certificate in the login keychain, libusb from Homebrew |
| `task release:notarize` | `scripts/notarize.sh`: `notarytool submit --wait`, staple, `spctl` assessment, writes `.sha256` | an App Store Connect API key (see below) |
| `task release:github` | tags `v<VERSION>`, pushes the tag, `gh release create` with the DMG + sha256 and `docs/release-notes/v<VERSION>.md` | `gh auth`, the release notes file |

`VERSION` lives in `scripts/package_app.sh`, `scripts/notarize.sh`, `Info.plist`
(`CFBundleShortVersionString`) and the Taskfile default. Bump all four, write
`docs/release-notes/v<VERSION>.md`, then `task release`.

## Credentials

**Signing**: `Developer ID Application: Ben Ebsworth (WFTX6CN23F)`. The
"3rd Party Mac Developer" certificates in the same keychain are App-Store-only;
Gatekeeper rejects a direct download signed with them. `EH_SANDBOX=1` selects
that flavour (a viewer that cannot claim hardware; see `Entitlements-direct.plist`
for why).

**Notarisation**: an App Store Connect API key. The `.p8` is the only secret
and lives at `~/.appstoreconnect/private_keys/AuthKey_<KEY_ID>.p8`; key ID and
issuer ID are defaults in `scripts/notarize.sh` and can be overridden with
`EH_NOTARY_KEY_ID`, `EH_NOTARY_ISSUER`, `EH_NOTARY_KEY`. Generate one at
App Store Connect → Users and Access → Integrations → App Store Connect API
(Developer role is enough).

## What the DMG must not contain

The carved `fmacfw_8800d80_u02_ipc.bin` is vendor-proprietary. The package
script refuses to bundle it unless `EH_BUNDLE_FIRMWARE=1`, and a build made
that way must not be published. Users produce the blob themselves with
`usbwifi firmware carve`, from the driver on their own dongle.

## Verifying a published DMG

```bash
curl -LO https://github.com/castlemilk/event-horizon/releases/latest/download/EventHorizon-1.0.0-macOS.dmg
spctl --assess --type open --context context:primary-signature -v EventHorizon-1.0.0-macOS.dmg
# → accepted, source=Notarized Developer ID
xcrun stapler validate EventHorizon-1.0.0-macOS.dmg
```
