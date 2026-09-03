package event

import (
	"context"
	"encoding/binary"
	"log"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
)

// Dispatch is the default Sink: routes known msg ids to typed decoders.
// Unknown msg ids are logged and dropped (continue).
type Dispatch struct {
	OnScanResult   func(lmac.ScanResultInd)
	OnScanStartCfm func(lmac.ScanStartCfm)
	OnScanDone     func()
	OnVersion      func(lmac.VersionCfm)
	OnStartCfm     func()
	OnAddIfCfm     func(lmac.AddIfCfm)
	OnResetCfm     func()
	OnMacAddr      func(lmac.MacAddrCfm)
	OnConnectCfm   func(status uint8)
	OnConnectInd   func(lmac.ConnectInd)
	OnAnyUnknown   func(msgID uint16, payload []byte)
	// OnRaw, if set, is called for every frame before typed routing —
	// msgID 0xFFFF marks a data frame. For raw diagnostics.
	OnRaw func(msgID uint16, payload []byte)
}

func (d *Dispatch) Handle(_ context.Context, msgID uint16, payload []byte) error {
	if d.OnRaw != nil {
		d.OnRaw(msgID, payload)
	}
	switch msgID {
	case 0xFFFF:
		// This firmware wraps config responses (SM_CONNECT_CFM/IND) inside a
		// data-typed frame: [0x11 0x00][id:2][dest:2][src:2][param_len:2]
		// [pattern:4][param...]. Scan for the SM connect messages and route them.
		if d.OnConnectCfm != nil || d.OnConnectInd != nil {
			for i := 0; i+14 <= len(payload); i++ {
				if payload[i] != 0x11 || payload[i+1] != 0x00 {
					continue
				}
				id := binary.LittleEndian.Uint16(payload[i+2 : i+4])
				plen := int(binary.LittleEndian.Uint16(payload[i+8 : i+10]))
				paramOff := i + 14
				// Validate before trusting: the id must be one we expect and
				// its param_len must match the struct size, so a stray [11 00]
				// in noise can't manufacture a spurious status_code.
				switch id {
				case lmac.SMConnectCfm:
					if plen == 1 && d.OnConnectCfm != nil && paramOff < len(payload) {
						d.OnConnectCfm(payload[paramOff])
					}
				case lmac.SMConnectInd:
					if plen == 852 && d.OnConnectInd != nil {
						end := paramOff + plen
						if end > len(payload) {
							end = len(payload)
						}
						var ind lmac.ConnectInd
						if err := ind.Decode(payload[paramOff:end]); err == nil {
							d.OnConnectInd(ind)
						}
					}
				}
			}
		}
		if d.OnScanResult != nil {
			// This firmware delivers scan beacons as data frames. The 802.11
			// MPDU sits at a fixed offset after the ~56-byte hw_rxhdr (record
			// offset 60 -> payload offset 56). Fall back to a search if the
			// fixed offset doesn't hold.
			for _, base := range beaconMPDUOffsets(payload) {
				if r, ok := decodeBeacon(payload[base:]); ok {
					d.OnScanResult(r)
					break
				}
			}
		}
		return nil
	case lmac.SCANUResultInd:
		if d.OnScanResult == nil {
			return nil
		}
		var r lmac.ScanResultInd
		if err := r.Decode(payload); err != nil {
			log.Printf("[dispatch] scan result decode: %v", err)
			return nil
		}
		d.OnScanResult(r)
		return nil
	case lmac.SCANStartCfm, lmac.SCANUStartCfm, lmac.SCANUStartCfmAdditional:
		if d.OnScanStartCfm == nil {
			return nil
		}
		var c lmac.ScanStartCfm
		if err := c.Decode(payload); err != nil {
			log.Printf("[dispatch] scan start cfm decode: %v", err)
			return nil
		}
		d.OnScanStartCfm(c)
		return nil
	case lmac.SCANDoneInd:
		if d.OnScanDone != nil {
			d.OnScanDone()
		}
		return nil
	case lmac.MMVersionCfm:
		if d.OnVersion == nil {
			return nil
		}
		var c lmac.VersionCfm
		if err := c.Decode(payload); err != nil {
			log.Printf("[dispatch] version cfm decode: %v", err)
			return nil
		}
		d.OnVersion(c)
		return nil
	case lmac.MMStartCfm:
		if d.OnStartCfm != nil {
			d.OnStartCfm()
		}
		return nil
	case lmac.MMAddIfCfm:
		if d.OnAddIfCfm == nil {
			return nil
		}
		var c lmac.AddIfCfm
		if err := c.Decode(payload); err != nil {
			log.Printf("[dispatch] add if cfm decode: %v", err)
			return nil
		}
		d.OnAddIfCfm(c)
		return nil
	case lmac.MMResetCfm:
		if d.OnResetCfm != nil {
			d.OnResetCfm()
		}
		return nil
	case lmac.MMGetMacAddrCfm:
		if d.OnMacAddr == nil {
			return nil
		}
		var c lmac.MacAddrCfm
		if err := c.Decode(payload); err != nil {
			log.Printf("[dispatch] mac addr cfm decode: %v", err)
			return nil
		}
		d.OnMacAddr(c)
		return nil
	case lmac.SMConnectCfm:
		if d.OnConnectCfm != nil && len(payload) >= 1 {
			d.OnConnectCfm(payload[0])
		}
		return nil
	case lmac.SMConnectInd:
		if d.OnConnectInd == nil {
			return nil
		}
		var ind lmac.ConnectInd
		if err := ind.Decode(payload); err != nil {
			log.Printf("[dispatch] connect ind decode: %v", err)
			return nil
		}
		d.OnConnectInd(ind)
		return nil
	default:
		if d.OnAnyUnknown != nil {
			d.OnAnyUnknown(msgID, payload)
		}
		return nil
	}
}

