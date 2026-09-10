#!/bin/bash
# aic-zerocd-eject.sh — recover the AIC8800D80 via a COMMANDED ZeroCD eject.
#
# Why this exists: libusb cannot win the bulk pipes from macOS's mass-storage
# driver while the dongle is in ZeroCD MSC mode (a69c:5723), so our libusb
# SCSI eject never reaches the chip. Left alone, the clone auto-switches to
# the WLAN PID (a69c:8d80) with a DEAD boot-ROM command processor — every
# bulk OUT then times out. This script instead lets the kernel's own MSC
# driver deliver the eject (diskutil eject == SCSI START STOP UNIT, LoEj=1),
# which is the modeswitch the chip expects, then runs the firmware loader
# the moment the live boot ROM appears.
#
# Usage: sudo scripts/aic-zerocd-eject.sh [firmware-dir] [fw-name]
#        When prompted, unplug the DONGLE (or its adapter) for ~10s, replug.
#        A crashed START_APP leaves a dead 8d80 that only a VBUS drop clears,
#        so every firmware boot experiment starts with a replug through here.
set -u

# Locate the loader binary. USBWIFI_BIN wins when set — "usbwifi cmdctl link"
# sets it to its own path, which is the only way this works inside the app
# bundle: there the script sits in Contents/Resources, so the dev layout
# (../bin/usbwifi) would resolve to Contents/bin/usbwifi and not exist.
HERE="$(cd "$(dirname "$0")" && pwd)"
if [ -n "${USBWIFI_BIN:-}" ] && [ -x "${USBWIFI_BIN}" ]; then
  BIN="${USBWIFI_BIN}"
elif [ -x "${HERE}/../bin/usbwifi" ]; then
  BIN="${HERE}/../bin/usbwifi"   # dev checkout
elif [ -x "${HERE}/usbwifi" ]; then
  BIN="${HERE}/usbwifi"          # bundled beside this script
else
  echo "[recover] cannot find the usbwifi binary (set USBWIFI_BIN)" >&2
  exit 1
fi
FWDIR="${1:-$HOME/.event-horizon/firmware/aic8800D80}"
FWNAME="${2:-}"
# Third arg "full" enables the no-zone-skip full-image write (genuine-RAM test).
[ "${3:-}" = "full" ] && export AIC_FULL=1 && echo "[recover] AIC_FULL=1 (no zone skip)"
# When run under sudo, $HOME is root's; prefer the invoking user's firmware dir.
if [ -n "${SUDO_USER:-}" ] && [ ! -d "$FWDIR" ]; then
  FWDIR="/Users/$SUDO_USER/.event-horizon/firmware/aic8800D80"
fi

MSC_PID=22307   # 0x5723  ZeroCD mass-storage mode
ROM_PID=36224   # 0x8d80  boot ROM
AIC_VID=42652   # 0xa69c

have_pid() {  # have_pid <idProduct-decimal>
  ioreg -p IOUSB -l 2>/dev/null | grep -q "\"idProduct\" = $1"
}

echo "[recover] making sure nothing can grab the dongle mid-flow..."
pkill -9 -f "Event Horizon.app" 2>/dev/null
pkill -9 -f "EventHorizonApp"   2>/dev/null
pkill -9 -f "usbwifi --port"    2>/dev/null
pkill -9 -f "usbwifi-mcp"       2>/dev/null

if have_pid $ROM_PID; then
  echo "[recover] dongle is currently in boot-ROM mode (a69c:8d80) — but if you're"
  echo "[recover] running this, that ROM is presumed dead (auto-switched, not ejected)."
fi

echo "[recover] >>> UNPLUG THE DONGLE NOW, count to ten, plug it back in <<<"
echo "[recover] waiting (no timeout) for the dongle in MSC mode (a69c:5723)..."
until have_pid $MSC_PID; do
  sleep 1
done
echo "[recover] MSC mode detected. Waiting for macOS to attach the disk..."

# The ZeroCD volume identifies as media name "flash", FAT16 volume "UGREEN",
# exactly 3784704 bytes — match on the size, the only stable discriminator.
disk=""
for _ in $(seq 1 30); do
  for d in $(diskutil list | awk '/^\/dev\/disk/{print $1}'); do
    if diskutil info "$d" 2>/dev/null | grep -q "3784704"; then
      disk="$d"; break 2
    fi
  done
  sleep 1
done

if [ -z "$disk" ]; then
  echo "[recover] no disk attached for the MSC device after 30s."
  echo "[recover] diskutil list follows (evidence — which disk is the Aic MSC?):"
  diskutil list
  echo "[recover] falling back: waiting to see if the chip auto-switches anyway..."
else
  echo "[recover] Aic MSC is $disk — sending the commanded eject via the kernel MSC driver"
  diskutil eject "$disk" || {
    echo "[recover] eject refused; trying unmountDisk then eject once more"
    diskutil unmountDisk "$disk" 2>/dev/null
    diskutil eject "$disk"
  }
fi

echo "[recover] waiting up to 30s for boot ROM re-enumeration (a69c:8d80)..."
found=""
for _ in $(seq 1 30); do
  if have_pid $ROM_PID; then found=1; break; fi
  sleep 1
done
if [ -z "$found" ]; then
  echo "[recover] FAIL: chip did not re-enumerate as 8d80 after the eject."
  ioreg -p IOUSB -l | grep -B2 -A4 '"idVendor" = 42652'
  exit 1
fi

echo "[recover] boot ROM is up — settling 2s, then uploading firmware"
sleep 2
if [ -n "$FWNAME" ]; then
  exec "$BIN" aicloader --kill-daemon --firmware-dir "$FWDIR" --fw-name "$FWNAME"
fi
exec "$BIN" aicloader --kill-daemon --firmware-dir "$FWDIR"
