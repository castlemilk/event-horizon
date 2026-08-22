package aic8800d80

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// realPatchTablePath points at the actual U02 patch-table blob fetched
// from radxa-pkg/aic8800 (cached by `firmware fetch`). Skips if absent.
func realPatchTablePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".event-horizon", "firmware", "fw_patch_table_8800d80_u02.bin")
}

// writeRecorder captures every MemWrite the loader would issue.
type writeRecorder struct {
	writes [][2]uint32
	fail   map[uint32]bool // addresses that should simulate a wedge
}

func (w *writeRecorder) write(addr, val uint32) error {
	if w.fail != nil && w.fail[addr] {
		return errSimWedge
	}
	w.writes = append(w.writes, [2]uint32{addr, val})
	return nil
}

var errSimWedge = &simWedgeError{}

type simWedgeError struct{}

func (*simWedgeError) Error() string { return "simulated device wedge" }

// TestApplyPatchTables_NeverWritesAddressZero is the regression test for
// the 2026-08-16 hardware wedge: applying the REAL patch table wrote its
// metadata pairs, including a write of the ext0 address (0x20b43c) TO
// ADDRESS 0x00000000. On hardware this corrupted the boot ROM's memory
// map: the fmacfw window truncated at 0x170000, the device answered
// error frame 0xf105 and halted. Linux avoids this by truncating the INF
// table to its first 4 pairs (aicbt_patch_info_unpack sets
// head_t->len = info_len).
func TestApplyPatchTables_NeverWritesAddressZero(t *testing.T) {
	blob, err := os.ReadFile(realPatchTablePath())
	if err != nil {
		t.Skipf("real patch table not present (%v) — run `./bin/usbwifi firmware fetch` to enable", err)
	}
	tables, err := protocol.ParsePatchTable(blob)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rec := &writeRecorder{}
	if err := applyPatchTablesTo(rec.write, tables); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, w := range rec.writes {
		if w[0] == 0 {
			t.Fatalf("REGRESSION: write to address 0 (value 0x%08x) — this is the exact bug that wedged the dongle at 0x170000", w[1])
		}
		if w[0] < 0x1000 {
			t.Errorf("suspicious low address write 0x%08x = 0x%08x", w[0], w[1])
		}
	}
}

// TestApplyPatchTables_INFTruncatedToFourPairs verifies against the real
// blob that the INF table contributes exactly 4 writes (the base_len
// pairs), not 6.
func TestApplyPatchTables_INFTruncatedToFourPairs(t *testing.T) {
	blob, err := os.ReadFile(realPatchTablePath())
	if err != nil {
		t.Skipf("real patch table not present (%v)", err)
	}
	tables, err := protocol.ParsePatchTable(blob)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var infWrites int
	rec := &writeRecorder{}
	recFail := rec
	_ = recFail
	// Instrument: count writes while applying only the INF table.
	infOnly := tables[:0]
	for _, tb := range tables {
		if tb.Type == protocol.AICBTPTInf {
			infOnly = append(infOnly, tb)
		}
	}
	rec2 := &writeRecorder{}
	if err := applyPatchTablesTo(func(addr, val uint32) error {
		infWrites++
		return rec2.write(addr, val)
	}, infOnly); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if infWrites != 4 {
		t.Errorf("INF table produced %d writes, want 4 (Linux truncates to base_len)", infWrites)
	}
}

// TestApplyPatchTables_PairsWrittenWithRealBlob is a smoke test that all
// non-INF tables' pairs ARE written in full (the truncation must only
// affect INF).
func TestApplyPatchTables_PairsWrittenWithRealBlob(t *testing.T) {
	blob, err := os.ReadFile(realPatchTablePath())
	if err != nil {
		t.Skipf("real patch table not present (%v)", err)
	}
	tables, err := protocol.ParsePatchTable(blob)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Count expected: TRAP(27) + B4(57) + BTMODE(18) + PWRON(3) + AF(31)
	// + INF(4 after truncation). VER_INFO (type 6) contributes 0.
	expected := 0
	for _, tb := range tables {
		switch tb.Type {
		case protocol.AICBTPTInf:
			n := int(tb.Len)
			if n > 4 {
				n = 4
			}
			expected += n
		case 0x06:
			// version string — skipped
		default:
			expected += int(tb.Len)
		}
	}
	rec := &writeRecorder{}
	if err := applyPatchTablesTo(rec.write, tables); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(rec.writes) != expected {
		t.Errorf("writes = %d, want %d", len(rec.writes), expected)
	}
}

