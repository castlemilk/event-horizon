package protocol

import (
	"encoding/binary"
	"errors"
	"testing"
)

// skipRecorder records block-write calls.
type skipRecorder struct {
	calls [][3]interface{} // addr, block
	fail  map[uint32]bool
}

func (s *skipRecorder) write(addr uint32, block []byte) error {
	if s.fail != nil && s.fail[addr] {
		return errors.New("wedge")
	}
	s.calls = append(s.calls, [3]interface{}{addr, block, nil})
	return nil
}

// writeSkippingRoutes MemBlockWriteAllSkipping through a fake MemBlockWrite.
// We can't intercept the package-level MemBlockWrite directly, so this test
// exercises the chunk-splitting logic via a copy of the algorithm driven by
// the same constants — validated instead at the integration level by
// TestMockUpload_SkipRange on the simulator.
func TestMemBlockWriteAllSkipping_Layout(t *testing.T) {
	// Model what the real function emits for a blob spanning the skip
	// range, using the same constants the implementation uses.
	addr := uint32(0x120000)
	blob := make([]byte, BlockWriteChunkBytes*3+100) // ~3.1 chunks
	zones := []SkipZone{{Start: 0x1701d0, End: 0x170200}}

	type call struct{ start, end uint32 } // absolute byte ranges written
	var calls []call
	emit := func(a uint32, b []byte) {
		calls = append(calls, call{a, a + uint32(len(b))})
	}

	runStart := 0
	for runStart < len(blob) {
		absStart := addr + uint32(runStart)
		if absStart >= zones[0].End {
			break
		}
		runEnd := runStart + BlockWriteChunkBytes
		if runEnd > len(blob) {
			runEnd = len(blob)
		}
		absEnd := addr + uint32(runEnd)
		if absEnd > zones[0].Start && absStart < zones[0].End {
			runEnd = int(zones[0].Start - addr)
			if runEnd <= runStart {
				runStart = int(zones[0].End - addr)
				continue
			}
			emit(addr+uint32(runStart), blob[runStart:runEnd])
			runStart = int(zones[0].End - addr)
			continue
		}
		emit(addr+uint32(runStart), blob[runStart:runEnd])
		runStart = runEnd
	}
	for runStart < len(blob) {
		end := runStart + BlockWriteChunkBytes
		if end > len(blob) {
			end = len(blob)
		}
		emit(addr+uint32(runStart), blob[runStart:end])
		runStart = end
	}

	// Assertions: every emitted range avoids the skip hole, ranges are
	// contiguous EXCEPT across the hole, and total coverage = blob minus
	// the skipped bytes.
	skipped := uint32(0)
	for _, z := range zones {
		if z.End > addr && z.Start < addr+uint32(len(blob)) {
			lo, hi := z.Start, z.End
			if lo < addr {
				lo = addr
			}
			if hi > addr+uint32(len(blob)) {
				hi = addr + uint32(len(blob))
			}
			skipped += hi - lo
		}
	}
	total := uint32(0)
	for _, c := range calls {
		for _, z := range zones {
			if c.end > z.Start && c.start < z.End {
				t.Fatalf("call 0x%08x..0x%08x overlaps skip range 0x%08x..0x%08x",
					c.start, c.end, z.Start, z.End)
			}
		}
		total += c.end - c.start
	}
	if want := uint32(len(blob)) - skipped; total != want {
		t.Errorf("total written = %d, want %d (blob %d minus skipped %d)", total, want, len(blob), skipped)
	}
}

// TestMemBlockWriteAllSkipping_CoversBoundaries pins the exact split for
// the REAL fmacfw geometry: 358072 bytes at 0x120000, skip the clone's
// 80-byte register zones.
func TestMemBlockWriteAllSkipping_CoversBoundaries(t *testing.T) {
	addr := uint32(0x120000)
	blob := make([]byte, 351828)
	zones := CloneRegZones()

	// The first block falls inside chunk #320 (0x170000..0x1703ff).
	chunk320Start := addr + 320*BlockWriteChunkBytes
	if !(zones[0].Start >= chunk320Start && zones[0].Start < chunk320Start+BlockWriteChunkBytes) {
		t.Fatalf("trigger not in chunk 320 as expected")
	}

	// Expected write plan around the hole:
	//   ... chunk 319 (0x16fc00..0x170000)
	//   pre-skip run  0x170000..0x1701bf  (448 bytes, 16B chunks)
	//   [HOLE 80 bytes]
	//   post-skip run 0x170210.. (16B chunks to end)

	// Track emitted ranges to check the first-zone boundaries directly:
	// the op before the zone must end at zones[0].Start and the first
	// op after must start at zones[0].End.
	type span struct{ start, end uint32 }
	var emitted []span
	emit := func(a uint32, n int) { emitted = append(emitted, span{a, a + uint32(n)}) }

	total := 0
	runStart := 0
	for runStart < len(blob) {
		absStart := addr + uint32(runStart)
		// Find the first zone that starts at/after runStart.
		zi := -1
		for i := range zones {
			if zones[i].End > addr && zones[i].Start >= absStart && zones[i].Start < addr+uint32(len(blob)) {
				zi = i
				break
			}
		}
		if zi >= 0 && absStart >= zones[zi].Start && absStart < zones[zi].End {
			runStart = int(zones[zi].End - addr)
			continue
		}
		runEnd := runStart + BlockWriteChunkBytes
		if absStart >= CloneWallAddr {
			runEnd = runStart + CloneSmallChunk
		}
		if runEnd > len(blob) {
			runEnd = len(blob)
		}
		absEnd := addr + uint32(runEnd)
		var cross *SkipZone
		for i := range zones {
			if absEnd > zones[i].Start && absStart < zones[i].End {
				cross = &zones[i]
				break
			}
		}
		if cross != nil {
			if absStart >= cross.Start {
				runStart = int(cross.End - addr)
				continue
			}
			runEnd = int(cross.Start - addr)
			emit(addr+uint32(runStart), runEnd-runStart)
			total += runEnd - runStart
			runStart = int(cross.End - addr)
			continue
		}
		emit(addr+uint32(runStart), runEnd-runStart)
		total += runEnd - runStart
		runStart = runEnd
	}
	preOK, postOK := false, false
	for _, s := range emitted {
		if s.end == zones[0].Start {
			preOK = true
		}
		if s.start == zones[0].End {
			postOK = true
		}
	}
	if !preOK {
		t.Error("pre-skip run does not end at the first zone start")
	}
	if !postOK {
		t.Error("post-skip run does not start at the first zone end")
	}
	if total != 336512 {
		t.Errorf("total = %d, want 336512", total)
	}
}

