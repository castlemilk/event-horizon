#!/bin/bash
# sign.sh — Code-sign the Event Horizon Daemon + DriverKit dext with the
# user's Developer ID Application certificate.
#
# StorageSentry equivalent: scripts/package_app.sh lines 144-156.
#
# Usage:
#   ./scripts/sign.sh                # sign daemon + app + driver
#   ./scripts/sign.sh --daemon       # sign daemon only
#   ./scripts/sign.sh --app         # sign Swift app bundle only
#   ./scripts/sign.sh --driver      # sign DriverKit .dext only
#
# Requires the Developer ID Application cert to be in your login keychain.
# Find yours with: security find-identity -v -p codesigning
#
# The signed binaries will work on macOS without Gatekeeper warnings and
# can grant TCC/keychain access prompts (since the binary has a stable
# signing identity that the OS can recognise).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Default Developer ID Application identity (matches StorageSentry).
SIGNING_IDENTITY="Developer ID Application: Ben Ebsworth (WFTX6CN23F)"
SIGNING_TEAM="WFTX6CN23F"

# What to sign
SIGN_DAEMON=true
SIGN_APP=true
SIGN_DRIVER=true

while [[ $# -gt 0 ]]; do
    case "$1" in
        --daemon) SIGN_DAEMON=true; SIGN_APP=false; SIGN_DRIVER=false; shift ;;
        --app) SIGN_DAEMON=false; SIGN_APP=true; SIGN_DRIVER=false; shift ;;
        --driver) SIGN_DAEMON=false; SIGN_APP=false; SIGN_DRIVER=true; shift ;;
        --identity) SIGNING_IDENTITY="$2"; shift 2 ;;
        --team) SIGNING_TEAM="$2"; shift 2 ;;
        --help|-h)
            grep '^# ' "$0" | sed 's/^# //'
            exit 0 ;;
        *) echo "unknown arg: $1"; exit 2 ;;
    esac
done

if ! security find-identity -v -p codesigning | grep -q "${SIGNING_IDENTITY}"; then
    echo "ERROR: Signing identity not found in keychain: ${SIGNING_IDENTITY}"
    echo "Available identities:"
    security find-identity -v -p codesigning | grep 'Developer ID\|Apple Development'
    exit 1
fi

# Sign the Go daemon (libusb + GUI helper).
sign_daemon() {
    echo "==> Building usbwifi..."
    go build -o bin/usbwifi ./cmd/usbwifi
    echo "==> Building usbwifi-mcp..."
    go build -o bin/usbwifi-mcp ./cmd/usbwifi-mcp

    echo "==> Bundling libusb next to daemon..."
    # Hardened runtime refuses dylibs signed by a different Team ID. We
    # copy the homebrew libusb dylib into bin/ and rpath the daemon to
    # it, then re-sign.
    if [[ -f /opt/homebrew/opt/libusb/lib/libusb-1.0.0.dylib ]]; then
        cp -f /opt/homebrew/opt/libusb/lib/libusb-1.0.0.dylib bin/libusb-1.0.0.dylib
        install_name_tool -id "@rpath/libusb-1.0.0.dylib" bin/libusb-1.0.0.dylib 2>/dev/null || true
        for bin in bin/usbwifi bin/usbwifi-mcp; do
            if [[ -f "$bin" ]]; then
                install_name_tool -change \
                    "/opt/homebrew/opt/libusb/lib/libusb-1.0.0.dylib" \
                    "@rpath/libusb-1.0.0.dylib" \
                    "$bin" 2>/dev/null || true
                # Add rpath so the daemon can find libusb next to itself.
                install_name_tool -add_rpath "@executable_path/." "$bin" 2>/dev/null || true
            fi
        done
    fi

    echo "==> Signing daemon binaries..."
    for bin in bin/usbwifi bin/usbwifi-mcp bin/libusb-1.0.0.dylib; do
        if [[ -f "$bin" ]]; then
            codesign --force --options runtime --timestamp --sign "${SIGNING_IDENTITY}" "$bin"
            echo "    signed: $bin"
        fi
    done
}

