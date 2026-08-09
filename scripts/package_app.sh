#!/bin/bash
set -e

APP_NAME="Universal WiFi Manager"
VERSION="1.0.0"
BUILD_DIR="build"
APP_BUNDLE="${BUILD_DIR}/${APP_NAME}.app"
CONTENTS_DIR="${APP_BUNDLE}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"
DMG_NAME="${BUILD_DIR}/UniversalWiFiManager-${VERSION}-macOS.dmg"
PKG_NAME="${BUILD_DIR}/UniversalWiFiManager-${VERSION}-AppStore.pkg"

echo "================================================================"
echo "🚀 Building ${APP_NAME} v${VERSION} for macOS App Store (Apple Silicon)"
echo "================================================================"

rm -rf "${BUILD_DIR}"
mkdir -p "${MACOS_DIR}"
mkdir -p "${RESOURCES_DIR}"

# Clear any stale SwiftPM lock files if leftover from interrupted builds
rm -f .build/*.active .build/index.db-wal .build/index.db-shm 2>/dev/null || true

# 1. Compile Go USB Wi-Fi Daemon Binary
echo "⚙️ [1/5] Compiling Go usbwifi daemon..."
mkdir -p bin
CGO_ENABLED=1 CGO_CFLAGS="-I/opt/homebrew/include" CGO_LDFLAGS="-L/opt/homebrew/lib -lusb-1.0" \
  go build -ldflags="-s -w" -o "${RESOURCES_DIR}/usbwifi" ./cmd/usbwifi
chmod +x "${RESOURCES_DIR}/usbwifi"
cp "${RESOURCES_DIR}/usbwifi" bin/usbwifi
chmod +x bin/usbwifi

# 2. Compile Release Swift App (Single invocation)
echo "⚙️ [2/5] Compiling Swift Release App..."
swift build -c release --triple arm64-apple-macosx14.0

SHOW_PATH="$(swift build -c release --triple arm64-apple-macosx14.0 --show-bin-path 2>/dev/null || echo ".build/arm64-apple-macosx/release")"
RELEASE_BIN="${SHOW_PATH}/StarlinkWiFiApp"

if [ ! -f "${RELEASE_BIN}" ]; then
    RELEASE_BIN=".build/release/StarlinkWiFiApp"
fi

cp "${RELEASE_BIN}" "${MACOS_DIR}/StarlinkWiFiApp"
chmod +x "${MACOS_DIR}/StarlinkWiFiApp"

# 3. Copy Plist and Entitlements
echo "⚙️ [3/5] Assembling Bundle Structure & Info.plist..."
cp Info.plist "${CONTENTS_DIR}/Info.plist"
chmod 644 "${CONTENTS_DIR}/Info.plist"

# 4. Sign App Bundle with Entitlements
echo "⚙️ [4/5] Code signing App Bundle..."
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
