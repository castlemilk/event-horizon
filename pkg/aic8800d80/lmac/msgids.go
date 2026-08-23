// Package lmac mirrors the LMAC host-target message protocol used by the
// AIC8800D80 firmware. Message IDs follow the LMAC_FIRST_MSG(task) layout
// from lmac_msg.h: bits[15..10] task index, bits[9..0] message index.
package lmac

// Task identifiers (lmac_msg.h TASK_*). Each task owns a 1024-message ID
// space at bit position (task << 10).
const (
	TaskMM    uint8 = 0
	TaskDBG   uint8 = 1
	TaskSCAN  uint8 = 2
	TaskTDLS  uint8 = 3
	TaskSCANU uint8 = 4
	TaskME    uint8 = 6
	TaskSM    uint8 = 7
	TaskAPM   uint8 = 8
	TaskBAM   uint8 = 9
	TaskLast  uint8 = 11 // TASK_RM
	TaskAPI   uint8 = 12
	TaskMax   uint8 = 13
)

// FirstMsg returns the base message ID for a task (LMAC_FIRST_MSG).
func FirstMsg(task uint8) uint16 {
	return uint16(task) << 10
}

// MM task messages (TASK_MM = 0).
const (
	MMResetReq   uint16 = 0x0000
	MMResetCfm   uint16 = 0x0001
	MMStartReq   uint16 = 0x0002
	MMStartCfm   uint16 = 0x0003
	MMVersionReq uint16 = 0x0004
	MMVersionCfm uint16 = 0x0005
	MMAddIfReq   uint16 = 0x0006
	MMAddIfCfm   uint16 = 0x0007
)

// DBG task messages (TASK_DBG = 1). The host uses DBG_MEM_* during the boot
// ROM stage; MM_DBG_TLV_CMD is the post-boot introspection channel.
const (
	DBGMemReadReq      uint16 = 0x0400
	DBGMemReadCfm      uint16 = 0x0401
	DBGMemWriteReq     uint16 = 0x0402
	DBGMemWriteCfm     uint16 = 0x0403
	DBGMemMaskWriteReq uint16 = 0x0404
	DBGMemMaskWriteCfm uint16 = 0x0405
	DBGSetModFilterReq uint16 = 0x0406
	DBGSetModFilterCfm uint16 = 0x0407
	DBGStartAppReq     uint16 = 0x0480
	DBGStartAppCfm     uint16 = 0x0481
	MMDbgTlvCmdReq     uint16 = 0x0482
	MMDbgTlvCmdCfm     uint16 = 0x0483
)

// SCANU task messages (TASK_SCANU = 4). SCANU is the user-space-initiated
// scan path used by fullmac firmwares.
const (
	SCANUStartReq           uint16 = 0x1000
	SCANUStartCfm           uint16 = 0x1001
	SCANUJoinReq            uint16 = 0x1002
	SCANUJoinCfm            uint16 = 0x1003
	SCANUResultInd          uint16 = 0x1004 // async scan-result indication
	SCANUFASTReq            uint16 = 0x1005
	SCANUFASTCfm            uint16 = 0x1006
	SCANUVendorIEReq        uint16 = 0x1007
	SCANUVendorIECfm        uint16 = 0x1008
	SCANUStartCfmAdditional uint16 = 0x1009
	SCANUCancelReq          uint16 = 0x100A
	SCANUCancelCfm          uint16 = 0x100B
)
