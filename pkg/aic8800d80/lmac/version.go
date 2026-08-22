package lmac

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// VersionReq is MM_VERSION_REQ — has no payload.
type VersionReq struct{}

// Encode returns the full lmac_msg (header + empty param[]).
func (VersionReq) Encode() ([]byte, error) {
	buf := make([]byte, HeaderSize)
	Header{ID: MMVersionReq, DestID: uint16(TaskMM), SrcID: DRVTaskID, ParamLen: 0}.Encode(buf)
	return buf, nil
}

// VersionCfm is MM_VERSION_CFM. Layout per rwnx_main.c::version_cfm_handler
// (rwnx_cmds.h):
//
//	u32 version;
//	u32 mac_version;
//	u32 fw_build_id;
//	u32 fw_build_date;        // unix timestamp (seconds)
//	char version_string[128]; // NUL-padded
type VersionCfm struct {
	Version       uint32
	MacVersion    uint32
	FwBuildID     uint32
	FwBuildDate   uint32
	VersionString string
}

func (c *VersionCfm) Decode(payload []byte) error {
	if len(payload) < 16 {
		return fmt.Errorf("version cfm: short payload (%d bytes)", len(payload))
	}
	c.Version = binary.LittleEndian.Uint32(payload[0:4])
	c.MacVersion = binary.LittleEndian.Uint32(payload[4:8])
	c.FwBuildID = binary.LittleEndian.Uint32(payload[8:12])
	c.FwBuildDate = binary.LittleEndian.Uint32(payload[12:16])
	str := payload[16:]
	if i := bytes.IndexByte(str, 0); i >= 0 {
		c.VersionString = string(str[:i])
	} else {
		c.VersionString = string(bytes.TrimRight(str, "\x00"))
	}
	return nil
}
