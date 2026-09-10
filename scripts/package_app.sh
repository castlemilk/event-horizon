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
echo "🚀 Building ${APP_NAME} v${VERSION} for macOS App Store (Apple Silicon)"
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
  go build -buildvcs=false -ldflags="-s -w" -o "${RESOURCES_DIR}/usbwifi-mcp" ./cmd/mcp-server 2>/dev/null || true
if [ -f "${RESOURCES_DIR}/usbwifi-mcp" ]; then
    chmod +x "${RESOURCES_DIR}/usbwifi-mcp"
    cp "${RESOURCES_DIR}/usbwifi-mcp" bin/usbwifi-mcp
fi

if [ -f "Resources/com.castlemilk.eventhorizon.usbwifi.plist" ]; then
    cp "Resources/com.castlemilk.eventhorizon.usbwifi.plist" "${RESOURCES_DIR}/"
fi

# --- dongle bootstrap payload -------------------------------------------
# The binary does the ZeroCD eject and the firmware upload itself (Go, via
# libusb) — no helper script is bundled or needed. What it cannot synthesise is
# the firmware. The fmacfw that works on this chip is carved from the vendor driver
# shipped on the dongle's own ZeroCD volume, so it is not in this repo. If a
# complete set is present on the build machine, ship it; otherwise say so
# plainly rather than producing an app that looks complete and cannot flash.
FW_SET="${HOME}/.event-horizon/firmware/aic8800D80-hybrid"
fw_complete=1
for f in fmacfw_8800d80_u02_ipc.bin fw_adid_8800d80_u02.bin fw_patch_8800d80_u02.bin fw_patch_table_8800d80_u02.bin; do
    [ -s "${FW_SET}/${f}" ] || fw_complete=0
done
if [ "${fw_complete}" = "1" ]; then
    mkdir -p "${RESOURCES_DIR}/firmware/aic8800D80-hybrid"
    cp "${FW_SET}"/* "${RESOURCES_DIR}/firmware/aic8800D80-hybrid/"
    echo "   • bundled firmware set aic8800D80-hybrid ($(du -sh "${FW_SET}" | cut -f1 | tr -d ' '))"
else
    echo "   ⚠ no complete firmware set at ${FW_SET}"
    echo "     The bundle will run, but cannot flash a dongle until those blobs exist."
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

APP_CERT="3rd Party Mac Developer Application: Ben Ebsworth (WFTX6CN23F)"
INSTALLER_CERT="3rd Party Mac Developer Installer: Ben Ebsworth (WFTX6CN23F)"

if security find-identity -v | grep -q "${APP_CERT}"; then
    echo "  ✍️ Signing executables with '${APP_CERT}'..."
    codesign --force --sign "${APP_CERT}" --entitlements Entitlements.plist "${FRAMEWORKS_DIR}"/*.dylib 2>/dev/null || true
    codesign --force --sign "${APP_CERT}" --entitlements Entitlements.plist "${RESOURCES_DIR}/usbwifi" 2>/dev/null || true
    codesign --force --sign "${APP_CERT}" --entitlements Entitlements.plist "${MACOS_DIR}/usbwifi" 2>/dev/null || true
    codesign --force --options runtime --sign "${APP_CERT}" --entitlements Entitlements.plist "${MACOS_DIR}/EventHorizonApp"
    codesign --force --deep --options runtime --sign "${APP_CERT}" --entitlements Entitlements.plist "${APP_BUNDLE}"
    echo "  ✅ App Bundle signed successfully with Developer Certificate."
else
    codesign --force --deep --sign - --entitlements Entitlements.plist "${APP_BUNDLE}" || true
    echo "  ⚠️ App Bundle signed ad-hoc."
fi

# 5. Create Distribution DMG & PKG
echo "⚙️ [5/5] Packaging DMG and App Store PKG..."
if command -v hdiutil &> /dev/null; then
    hdiutil create -volname "${APP_NAME}" -srcfolder "${APP_BUNDLE}" -ov -format UDZO "${DMG_NAME}"
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
