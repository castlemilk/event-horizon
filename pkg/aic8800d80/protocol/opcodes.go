// Package protocol implements the AIC8800D80 firmware upload protocol
// over USB bulk transfers. The wire format and command catalogue are
// ported from the Linux driver at:
//
//   github.com/radxa-pkg/aic8800 (src/USB/driver_fw/drivers/aic8800/aic_load_fw/)
//
// All vendor commands are host-target messages on the lmac_msg envelope:
// a fixed 8-byte header followed by a `param` payload. The chip is
// addressed via the bulk OUT endpoint (commands) and bulk IN endpoint
// (confirms).
package protocol

// LMAC task IDs. The first 10 bits of a message id encode the destination
// task; the remaining bits encode the command within that task.
const (
	TaskNone lmacTaskID = 0xff

	TaskMM    lmacTaskID = 0
	TaskDBG   lmacTaskID = 1
	TaskSCAN  lmacTaskID = 2
	TaskTDLS  lmacTaskID = 3
	TaskSCANU lmacTaskID = 4
	TaskME    lmacTaskID = 5
	TaskSM    lmacTaskID = 6
	TaskAPM   lmacTaskID = 7
	TaskBAM   lmacTaskID = 8
	TaskMESH  lmacTaskID = 9
	TaskRXU   lmacTaskID = 10
	TaskAPI   lmacTaskID = 11
)

// DrvTaskID is the host-side task ID used as `src_id` on every command.
const DrvTaskID lmacTaskID = 100

type lmacTaskID uint16

// MakeMsgID constructs a (task<<10) | idx message id.
func MakeMsgID(task lmacTaskID, idx uint16) uint16 {
	return (uint16(task) << 10) | idx
}

// Debug-task command IDs. Only the subset we actually need for firmware
// upload is enumerated. Indexes match the C enum dbg_msg_tag from
// aicbluetooth_cmds.h.
const (
	DBGMemReadReq       uint16 = 1 << 10 // 1024
	DBGMemReadCfm              = DBGMemReadReq + 1
	DBGMemWriteReq             = DBGMemReadReq + 2
	DBGMemWriteCfm             = DBGMemReadReq + 3
	DBGSetModFilterReq         = DBGMemReadReq + 4
	DBGSetModFilterCfm         = DBGMemReadReq + 5
	DBGSetSevFilterReq         = DBGMemReadReq + 6
	DBGSetSevFilterCfm         = DBGMemReadReq + 7
	DBGErrorInd                = DBGMemReadReq + 8
	DBGGetSysStatReq           = DBGMemReadReq + 9
	DBGGetSysStatCfm           = DBGMemReadReq + 10
	DBGMemBlockWriteReq        = DBGMemReadReq + 11
	DBGMemBlockWriteCfm        = DBGMemReadReq + 12
	DBGStartAppReq             = DBGMemReadReq + 13
	DBGStartAppCfm             = DBGMemReadReq + 14
	DBGStartNPCReq             = DBGMemReadReq + 15
	DBGStartNPCCfm             = DBGMemReadReq + 16
	DBGMemMaskWriteReq         = DBGMemReadReq + 17
	DBGMemMaskWriteCfm         = DBGMemReadReq + 18
)

// HOST_START_APP_* boot modes for DBG_START_APP_REQ.
const (
	HostStartAppAuto   uint32 = 1
	HostStartAppCustom uint32 = 2
	HostStartAppReboot uint32 = 3
	HostStartAppFNCall uint32 = 4
	HostStartAppDummy  uint32 = 5
)

// AIC8800D80 firmware blob file names. Required for ADID / PATCH / fmacfw /
// lmacfw variants — both U01 and U02 chip revisions. Names follow the
// Linux driver convention.
const (
	FWBaseName8800D80        = "fmacfw_8800d80.bin"
	FWBaseName8800D80U02     = "fmacfw_8800d80_u02.bin"
	FWBaseName8800D80HU02    = "fmacfw_8800d80_h_u02.bin"
	FWPatchBaseName8800D80   = "fw_patch_8800d80.bin"
	FWPatchBaseName8800D80U02 = "fw_patch_8800d80_u02.bin"
	// FWPatchBaseName8800D80U02Ext is the prefix for supplementary patch
	// blobs — the patch info table appends the ext id ("<prefix><id>.bin").
	FWPatchBaseName8800D80U02Ext = "fw_patch_8800d80_u02_ext"
	FWAdidBaseName8800D80    = "fw_adid_8800d80.bin"
	FWAdidBaseName8800D80U02 = "fw_adid_8800d80_u02.bin"
	FWPatchTableName8800D80  = "fw_patch_table_8800d80.bin"
	FWPatchTableName8800D80U02 = "fw_patch_table_8800d80_u02.bin"
)

// RAM addresses for the firmware blobs (chips decode the cmd inside
// DBGStartAppReq).
const (
	RAMFMACFWAddr8800D80      uint32 = 0x100000
	RAMFMACFWAddr8800D80U02   uint32 = 0x120000
	FWRAMAdidBaseAddr8800D80  uint32 = 0x002017E0
	FWRAMAdidBaseAddr8800D80U02 uint32 = 0x00201940
	FWRAMPatchBaseAddr8800D80 uint32 = 0x0020B2B0
	FWRAMPatchBaseAddr8800D80U02 uint32 = 0x0020B43C
)

// Chip revision IDs read from address 0x40500000 after USB attach
// (value >> 16 & 0xFF). Must match aicwf_usb.h:
//
//	CHIP_REV_U01 0x1, U02 0x3, U03 0x7, U04 0xf, U05 0x1f
//
// U03 and U04 reuse the U02 firmware set (see aic_compat_8800d80.c).
const (
	ChipRevU01 uint8 = 0x01
	ChipRevU02 uint8 = 0x03
	ChipRevU03 uint8 = 0x07
	ChipRevU04 uint8 = 0x0F
	ChipRevU05 uint8 = 0x1F
)

// Bulk transfer sizing.
const (
	BlockWriteChunkBytes = 1024 // matches Linux driver's 1KiB split
	// LmacMsgHeaderBytes is the fixed header length of an lmac_msg
	// envelope (4 bytes length + 4 bytes dummy + 8 bytes fixed fields).
	LmacMsgHeaderBytes = 16
)
