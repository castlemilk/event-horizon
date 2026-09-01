package lmac

import "encoding/binary"

// The RF-calibration / stack-start sequence that brings the radio PHY online.
// Without it the MAC accepts config messages but the radio never transmits or
// receives, so SCANU_START_REQ gets no response. Reference order (D81):
//
//	MM_SET_STACK_START_REQ -> MM_SET_TXPWR_IDX_LVL_REQ -> MM_SET_RF_CALIB_REQ
//	-> MM_GET_MAC_ADDR_REQ
//
// (txpwr_lvl_adj and txpwr_ofst2x are skipped: their userconfig `enable` is 0
// in the shipped D80 bundle, so the reference driver sends nothing for them.)

// StackStartReq is MM_SET_STACK_START_REQ (struct mm_set_stack_start_req, 4B).
// For D81 with USE_5G: is_stack_start=1, efuse_valid=0, set_vendor_info=CO_BIT(5)
// =0x20, fwtrace_redir=0.
type StackStartReq struct{}

func (StackStartReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize+4)
	Header{ID: MMSetStackStartReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: 4}.Encode(buf)
	p := buf[HeaderSize:]
	p[0] = 1    // is_stack_start
	p[1] = 0    // efuse_valid
	p[2] = 0x20 // set_vendor_info = CO_BIT(5)
	p[3] = 0    // fwtrace_redir
	return buf, nil
}

// txpwrLvl2G4 / txpwrLvl5G hold the per-rate TX power levels parsed by the
// reference driver from aic_userconfig_8800d80.txt (# txpwr_lvl, enable=1).
var (
	txpwr11bg2G4 = []int8{18, 18, 18, 18, 18, 18, 18, 18, 16, 16, 15, 15}
	txpwr11n2G4  = []int8{18, 18, 18, 18, 16, 16, 15, 15, 14, 14}
	txpwr11ax2G4 = []int8{18, 18, 18, 18, 16, 16, 15, 15, 14, 14, 13, 13}
	txpwr11a5G   = []int8{18, 18, 18, 18, 16, 16, 15, 15, 15, 15, 15, 15}
	txpwr11n5G   = []int8{18, 18, 18, 18, 16, 16, 15, 15, 14, 14}
	txpwr11ax5G  = []int8{18, 18, 18, 18, 16, 16, 14, 14, 13, 13, 12, 12}
)

// TxpwrLvlReq is MM_SET_TXPWR_IDX_LVL_REQ. The wire struct is a union whose
// largest member (v4) is 95 bytes, so param_len is 95, but only the first
// 69 bytes — the txpwr_lvl_conf_v3 view — are written:
//
//	u8 enable; s8 11b_11ag_2g4[12]; s8 11n_11ac_2g4[10]; s8 11ax_2g4[12];
//	s8 11a_5g[12]; s8 11n_11ac_5g[10]; s8 11ax_5g[12];
type TxpwrLvlReq struct{}

func (TxpwrLvlReq) Encode() ([]byte, error) {
	const paramLen = 95
	buf := make([]byte, HeaderSize+paramLen)
	Header{ID: MMSetTxpwrIdxLvlReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: paramLen}.Encode(buf)
	p := buf[HeaderSize:]
	p[0] = 1 // enable
	off := 1
	put := func(vals []int8, n int) {
		for i := 0; i < n; i++ {
			if i < len(vals) {
				p[off] = byte(vals[i])
			}
			off++
		}
	}
	put(txpwr11bg2G4, 12)
	put(txpwr11n2G4, 10)
	put(txpwr11ax2G4, 12)
	put(txpwr11a5G, 12)
	put(txpwr11n5G, 10)
	put(txpwr11ax5G, 12)
	return buf, nil
}

// RFCalibReq is MM_SET_RF_CALIB_REQ (struct mm_set_rf_calib_req, 24B). D81
// values: cal_cfg_24g=0x0f8f, cal_cfg_5g=0x0f0f, param_alpha=0x0c34c008,
// bt_calib_en=0, bt_calib_param=0x264203, xtal_cap=0, xtal_cap_fine=0.
type RFCalibReq struct{}

func (RFCalibReq) Encode() ([]byte, error) {
	const paramLen = 24
	buf := make([]byte, HeaderSize+paramLen)
	Header{ID: MMSetRFCalibReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: paramLen}.Encode(buf)
	p := buf[HeaderSize:]
	binary.LittleEndian.PutUint32(p[0:4], 0x0f8f)      // cal_cfg_24g
	binary.LittleEndian.PutUint32(p[4:8], 0x0f0f)      // cal_cfg_5g
	binary.LittleEndian.PutUint32(p[8:12], 0x0c34c008) // param_alpha
	binary.LittleEndian.PutUint32(p[12:16], 0)         // bt_calib_en
	binary.LittleEndian.PutUint32(p[16:20], 0x264203)  // bt_calib_param
	p[20] = 0                                          // xtal_cap
	p[21] = 0                                          // xtal_cap_fine
	// p[22:24] trailing pad
	return buf, nil
}

// GetMacAddrReq is MM_GET_MAC_ADDR_REQ (struct mm_get_mac_addr_req {u32 get}).
type GetMacAddrReq struct{}

func (GetMacAddrReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize+4)
	Header{ID: MMGetMacAddrReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: 4}.Encode(buf)
	binary.LittleEndian.PutUint32(buf[HeaderSize:], 1) // get = 1
	return buf, nil
}

// MacAddrCfm is MM_GET_MAC_ADDR_CFM (struct mm_get_mac_addr_cfm {u8 mac[6]}).
type MacAddrCfm struct {
	MAC [6]byte
}

func (c *MacAddrCfm) Decode(payload []byte) error {
	if len(payload) < 6 {
		return &SubmitError{Kind: ErrShortFrame, MsgID: MMGetMacAddrCfm}
	}
	copy(c.MAC[:], payload[0:6])
	return nil
}
