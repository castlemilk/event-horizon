// probe.go — read-only boot-ROM introspection for the AIC8800D80.
//
// Usage: sudo ./bin/usbwifi aicloader --probe   (device at BootROM stage)
//
// Answers three questions the upload failures raised:
//
//  1. What do the system/window registers 0x40500000..0x40500060 hold?
//     (The disabled Linux code at aic_compat_8800d80.c:477 writes a
//     window register at 0x40500048 — strong evidence the 0x4050004x
//     range controls SRAM mapping.)
//  2. Is memory readable at/after the 0x170000 fault boundary? Maps the
//     readable/unreadable regions around it.
//  3. If a prior run wedged the device: capture the post-fault bytes
//     (the "0xf105" dump record) in full, to /tmp/aic-fault-dump.bin.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

func runAICProbe() int {
	c, err := protocol.Init()
	if err != nil {
		fmt.Fprintf(os.Stderr, "libusb init: %v\n", err)
		return 1
	}
	defer protocol.Deinit(c)

	// If the device sits at ZeroCD (watchdog recycle state), mode-switch
	// it to boot ROM first — same path the loader uses.
	if locs, lerr := protocol.ListByVIDPID(c, protocol.VID_AIC8800D80_Storage, protocol.PID_AIC8800D80_Storage); lerr == nil && len(locs) > 0 {
		fmt.Println("device at ZeroCD (a69c:5723) — running mode-switch to reach boot ROM...")
		l := aic8800d80.NewLoader()
		runCtx := context.Background()
		if err := l.SwitchToBootROM(runCtx); err != nil {
			fmt.Fprintf(os.Stderr, "mode-switch: %v\n", err)
			return 1
		}
		time.Sleep(500 * time.Millisecond)
	}

	dev, err := protocol.OpenByVIDPID(c, protocol.VID_AIC8800D80_BootROM, protocol.PID_AIC8800D80_BootROM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open boot ROM: %v\n", err)
		fmt.Fprintln(os.Stderr, "(device must be at Stage 1 / a69c:8d80 — replug and wait for the watchdog if needed)")
		return 1
	}
	defer dev.Close()

	if err := dev.DetachKernelDriver(0); err != nil {
		fmt.Fprintf(os.Stderr, "detach kernel driver: %v (continuing)\n", err)
	}
	if err := dev.ClaimInterface(0); err != nil {
		fmt.Fprintf(os.Stderr, "claim interface 0: %v\n", err)
		return 1
	}
	defer dev.ReleaseInterface(0)

	protocol.SetTxLenConv(protocol.ConvLinux)
	protocol.Drain(dev, 16)

	readSafe := func(addr uint32) (uint32, bool) {
		// A wedged device won't answer at all; a faulted read region may
		// answer with garbage or an error record. Either way, treat a
		// clean DBG_MEM_READ_CFM as "readable".
		v, err := protocol.MemRead(dev, addr)
		if err != nil {
			return 0, false
		}
		return v, true
	}

	fmt.Println("═══ AIC8800D80 boot-ROM probe ═══")

	// 1. Chip identity.
	if v, ok := readSafe(0x40500000); ok {
		fmt.Printf("0x40500000 = 0x%08x  chip_id=0x%02x mcu_bit=%d\n",
			v, (v>>16)&0xFF, (v>>25)&1)
	} else {
		fmt.Println("0x40500000: UNREADABLE — device wedged? replug and retry")
		return 1
	}

	// 2. System/window register block.
	fmt.Println("\n── registers 0x40500000..0x4050005f ──")
	for addr := uint32(0x40500000); addr <= 0x4050005c; addr += 4 {
		if v, ok := readSafe(addr); ok {
			marker := ""
			switch addr {
			case 0x40500048:
				marker = "  ← suspected SRAM window/base register (written by disabled Linux test code)"
			case 0x4050004c, 0x40500050:
				marker = "  ← adjacent window-control candidate"
			case 0x40500150:
			}
			fmt.Printf("  0x%08x = 0x%08x%s\n", addr, v, marker)
		} else {
			fmt.Printf("  0x%08x = READ FAILED\n", addr)
		}
	}
	// The patch-info reset/adid flag register.
	if v, ok := readSafe(0x40500150); ok {
		fmt.Printf("  0x40500150 = 0x%08x  (patch reset/adid flag)\n", v)
	}

	// 3. Map the fault boundary: fmacfw loads at 0x120000 and the
	// device faults on the first block past 0x170000. Probe readable
	// memory in a sweep around it.
	fmt.Println("\n── memory map around 0x170000 (fmacfw fault boundary) ──")
	probes := []uint32{
		0x0011fffc, // fmacfw base - 4 (should be readable)
		0x00120000, // fmacfw base
		0x0016fffc, // last word before boundary
		0x00170000, // THE boundary — write here faults
		0x00170004,
		0x00170008,
		0x00170ffc,
		0x00171000,
		0x00172430, // slice-1 window coverage (word load, trimmed)
		0x00172434, // negative control — poison 0x172434 never written
		0x00173094, // negative control — poison 0x173094 never written
		0x00173014, // slice-2 distinctive word (0x00010080)
		0x00174000, // slice-2 mid
		0x001745d8, // slice-2 last word
		0x00177ffc, // where the full 358072-byte image would end
		0x00178000,
		0x00180000,
		0x001e0000, // patch region (works)
		0x00201940, // adid region (works)
		0x0020b43c, // ext0 region (works)
	}
	readableAfter := false
	for _, a := range probes {
		if v, ok := readSafe(a); ok {
			fmt.Printf("  0x%08x : readable = 0x%08x\n", a, v)
			if a >= 0x00170004 {
				readableAfter = true
			}
		} else {
			fmt.Printf("  0x%08x : READ FAILED (fault or no answer)\n", a)
		}
	}

	if readableAfter {
		fmt.Println("\n→ memory past 0x170000 IS readable: the wall is not a hard limit;")
		fmt.Println("  likely a write-protection or window-tumble register — compare the register block above.")
	} else {
		fmt.Println("\n→ memory past 0x170000 NOT readable: the SRAM window ends there on this silicon.")
		fmt.Println("  Options: locate a window-extend register, or split/relocate the firmware load.")
	}

	// 4. If anything is still queued (fault dump from a previous run),
	// capture it in full.
	fmt.Println("\n── residual RX capture ──")
	dump := protocol.DrainCapture(dev, 32)
	if len(dump) > 0 {
		path := "/tmp/aic-fault-dump.bin"
		if err := os.WriteFile(path, dump, 0o644); err == nil {
			fmt.Printf("captured %d residual bytes → %s\n", len(dump), path)
			fmt.Printf("first 96 bytes: % x\n", dump[:min(len(dump), 96)])
		}
	} else {
		fmt.Println("no residual bytes queued (clean state)")
	}

	fmt.Println("\n═══ probe complete ═══")
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
