// Package firmware manages the proprietary firmware blobs that the
// AIC8800D80 loader needs. The blobs are GPL-tainted and fetched on
// demand from the Android common kernel tree at android.googlesource.com.
package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Blob is a single firmware blob expected by the loader.
type Blob struct {
	// Name is the file name as expected by the loader (e.g.
	// "fmacfw_8800d80_u02.bin").
	Name string
	// Path is the upstream path relative to the Android kernel tree
	// root. Used by the fetcher to locate the blob.
	UpstreamPath string
	// Size is the expected file size in bytes. Verified at fetch time.
	Size int64
	// SHA256 is the expected SHA-256 hash of the blob. Verified at
	// fetch time.
	SHA256 string
}

// Expected matches the firmware files required by the AIC8800D80 loader.
// The blobs are organized by chip revision.
type Expected struct {
	// ChipRev is the chip revision this set of blobs targets. 1 = U01,
	// 2 = U02, 3 = U03.
	ChipRev uint8
	// Blobs is the list of files expected in the bundle directory.
	Blobs []Blob
}

// Manifest_U01 is the firmware set for AIC8800D80 chip revision U01.
var Manifest_U01 = Expected{
	ChipRev: 1,
	Blobs: []Blob{
		{
			Name:         "fmacfw_8800d80.bin",
			UpstreamPath: "src/USB/driver_fw/fw/aic8800D80/fmacfw_8800d80.bin",
			Size:         0,
			SHA256:       "",
		},
		{
			Name:         "fw_patch_8800d80.bin",
			UpstreamPath: "src/USB/driver_fw/fw/aic8800D80/fw_patch_8800d80.bin",
			Size:         0,
			SHA256:       "",
		},
		{
			Name:         "fw_adid_8800d80.bin",
			UpstreamPath: "src/USB/driver_fw/fw/aic8800D80/fw_adid_8800d80.bin",
			Size:         0,
			SHA256:       "",
		},
		{
			Name:         "fw_patch_table_8800d80.bin",
			UpstreamPath: "src/USB/driver_fw/fw/aic8800D80/fw_patch_table_8800d80.bin",
			Size:         0,
			SHA256:       "",
		},
	},
}

// Manifest_U02 is the firmware set for AIC8800D80 chip revision U02/U03.
var Manifest_U02 = Expected{
	ChipRev: 2,
	Blobs: []Blob{
		{
			Name:         "fmacfw_8800d80_u02.bin",
			UpstreamPath: "src/USB/driver_fw/fw/aic8800D80/fmacfw_8800d80_u02.bin",
			Size:         0,
			SHA256:       "",
		},
		{
			Name:         "fw_patch_8800d80_u02.bin",
			UpstreamPath: "src/USB/driver_fw/fw/aic8800D80/fw_patch_8800d80_u02.bin",
			Size:         0,
			SHA256:       "",
		},
		{
			Name:         "fw_adid_8800d80_u02.bin",
			UpstreamPath: "src/USB/driver_fw/fw/aic8800D80/fw_adid_8800d80_u02.bin",
			Size:         0,
			SHA256:       "",
		},
		{
			Name:         "fw_patch_table_8800d80_u02.bin",
			UpstreamPath: "src/USB/driver_fw/fw/aic8800D80/fw_patch_table_8800d80_u02.bin",
			Size:         0,
			SHA256:       "",
		},
	},
}

// ManifestForRev returns the manifest for the given chip revision.
func ManifestForRev(chipRev uint8) (*Expected, error) {
	switch chipRev {
	case 1:
		return &Manifest_U01, nil
	case 2, 3:
		return &Manifest_U02, nil
	default:
		return nil, fmt.Errorf("no firmware manifest for chip revision 0x%02x", chipRev)
	}
}

// Verify checks that the blobs in `dir` are present and match the
// expected SHA-256 hashes. Returns a list of missing/mismatched blobs.
func (m *Expected) Verify(dir string) ([]string, error) {
	var problems []string
	for _, b := range m.Blobs {
		path := filepath.Join(dir, b.Name)
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("missing %s: %v", b.Name, err))
			continue
		}
		if b.SHA256 != "" {
			sum := sha256.Sum256(data)
			got := hex.EncodeToString(sum[:])
			if got != b.SHA256 {
				problems = append(problems, fmt.Sprintf("hash mismatch %s: got %s, want %s", b.Name, got, b.SHA256))
			}
		}
		if b.Size > 0 && int64(len(data)) != b.Size {
			problems = append(problems, fmt.Sprintf("size mismatch %s: got %d, want %d", b.Name, len(data), b.Size))
		}
	}
	if len(problems) > 0 {
		return problems, errors.New("firmware verification failed")
	}
	return nil, nil
}
