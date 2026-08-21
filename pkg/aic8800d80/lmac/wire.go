package lmac

// WrapCommand frames an encoded lmac_msg for the USB bulk command pipe,
// mirroring rwnx_cmds.c::aicwf_set_cmd_tx:
//
//	[0..1] record length = len(lmac_msg)+4 (high nibble of byte 1 masked off)
//	[2]    type = 0x11 (USBTypeCfgCmdRsp — host→target command)
//	[3]    padding
//	[4..7] dummy word (zeros)
//	[8..]  lmac_msg (id, dest_id, src_id, param_len, params)
func WrapCommand(lmacMsg []byte) []byte {
	n := len(lmacMsg)
	out := make([]byte, 8+n)
	recLen := n + 4
	out[0] = byte(recLen & 0xff)
	out[1] = byte((recLen >> 8) & 0x0f)
	out[2] = 0x11
	out[3] = 0x00
	copy(out[8:], lmacMsg)
	return out
}