// beaconMPDUOffsets returns candidate offsets in a data-frame payload where an
// 802.11 beacon/probe-resp MPDU may begin: the fixed hw_rxhdr offset (record
// offset 60 -> payload offset 56) first, then any frame-control match as a
// fallback for firmware whose header length differs.
func beaconMPDUOffsets(payload []byte) []int {
	offs := []int{}
	if len(payload) > 56 {
		offs = append(offs, 56)
	}
	for i := 0; i+38 <= len(payload); i++ {
		if (payload[i] == 0x80 || payload[i] == 0x50) && payload[i+1] == 0x00 {
			offs = append(offs, i)
		}
	}
	return offs
}

// decodeBeacon parses an 802.11 beacon (fc 0x80) / probe-resp (fc 0x50) MPDU at
// mpdu[0]: BSSID (addr3) at +16, capability at +34, tagged IEs at +36. Returns
// ok=false if the bytes are not a plausible beacon (validated by fc + a leading
// SSID element), so a wrong offset is rejected rather than yielding garbage.
func decodeBeacon(mpdu []byte) (lmac.ScanResultInd, bool) {
	if len(mpdu) < 38 {
		return lmac.ScanResultInd{}, false
	}
	if mpdu[0] != 0x80 && mpdu[0] != 0x50 {
		return lmac.ScanResultInd{}, false
	}
	ies := mpdu[36:]
	// A real beacon's tagged IEs start with the SSID element (EID 0); require
	// it so a coincidental fc match at a wrong offset is rejected.
	if len(ies) < 2 || ies[0] != 0 || int(ies[1]) > 32 || 2+int(ies[1]) > len(ies) {
		return lmac.ScanResultInd{}, false
	}
	var r lmac.ScanResultInd
	copy(r.BSSID[:], mpdu[16:22])
	capab := binary.LittleEndian.Uint16(mpdu[34:36])
	r.IE = append([]byte(nil), ies...)
	for off := 0; off+2 <= len(ies); {
		eid, elen := ies[off], int(ies[off+1])
		if off+2+elen > len(ies) {
			break
		}
		b := ies[off+2 : off+2+elen]
		switch eid {
		case 0:
			r.SSID = string(b)
		case 3:
			if len(b) >= 1 {
				r.Channel = uint16(b[0])
			}
		}
		off += 2 + elen
	}
	r.Security = lmac.ClassifySecurity(ies, capab)
	return r, true
}
