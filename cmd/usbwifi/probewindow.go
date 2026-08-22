// probewindow.go — map the true wedge-zone boundaries around the clone's
// register blocks, past the current 80B halo zones.
//
// Usage: sudo ./bin/usbwifi aicloader --probe-window [start-index]
//
// Background: 16B block writes AND word writes die at the FIRST op past
// zone2's end (0x172430, block2+0x30) — runs F/H (16B) and the 09:46 word
// run all died there, while 1KB writes starting at 0x172430 succeed. The
// true +side boundary of block2's wedge zone is unknown, and block3/4
// zone ends have never been exercised (no run got past 0x172430).
//
// This sweep probes word writes + readbacks and 16B block writes +
// readbacks at each address, stopping at the first wedge (the device is
// then dead — power cycle). Every probed address that survives is
// provably loadable by BOTH the word mode and the 16B adaptive plan.
//
// Fine granularity (16B steps) for the first 0x100 past zone2's end, then
// coarse steps to 0x172840, then the block3/4 zone ends.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

func probeWindowAddrs() []uint32 {
	addrs := []uint32{}
	// Fine sweep past zone2 end (0x172430) — 16B steps to 0x172540.
	for a := uint32(0x172440); a < 0x172540; a += 16 {
		addrs = append(addrs, a)
	}
	// Coarse sweep onward.
	for _, a := range []uint32{
		0x172540, 0x172580, 0x1725c0, 0x172600, 0x172700, 0x172800,
		0x172820, 0x172830, 0x172840,
	} {
		addrs = append(addrs, a)
	}
	// Block3 (0x174620) and block4 (0x176840) zone ends: +0x30 and beyond.
	for _, a := range []uint32{
		0x174650, 0x174660, 0x174680, 0x1746a0, 0x174700, 0x174800,
		0x176870, 0x176880, 0x1768a0, 0x1768c0, 0x176900,
	} {
		addrs = append(addrs, a)
	}
	// Control: block1 +0x30 (zone1 end) — must be alive in every run.
	addrs = append(addrs, 0x170210)
	return addrs
}

func runAICProbeWindow(startIdx int) int {
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

	fmt.Println("═══ register-window wedge-zone boundary sweep ═══")
	if v, err := protocol.MemRead(dev, 0x40500000); err != nil {
		fmt.Println("device unresponsive at start — wait for the watchdog (ZeroCD) or replug, then retry")
		return 1
	} else {
		fmt.Printf("chip_id=0x%02x\n\n", (v>>16)&0xFF)
	}

	addrs := probeWindowAddrs()
	if startIdx < 0 || startIdx >= len(addrs) {
		fmt.Fprintf(os.Stderr, "start index %d out of range (0..%d)\n", startIdx, len(addrs)-1)
		return 1
	}
	fmt.Printf("probing %d addresses (first wedge stops the sweep; device then needs a power cycle)\n", len(addrs))

	pattern := func(i int) byte { return byte(0xA5 ^ (i & 0x3F)) }
	for i, a := range addrs {
		if i < startIdx {
			continue
		}
		fmt.Printf("── [%02d] 0x%08x ──\n", i, a)

		// 1. word write + readback.
		val := uint32(pattern(int(a))) | uint32(pattern(int(a)+1))<<8 | uint32(pattern(int(a)+2))<<16 | uint32(pattern(int(a)+3))<<24
		if err := protocol.MemWrite(dev, a, val); err != nil {
			fmt.Printf("  WEDGE on WORD WRITE at 0x%08x — zone end must be ≤ this\n", a)
			fmt.Printf("  continue from index %d after power cycle\n", i+1)
			return 1
		}
		if v, err := protocol.MemRead(dev, a); err != nil {
			fmt.Printf("  WEDGE on WORD READBACK at 0x%08x\n", a)
			fmt.Printf("  continue from index %d after power cycle\n", i+1)
			return 1
		} else if v == val {
			fmt.Printf("  word write+read OK (0x%08x)\n", v)
		} else {
			fmt.Printf("  word write CFM ok but readback 0x%08x ≠ 0x%08x (no retain — register window?)\n", v, val)
		}

		// 2. 16B block write + first-word readback.
		chunk := make([]byte, 16)
		for j := range chunk {
			chunk[j] = pattern(int(a) + j)
		}
		if err := protocol.MemBlockWrite(dev, a, chunk); err != nil {
			fmt.Printf("  WEDGE on 16B WRITE at 0x%08x — zone end must be ≤ this\n", a)
			fmt.Printf("  continue from index %d after power cycle\n", i+1)
			return 1
		}
		if v, err := protocol.MemRead(dev, a); err != nil {
			fmt.Printf("  WEDGE on 16B READBACK at 0x%08x\n", a)
			fmt.Printf("  continue from index %d after power cycle\n", i+1)
			return 1
		} else {
			want := uint32(chunk[0]) | uint32(chunk[1])<<8 | uint32(chunk[2])<<16 | uint32(chunk[3])<<24
			if v == want {
				fmt.Printf("  16B write+read OK\n")
			} else {
				fmt.Printf("  16B write CFM ok but readback 0x%08x ≠ 0x%08x (no retain)\n", v, want)
			}
		}
	}

	fmt.Println("\n═══ sweep complete — ALL probed addresses safe for word AND 16B ops ═══")
	fmt.Println("zone ends must be widened to cover the first deadly address (if any was found);")
	fmt.Println("if the whole sweep survived, widen zone2 beyond 0x172840 and re-probe.")
	return 0
}