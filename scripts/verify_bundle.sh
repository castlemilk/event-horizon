#!/bin/bash
set -e

APP_NAME="Event Horizon"
BUILD_DIR="build"
APP_BUNDLE="${BUILD_DIR}/${APP_NAME}.app"
CONTENTS_DIR="${APP_BUNDLE}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"
FRAMEWORKS_DIR="${CONTENTS_DIR}/Frameworks"

echo "================================================================"
echo "🧪 Running Automated E2E App Bundle Integrity Verification"
echo "================================================================"

# 1. Verify App Bundle Structure
if [ ! -d "${APP_BUNDLE}" ]; then
    echo "❌ ERROR: App Bundle missing at ${APP_BUNDLE}. Run task build first."
    exit 1
fi
echo "  ✅ App Bundle exists: ${APP_BUNDLE}"

# 2. Verify Main Binary Executable
MAIN_BIN="${MACOS_DIR}/EventHorizonApp"
if [ ! -x "${MAIN_BIN}" ]; then
    echo "❌ ERROR: Main executable missing or not executable at ${MAIN_BIN}"
    exit 1
fi
echo "  ✅ Main executable present and executable: ${MAIN_BIN}"

# 3. Verify Daemon Binary
DAEMON_BIN="${RESOURCES_DIR}/usbwifi"
if [ ! -x "${DAEMON_BIN}" ]; then
    echo "❌ ERROR: Daemon binary missing or not executable at ${DAEMON_BIN}"
    exit 1
fi
echo "  ✅ Daemon binary present and executable: ${DAEMON_BIN}"

# 4. Verify Black Hole Logo Asset
LOGO_ASSET="${RESOURCES_DIR}/blackhole_logo.jpg"
if [ ! -f "${LOGO_ASSET}" ] || [ ! -s "${LOGO_ASSET}" ]; then
    echo "❌ ERROR: Black hole logo asset missing or 0 bytes at ${LOGO_ASSET}"
    exit 1
fi
echo "  ✅ Black hole logo asset verified: ${LOGO_ASSET}"

# 5. Verify Framework Dynamic Libraries
LIBUSB_DYLIB="${FRAMEWORKS_DIR}/libusb-1.0.0.dylib"
if [ ! -f "${LIBUSB_DYLIB}" ]; then
    echo "❌ ERROR: Bundled libusb dylib missing at ${LIBUSB_DYLIB}"
    exit 1
fi
echo "  ✅ Bundled libusb framework verified: ${LIBUSB_DYLIB}"

# 6. Verify Info.plist Keys
PLIST_FILE="${CONTENTS_DIR}/Info.plist"
if [ ! -f "${PLIST_FILE}" ]; then
    echo "❌ ERROR: Info.plist missing at ${PLIST_FILE}"
    exit 1
fi

if ! grep -q "com.castlemilk.eventhorizon" "${PLIST_FILE}"; then
    echo "❌ ERROR: Bundle ID com.castlemilk.eventhorizon missing in Info.plist"
    exit 1
fi

if ! grep -q "ITSAppUsesNonExemptEncryption" "${PLIST_FILE}"; then
    echo "❌ ERROR: App Store encryption compliance key missing in Info.plist"
    exit 1
fi
echo "  ✅ Info.plist metadata & App Store compliance keys verified."

# 7. Verify Codesign Validity
if command -v codesign &> /dev/null; then
    codesign --verify --deep --strict "${APP_BUNDLE}" 2>&1
    echo "  ✅ Codesign deep verification passed cleanly."
fi

echo ""
echo "================================================================"
echo "🎉 ALL E2E BUNDLE INTEGRITY CHECKS PASSED SUCCESSFULLY!"
echo "================================================================"
