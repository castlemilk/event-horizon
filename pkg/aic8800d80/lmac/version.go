package lmac

import (
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

// VersionCfm is MM_VERSION_CFM. Layout per struct mm_version_cfm (lmac_msg.h):
//
//	u32 version_lmac;
//	u32 version_machw_1;
//	u32 version_machw_2;
//	u32 version_phy_1;
//	u32 version_phy_2;
//	u32 features;
//	u16 max_sta_nb;
//	u8  max_vif_nb;
type VersionCfm struct {
	VersionLMAC   uint32
	VersionMacHW1 uint32
	VersionMacHW2 uint32
	VersionPHY1   uint32
	VersionPHY2   uint32
	Features      uint32
	MaxStaNb      uint16
	MaxVifNb      uint8
	VersionString string
}

func (c *VersionCfm) Decode(payload []byte) error {
	if len(payload) < 20 {
		return fmt.Errorf("version cfm: short payload (%d bytes)", len(payload))
	}
	c.VersionLMAC = binary.LittleEndian.Uint32(payload[0:4])
	c.VersionMacHW1 = binary.LittleEndian.Uint32(payload[4:8])
	c.VersionMacHW2 = binary.LittleEndian.Uint32(payload[8:12])
	c.VersionPHY1 = binary.LittleEndian.Uint32(payload[12:16])
	c.VersionPHY2 = binary.LittleEndian.Uint32(payload[16:20])
	if len(payload) >= 24 {
		c.Features = binary.LittleEndian.Uint32(payload[20:24])
	}
	if len(payload) >= 26 {
		c.MaxStaNb = binary.LittleEndian.Uint16(payload[24:26])
	}
	if len(payload) >= 27 {
		c.MaxVifNb = payload[26]
	}
	vers := c.VersionLMAC
	c.VersionString = fmt.Sprintf("%d.%d.%d.%d",
		(vers>>24)&0xff, (vers>>16)&0xff, (vers>>8)&0xff, vers&0xff)
	return nil
}
