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
	TaskME    uint8 = 5
	TaskSM    uint8 = 6
	TaskAPM   uint8 = 7
	TaskBAM   uint8 = 8
	TaskMESH  uint8 = 9
	TaskRXU   uint8 = 10
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
	MMSetCoexReq uint16 = 0x0067 // MM_SET_COEX_REQ (ordinal 103 in mm_msg_tag)
	MMSetCoexCfm uint16 = 0x0068
	// RF-calibration / stack-start sequence (all TASK_MM). Ordinals counted
	// directly from the reference mm_msg_tag enum, anchored on the verified
	// MM_ADD_IF_REQ=6=0x0006. NOTE: an earlier extraction was 2 too low for
	// every id past coex (it missed the MM_SET_RF_CONFIG req/cfm pair at
	// ordinals 105/106), which made rf_calib collide with RF_CONFIG (0x0069)
	// and stack_start with TXPWR_OFST (0x007B) — the cause of the "wedge".
	MMSetRFConfigReq    uint16 = 0x0069 // ordinal 105 (was mislabelled rf_calib)
	MMSetRFConfigCfm    uint16 = 0x006A
	MMSetRFCalibReq     uint16 = 0x006B // ordinal 107
	MMSetRFCalibCfm     uint16 = 0x006C
	MMGetMacAddrReq     uint16 = 0x0075 // ordinal 117
	MMGetMacAddrCfm     uint16 = 0x0076
	MMSetTxpwrIdxLvlReq uint16 = 0x0079 // ordinal 121
	MMSetTxpwrIdxLvlCfm uint16 = 0x007A
	MMSetTxpwrOfstReq   uint16 = 0x007B // ordinal 123
	// stack_start responds at 0x007B (is_5g_support=1) but sending it with
	// is_stack_start=1 WEDGES every subsequent command — this firmware
	// auto-starts its stack, so a host stack_start double-starts and hangs it.
	// Point it at the silent 0x007D so the RF sequence (txpwr/rf_calib) runs.
	MMSetStackStartReq uint16 = 0x007D
	MMSetStackStartCfm uint16 = 0x007E
	MMGetFwVersionReq  uint16 = 0x0082 // ordinal 130
	MMGetFwVersionCfm  uint16 = 0x0083
	MMKeyAddReq        uint16 = 0x0025 // ordinal 37
	MMKeyAddCfm        uint16 = 0x0026
	MMKeyDelReq        uint16 = 0x0027 // ordinal 39
)

// SM (station management) task messages (TASK_SM = 6).
const (
	SMConnectReq    uint16 = 0x1800
	SMConnectCfm    uint16 = 0x1801
	SMConnectInd    uint16 = 0x1802
	SMDisconnectReq uint16 = 0x1803
	SMDisconnectCfm uint16 = 0x1804
	SMDisconnectInd uint16 = 0x1805
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

// SCAN task messages (TASK_SCAN = 2). SCAN is the LMAC/SoftMAC scan engine.
const (
	SCANStartReq  uint16 = 0x0800
	SCANStartCfm  uint16 = 0x0801
	SCANDoneInd   uint16 = 0x0802
	SCANCancelReq uint16 = 0x0803
	SCANCancelCfm uint16 = 0x0804
)

// ME task messages (TASK_ME = 6). ME carries the host-side MAC engine
// configuration the firmware needs before a station can scan or connect.
const (
	MEConfigReq     uint16 = 0x1400
	MEConfigCfm     uint16 = 0x1401
	MEChanConfigReq uint16 = 0x1402
	MEChanConfigCfm uint16 = 0x1403
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
