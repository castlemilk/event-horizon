// retentionmap.go — map which write path retains at each 16B address in
// the loadable window prefix (0x170000..0x17238f, skipping zone1's halo).
//
// Usage: sudo ./bin/usbwifi aicloader --retention-map [start-index]
//
// The clone's ROM wedges after ~11,828 B of window writes per power
// cycle (2026-08-20: pure-word sweeps die at 2,957 words; loader runs
// with the 320x1KB below-wall phase die at 2,277 words — the 1KB phase
// consumes ~2,720 B of the same budget). Reads are free.
//
// Per address: word write + read, then 16B block write + read. Writes
// per address = 20 B; 564 addresses = 11,280 B — fits in ONE power
// cycle without the below-wall phase.
//
// Classes (2026-08-19 probe, 37 addresses, quasi-periodic ~0x30):
//
//	both  — word AND 16B retain
//	word  — word retains, 16B does not (write zeros)
//	blk   — 16B retains, word does not
//	none  — neither (never observed)
//
// Output written to /tmp/aic-retention.txt: one "0x<addr> <class>" per
// line. The loader's AIC_HYBRID=1 mode consumes it to write each chunk
// via its retaining path — a fully-retained 9,024 B window.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80"
	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

const (
	retentionMapStart = uint32(0x170000) // wall — first chunk of the window prefix
	retentionMapEnd   = uint32(0x172390) // trimmed window end (12:50-run geometry)
)

func retentionMapAddrs() []uint32 {
	zones := protocol.CloneRegZones()
	var addrs []uint32
	for a := retentionMapStart; a < retentionMapEnd; a += 16 {
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

func runAIRetentionMap(startIdx int) int {
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

	fmt.Println("═══ register-window retention map ═══")
	if v, err := protocol.MemRead(dev, 0x40500000); err != nil {
		fmt.Println("device unresponsive at start — wait for the watchdog (ZeroCD) or replug, then retry")
		return 1
	} else {
		fmt.Printf("chip_id=0x%02x\n\n", (v>>16)&0xFF)
	}

	addrs := retentionMapAddrs()
	if startIdx < 0 || startIdx >= len(addrs) {
		fmt.Fprintf(os.Stderr, "start index %d out of range (0..%d)\n", startIdx, len(addrs)-1)
		return 1
	}
	fmt.Printf("mapping %d addresses (20B writes each, %d bytes total) — no below-wall phase, fits the budget\n",
		len(addrs), len(addrs)*20)

	pattern := func(a uint32) uint32 {
		b := byte(a)
		return uint32(b) | uint32(b+1)<<8 | uint32(b+2)<<16 | uint32(b+3)<<24
	}

	var out []string
	var counts [4]int
	start := time.Now()
	for i, a := range addrs {
		if i < startIdx {
			continue
		}
		wordOK, blkOK := true, true

		val := pattern(a)
		if err := protocol.MemWrite(dev, a, val); err != nil {
			fmt.Printf("\nWEDGED at word write 0x%08x (op %d/%d) — device dead, power cycle and resume from index %d\n",
				a, i+1, len(addrs), i+1)
			return 1
		}
		if v, err := protocol.MemRead(dev, a); err != nil || v != val {
			wordOK = false
		}

		block := make([]byte, 16)
		for j := range block {
			block[j] = byte(a) + byte(j)
		}
		if err := protocol.MemBlockWrite(dev, a, block); err != nil {
			fmt.Printf("\nWEDGED at 16B write 0x%08x (op %d/%d) — device dead, power cycle and resume from index %d\n",
				a, i+1, len(addrs), i+1)
			return 1
		}
		if v, err := protocol.MemRead(dev, a); err != nil || v != uint32(block[0])|uint32(block[1])<<8|uint32(block[2])<<16|uint32(block[3])<<24 {
			blkOK = false
		}

		cls := "none"
		idx := 3
		switch {
		case wordOK && blkOK:
			cls, idx = "both", 0
		case wordOK:
			cls, idx = "word", 1
		case blkOK:
			cls, idx = "blk", 2
		}
		counts[idx]++
		out = append(out, fmt.Sprintf("0x%08x %s", a, cls))

		if (i+1)%100 == 0 {
			fmt.Printf("  %d/%d (0x%08x, %v)\n", i+1, len(addrs), a, time.Since(start).Round(time.Millisecond))
		}
	}

	f, err := os.Create("/tmp/aic-retention.txt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "write map: %v\n", err)
		return 1
	}
	for _, line := range out {
		fmt.Fprintln(f, line)
	}
	f.Close()

	fmt.Printf("\n═══ map complete — %d addresses: both=%d word=%d blk=%d none=%d (%v) → /tmp/aic-retention.txt ═══\n",
		len(addrs), counts[0], counts[1], counts[2], counts[3], time.Since(start).Round(time.Millisecond))
	fmt.Println("Now run the hybrid load: sudo AIC_HYBRID=1 AIC_NO_VERIFY=1 ./bin/usbwifi aicloader --kill-daemon --firmware-dir=<dir>")
	return 0
}