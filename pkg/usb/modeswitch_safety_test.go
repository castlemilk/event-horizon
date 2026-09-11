package usb

import "testing"

// The safety property that matters: the filter must never select a real drive.
// This machine has a 2 TB external NVMe that is USB and ejectable, and the only
// thing separating it from a ZeroCD image is size.
func TestZeroCDSizeFilterExcludesRealDrives(t *testing.T) {
	cases := []struct {
		name  string
		info  diskutilInfo
		match bool
	}{
		{"UGREEN ZeroCD image", diskutilInfo{BusProtocol: "USB", Ejectable: true, TotalSize: 3_700_000, MediaName: "UGREEN"}, true},
		{"2TB external NVMe", diskutilInfo{BusProtocol: "USB", Ejectable: true, TotalSize: 2_048_408_248_320, MediaName: "2462 NVME"}, false},
		{"64GB USB stick", diskutilInfo{BusProtocol: "USB", Ejectable: true, TotalSize: 64_000_000_000, MediaName: "Cruzer"}, false},
		{"internal disk", diskutilInfo{BusProtocol: "USB", Ejectable: true, TotalSize: 3_700_000, Internal: true}, false},
		{"thunderbolt, not USB", diskutilInfo{BusProtocol: "Thunderbolt", Ejectable: true, TotalSize: 3_700_000}, false},
		{"not ejectable", diskutilInfo{BusProtocol: "USB", Ejectable: false, TotalSize: 3_700_000}, false},
		{"zero size", diskutilInfo{BusProtocol: "USB", Ejectable: true, TotalSize: 0}, false},
	}
	for _, c := range cases {
		i := c.info
		got := !i.Internal && i.Ejectable && i.BusProtocol == "USB" && i.TotalSize > 0 && i.TotalSize <= maxZeroCDBytes
		if got != c.match {
			t.Errorf("%s: filter matched=%v, want %v — a wrong answer here ejects the user's disk", c.name, got, c.match)
		}
	}
}
