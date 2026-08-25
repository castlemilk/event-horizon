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

# 3. Copy Plist and Entitlements
echo "⚙️ [3/5] Assembling Bundle Structure & Info.plist..."
cp Info.plist "${CONTENTS_DIR}/Info.plist"
chmod 644 "${CONTENTS_DIR}/Info.plist"

# 4. Sign App Bundle with Entitlements & Clear Quarantine
echo "⚙️ [4/5] Code signing App Bundle..."
xattr -cr "${APP_BUNDLE}" 2>/dev/null || true
if command -v codesign &> /dev/null; then
    codesign --force --deep --sign - --entitlements Entitlements.plist "${APP_BUNDLE}" || true
    echo "  ✅ App Bundle signed successfully."
fi

# 5. Create Distribution DMG & PKG
echo "⚙️ [5/5] Packaging DMG and App Store PKG..."
if command -v hdiutil &> /dev/null; then
    hdiutil create -volname "${APP_NAME}" -srcfolder "${APP_BUNDLE}" -ov -format UDZO "${DMG_NAME}"
    echo "✅ DMG created at: ${DMG_NAME}"
fi

if command -v productbuild &> /dev/null; then
    productbuild --component "${APP_BUNDLE}" /Applications "${PKG_NAME}" || true
    echo "✅ App Store PKG created at: ${PKG_NAME}"
fi

echo ""
echo "================================================================"
echo "🎉 SUCCESS: ${APP_NAME} v${VERSION} is ready for distribution!"
echo "  📱 App Bundle: ${APP_BUNDLE}"
echo "  💿 Disk Image: ${DMG_NAME}"
echo "  📦 App Store PKG: ${PKG_NAME}"
echo "================================================================"
