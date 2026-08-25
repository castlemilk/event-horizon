#!/bin/bash
# e2e.sh — End-to-end install: firmware + loader + DriverKit driver.
#
# Smart about skipping work that is already done:
#   - firmware fetch is skipped when the cached blobs verify OK
#   - the loader is skipped when the dongle is already operational
#   - the .dext build is incremental (make)
#   - signing is skipped when the existing signature verifies
#   - the /Library/SystemExtensions copy is skipped when identical
#
# Run with sudo:  sudo ./scripts/e2e.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

if [[ $EUID -ne 0 ]]; then
    echo "ERROR: this script requires sudo. Re-run with: sudo $0"
    exit 1
fi

FW_DIR="${SUDO_USER_HOME:-/Users/benebsworth}/.event-horizon/firmware"
if [[ -n "${SUDO_USER:-}" ]]; then
    FW_DIR="$(sudo -u "$SUDO_USER" echo "$HOME")/.event-horizon/firmware"
fi

# Upload mode plumbing. WINDOW selects the loader's register-window
# strategy (see loader.go); the loader reads AIC_* env vars directly.
WINDOW="${WINDOW:-skip}"
AIC_MODE=""
case "$WINDOW" in
    1KB)  AIC_MODE=AIC_WINDOW_1KB ;;
    word) AIC_MODE=AIC_WINDOW_WORD ;;
    full) AIC_MODE=AIC_FULL_WINDOW ;;
    skip) AIC_MODE="" ;;
    *)
        echo "ERROR: unknown WINDOW mode '$WINDOW' (1KB|word|full|skip)"
        exit 1
        ;;
esac
AIC_NO_VERIFY="${AIC_NO_VERIFY:-0}"
echo "==> Upload window mode: ${WINDOW}${AIC_MODE:+" ($AIC_MODE=1)"}${AIC_NO_VERIFY:+ no-verify}"
SIGNING_IDENTITY="Developer ID Application: Ben Ebsworth (WFTX6CN23F)"
DEXT_DIR="DriverKit/AIC8800D80/build/AIC8800D80Driver.dext"
DRIVER_BUNDLE_ID="com.eventhorizon.driver.AIC8800D80"

echo "==> [1/7] Firmware blobs ($FW_DIR)"
NEED_FETCH=0
if ! ./bin/usbwifi firmware verify --target=aic8800D80 --in="$FW_DIR" >/dev/null 2>&1; then
    NEED_FETCH=1
