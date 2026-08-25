package main

import (
	"fmt"
	"os"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

func main() {
	blob, err := os.ReadFile(os.Args[1])
	if err != nil { panic(err) }
	tables, err := protocol.ParsePatchTable(blob)
	if err != nil { panic(err) }
	for _, t := range tables {
		fmt.Printf("table %-16q type=%-4d len=%-4d\n", t.Name, t.Type, t.Len)
	}
	pi, err := protocol.UnpackPatchInfo(tables)
	if err != nil { panic(err) }
	fmt.Printf("\npatch info:\n  addr_adid     = 0x%08x\n  addr_patch    = 0x%08x\n  reset         = @0x%08x = 0x%08x\n  adid_flag     = @0x%08x = 0x%08x\n  ext_patch_nb  = %d\n",
		pi.AddrAdid, pi.AddrPatch, pi.ResetAddr, pi.ResetVal, pi.AdidFlagAddr, pi.AdidFlag, pi.ExtPatchNb)
	for i := range pi.ExtPatchID {
		fmt.Printf("  ext[%d]: id=%d addr=0x%08x\n", i, pi.ExtPatchID[i], pi.ExtPatchAddr[i])
	}
}
