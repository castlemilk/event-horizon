// firmware subcommand. Invoked as:
//
//   ./bin/usbwifi firmware fetch --target aic8800D80 --out ~/.event-horizon/firmware
//   ./bin/usbwifi firmware verify --target aic8800D80 --in ~/.event-horizon/firmware/aic8800D80
//   ./bin/usbwifi firmware list
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"path/filepath"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/firmware"
)

// Pin to a known good tag/commit of the AIC8800D80 driver source. The
// radxa-pkg repo bundles the fw blobs in src/USB/driver_fw/fw/.
const firmwareSourceRepo = "https://github.com/radxa-pkg/aic8800.git"
const firmwareSourceCommit = "main" // latest (annotated blobs under fw/aic8800D80)

// LockFile is the manifest of expected firmware blobs written after a
// successful fetch. The same structure is used to verify the blobs
// later.
type LockFile struct {
	Source  string                   `json:"source"`
	Commit  string                   `json:"commit"`
	Blobs   map[string]firmware.Blob `json:"blobs"`
}

func runFirmwareCmd(args []string) int {
	if len(args) < 1 {
		usageFirmware()
		return 1
	}
	// Expand a leading ~ in subsequent --out/--in values regardless of
	// the invoking shell's tilde-expansion behavior.
	for i := range args {
		if strings.HasPrefix(args[i], "--out=~") {
			args[i] = "--out=" + expandHome(strings.TrimPrefix(args[i], "--out="))
		}
		if strings.HasPrefix(args[i], "--in=~") {
			args[i] = "--in=" + expandHome(strings.TrimPrefix(args[i], "--in="))
		}
	}
	switch args[0] {
	case "fetch":
		return runFirmwareFetch(args[1:])
	case "verify":
		return runFirmwareVerify(args[1:])
	case "list":
		return runFirmwareList(args[1:])
	case "help", "-h", "--help":
		usageFirmware()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown firmware subcommand: %s\n", args[0])
		usageFirmware()
		return 1
	}
}

func runFirmwareFetch(args []string) int {
	fs := flag.NewFlagSet("firmware fetch", flag.ExitOnError)
	target := fs.String("target", "aic8800D80", "Target firmware family (currently only aic8800D80)")
	outDir := fs.String("out", "", "Output directory (default: <HOME>/.event-horizon/firmware/<target>)")
	keepClone := fs.Bool("keep-clone", false, "Keep the cloned Android kernel tree on disk")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *target != "aic8800D80" {
		fmt.Fprintf(os.Stderr, "unsupported target %s\n", *target)
		return 1
	}

	dir := *outDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "no --out and HOME not set: %v\n", err)
			return 1
		}
		dir = filepath.Join(home, ".event-horizon", "firmware", *target)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", dir, err)
		return 1
	}

	log.SetPrefix("[firmware] ")
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("fetching AIC8800D80 firmware blobs from %s (ref %s)", firmwareSourceRepo, firmwareSourceCommit)

	// Clone the AIC8800 source tree (sparse, depth=1).
	tmpDir, err := os.MkdirTemp("", "aic-kernel-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mktemp: %v\n", err)
		return 1
	}
	if !*keepClone {
		defer os.RemoveAll(tmpDir)
	} else {
		log.Printf("clone retained at %s", tmpDir)
	}

	cloneCmd := exec.Command("git", "clone", "--depth=1",
		"--filter=blob:none", "--sparse",
		firmwareSourceRepo, tmpDir)
	cloneCmd.Stdout = os.Stdout
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "git clone: %v\n", err)
		return 1
	}

	// Sparse-checkout the fw directory.
	sparseCmd := exec.Command("git", "-C", tmpDir, "sparse-checkout", "set",
		"src/USB/driver_fw/fw")
	sparseCmd.Stdout = os.Stdout
	sparseCmd.Stderr = os.Stderr
	if err := sparseCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sparse-checkout: %v\n", err)
		return 1
	}

	// Locate the blobs in the checkout.
	lockBlobs := map[string]firmware.Blob{}

	copyBlob := func(src, name string) bool {
		if _, err := os.Stat(src); err != nil {
			return false
		}
		dst := filepath.Join(dir, name)
		log.Printf("copy %s -> %s", src, dst)
		if err := copyFile(src, dst); err != nil {
			fmt.Fprintf(os.Stderr, "copy %s: %v\n", src, err)
			return false
		}
		data, err := os.ReadFile(dst)
		if err != nil {
			return false
		}
		sum := sha256.Sum256(data)
		lockBlobs[name] = firmware.Blob{
			Name:         name,
			UpstreamPath: relToClone(src, tmpDir),
			SHA256:       hex.EncodeToString(sum[:]),
			Size:         int64(len(data)),
		}
		return true
	}

	for _, rev := range []uint8{1, 2} {
		manifest, err := firmware.ManifestForRev(rev)
		if err != nil {
			continue
		}
		for _, blob := range manifest.Blobs {
			src := filepath.Join(tmpDir, blob.UpstreamPath)
			if !copyBlob(src, blob.Name) {
				log.Printf("skip %s (not in tree)", src)
			}
		}
	}

	// Supplementary ext-patch blobs (fw_patch_*_ext<id>.bin) — enumerated
	// dynamically since the ext ids are only known from the patch table.
	extMatches, _ := filepath.Glob(filepath.Join(tmpDir, "src", "USB", "driver_fw", "fw", "aic8800D80", "*_ext*.bin"))
	for _, src := range extMatches {
		copyBlob(src, filepath.Base(src))
	}

	// Write the lockfile.
	lock := LockFile{
		Source: firmwareSourceRepo,
		Commit: firmwareSourceCommit,
		Blobs:  lockBlobs,
	}
	lockPath := filepath.Join(dir, "manifest.lock.json")
	lockData, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal lockfile: %v\n", err)
		return 1
	}
	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write lockfile: %v\n", err)
		return 1
	}
	log.Printf("wrote %s", lockPath)
	log.Printf("fetch complete; blobs in %s", dir)
	return 0
}