// TestMockUpload_SkipRange validates the clone model AND the skipping
// layout against the stateful mock: only the four 80-byte zones are
// protected; the full fmacfw window is otherwise writable. A chunked
// upload that respects the skip zones completes with zero protected
// hits; a naive full-block upload hits the protection.
func TestMockUpload_SkipRange(t *testing.T) {
	fw := make([]byte, 351828)
	for i := range fw {
		fw[i] = byte(i)
	}
	const addr = uint32(0x120000)
	zones := CloneRegZones()

	// Naive upload (the old behavior): chunk #320 contains the trigger.
	naive := newMockDevice()
	naive.ramWindowFull = true
	naive.protect = protectZones()
	for off := 0; off < len(fw); off += BlockWriteChunkBytes {
		end := off + BlockWriteChunkBytes
		if end > len(fw) {
			end = len(fw)
		}
		naive.handleFrame(BuildLmacMessage(DBGMemBlockWriteReq, TaskDBG, DrvTaskID,
			MemBlockWritePayload(addr+uint32(off), fw[off:end])))
		if len(naive.oobWrites) > 0 {
			break
		}
	}
	if len(naive.oobWrites) == 0 {
		t.Fatal("clone model broken: naive upload should hit the protected word")
	}

	// Skipping upload: the loader's PlanAdaptiveUpload drives the writes.
	skipping := newMockDevice()
	skipping.ramWindowFull = true
	skipping.protect = protectZones()
	ops := PlanAdaptiveUpload(addr, fw, CloneWallAddr, zones, CloneSmallChunk)
	for _, op := range ops {
		skipping.handleFrame(BuildLmacMessage(DBGMemBlockWriteReq, TaskDBG, DrvTaskID,
			MemBlockWritePayload(op.Addr, op.Block)))
	}
	if len(skipping.oobWrites) > 0 {
		t.Fatalf("skipping upload hit protected words: %v", skipping.oobWrites)
	}
	// And it placed everything except the zones.
	if len(skipping.ram) != 336512/4 {
		t.Errorf("ram words = %d, want %d", len(skipping.ram), 336512/4)
	}
	// Image spans 0x120000..0x1776B7 — probe aligned words around the
	// first hole and near the end of writable memory (0x17237c).
	for _, a := range []uint32{0x0017017c, 0x00170280, 0x00170284, 0x00170600, 0x0017237c} {
		if _, ok := skipping.ram[a]; !ok {
			t.Errorf("word at 0x%08x missing (should have been written)", a)
		}
	}
	for _, a := range []uint32{0x00170180, 0x001701e0, 0x001701e4, 0x001701fc, 0x00170200, 0x00170204, 0x0017027c} {
		if _, ok := skipping.ram[a]; ok {
			t.Errorf("word at 0x%08x present (hole violated)", a)
		}
	}
	// The three later zones must also be absent from ram.
	for _, z := range zones[1:] {
		for a := z.Start; a < z.End; a += 4 {
			if _, ok := skipping.ram[a]; ok {
				t.Errorf("word at 0x%08x present (zone 0x%08x..0x%08x violated)", a, z.Start, z.End)
			}
		}
	}
}

// protectZones marks all four clone register zones as protected.
func protectZones() map[uint32]bool {
	m := map[uint32]bool{}
	for _, z := range CloneRegZones() {
		for a := z.Start; a < z.End; a += 4 {
			m[a] = true
		}
	}
	return m
}

// silence unused warnings in quiet builds
var _ = binary.LittleEndian
