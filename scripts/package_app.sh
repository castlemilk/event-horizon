#!/bin/bash
set -e

APP_NAME="Event Horizon"
VERSION="1.0.0"
BUILD_DIR="build"
APP_BUNDLE="${BUILD_DIR}/${APP_NAME}.app"
CONTENTS_DIR="${APP_BUNDLE}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"
FRAMEWORKS_DIR="${CONTENTS_DIR}/Frameworks"
DMG_NAME="${BUILD_DIR}/EventHorizon-${VERSION}-macOS.dmg"
PKG_NAME="${BUILD_DIR}/EventHorizon-${VERSION}-AppStore.pkg"

echo "================================================================"
echo "🚀 Building ${APP_NAME} v${VERSION} for macOS (Apple Silicon)"
echo "================================================================"

rm -rf "${BUILD_DIR}"
mkdir -p "${MACOS_DIR}"
mkdir -p "${RESOURCES_DIR}"
mkdir -p "${FRAMEWORKS_DIR}"

# Clear any stale SwiftPM lock files if leftover from interrupted builds
rm -f .build/*.active .build/index.db-wal .build/index.db-shm 2>/dev/null || true

# 1. Compile Go USB Wi-Fi Daemon and MCP Agent Binaries
echo "⚙️ [1/5] Compiling Go usbwifi daemon & MCP server..."
mkdir -p bin
CGO_ENABLED=1 CGO_CFLAGS="-I/opt/homebrew/include" CGO_LDFLAGS="-L/opt/homebrew/lib -lusb-1.0" \
  go build -buildvcs=false -ldflags="-s -w" -o "${RESOURCES_DIR}/usbwifi" ./cmd/usbwifi
chmod +x "${RESOURCES_DIR}/usbwifi"
cp "${RESOURCES_DIR}/usbwifi" "${MACOS_DIR}/usbwifi"
cp "${RESOURCES_DIR}/usbwifi" bin/usbwifi
chmod +x bin/usbwifi

CGO_ENABLED=1 CGO_CFLAGS="-I/opt/homebrew/include" CGO_LDFLAGS="-L/opt/homebrew/lib -lusb-1.0" \
  go build -buildvcs=false -ldflags="-s -w" -o "${RESOURCES_DIR}/usbwifi-mcp" ./cmd/usbwifi-mcp
if [ -f "${RESOURCES_DIR}/usbwifi-mcp" ]; then
    chmod +x "${RESOURCES_DIR}/usbwifi-mcp"
    cp "${RESOURCES_DIR}/usbwifi-mcp" bin/usbwifi-mcp
fi

if [ -f "Resources/com.castlemilk.eventhorizon.usbwifi.plist" ]; then
    cp "Resources/com.castlemilk.eventhorizon.usbwifi.plist" "${RESOURCES_DIR}/"
fi

# --- dongle bootstrap payload -------------------------------------------
# The binary does the ZeroCD eject and the firmware upload itself (Go, via
# libusb). It does NOT ship firmware. The image that runs on this chip is
# carved from the vendor driver on the dongle's own ZeroCD volume
# (`usbwifi firmware carve`), and the other three blobs are fetched from a
# public repo (`usbwifi firmware fetch`). Bundling the carved blob would put
# vendor-proprietary code in a public DMG, so the app tells the user how to
# produce it instead. EH_BUNDLE_FIRMWARE=1 overrides this for a private build.
FW_SET="${HOME}/.event-horizon/firmware/aic8800D80-hybrid"
if [ "${EH_BUNDLE_FIRMWARE:-0}" = "1" ] && [ -s "${FW_SET}/fmacfw_8800d80_u02_ipc.bin" ]; then
    mkdir -p "${RESOURCES_DIR}/firmware/aic8800D80-hybrid"
    cp "${FW_SET}"/* "${RESOURCES_DIR}/firmware/aic8800D80-hybrid/"
    echo "   • PRIVATE BUILD: bundled firmware set from ${FW_SET} — do not publish this DMG"
else
    echo "   • no firmware bundled (public build): users run 'usbwifi firmware fetch' + 'firmware carve'"
fi

# Bundle libusb dynamic library inside Contents/Frameworks for Sandbox & Gatekeeper compliance
if [ -f "/opt/homebrew/opt/libusb/lib/libusb-1.0.0.dylib" ]; then
    cp -f "/opt/homebrew/opt/libusb/lib/libusb-1.0.0.dylib" "${FRAMEWORKS_DIR}/libusb-1.0.0.dylib"
    chmod 755 "${FRAMEWORKS_DIR}/libusb-1.0.0.dylib"
    install_name_tool -id "@executable_path/../Frameworks/libusb-1.0.0.dylib" "${FRAMEWORKS_DIR}/libusb-1.0.0.dylib" 2>/dev/null || true
    install_name_tool -change "/opt/homebrew/opt/libusb/lib/libusb-1.0.0.dylib" "@executable_path/../Frameworks/libusb-1.0.0.dylib" "${RESOURCES_DIR}/usbwifi" 2>/dev/null || true
    install_name_tool -change "/opt/homebrew/opt/libusb/lib/libusb-1.0.0.dylib" "@executable_path/../Frameworks/libusb-1.0.0.dylib" "${MACOS_DIR}/usbwifi" 2>/dev/null || true
fi

# 2. Compile Release Swift App (Single invocation)
echo "⚙️ [2/5] Compiling Swift Release App..."
swift build -c release --triple arm64-apple-macosx14.0

SHOW_PATH=".build/arm64-apple-macosx/release"
if [ ! -d "${SHOW_PATH}" ]; then
    SHOW_PATH=".build/release"
fi
RELEASE_BIN="${SHOW_PATH}/EventHorizonApp"

cp "${RELEASE_BIN}" "${MACOS_DIR}/EventHorizonApp"
chmod +x "${MACOS_DIR}/EventHorizonApp"

# Copy SwiftPM resource bundles (e.g. UniversalWiFiManager_EventHorizonApp.bundle) into Contents/Resources/
for b in "${SHOW_PATH}"/*.bundle; do
    if [ -d "$b" ]; then
        cp -R "$b" "${RESOURCES_DIR}/"
    fi
done
cp Sources/EventHorizonApp/Resources/blackhole_logo.jpg "${RESOURCES_DIR}/blackhole_logo.jpg" 2>/dev/null || true
if [ -f "Resources/AppIcon.icns" ]; then
    cp "Resources/AppIcon.icns" "${RESOURCES_DIR}/AppIcon.icns"
fi

# 3. Copy Plist and Entitlements
echo "⚙️ [3/5] Assembling Bundle Structure & Info.plist..."
cp Info.plist "${CONTENTS_DIR}/Info.plist"
chmod 644 "${CONTENTS_DIR}/Info.plist"

# 4. Sign App Bundle with Entitlements & Clear Quarantine
echo "⚙️ [4/5] Code signing App Bundle..."
xattr -cr "${APP_BUNDLE}" 2>/dev/null || true

# Which entitlements to sign with.
#
# The default is the DIRECT (non-sandboxed) build, because the sandboxed one
# cannot drive a dongle at all: raw libusb is unavailable inside the App
# Sandbox, and the supervisor's authorization prompt is refused outright
# (AppleScript -60005, which reports as a wrong password but is the sandbox
# declining to escalate). Building the sandboxed flavour by default produced an
# app that launched, looked healthy, and could never claim hardware.
#
# EH_SANDBOX=1 selects the App Store flavour, which is a viewer only.
if [ "${EH_SANDBOX:-0}" = "1" ]; then
    ENTITLEMENTS="Entitlements.plist"
    echo "   • signing SANDBOXED (App Store) — this build CANNOT claim a dongle"
else
    ENTITLEMENTS="Entitlements-direct.plist"
    echo "   • signing DIRECT (non-sandboxed) — required for USB + privilege escalation"
fi

# Signing identity. Distribution outside the App Store must be signed with
# Developer ID and notarised; the "3rd Party Mac Developer" certificates are
# App-Store-only and Gatekeeper rejects them on a direct download. Override
# with EH_SIGN_IDENTITY, or set EH_SANDBOX=1 for the App Store flavour.
if [ "${EH_SANDBOX:-0}" = "1" ]; then
    APP_CERT="${EH_SIGN_IDENTITY:-3rd Party Mac Developer Application: Ben Ebsworth (WFTX6CN23F)}"
else
    APP_CERT="${EH_SIGN_IDENTITY:-Developer ID Application: Ben Ebsworth (WFTX6CN23F)}"
fi
INSTALLER_CERT="3rd Party Mac Developer Installer: Ben Ebsworth (WFTX6CN23F)"

if security find-identity -v -p codesigning | grep -q "${APP_CERT}"; then
    echo "  ✍️ Signing with '${APP_CERT}' (hardened runtime)..."
    # Inside-out: libraries, then helper executables, then the main binary,
    # then the bundle. Every Mach-O gets the hardened runtime, which
    # notarisation requires; the Go binaries need the entitlements too
    # (allow-jit + disable-library-validation for libusb).
    for dylib in "${FRAMEWORKS_DIR}"/*.dylib; do
        [ -f "$dylib" ] && codesign --force --timestamp --options runtime --sign "${APP_CERT}" "$dylib"
    done
    for helper in "${RESOURCES_DIR}/usbwifi" "${RESOURCES_DIR}/usbwifi-mcp" "${MACOS_DIR}/usbwifi"; do
        [ -f "$helper" ] && codesign --force --timestamp --options runtime --entitlements "${ENTITLEMENTS}" --sign "${APP_CERT}" "$helper"
    done
    codesign --force --timestamp --options runtime --entitlements "${ENTITLEMENTS}" --sign "${APP_CERT}" "${MACOS_DIR}/EventHorizonApp"
    codesign --force --timestamp --options runtime --entitlements "${ENTITLEMENTS}" --sign "${APP_CERT}" "${APP_BUNDLE}"
    codesign --verify --deep --strict --verbose=2 "${APP_BUNDLE}"
    echo "  ✅ App bundle signed."
    SIGNED=1
else
    codesign --force --deep --sign - --entitlements "${ENTITLEMENTS}" "${APP_BUNDLE}" || true
    echo "  ⚠️ '${APP_CERT}' not in the keychain — bundle signed ad-hoc (will NOT pass Gatekeeper)."
    SIGNED=0
fi

# 5. Create Distribution DMG & PKG
echo "⚙️ [5/5] Packaging DMG and App Store PKG..."
if command -v hdiutil &> /dev/null; then
    # Stage the volume with an /Applications symlink so the DMG is drag-to-install.
    STAGE="${BUILD_DIR}/dmg-stage"
    rm -rf "${STAGE}" && mkdir -p "${STAGE}"
    cp -R "${APP_BUNDLE}" "${STAGE}/"
    ln -s /Applications "${STAGE}/Applications"
    hdiutil create -volname "${APP_NAME}" -srcfolder "${STAGE}" -ov -format UDZO "${DMG_NAME}"
    rm -rf "${STAGE}"
    if [ "${SIGNED}" = "1" ]; then
        codesign --force --timestamp --sign "${APP_CERT}" "${DMG_NAME}"
    fi
    echo "✅ DMG created at: ${DMG_NAME}"
fi

if command -v productbuild &> /dev/null; then
    if security find-identity -v | grep -q "${INSTALLER_CERT}"; then
        productbuild --sign "${INSTALLER_CERT}" --component "${APP_BUNDLE}" /Applications "${PKG_NAME}"
        echo "✅ Signed App Store PKG created at: ${PKG_NAME}"
    else
        productbuild --component "${APP_BUNDLE}" /Applications "${PKG_NAME}" || true
        echo "✅ App Store PKG created at: ${PKG_NAME}"
    fi
fi

echo ""
echo "================================================================"
echo "🎉 SUCCESS: ${APP_NAME} v${VERSION} is ready for distribution!"
echo "  📱 App Bundle: ${APP_BUNDLE}"
echo "  💿 Disk Image: ${DMG_NAME}"
echo "  📦 App Store PKG: ${PKG_NAME}"
echo "================================================================"