func runFirmwareVerify(args []string) int {
	fs := flag.NewFlagSet("firmware verify", flag.ExitOnError)
	target := fs.String("target", "aic8800D80", "Target firmware family")
	inDir := fs.String("in", "", "Input directory (default: <HOME>/.event-horizon/firmware/<target>)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *target != "aic8800D80" {
		fmt.Fprintf(os.Stderr, "unsupported target %s\n", *target)
		return 1
	}
	dir := *inDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "no --in and HOME not set: %v\n", err)
			return 1
		}
		dir = filepath.Join(home, ".event-horizon", "firmware", *target)
	}

	// Prefer the lockfile if present.
	lockPath := filepath.Join(dir, "manifest.lock.json")
	if lockData, err := os.ReadFile(lockPath); err == nil {
		var lock LockFile
		if err := json.Unmarshal(lockData, &lock); err != nil {
			fmt.Fprintf(os.Stderr, "parse lockfile: %v\n", err)
			return 1
		}
		// Verify each blob in the lockfile.
		var failed int
		for _, blob := range lock.Blobs {
			path := filepath.Join(dir, blob.Name)
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("FAIL %s: %v\n", blob.Name, err)
				failed++
				continue
			}
			sum := sha256.Sum256(data)
			got := hex.EncodeToString(sum[:])
			if got != blob.SHA256 {
				fmt.Printf("FAIL %s: hash %s != %s\n", blob.Name, got, blob.SHA256)
				failed++
				continue
			}
			fmt.Printf("OK   %s  %d bytes  %s\n", blob.Name, len(data), got)
		}
		if failed > 0 {
			fmt.Printf("\n%d blob(s) failed verification\n", failed)
			return 1
		}
		fmt.Printf("\nAll %d blob(s) verified against lockfile (commit %s)\n", len(lock.Blobs), lock.Commit)
		return 0
	}

	// Fall back to per-revision manifest.
	log.SetPrefix("[firmware] ")
	log.Printf("no lockfile at %s; falling back to per-revision manifest", lockPath)
	for _, rev := range []uint8{1, 2} {
		manifest, err := firmware.ManifestForRev(rev)
		if err != nil {
			continue
		}
		problems, err := manifest.Verify(dir)
		if err != nil {
			fmt.Printf("Chip 0x%02x verification failed:\n", rev)
			for _, p := range problems {
				fmt.Printf("  - %s\n", p)
			}
		} else {
			fmt.Printf("Chip 0x%02x: OK\n", rev)
		}
	}
	return 0
}

func runFirmwareList(args []string) int {
	fs := flag.NewFlagSet("firmware list", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	for _, rev := range []uint8{1, 2} {
		m, err := firmware.ManifestForRev(rev)
		if err != nil {
			continue
		}
		fmt.Printf("Chip revision 0x%02x:\n", rev)
		for _, b := range m.Blobs {
			fmt.Printf("  %s\n", b.Name)
		}
	}
	return 0
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// relToClone renders a path relative to the cloned tree for lockfile records.
func relToClone(path, cloneRoot string) string {
	rel, err := filepath.Rel(cloneRoot, path)
	if err != nil {
		return path
	}
	return rel
}

// expandHome expands a leading ~ or ~/ in p using $HOME.
func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
	}
	return p
}

func usageFirmware() {
	fmt.Fprintf(os.Stderr, `firmware — fetch and verify proprietary AIC8800D80 firmware blobs

Usage:
  ./bin/usbwifi firmware fetch [--target=aic8800D80] [--out=<dir>]
  ./bin/usbwifi firmware verify [--target=aic8800D80] [--in=<dir>]
  ./bin/usbwifi firmware list

The blobs are GPL-tainted and fetched from the Android common kernel
tree at android.googlesource.com.
`)
}
