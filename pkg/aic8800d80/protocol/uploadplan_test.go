package protocol

import "testing"

// TestPlanAdaptiveUpload_RealGeometry pins the exact op layout for the
// live fmacfw upload: 358072 bytes at 0x120000, wall 0x170000, four
// 80-byte register zones at 0x2220 stride, small chunks 16B.
func TestPlanAdaptiveUpload_RealGeometry(t *testing.T) {
	addr := uint32(0x120000)
	blob := make([]byte, 358072)
	zones := CloneRegZones()
	ops := PlanAdaptiveUpload(addr, blob, CloneWallAddr, zones, CloneSmallChunk)

	// Classification by address.
	var bigOps, preWallSmall, postSkipSmall, tailSmall int
	var total int
	for _, op := range ops {
		// No op may touch any register zone.
		for _, z := range zones {
			if op.Addr < z.End && op.Addr+uint32(len(op.Block)) > z.Start {
				t.Fatalf("op 0x%08x+%d overlaps zone 0x%08x..0x%08x", op.Addr, len(op.Block), z.Start, z.End)
			}
		}
		switch {
		case op.Addr < CloneWallAddr:
			if len(op.Block) != BlockWriteChunkBytes {
				t.Fatalf("op below wall has size %d, want %d", len(op.Block), BlockWriteChunkBytes)
			}
			bigOps++
		case op.Addr < CloneRegBlockStart:
			if len(op.Block) != CloneSmallChunk {
				t.Fatalf("op in pre-skip small region has size %d", len(op.Block))
			}
			preWallSmall++
		case op.Addr == CloneRegBlockEnd:
			// First op after the first skip must start exactly at RegBlockEnd.
			postSkipSmall++
		default:
			if len(op.Block) > CloneSmallChunk {
				t.Fatalf("op at/above wall has size %d, want ≤%d", len(op.Block), CloneSmallChunk)
			}
			tailSmall++
		}
		total += len(op.Block)
	}

	// 320 × 1KB below the wall (0x120000..0x16ffff).
	if bigOps != 320 {
		t.Errorf("1KB ops = %d, want 320", bigOps)
	}
	// 0x170000..0x17017f = 384 bytes = 24×16.
	if preWallSmall != 24 {
		t.Errorf("pre-skip small ops = %d, want 24 (384 bytes)", preWallSmall)
	}
	if total != 348472 {
		t.Errorf("total bytes = %d, want 348472", total)
	}
}

// TestPlanAdaptiveUpload_Boundaries verifies the critical boundary ops.
func TestPlanAdaptiveUpload_Boundaries(t *testing.T) {
	addr := uint32(0x120000)
	blob := make([]byte, 358072)
	zones := CloneRegZones()
	ops := PlanAdaptiveUpload(addr, blob, CloneWallAddr, zones, CloneSmallChunk)

	var lastBeforeWall, lastPreSkip, firstPostSkip, last *ChunkOp
	for i := range ops {
		op := &ops[i]
		switch {
		case op.Addr+uint32(len(op.Block)) == CloneWallAddr:
			lastBeforeWall = op
		case op.Addr+uint32(len(op.Block)) == CloneRegBlockStart:
			lastPreSkip = op
		case op.Addr == CloneRegBlockEnd && firstPostSkip == nil:
			firstPostSkip = op
		}
		last = op
	}
	if lastBeforeWall == nil || lastBeforeWall.Addr != CloneWallAddr-BlockWriteChunkBytes {
		t.Errorf("last 1KB op should end at the wall, got %+v", lastBeforeWall)
	}
	if lastPreSkip == nil || lastPreSkip.Addr != CloneRegBlockStart-CloneSmallChunk || len(lastPreSkip.Block) != CloneSmallChunk {
		t.Errorf("last pre-skip op should end at 0x%08x, got 0x%08x+%d", CloneRegBlockStart, lastPreSkip.Addr, len(lastPreSkip.Block))
	}
	if firstPostSkip == nil || firstPostSkip.Addr != CloneRegBlockEnd {
		t.Errorf("first post-skip op should start at 0x%08x, got %+v", CloneRegBlockEnd, firstPostSkip)
	}
	// Image ends at 0x120000+358072 = 0x1776b8.
	wantLast := addr + uint32(len(blob))
	if last.Addr+uint32(len(last.Block)) != wantLast {
		t.Errorf("last op ends at 0x%08x, want 0x%08x", last.Addr+uint32(len(last.Block)), wantLast)
	}
}