fi
# The patch info table requires supplementary ext-patch blobs — fetch if
# none are present.
if ! ls "$FW_DIR"/*_ext*.bin >/dev/null 2>&1; then
    NEED_FETCH=1
fi
if [[ $NEED_FETCH -eq 0 ]]; then
    echo "    cached blobs verified OK — skipping fetch"
else
    echo "    fetching from github.com/radxa-pkg/aic8800..."
    ./bin/usbwifi firmware fetch --target=aic8800D80 --out="$FW_DIR"
    ./bin/usbwifi firmware verify --target=aic8800D80 --in="$FW_DIR" || true
fi
# Keep the cache user-owned so non-sudo runs can refresh it later.
if [[ -n "${SUDO_USER:-}" ]]; then
    chown -R "$SUDO_USER":staff "$(dirname "$FW_DIR")" 2>/dev/null || true
fi

echo "==> [2/7] Detecting dongle stage"
STAGE_OUT="$(./bin/usbwifi aicloader --status 2>/dev/null || true)"
echo "    $STAGE_OUT"

echo "==> [3/7] Stopping daemon AND the Swift app"
# The EventHorizonApp's RuntimeSupervisor RESPAWNS the usbwifi daemon,
# and the daemon opens every USB device every 3s (topology poll) — that
# interferes with the loader's session on the dongle and explains the
# intermittent silent-probe failures. The starlink-sdk usbwifi-mcp MCP
# server polls the same way. All must be dead for the run.
pkill -9 -x EventHorizonApp 2>/dev/null && echo "    killed EventHorizonApp" || true
pkill -9 -x usbwifi 2>/dev/null && echo "    killed usbwifi" || true
pkill -9 -x usbwifi-mcp 2>/dev/null && echo "    killed usbwifi-mcp" || true
sleep 1
# Verify none resurrected (the app can take a moment to die).
for i in 1 2 3; do
    if pgrep -x usbwifi >/dev/null 2>&1 || pgrep -x usbwifi-mcp >/dev/null 2>&1; then
        echo "    usbwifi respawned — killing again"
        pkill -9 -x usbwifi 2>/dev/null || true
        pkill -9 -x usbwifi-mcp 2>/dev/null || true
        sleep 1
    fi
done
if pgrep -fl 'usbwifi|EventHorizonApp' >/dev/null 2>&1; then
    echo "    WARNING: processes still alive:"
    pgrep -fl 'usbwifi|EventHorizonApp'
fi

echo "==> [4/7] Firmware loader (ZeroCD/BootROM -> Operational)"
AIC_LOG=/tmp/aic-e2e.log
if echo "$STAGE_OUT" | grep -q "Operational"; then
    echo "    dongle already operational — skipping loader"
else
    # Retry loop: a wedged dongle recycles itself via watchdog (to
    # ZeroCD) within a few minutes; the loader handles mode-switch.
    # A firmware that crashed back to BootROM does NOT watchdog-recycle
    # and the leftover BootROM is DBG-silent — retry immediately (cheap)
    # rather than burning 20 min waiting for a ZeroCD that never comes.
    LOADER_OK=0
    for attempt in 1 2 3; do
        echo "    ── loader attempt $attempt/3 ──"
        set +e
        env ${AIC_MODE:+$AIC_MODE=1} ${AIC_NO_VERIFY:+AIC_NO_VERIFY=1} \
        ${AIC_ZONE_END2:+AIC_ZONE_END2=$AIC_ZONE_END2} \
        ${AIC_ZONE_END3:+AIC_ZONE_END3=$AIC_ZONE_END3} \
        ${AIC_ZONE_END4:+AIC_ZONE_END4=$AIC_ZONE_END4} \
        ./bin/usbwifi aicloader --firmware-dir="$FW_DIR" 2>&1 | tee "$AIC_LOG" | tail -60
        LOADER_RC=${PIPESTATUS[0]}
        set -e
        if [[ $LOADER_RC -eq 0 ]]; then
            LOADER_OK=1
            break
        fi
        echo "    attempt $attempt failed — key lines:"
        grep -E 'FAILED|SUCCESS|WEDGE|UNEXPECTED|wedge|re-enumerat|hole' "$AIC_LOG" | tail -10 || true
        if [[ $attempt -lt 3 ]]; then
            if grep -q 'crashed back to boot ROM' "$AIC_LOG"; then
                echo "    firmware crashed back to BootROM — retrying immediately (this state does not watchdog-recycle)"
                continue
            fi
            echo "    waiting for the dongle watchdog to recycle to ZeroCD (observed ~18 min; polling up to 20 min)..."
            waited=0
            while [[ $waited -lt 1200 ]]; do
                # Only ZeroCD proves a true recycle — a WEDGED BootROM is
                # also "available" and probing it wastes an attempt.
                STATUS="$(./bin/usbwifi aicloader --status 2>/dev/null || true)"
                if echo "$STATUS" | grep -q "ZeroCD"; then
                    echo "    watchdog recycled the device (ZeroCD) — retrying"
                    break
                fi
                sleep 5
                waited=$((waited+5))
            done
            if [[ $waited -ge 1200 ]]; then
                echo "    watchdog did not recycle within 20 min — replug the dongle and rerun"
            fi
        fi
    done
    if [[ $LOADER_OK -ne 1 ]]; then
        echo
        echo "    FULL loader log preserved at: $AIC_LOG"
        exit 1
    fi
fi

echo "==> [5/7] Building DriverKit driver (incremental)"
make -C DriverKit/AIC8800D80 >/dev/null

echo "==> [6/7] Signing .dext"
if codesign --verify --strict "$DEXT_DIR" >/dev/null 2>&1; then
    echo "    existing signature valid — skipping re-sign"
else
    codesign --force --options runtime \
        --entitlements DriverKit/AIC8800D80/AIC8800D80Driver.entitlements \
        --sign "$SIGNING_IDENTITY" \
        "$DEXT_DIR/AIC8800D80Driver"
    codesign --force \
        --entitlements DriverKit/AIC8800D80/AIC8800D80Driver.entitlements \
        --sign "$SIGNING_IDENTITY" \
        "$DEXT_DIR"
fi

echo "==> [7/7] Installing system extension"
if [[ -d "/Library/SystemExtensions/AIC8800D80Driver.dext" ]] && \
   diff -r "$DEXT_DIR" /Library/SystemExtensions/AIC8800D80Driver.dext >/dev/null 2>&1; then
    echo "    installed copy identical — skipping copy"
else
    mkdir -p /Library/SystemExtensions
    rm -rf /Library/SystemExtensions/AIC8800D80Driver.dext
    cp -R "$DEXT_DIR" /Library/SystemExtensions/
    systemextensionsctl developer on 2>/dev/null || true
    systemextensionsctl reset 2>/dev/null || true
    systemextensionsctl load "$DRIVER_BUNDLE_ID" || \
        echo "    NOTE: systemextensionsctl load failed — on modern macOS a dext must be"
    echo "    activated by a host app (OSSystemExtensionRequest). Falling back to developer mode."
fi

echo
echo "==> Verification"
sleep 2
echo "--- systemextensionsctl ---"
systemextensionsctl list 2>/dev/null | grep -i aic || echo "    (not listed)"
echo "--- USB device state ---"
ioreg -p IOUSB -l 2>/dev/null | grep -B 1 -A 3 'AIC\|a69c' | grep -E 'USB Product Name|idProduct|idVendor|AIC' | head -8 || true
echo "--- enX interfaces ---"
ifconfig -l | tr ' ' '\n' | grep '^en' | tr '\n' ' '; echo
echo
echo "Done. If the dongle shows idProduct 36225 (0x8d81), the driver's match"
echo "dictionary will bind on the next replug."