# Sign the Swift app bundle (build the .app first via `task build` or
# scripts/package_app.sh).
sign_app() {
    echo "==> Building Swift app bundle..."
    ./scripts/package_app.sh
    local bundle="$(./scripts/package_app.sh 2>/dev/null | grep -oE 'build/[^ ]*\.app' | head -1)"
    if [[ -z "$bundle" || ! -d "$bundle" ]]; then
        echo "ERROR: no .app bundle produced; run scripts/package_app.sh first"
        exit 1
    fi

    echo "==> Clearing macOS Gatekeeper quarantine..."
    xattr -cr "$bundle" || true

    echo "==> Signing Swift app bundle..."
    # Sign nested binaries first, then the bundle.
    if [[ -d "$bundle/Contents/MacOS" ]]; then
        for exe in "$bundle"/Contents/MacOS/*; do
            if file "$exe" | grep -q "Mach-O"; then
                codesign --force --options runtime --entitlements Entitlements.plist \
                    --sign "${SIGNING_IDENTITY}" "$exe"
            fi
        done
    fi
    if [[ -d "$bundle/Contents/Frameworks" ]]; then
        for fw in "$bundle"/Contents/Frameworks/*.framework; do
            codesign --force --options runtime --sign "${SIGNING_IDENTITY}" "$fw"
        done
    fi
    codesign --force --deep --options runtime --entitlements Entitlements.plist \
        --sign "${SIGNING_IDENTITY}" "$bundle"
    echo "    signed: $bundle"
}

# Sign the DriverKit .dext bundle.
sign_driver() {
    if [[ ! -f DriverKit/AIC8800D80/build/AIC8800D80Driver.dext ]]; then
        echo "==> Building DriverKit driver (make)..."
        make -C DriverKit/AIC8800D80
    fi
    local dext="DriverKit/AIC8800D80/build/AIC8800D80Driver.dext"
    if [[ ! -d "$dext" ]]; then
        echo "ERROR: $dext not produced"
        exit 1
    fi

    echo "==> Signing DriverKit .dext..."
    # Sign nested executable first.
    codesign --force --options runtime \
        --entitlements DriverKit/AIC8800D80/AIC8800D80Driver.entitlements \
        --sign "${SIGNING_IDENTITY}" \
        "$dext/AIC8800D80Driver"
    # Then sign the bundle.
    codesign --force \
        --entitlements DriverKit/AIC8800D80/AIC8800D80Driver.entitlements \
        --sign "${SIGNING_IDENTITY}" \
        "$dext"
    echo "    signed: $dext"
    codesign --verify --deep --strict --verbose=2 "$dext" || true
}

echo "==> Signing with: ${SIGNING_IDENTITY}"
echo "    team: ${SIGNING_TEAM}"
echo

[[ "$SIGN_DAEMON" == "true" ]] && sign_daemon
[[ "$SIGN_APP" == "true" ]] && sign_app
[[ "$SIGN_DRIVER" == "true" ]] && sign_driver

echo
echo "=========================================="
echo " SIGNED:"
[[ "$SIGN_DAEMON" == "true" ]] && echo "  bin/usbwifi        (libusb daemon)"
[[ "$SIGN_DAEMON" == "true" ]] && echo "  bin/usbwifi-mcp    (MCP server)"
[[ "$SIGN_APP" == "true" ]] && echo "  Event Horizon.app  (Swift app bundle)"
[[ "$SIGN_DRIVER" == "true" ]] && echo "  AIC8800D80Driver.dext  (DriverKit driver)"
echo "=========================================="
echo
echo "Verify signatures:"
[[ "$SIGN_DAEMON" == "true" ]] && codesign --verify --deep --strict --verbose=1 bin/usbwifi bin/usbwifi-mcp
[[ "$SIGN_DRIVER" == "true" ]] && codesign --verify --deep --strict --verbose=1 DriverKit/AIC8800D80/build/AIC8800D80Driver.dext
