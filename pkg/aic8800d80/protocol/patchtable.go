package protocol

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// AICBT patch-table entry types (aicbluetooth_cmds.h).
const (
	AICBTPTTag    = "AICBT_PT_TAG" // 16-byte file header tag
	AICBTPTInf    = 0x00           // patch info: real RAM addresses
	AICBTPTTrap   = 0x01
	AICBTPTB4     = 0x02
	AICBTPTBTMode = 0x03
	AICBTPTPWRON  = 0x04
	AICBTPTAF     = 0x05
)

// PatchTable is one parsed entry of fw_patch_table_*.bin. Data is a
// stream of (address, value) u32 pairs applied with DBG_MEM_WRITE.
type PatchTable struct {
	Name string
	Type uint32
	Len  uint32   // pair count
	Data []uint32 // Len*2 u32s
}

// ParsePatchTable parses a fw_patch_table blob:
//
//	[16B tag "AICBT_PT_TAG"] then repeated entries:
//	[16B name][u32 type][u32 len][len*8B data]
//
// Entries with type >= 1000 or len == 0 carry no data (vendor
// workaround in aicbt_patch_table_alloc).
func ParsePatchTable(blob []byte) ([]PatchTable, error) {
	const tagLen = 16
	if len(blob) < tagLen {
		return nil, fmt.Errorf("patch table: too short (%d bytes)", len(blob))
	}
	// The tag is "AICBT_PT_TAG" NUL-padded to 16 bytes (C code memcmps
	// the full 16).
	if !strings.HasPrefix(string(blob[:tagLen]), AICBTPTTag) {
		return nil, fmt.Errorf("patch table: bad tag %q", string(blob[:tagLen]))
	}
	p := tagLen
	var tables []PatchTable
	for p+24 <= len(blob) {
		t := PatchTable{
			Name: string(blob[p : p+16]),
			Type: binary.LittleEndian.Uint32(blob[p+16 : p+20]),
			Len:  binary.LittleEndian.Uint32(blob[p+20 : p+24]),
		}
		p += 24
		if t.Type >= 1000 || t.Len == 0 {
			t.Len = 0
			tables = append(tables, t)
			continue
		}
		need := int(t.Len) * 8
		if p+need > len(blob) {
			return nil, fmt.Errorf("patch table %q: truncated (need %d bytes at %d)", t.Name, need, p)
		}
		t.Data = make([]uint32, t.Len*2)
		for i := range t.Data {
			t.Data[i] = binary.LittleEndian.Uint32(blob[p+4*i : p+4*i+4])
		}
		p += need
		tables = append(tables, t)
	}
	return tables, nil
}

// PatchInfo carries the real RAM addresses and control values unpacked
// from the AICBT_PT_INF entry (aicbt_patch_info_unpack).
type PatchInfo struct {
	AddrAdid    uint32
	AddrPatch   uint32
	ResetAddr   uint32
	ResetVal    uint32
	AdidFlagAddr uint32
	AdidFlag    uint32
	// ExtPatchNb + (id, addr) pairs for supplementary patch blobs
	// (fw_patch_*_ext<id>.bin).
	ExtPatchNb   uint32
	ExtPatchID   []uint32
	ExtPatchAddr []uint32
}

// UnpackPatchInfo extracts PatchInfo from the first AICBT_PT_INF table.
// Layout mirrors aicbt_patch_info_unpack: data pairs —
// (adid_addrinf, addr_adid), (patch_addrinf, addr_patch),
// (reset_addr, reset_val), (adid_flag_addr, adid_flag),
// optionally (x, ext_patch_nb) followed by (id, addr) per ext patch.
// base_len = 4 pairs.
func UnpackPatchInfo(tables []PatchTable) (*PatchInfo, error) {
	for _, t := range tables {
		if t.Type != AICBTPTInf {
			continue
		}
		const baseLen = 4
		pi := &PatchInfo{}
		n := int(t.Len)
		if n > baseLen+1 && len(t.Data) >= (baseLen+1)*2 {
			// full form with ext patch info
			n = baseLen + 1
		} else if n > baseLen {
			n = baseLen
		}
		if n < baseLen {
			return nil, fmt.Errorf("patch info: table too short (%d pairs)", t.Len)
		}
		pi.AddrAdid = t.Data[1]
		pi.AddrPatch = t.Data[3]
		pi.ResetAddr = t.Data[4]
		pi.ResetVal = t.Data[5]
		pi.AdidFlagAddr = t.Data[6]
		pi.AdidFlag = t.Data[7]
		if t.Len > baseLen && len(t.Data) >= (baseLen+1)*2 {
			pi.ExtPatchNb = t.Data[(baseLen+1)*2-1]
			for i := 0; i < int(pi.ExtPatchNb); i++ {
				base := (baseLen + 1 + i) * 2
				if base+1 >= len(t.Data) {
					break
				}
				pi.ExtPatchID = append(pi.ExtPatchID, t.Data[base])
				pi.ExtPatchAddr = append(pi.ExtPatchAddr, t.Data[base+1])
			}
		}
		return pi, nil
	}
	return nil, fmt.Errorf("patch info: no AICBT_PT_INF (type 0) entry in table")
}
