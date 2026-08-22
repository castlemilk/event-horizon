package firmware

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManifestForRev_Valid verifies that the supported chip revisions
// return manifests. U03 shares U02's firmware, so the returned manifest
// has ChipRev=2 even when queried with rev=3.
func TestManifestForRev_Valid(t *testing.T) {
	for _, rev := range []uint8{1, 2, 3} {
		m, err := ManifestForRev(rev)
		if err != nil {
			t.Errorf("ManifestForRev(0x%02x): %v", rev, err)
			continue
		}
		// For U03, the manifest's ChipRev is the U02 value (2).
		expectedChipRev := rev
		if rev == 3 {
			expectedChipRev = 2
		}
		if m.ChipRev != expectedChipRev {
			t.Errorf("rev=0x%02x: ChipRev = 0x%02x, want 0x%02x", rev, m.ChipRev, expectedChipRev)
		}
		if len(m.Blobs) != 4 {
			t.Errorf("expected 4 blobs, got %d", len(m.Blobs))
		}
	}
}

// TestManifestForRev_Unknown verifies the unknown-revision error.
func TestManifestForRev_Unknown(t *testing.T) {
	if _, err := ManifestForRev(0xFF); err == nil {
		t.Errorf("expected error for unknown revision")
	}
}

// TestManifest_U01_FileNames verifies the U01 blob names match the
// loader's expected file names.
func TestManifest_U01_FileNames(t *testing.T) {
	m, _ := ManifestForRev(1)
	names := map[string]bool{}
	for _, b := range m.Blobs {
		names[b.Name] = true
	}
	for _, want := range []string{
		"fmacfw_8800d80.bin",
		"fw_patch_8800d80.bin",
		"fw_adid_8800d80.bin",
		"fw_patch_table_8800d80.bin",
	} {
		if !names[want] {
			t.Errorf("U01 manifest missing %s", want)
		}
	}
}

// TestManifest_U02_FileNames verifies the U02 file names.
func TestManifest_U02_FileNames(t *testing.T) {
	m, _ := ManifestForRev(2)
	names := map[string]bool{}
	for _, b := range m.Blobs {
		names[b.Name] = true
	}
	for _, want := range []string{
		"fmacfw_8800d80_u02.bin",
		"fw_patch_8800d80_u02.bin",
		"fw_adid_8800d80_u02.bin",
		"fw_patch_table_8800d80_u02.bin",
	} {
		if !names[want] {
			t.Errorf("U02 manifest missing %s", want)
		}
	}
}

// TestVerify_HappyPath verifies that a directory with all blobs at
// the expected SHA-256 hashes passes verification.
func TestVerify_HappyPath(t *testing.T) {
	dir := t.TempDir()
	m, _ := ManifestForRev(1)
	h := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	for _, b := range []struct {
		name, content string
	}{
		{"fmacfw_8800d80.bin", "FAKE_FMACFW"},
		{"fw_patch_8800d80.bin", "FAKE_PATCH"},
		{"fw_adid_8800d80.bin", "FAKE_ADID"},
		{"fw_patch_table_8800d80.bin", "FAKE_PATCH_TABLE"},
	} {
		if err := os.WriteFile(filepath.Join(dir, b.name), []byte(b.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", b.name, err)
		}
	}
	// Patch the manifest in-place with real hashes.
	for i := range m.Blobs {
		path := filepath.Join(dir, m.Blobs[i].Name)
		data, _ := os.ReadFile(path)
		m.Blobs[i].SHA256 = h(string(data))
		m.Blobs[i].Size = int64(len(data))
	}
	problems, err := m.Verify(dir)
	if err != nil {
		t.Errorf("Verify: %v (%v)", err, problems)
	}
}

// TestVerify_MissingFile verifies that a missing file is reported.
func TestVerify_MissingFile(t *testing.T) {
	dir := t.TempDir()
	m, _ := ManifestForRev(1)
	problems, err := m.Verify(dir)
	if err == nil {
		t.Errorf("expected error for missing files")
	}
	if len(problems) == 0 {
		t.Errorf("expected problem list to be non-empty")
	}
	if !strings.Contains(problems[0], "missing") {
		t.Errorf("problem = %v, want 'missing'", problems[0])
	}
}

// TestVerify_HashMismatch verifies that a wrong hash is reported.
func TestVerify_HashMismatch(t *testing.T) {
	dir := t.TempDir()
	m, _ := ManifestForRev(1)
	// Reset blob metadata so prior test mutations don't leak.
	for i := range m.Blobs {
		m.Blobs[i].SHA256 = strings.Repeat("0", 64)
		m.Blobs[i].Size = 0
	}
	for _, b := range m.Blobs {
		if err := os.WriteFile(filepath.Join(dir, b.Name), []byte("X"), 0o644); err != nil {
			t.Fatalf("write %s: %v", b.Name, err)
		}
	}
	problems, err := m.Verify(dir)
	if err == nil {
		t.Errorf("expected error for hash mismatch")
	}
	for _, p := range problems {
		if !strings.Contains(p, "hash mismatch") {
			t.Errorf("problem = %v, want 'hash mismatch'", p)
		}
	}
}
