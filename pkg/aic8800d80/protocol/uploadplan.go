package protocol

import (
	"os"
	"strconv"
	"strings"
)

// Clone ROM quirks of the Pandora-clone AIC8800D80 (verified on
// hardware 2026-08-16..17 via --probe / --probe-write bisect):
//
//   - DBG_MEM_BLOCK_WRITE with 1024B blocks works for the whole fmacfw
//     range BELOW 0x170000 (chunks #0..#319, proven across many runs)
//   - 1024B blocks AT/ABOVE 0x170000 also WORK (run 2026-08-18 22:27):
//     the old "1KB wall" was really block1's halo — chunk #320 spans
//     block1. But the ROM NAKs hard in the register window (~460ms/op
//     vs 0.11ms below) and the verify read at 0x172830 wedged it
//   - 16B blocks at/above 0x170000 work and RETAIN (readback verified
//     0x1701f0..0x170fff); word writes retain too (0x170100)
//   - 0x1701e0..0x1701ff is a 32-byte register block: writing 0x1701e4
//     wedges the ROM outright; the rest accepts writes but never retains
//
// The upload plan therefore switches to small chunks at the wall and
// skips the register blocks. Verify reads are only safe at/above
// CloneVerifySafeAddr — reads in 0x1701d0..0x1701ff wedge the ROM
// (hardware-verified: read at 0x1701d0 timed out and killed the device
// while reads at 0x170000..0x1701c0 succeeded).
const (
	CloneWallAddr      uint32 = 0x170000 // ≥ this: 1KB block writes wedge
	CloneRegBlockStart uint32 = 0x170180 // no-write zone (256 B)
	CloneRegBlockEnd   uint32 = 0x170280
	CloneSmallChunk           = 16 // verified-safe small-block size ≥ wall
	// CloneVerifySafeAddr: verify reads allowed only at/above this.
	CloneVerifySafeAddr uint32 = 0x170280
)

// ChunkOp is one planned block write.
type ChunkOp struct {
	Addr  uint32
	Block []byte
}

// PlanAdaptiveUpload lays out the block writes for a blob:
//
//   - below wallAddr: BlockWriteChunkBytes (1KB) chunks
//   - at/above wallAddr: smallChunk-sized chunks (16B)
//   - the absolute ranges in zones are never written (ignored when empty)
//
// Pure function — fully unit-testable. The loader executes the ops and
// readback-verifies the ops at/above the wall.
func PlanAdaptiveUpload(addr uint32, blob []byte, wallAddr uint32, zones []SkipZone, smallChunk int) []ChunkOp {
	if smallChunk <= 0 {
		smallChunk = CloneSmallChunk
	}
	var ops []ChunkOp
	emit := func(a uint32, b []byte) {
		ops = append(ops, ChunkOp{Addr: a, Block: b})
	}
	// Walk the blob segment by segment. Each iteration emits one op,
	// choosing chunk size by the op's START address, and clips around
	// the skip ranges.
	off := 0
	for off < len(blob) {
		abs := addr + uint32(off)
		size := BlockWriteChunkBytes
		if abs >= wallAddr {
			size = smallChunk
		}
		end := off + size
		if end > len(blob) {
			end = len(blob)
		}
		absEnd := addr + uint32(end)
		// Find the first zone this op touches.
		var zone *SkipZone
		for i := range zones {
			if absEnd > zones[i].Start && abs < zones[i].End {
				zone = &zones[i]
				break
			}
		}
		if zone != nil {
			// This op would touch the skip zone.
			if abs >= zone.Start {
				// Op starts inside the zone — jump past it.
				off = int(zone.End - addr)
				continue
			}
			// Op starts before the zone — clip it to end at zone.Start,
			// emit, then jump past the zone.
			end = int(zone.Start - addr)
			emit(abs, blob[off:end])
			off = int(zone.End - addr)
			continue
		}
		emit(abs, blob[off:end])
		off = end
	}
	return ops
}

// SkipZone is an absolute address range that must never be touched.
type SkipZone struct {
	Start uint32
	End   uint32
}

// CloneRegZones returns the no-touch zones for the U02 fmacfw layout.
// Hardware-verified 2026-08-17: register blocks sit 0x2220 apart
// (0x1701e0, 0x172400, 0x174620, 0x176840). Each block is a 32-byte
// register window punched into SRAM, and the clone's ROM wedges when a
// 16B op lands within [block-0x20, block+0x20) of it — observed wedge
// points (write OR read, both directions):
//
//	block1 0x1701e0: read@0x1701d0 (B), write@0x170200 (A)
//	block2 0x172400: write@0x1723e0 (E), read@0x1723f0 (C), read@0x172420 (D)
//
// Each zone is therefore 80 bytes: [block-0x20, block+0x30) (one 16B op
// of margin past the outermost observed point).
func CloneRegZones() []SkipZone {
	zones := []SkipZone{
		{Start: 0x170180, End: 0x170280},
		{Start: 0x172380, End: 0x1724a0},
		{Start: 0x173000, End: 0x173400},
		{Start: 0x174580, End: 0x1746a0},
		{Start: 0x176780, End: 0x1768a0},
	}
	return zones
}

// MergePoisonZones extends a zone list with the small-write poison
// addresses discovered by --poison-map (appended to /tmp/aic-poisons.txt,
// one "0x..." per line). Each poison becomes a 4-byte no-touch zone.
// The file is only read when $AIC_POISONS is set, so existing behavior
// is unchanged unless the user opts in.
func MergePoisonZones(zones []SkipZone) []SkipZone {
	path := os.Getenv("AIC_POISONS")
	if path == "" {
		return zones
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return zones
	}
	merged := append([]SkipZone(nil), zones...)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "0x") {
			continue
		}
		addr, err := strconv.ParseUint(line[2:], 16, 32)
		if err != nil {
			continue
		}
		dup := false
		for _, z := range merged {
			if z.Start == uint32(addr) && z.End == uint32(addr)+4 {
				dup = true
				break
			}
		}
		if !dup {
			merged = append(merged, SkipZone{Start: uint32(addr), End: uint32(addr) + 4})
		}
	}
	return merged
}