// TestPlanAdaptiveUpload_AboveWall1KB verifies the AIC_WINDOW_1KB
// geometry: 1KB chunks above the wall, clipped around the halo zones.
// This is the run-I hypothesis — the "1KB wall" may be block1's halo.
func TestPlanAdaptiveUpload_AboveWall1KB(t *testing.T) {
	addr := uint32(0x120000)
	blob := make([]byte, 358072)
	zones := CloneRegZones()
	ops := PlanAdaptiveUpload(addr, blob, CloneWallAddr, zones, BlockWriteChunkBytes)

	var clipped []int
	total := 0
	for i, op := range ops {
		for _, z := range zones {
			if op.Addr < z.End && op.Addr+uint32(len(op.Block)) > z.Start {
				t.Fatalf("op 0x%08x+%d overlaps zone 0x%08x..0x%08x", op.Addr, len(op.Block), z.Start, z.End)
			}
		}
		if op.Addr >= CloneWallAddr && len(op.Block) != BlockWriteChunkBytes {
			clipped = append(clipped, i)
		}
		if len(op.Block) > BlockWriteChunkBytes {
			t.Fatalf("op size %d exceeds 1KB", len(op.Block))
		}
		total += len(op.Block)
	}
	if total != 348472 {
		t.Errorf("total = %d, want 348472", total)
	}
	firstAbove := ops[320]
	if firstAbove.Addr != CloneWallAddr || len(firstAbove.Block) != 384 {
		t.Errorf("first above-wall op = 0x%08x+%d, want 0x%08x+384", firstAbove.Addr, len(firstAbove.Block), CloneWallAddr)
	}
	last := ops[len(ops)-1]
	if last.Addr+uint32(len(last.Block)) != addr+uint32(len(blob)) {
		t.Errorf("last op ends at 0x%08x, want 0x%08x", last.Addr+uint32(len(last.Block)), addr+uint32(len(blob)))
	}
}

// TestPlanAdaptiveUpload_NoWall verifies the U01 path: wall beyond the
// blob end degenerates to plain 1KB chunking.
func TestPlanAdaptiveUpload_NoWall(t *testing.T) {
	addr := uint32(0x100000)
	blob := make([]byte, 358072)
	ops := PlanAdaptiveUpload(addr, blob, 0xFFFFFFFF, nil, CloneSmallChunk)
	if len(ops) != 350 { // 358072/1024 = 349.68 → 350 ops
		t.Errorf("ops = %d, want 350", len(ops))
	}
	for _, op := range ops {
		if len(op.Block) > BlockWriteChunkBytes {
			t.Fatalf("op size %d exceeds 1KB", len(op.Block))
		}
	}
}

// TestPlanAdaptiveUpload_SkipAlignedInsideChunk covers the clipping
// path: a skip range not aligned to chunk boundaries.
func TestPlanAdaptiveUpload_SkipAlignedInsideChunk(t *testing.T) {
	addr := uint32(0x120000)
	blob := make([]byte, 2000)
	// Skip in the middle of chunk #1 (0x120400..0x1207ff).
	zones := []SkipZone{{Start: 0x120410, End: 0x120420}}
	ops := PlanAdaptiveUpload(addr, blob, 0xFFFFFFFF, zones, 16)
	total := 0
	for _, op := range ops {
		if op.Addr < 0x120420 && op.Addr+uint32(len(op.Block)) > 0x120410 {
			t.Fatalf("op overlaps skip: 0x%08x+%d", op.Addr, len(op.Block))
		}
		total += len(op.Block)
	}
	if total != 2000-16 {
		t.Errorf("total = %d, want %d", total, 2000-16)
	}
}

// TestPlanAdaptiveUpload_MultiZone verifies the multi-zone clipping path
// with three adjacent zones.
func TestPlanAdaptiveUpload_MultiZone(t *testing.T) {
	addr := uint32(0x120000)
	blob := make([]byte, 4096)
	zones := []SkipZone{
		{Start: 0x120100, End: 0x120130},
		{Start: 0x120500, End: 0x120520},
		{Start: 0x120900, End: 0x1209C0},
	}
	ops := PlanAdaptiveUpload(addr, blob, 0xFFFFFFFF, zones, 16)
	total := 0
	for _, op := range ops {
		for _, z := range zones {
			if op.Addr < z.End && op.Addr+uint32(len(op.Block)) > z.Start {
				t.Fatalf("op overlaps zone: 0x%08x+%d vs 0x%08x..0x%08x", op.Addr, len(op.Block), z.Start, z.End)
			}
		}
		total += len(op.Block)
	}
	want := 4096 - 0x30 - 0x20 - 0xC0
	if total != want {
		t.Errorf("total = %d, want %d", total, want)
	}
}
