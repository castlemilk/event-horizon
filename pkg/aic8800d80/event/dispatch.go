package event

import (
	"context"
	"log"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/lmac"
)

// Dispatch is the default Sink: routes known msg ids to typed decoders.
// Unknown msg ids are logged and dropped (continue).
type Dispatch struct {
	OnScanResult func(lmac.ScanResultInd)
	OnVersion    func(lmac.VersionCfm)
	OnAnyUnknown func(msgID uint16, payload []byte)
}

func (d *Dispatch) Handle(_ context.Context, msgID uint16, payload []byte) error {
	switch msgID {
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
	default:
		if d.OnAnyUnknown != nil {
			d.OnAnyUnknown(msgID, payload)
		}
		return nil
	}
}
