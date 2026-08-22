// probewrite.go — verification for the 0x1701e4 skip workaround.
//
// Usage: sudo ./bin/usbwifi aicloader --probe-write
//
// Established by the 2026-08-17 bisect: the word at 0x1701e4 (within
// chunk 0x1701e0..0x1701ef) wedges the clone's boot ROM into echo mode
// when written — ADDRESS-based (zeros trip it). Reads work everywhere;
// writes work at 0x170000..0x1701e3.
//
// This run verifies the loader's skip workaround on a FRESH device:
//
//   P1  word write @ 0x1701e0 — pins the trigger to exactly 0x1701e4
//       (echo evidence says 0x1701e0..e3 are normal SRAM)
//   P2  16B chunks 0x1701f0..0x171000 — writes PAST the trigger must
//       land fine when the trigger word is never touched
//   P3  readback sweep to confirm the data actually stuck
//
// If P2 lands, `task aic:e2e` runs the full upload with the skip and
// START_APPs the firmware.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

func runAICProbeWrite() int {
	c, err := protocol.Init()
	if err != nil {
		fmt.Fprintf(os.Stderr, "libusb init: %v\n", err)
		return 1
	}
	defer protocol.Deinit(c)

	if locs, lerr := protocol.ListByVIDPID(c, protocol.VID_AIC8800D80_Storage, protocol.PID_AIC8800D80_Storage); lerr == nil && len(locs) > 0 {
		fmt.Println("device at ZeroCD (a69c:5723) — running mode-switch...")
		l := aic8800d80.NewLoader()
		if err := l.SwitchToBootROM(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "mode-switch: %v\n", err)
			return 1
		}
		time.Sleep(500 * time.Millisecond)
	}

	dev, err := protocol.OpenByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open boot ROM: %v\n", err)
		return 1
	}
	defer dev.Close()
	if err := dev.DetachKernelDriver(0); err != nil {
		fmt.Fprintf(os.Stderr, "detach kernel driver: %v (continuing)\n", err)
	}
	if err := dev.ClaimInterface(0); err != nil {
		fmt.Fprintf(os.Stderr, "claim: %v\n", err)
		return 1
	}
	defer dev.ReleaseInterface(0)

	protocol.SetTxLenConv(protocol.ConvLinux)
	protocol.Drain(dev, 16)

	fmt.Println("═══ skip-workaround verification (protected word 0x1701e4) ═══")
	if v, err := protocol.MemRead(dev, 0x40500000); err != nil {
		fmt.Println("device unresponsive at start — wait for the watchdog (ZeroCD) or replug, then retry")
		return 1
	} else {
		fmt.Printf("chip_id=0x%02x\n\n", (v>>16)&0xFF)
	}

	// P1: pin the boundary — word write at 0x1701e0 must LAND.
	fmt.Println("── P1: word write @ 0x1701e0 (trigger should be 0x1701e4, not here) ──")
	if err := protocol.MemWrite(dev, 0x001701e0, 0xCAFEBABE); err != nil {
		fmt.Printf("  WEDGED — trigger includes 0x1701e0! widen the skip range.\n")
		return reportWedge(dev)
	}
	if v, err := protocol.MemRead(dev, 0x001701e0); err == nil && v == 0xCAFEBABE {
		fmt.Println("  LANDED — 0x1701e0 is normal SRAM; trigger is exactly 0x1701e4")
	} else {
		fmt.Printf("  CFM ok but readback 0x%08x (expected 0xcafebabe) — write-through unclear\n", v)
	}

	// P2: chunks past the trigger, never touching 0x1701e4..0x1701e7.
	fmt.Println("\n── P2: 16B chunks 0x1701f0..0x170fff (past the trigger) ──")
	pattern := func(i int) byte { return byte(0x5A ^ (i & 0x1F)) }
	wedged := false
	for a := uint32(0x001701f0); a < 0x00171000; a += 16 {
		chunk := make([]byte, 16)
		for i := range chunk {
			chunk[i] = pattern(int(a) + i)
		}
		if err := protocol.MemBlockWrite(dev, a, chunk); err != nil {
			fmt.Printf("  WEDGE at 0x%08x — writes past the trigger are ALSO blocked.\n", a)
			fmt.Println("  The skip workaround is insufficient; need a window-register fix.")
			wedged = true
			break
		}
	}
	if !wedged {
		fmt.Println("  all 0x1701f0..0x170fff chunks LANDED — skip workaround is sufficient")
	}

	// P3: readback sweep.
	if !wedged {
		fmt.Println("\n── P3: readback ──")
		for _, a := range []uint32{0x001701f0, 0x00170200, 0x00170400, 0x00170800, 0x00170ff0} {
			if v, err := protocol.MemRead(dev, a); err == nil {
				want := uint32(pattern(int(a))) | uint32(pattern(int(a)+1))<<8 | uint32(pattern(int(a)+2))<<16 | uint32(pattern(int(a)+3))<<24
				ok := v == want
				fmt.Printf("  0x%08x = 0x%08x %v\n", a, v, map[bool]string{true: "(matches)", false: "(MISMATCH)"}[ok])
			}
		}
		fmt.Println("\n═══ verification complete — run `task aic:e2e` for the full boot ═══")
	}
	return 0
}

func reportWedge(dev *protocol.USBDevice) int {
	resid := protocol.DrainCapture(dev, 8)
	if len(resid) > 0 {
		fmt.Printf("  echo began with: % x\n", truncBytes(resid, 24))
	}
	fmt.Println("  replug (or wait for watchdog → ZeroCD) before the next experiment.")
	return 1
}

func truncBytes(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}
