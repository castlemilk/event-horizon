#!/bin/bash
# notarize.sh — submit the DMG to Apple, staple the ticket, prove Gatekeeper
# accepts it. Run after scripts/package_app.sh (task build).
#
# Credentials are an App Store Connect API key (Users and Access → Integrations
# → App Store Connect API). The .p8 is the only secret; the key ID and issuer
# ID are not. Defaults below are the team's; override with env vars.
#
#   EH_NOTARY_KEY      path to AuthKey_<id>.p8   (default ~/.appstoreconnect/private_keys/AuthKey_$EH_NOTARY_KEY_ID.p8)
#   EH_NOTARY_KEY_ID   key id                     (default 3FM8M7Y8DG)
#   EH_NOTARY_ISSUER   issuer uuid                (default the castlemilk team issuer)
set -euo pipefail

VERSION="${VERSION:-1.0.0}"
BUILD_DIR="build"
APP_BUNDLE="${BUILD_DIR}/Event Horizon.app"
DMG="${BUILD_DIR}/EventHorizon-${VERSION}-macOS.dmg"

KEY_ID="${EH_NOTARY_KEY_ID:-3FM8M7Y8DG}"
ISSUER="${EH_NOTARY_ISSUER:-f4c22181-b343-4e92-8fb3-e90dab991b8f}"
KEY="${EH_NOTARY_KEY:-$HOME/.appstoreconnect/private_keys/AuthKey_${KEY_ID}.p8}"

[ -f "${DMG}" ] || { echo "notarize: ${DMG} not found — run 'task build' first" >&2; exit 1; }
[ -f "${KEY}" ]  || { echo "notarize: API key ${KEY} not found (set EH_NOTARY_KEY)" >&2; exit 1; }

# Refuse to notarise something Gatekeeper would reject anyway.
sig=$(codesign -dvv "${DMG}" 2>&1 || true)
if ! grep -q "Authority=Developer ID Application" <<< "${sig}"; then
    echo "notarize: ${DMG} is not signed with Developer ID — rebuild with the certificate in the keychain" >&2
    exit 1
fi

echo "==> Submitting ${DMG} to Apple notary service (this takes a few minutes)..."
xcrun notarytool submit "${DMG}" \
    --key "${KEY}" --key-id "${KEY_ID}" --issuer "${ISSUER}" \
    --wait --timeout 30m --output-format plist > "${BUILD_DIR}/notary-result.plist"

status=$(/usr/libexec/PlistBuddy -c 'Print :status' "${BUILD_DIR}/notary-result.plist" 2>/dev/null || echo unknown)
id=$(/usr/libexec/PlistBuddy -c 'Print :id' "${BUILD_DIR}/notary-result.plist" 2>/dev/null || echo "")
echo "==> notary status: ${status} (submission ${id})"
if [ "${status}" != "Accepted" ]; then
    echo "==> fetching the notary log..." >&2
    xcrun notarytool log "${id}" --key "${KEY}" --key-id "${KEY_ID}" --issuer "${ISSUER}" || true
    exit 1
fi

echo "==> Stapling the ticket to the DMG and the app bundle..."
xcrun stapler staple "${DMG}"
xcrun stapler staple "${APP_BUNDLE}" || true   # the DMG ticket covers the app inside it
xcrun stapler validate "${DMG}"

echo "==> Gatekeeper assessment..."
spctl --assess --type open --context context:primary-signature --verbose=2 "${DMG}"
spctl --assess --type execute --verbose=2 "${APP_BUNDLE}"

shasum -a 256 "${DMG}" | tee "${DMG}.sha256"
echo "==> ${DMG} is notarised, stapled and ready to publish."
