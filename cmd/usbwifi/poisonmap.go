// poisonmap.go — map EVERY wedge address in the register window region.
//
// Usage: sudo ./bin/usbwifi aicloader --poison-map [start-index]
//
// Question: is the ~9.1KB "window write budget" (runs 09:46/11:12/11:17
// all survived ~2,276 word writes and died at the NEXT op) really a byte
// budget, or an artifact of the op sequence hitting ONE poison address
// (0x172430 — the 09:46 word run died writing exactly there, and the
// 16B runs F/H died on the same address)?
//
// This sweep word-writes every 4B address in 0x170210..0x1776b8 (7,598
// writes = 30,392 B — well over the alleged 9,104 B budget), skipping
// the four known 80B zones. Outcomes:
//
//   - sweep dies at ~op 2,277 (0x172440-ish): the budget is REAL — the
//     window is capped at ~9.1KB of writes per power cycle
//   - sweep completes: no budget — every death so far was a poison
//     address; the discovered poisons (if any) get appended to the zone
//     list and the FULL 30KB window becomes loadable in one pass
//
// Readbacks: the 11:12 run showed the ROM can stop answering READS
// while writes still land (START_APP succeeded in the 11:17 run after
// 2,276 writes). On the first read timeout the sweep switches to
// writes-only and keeps mapping. Poison writes are reported to
// /tmp/aic-poisons.txt and the sweep stops (device dead).
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

func poisonMapAddrs() []uint32 {
	zones := protocol.MergePoisonZones(protocol.CloneRegZones())
	var addrs []uint32
	for a := uint32(0x170210); a <= 0x1776b8; a += 4 {
		skip := false
		for _, z := range zones {
			if a >= z.Start && a < z.End {
				skip = true
				break
			}
		}
		if !skip {
			addrs = append(addrs, a)
		}
	}
	return addrs
}

func runAICPoisonMap(startIdx int) int {
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

	fmt.Println("═══ register-window poison map ═══")
	if v, err := protocol.MemRead(dev, 0x40500000); err != nil {
		// The ROM may be in crash-back: reads dead, but WRITES might
		// still land (the 11:17 run's START_APP write was answered after
		// the window writes). If writes work here, multi-cycle window
		// accumulation is possible (SRAM survives the crash's watchdog
		// reset?).
		fmt.Println("device unresponsive to reads (crash-back?) — testing whether WRITES still land...")
		if werr := protocol.MemWrite(dev, 0x177740, 0xfeedface); werr != nil {
			fmt.Printf("write test FAILED: %v — ROM fully dead; power cycle required\n", werr)
			return 1
		}
		fmt.Println("WRITE TEST OK — the ROM still accepts DBG writes!")
		fmt.Println("Multi-cycle accumulation may work: load a slice, START_APP to crash-back,")
		fmt.Println("then load the next slice without a power cycle. SRAM survival is unproven.")
		fmt.Println("Stopping here — the sweep must not run on a crashed-back device (a surviving")
		fmt.Println("SRAM would be corrupted by the sweep's writes). Power cycle for a real sweep.")
		return 0
	} else {
		fmt.Printf("chip_id=0x%02x\n\n", (v>>16)&0xFF)
	}

	addrs := poisonMapAddrs()
	if startIdx < 0 || startIdx >= len(addrs) {
		fmt.Fprintf(os.Stderr, "start index %d out of range (0..%d)\n", startIdx, len(addrs)-1)
		return 1
	}
	fmt.Printf("sweeping %d addresses (4B writes, %d bytes total) — a write wedge stops the sweep (device dead)\n",
		len(addrs), len(addrs)*4)

	poisonFile := "/tmp/aic-poisons.txt"
	pattern := func(i int) byte { return byte(0x5C ^ (i & 0x3F)) }

	writesOnly := false
	noRetain := 0
	start := time.Now()
	for i, a := range addrs {
		if i < startIdx {
			continue
		}
		val := uint32(pattern(int(a))) | uint32(pattern(int(a)+1))<<8 | uint32(pattern(int(a)+2))<<16 | uint32(pattern(int(a)+3))<<24
		if err := protocol.MemWrite(dev, a, val); err != nil {
			fmt.Printf("\nPOISON: word write at 0x%08x WEDGED (op %d/%d)\n", a, i+1, len(addrs))
			f, ferr := os.OpenFile(poisonFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if ferr == nil {
				fmt.Fprintf(f, "0x%08x\n", a)
				f.Close()
				fmt.Printf("appended to %s\n", poisonFile)
			}
			fmt.Printf("resume from index %d after power cycle\n", i+1)
			return 1
		}
		if !writesOnly {
			if v, err := protocol.MemRead(dev, a); err != nil {
				fmt.Printf("\nREAD PATH DIED at 0x%08x (op %d) — continuing writes-only\n", a, i+1)
				writesOnly = true
			} else if v != val {
				noRetain++
			}
		}
		if (i+1)%500 == 0 {
			el := time.Since(start).Round(time.Millisecond)
			fmt.Printf("  %d/%d addresses (0x%08x, %v)\n", i+1, len(addrs), a, el)
		}
	}

	fmt.Printf("\n═══ sweep complete — %d addresses, %d no-retain, %v ═══\n", len(addrs), noRetain, time.Since(start).Round(time.Millisecond))
	if writesOnly {
		fmt.Println("NOTE: read path died mid-sweep; writes-only from that point")
	}
	fmt.Println("If no poison was found: the ~9.1KB 'budget' theory is DEAD — the full window")
	fmt.Println("is loadable in one pass. Run the full upload with AIC_WINDOW_WORD=1 next.")
	return 0
}