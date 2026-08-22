package protocol

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFirmwareBundle_U01 verifies that all four U01 blobs are loaded.
func TestLoadFirmwareBundle_U01(t *testing.T) {
	dir := t.TempDir()
	blobs := map[string][]byte{
		FWBaseName8800D80:        []byte("FAKE_FMACFW"),
		FWPatchBaseName8800D80:   []byte("FAKE_PATCH"),
		FWAdidBaseName8800D80:    []byte("FAKE_ADID"),
		FWPatchTableName8800D80:  []byte("FAKE_PATCH_TABLE"),
	}
	for name, data := range blobs {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	bundle, err := LoadFirmwareBundle(dir, ChipRevU01)
	if err != nil {
		t.Fatalf("LoadFirmwareBundle: %v", err)
	}
	if bundle.ChipRev != ChipRevU01 {
		t.Errorf("ChipRev = 0x%02x, want 0x%02x", bundle.ChipRev, ChipRevU01)
	}
	for name, want := range blobs {
		got, ok := bundle.Get(name)
		if !ok {
			t.Errorf("missing %s", name)
		}
		if string(got) != string(want) {
			t.Errorf("%s got %q, want %q", name, got, want)
		}
	}
}

// TestLoadFirmwareBundle_U02 verifies the U02 file names are used.
func TestLoadFirmwareBundle_U02(t *testing.T) {
	dir := t.TempDir()
	blobs := map[string][]byte{
		FWBaseName8800D80U02:        []byte("FAKE_FMACFW_U02"),
		FWPatchBaseName8800D80U02:   []byte("FAKE_PATCH_U02"),
		FWAdidBaseName8800D80U02:    []byte("FAKE_ADID_U02"),
		FWPatchTableName8800D80U02:  []byte("FAKE_PATCH_TABLE_U02"),
	}
	for name, data := range blobs {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	bundle, err := LoadFirmwareBundle(dir, ChipRevU02)
	if err != nil {
		t.Fatalf("LoadFirmwareBundle U02: %v", err)
	}
	if bundle.ChipRev != ChipRevU02 {
		t.Errorf("ChipRev = 0x%02x, want 0x%02x", bundle.ChipRev, ChipRevU02)
	}
}

// TestLoadFirmwareBundle_MissingFile verifies the loader errors when a
// blob is absent.
func TestLoadFirmwareBundle_MissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadFirmwareBundle(dir, ChipRevU01); err == nil {
		t.Errorf("expected error for missing firmware files")
	}
}

// TestLoadFirmwareBundle_UnknownRev verifies the loader rejects
// revisions outside the supported set.
func TestLoadFirmwareBundle_UnknownRev(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadFirmwareBundle(dir, 0xFF); err == nil {
		t.Errorf("expected error for unknown chip revision")
	}
}

// TestFirmwareBundle_GetMissing verifies Get returns (nil, false) for an
// unknown file name.
func TestFirmwareBundle_GetMissing(t *testing.T) {
	b := &FirmwareBundle{}
	if d, ok := b.Get("nope.bin"); ok || d != nil {
		t.Errorf("Get on empty bundle: got (%v, %v), want (nil, false)", d, ok)
	}
	var nilB *FirmwareBundle
	if d, ok := nilB.Get("nope.bin"); ok || d != nil {
		t.Errorf("Get on nil bundle: got (%v, %v), want (nil, false)", d, ok)
	}
}