// TestApplyPatchTables_BTModeOverrides verifies the D80 BT-mode defaults
// land in the written values (btmode=5, txpwr=0x6F2F).
func TestApplyPatchTables_BTModeOverrides(t *testing.T) {
	// Synthetic BTMODE table with 18 pairs.
	tb := protocol.PatchTable{
		Name: "AICBT_MODE_T",
		Type: protocol.AICBTPTBTMode,
		Len:  18,
		Data: make([]uint32, 36),
	}
	rec := &writeRecorder{}
	if err := applyPatchTablesTo(rec.write, []protocol.PatchTable{tb}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(rec.writes) != 18 {
		t.Fatalf("writes = %d, want 18", len(rec.writes))
	}
	// pair index 3 (Data[6],Data[7]) = btmode → 5
	if rec.writes[3][1] != 5 {
		t.Errorf("btmode = %d, want 5 (BT_ONLY_COANT)", rec.writes[3][1])
	}
	// pair 8 (Data[16],Data[17]) = txpwr → 0x6F2F
	if rec.writes[8][1] != 0x00006F2F {
		t.Errorf("txpwr = 0x%08x, want 0x00006F2F", rec.writes[8][1])
	}
	// pair 5 baud → 1500000
	if rec.writes[5][1] != 1500000 {
		t.Errorf("baud = %d, want 1500000", rec.writes[5][1])
	}
}

// TestUnpackPatchInfo_RealBlob validates the addresses extracted from
// the real blob match what the loader must use.
func TestUnpackPatchInfo_RealBlob(t *testing.T) {
	blob, err := os.ReadFile(realPatchTablePath())
	if err != nil {
		t.Skipf("real patch table not present (%v)", err)
	}
	tables, err := protocol.ParsePatchTable(blob)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pi, err := protocol.UnpackPatchInfo(tables)
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if pi.AddrAdid != 0x00201940 {
		t.Errorf("addr_adid = 0x%08x, want 0x00201940", pi.AddrAdid)
	}
	if pi.AddrPatch != 0x001e0000 {
		t.Errorf("addr_patch = 0x%08x, want 0x001e0000", pi.AddrPatch)
	}
	if pi.ExtPatchNb != 1 || len(pi.ExtPatchAddr) != 1 {
		t.Fatalf("ext: nb=%d addrs=%v", pi.ExtPatchNb, pi.ExtPatchAddr)
	}
	if pi.ExtPatchAddr[0] != 0x0020b43c {
		t.Errorf("ext0 addr = 0x%08x, want 0x0020b43c", pi.ExtPatchAddr[0])
	}
}

// TestRealBlob_INFMetadataPairsDocumentsHazard documents exactly which
// pairs are metadata and would corrupt the device if written.
func TestRealBlob_INFMetadataPairsDocumentsHazard(t *testing.T) {
	blob, err := os.ReadFile(realPatchTablePath())
	if err != nil {
		t.Skipf("real patch table not present (%v)", err)
	}
	tables, err := protocol.ParsePatchTable(blob)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var inf *protocol.PatchTable
	for i := range tables {
		if tables[i].Type == protocol.AICBTPTInf {
			inf = &tables[i]
			break
		}
	}
	if inf == nil || len(inf.Data) < 12 {
		t.Fatalf("INF table malformed")
	}
	// Pair 5 = (ext id, ext addr) — its ADDRESS field is 0.
	pair5Addr := inf.Data[10]
	pair5Val := inf.Data[11]
	if pair5Addr != 0 {
		t.Errorf("expected pair 5 address 0 (metadata), got 0x%08x", pair5Addr)
	}
	_ = pair5Val
}

// guard: keep binary.LittleEndian referenced (used if fixtures grow).
var _ = binary.LittleEndian
var _ = strings.Contains
