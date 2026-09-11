package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/firmware"
)

// firmware carve — produce the one blob that cannot be downloaded.
//
// The fmacfw image that runs on chip_id=0x07 is embedded in the vendor's
// Windows boot-ROM loader, which ships ON the dongle: in ZeroCD mode the
// device is a FAT16 volume holding Setup.exe (Inno Setup 6, LZMA). So every
// owner already has the firmware; this command digs it out.
//
//	Setup.exe --innoextract--> app/win10_x64/aicloadfw.Sys --signature--> fmacfw
//
// innoextract is the one external dependency (brew install innoextract).
// Reimplementing Inno's LZMA container in Go is not worth it for one file.
func runFirmwareCarve(args []string) int {
	fs := flag.NewFlagSet("firmware carve", flag.ExitOnError)
	from := fs.String("from", "", "Setup.exe, aicloadfw.Sys, or a directory holding either (default: the mounted ZeroCD volume under /Volumes)")
	out := fs.String("out", "", "Directory to write "+firmware.CarvedName+" into (default: "+defaultFirmwareSetDir()+")")
	keep := fs.Bool("keep", false, "Keep the innoextract scratch directory")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `firmware carve — extract fmacfw_8800d80_u02_ipc.bin from the dongle's own Windows driver

Usage:
  ./bin/usbwifi firmware carve [--from <Setup.exe|aicloadfw.Sys|dir>] [--out <dir>]

With no --from, looks for the ZeroCD volume macOS mounts when the dongle is
plugged in fresh (/Volumes/UGREEN*). Needs innoextract on PATH unless --from
already points at an unpacked aicloadfw.Sys:

  brew install innoextract

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	outDir := *out
	if outDir == "" {
		outDir = defaultFirmwareSetDir()
	}
	outDir = expandHome(outDir)

	src, err := resolveCarveSource(expandHome(*from))
	if err != nil {
		fmt.Fprintln(os.Stderr, "carve:", err)
		return 1
	}
	fmt.Printf("carve: source %s\n", src)

	loaderPath := src
	if !isLoaderFile(src) {
		// It is Setup.exe: unpack it.
		if _, err := exec.LookPath("innoextract"); err != nil {
			fmt.Fprintln(os.Stderr, "carve: innoextract is not installed. Install it with:")
			fmt.Fprintln(os.Stderr, "         brew install innoextract")
			return 1
		}
		tmp, err := os.MkdirTemp("", "eh-carve-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "carve:", err)
			return 1
		}
		if !*keep {
			defer os.RemoveAll(tmp)
		}
		cmd := exec.Command("innoextract", "-e", "-s", "-d", tmp, src)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "carve: innoextract failed: %v\n", err)
			return 1
		}
		lp, err := findLoader(tmp)
		if err != nil {
			fmt.Fprintln(os.Stderr, "carve:", err)
			return 1
		}
		loaderPath = lp
		if *keep {
			fmt.Printf("carve: unpacked installer kept at %s\n", tmp)
		}
	}
	fmt.Printf("carve: loader %s\n", loaderPath)

	res, err := firmware.CarveFromLoaderFile(loaderPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "carve: %v\n", err)
		return 1
	}
	sum := sha256.Sum256(res.Image)
	fmt.Printf("carve: image at 0x%x, %d bytes, sha256 %s\n", res.Offset, res.Size, hex.EncodeToString(sum[:]))
	if res.Size != firmware.CarveExpectedLen {
		fmt.Printf("carve: note: length differs from the verified win10_x64 image (%d bytes); a different vendor build?\n", firmware.CarveExpectedLen)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "carve:", err)
		return 1
	}
	dst := filepath.Join(outDir, firmware.CarvedName)
	if err := os.WriteFile(dst, res.Image, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "carve:", err)
		return 1
	}
	fmt.Printf("carve: wrote %s\n", dst)

	// Complete the set. The three public blobs land in a sibling directory
	// when fetched with the defaults (~/.event-horizon/firmware/aic8800D80),
	// so pull them across rather than making the user do it.
	completeSetFromFetched(outDir)

	if firmwareSetPresent(outDir) {
		fmt.Printf("carve: firmware set at %s is complete.\n", outDir)
	} else {
		fmt.Printf("carve: the other three blobs are public; fetch them, then re-run carve:\n")
		fmt.Printf("         ./bin/usbwifi firmware fetch\n")
	}
	return 0
}

// publicBlobs are the three blobs that come from radxa-pkg/aic8800.
var publicBlobs = []string{
	"fw_adid_8800d80_u02.bin",
	"fw_patch_8800d80_u02.bin",
	"fw_patch_table_8800d80_u02.bin",
}

// completeSetFromFetched copies any missing public blob into dir from the
// places `firmware fetch` writes to: the sibling aic8800D80 directory and
// the parent directory itself.
func completeSetFromFetched(dir string) {
	candidates := []string{
		filepath.Join(filepath.Dir(dir), "aic8800D80"),
		filepath.Dir(dir),
	}
	for _, name := range publicBlobs {
		dst := filepath.Join(dir, name)
		if fileExists(dst) {
			continue
		}
		for _, c := range candidates {
			src := filepath.Join(c, name)
			if !fileExists(src) {
				continue
			}
			if b, err := os.ReadFile(src); err == nil && len(b) > 0 {
				if err := os.WriteFile(dst, b, 0o644); err == nil {
					fmt.Printf("carve: copied %s from %s\n", name, c)
					break
				}
			}
		}
	}
}

// defaultFirmwareSetDir is where `cmdctl link` looks first.
func defaultFirmwareSetDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".event-horizon", "firmware", "aic8800D80-hybrid")
	}
	return filepath.Join(home, ".event-horizon", "firmware", "aic8800D80-hybrid")
}

func isLoaderFile(p string) bool {
	return strings.EqualFold(filepath.Base(p), "aicloadfw.sys")
}

// resolveCarveSource turns the --from argument (or nothing) into a path to
// either Setup.exe or aicloadfw.Sys.
func resolveCarveSource(from string) (string, error) {
	if from == "" {
		vols, _ := filepath.Glob("/Volumes/UGREEN*")
		if len(vols) == 0 {
			return "", fmt.Errorf("no ZeroCD volume under /Volumes. Unplug the dongle, wait ~10s, plug it back in\n" +
				"       (it mounts as a small disk named UGREEN), or pass --from <Setup.exe>")
		}
		from = vols[0]
	}
	st, err := os.Stat(from)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return from, nil
	}
	// A directory: prefer an already-unpacked loader, else Setup.exe.
	if lp, err := findLoader(from); err == nil {
		return lp, nil
	}
	if p := filepath.Join(from, "Setup.exe"); fileExists(p) {
		return p, nil
	}
	var found string
	_ = filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "setup.exe") {
			found = p
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("no Setup.exe or aicloadfw.Sys under %s", from)
	}
	return found, nil
}

// findLoader locates win10_x64/aicloadfw.Sys under root. The win7 image is
// the same length but differs in 11 bytes, so the variant is pinned.
func findLoader(root string) (string, error) {
	var win10, any string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isLoaderFile(p) {
			return nil
		}
		if strings.Contains(strings.ToLower(p), "win10_x64") {
			win10 = p
		} else if any == "" {
			any = p
		}
		return nil
	})
	if win10 != "" {
		return win10, nil
	}
	if any != "" {
		return any, nil
	}
	return "", fmt.Errorf("no aicloadfw.Sys under %s", root)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
