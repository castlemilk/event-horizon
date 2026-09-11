#!/bin/bash
# aic-zerocd-capture.sh — copy the dongle's built-in ZeroCD driver volume.
#
# The UGREEN AX900 ships its Windows driver ON the dongle: in ZeroCD mode
# (a69c:5723) it presents a 3.7MB FAT16 "UGREEN" volume holding the driver
# package — and therefore the firmware images the Windows driver actually
# uploads to THIS chip. That is the ground truth we could never get from the
# Linux reference. This script waits for the volume, mounts it if macOS did
# not, and copies everything (with a raw image of the partition as well) to
# ~/.event-horizon/zerocd-capture/. It does NOT eject.
set -u

OUT="${1:-$HOME/.event-horizon/zerocd-capture}"
mkdir -p "$OUT"

echo "[capture] waiting for the ZeroCD volume (a69c:5723)..."
for _ in $(seq 1 60); do
  ioreg -p IOUSB -l 2>/dev/null | grep -q '"idProduct" = 22307' && break
  sleep 1
done
ioreg -p IOUSB -l 2>/dev/null | grep -q '"idProduct" = 22307' || { echo "[capture] no ZeroCD device seen in 60s"; exit 1; }
echo "[capture] MSC device present. Locating its disk (3784704-byte media)..."

disk=""
for _ in $(seq 1 30); do
  for d in $(diskutil list | awk '/^\/dev\/disk/{print $1}'); do
    if diskutil info "$d" 2>/dev/null | grep -q "3784704"; then disk="$d"; break 2; fi
  done
  sleep 1
done
[ -z "$disk" ] && { echo "[capture] disk never attached"; diskutil list; exit 1; }
echo "[capture] disk is $disk"

# Raw image of the whole device (needs root; best-effort, never prompts).
if sudo -n true 2>/dev/null; then
  echo "[capture] imaging raw device to $OUT/zerocd.img ..."
  sudo -n dd if="$disk" of="$OUT/zerocd.img" bs=64k 2>/dev/null && ls -l "$OUT/zerocd.img"
else
  echo "[capture] (skipping raw image — no passwordless root; file copy needs none)"
fi

# Mount (macOS usually auto-mounts as /Volumes/UGREEN).
part=$(diskutil list "$disk" | awk '/FAT|DOS|Windows/{print $NF}' | head -1)
[ -n "$part" ] && diskutil mount "/dev/$part" >/dev/null 2>&1
mp=$(diskutil info "/dev/${part:-$disk}" 2>/dev/null | awk -F': *' '/Mount Point/{print $2}')
if [ -z "$mp" ] || [ ! -d "$mp" ]; then
  mp=$(ls -d /Volumes/UGREEN* 2>/dev/null | head -1)
fi
if [ -n "$mp" ] && [ -d "$mp" ]; then
  echo "[capture] mounted at $mp — copying files..."
  mkdir -p "$OUT/files"
  cp -R "$mp"/. "$OUT/files"/ 2>/dev/null
  echo "[capture] contents:"
  find "$OUT/files" -type f -exec ls -l {} \; | awk '{print $5, $9}' | sort -k2
else
  echo "[capture] could not mount — raw image still captured; extract with: 7z x $OUT/zerocd.img"
fi
echo "[capture] done. Volume left mounted (NOT ejected)."
