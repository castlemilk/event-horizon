package event

import (
	"context"
	"log"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
	"github.com/castlemilk/event-horizon/pkg/wifi"
)

// Dispatch is the default Sink: routes known msg ids to typed decoders.
// Unknown msg ids are logged and dropped (continue).
type Dispatch struct {
	OnScanResult   func(lmac.ScanResultInd)
	OnScanStartCfm func(lmac.ScanStartCfm)
	OnScanDone     func()
	OnVersion      func(lmac.VersionCfm)
	OnStartCfm    func()
	OnAddIfCfm    func(lmac.AddIfCfm)
	OnResetCfm    func()
	OnAnyUnknown   func(msgID uint16, payload []byte)
}

func (d *Dispatch) Handle(_ context.Context, msgID uint16, payload []byte) error {
	switch msgID {
	case 0xFFFF:
		if len(payload) > 60 {
			raw80211 := payload[60:]
			rssi := int8(payload[11])
			if frame, err := wifi.ParseFrame(raw80211, rssi); err == nil {
				if ap, err := frame.ParseBeacon(); err == nil && d.OnScanResult != nil {
					var bssid [6]byte
					copy(bssid[:], frame.Address3[:])
					d.OnScanResult(lmac.ScanResultInd{
						SSID:    ap.SSID,
						BSSID:   bssid,
						Channel: uint16(ap.Channel),
						RSSI:    ap.RSSI,
					})
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
	default:
		if d.OnAnyUnknown != nil {
			d.OnAnyUnknown(msgID, payload)
		}
		return nil
	}
}
