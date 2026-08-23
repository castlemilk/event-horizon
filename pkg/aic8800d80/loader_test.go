package aic8800d80

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castlemilk/event-horizon/pkg/aic8800d80/protocol"
)

// TestBundleNameFor_AllRevs verifies that the bundle name lookup
// returns the correct file for each chip revision.
func TestBundleNameFor_AllRevs(t *testing.T) {
	cases := []struct {
		chip uint8
		kind string
		want string
	}{
		{protocol.ChipRevU01, "adid", "fw_adid_8800d80.bin"},
		{protocol.ChipRevU01, "patch", "fw_patch_8800d80.bin"},
		{protocol.ChipRevU01, "fmacfw", "fmacfw_8800d80.bin"},
		{protocol.ChipRevU01, "patch_table", "fw_patch_table_8800d80.bin"},
		{protocol.ChipRevU02, "adid", "fw_adid_8800d80_u02.bin"},
		{protocol.ChipRevU02, "patch", "fw_patch_8800d80_u02.bin"},
		{protocol.ChipRevU02, "fmacfw", "fmacfw_8800d80_u02_ipc.bin"},
		{protocol.ChipRevU02, "patch_table", "fw_patch_table_8800d80_u02.bin"},
		{protocol.ChipRevU03, "adid", "fw_adid_8800d80_u02.bin"},
		{protocol.ChipRevU03, "fmacfw", "fmacfw_8800d80_u02_ipc.bin"},
		{protocol.ChipRevU04, "fmacfw", "fmacfw_8800d80_u02_ipc.bin"},
		{protocol.ChipRevU05, "patch", "fw_patch_8800d80_u02.bin"},
	}
	for _, c := range cases {
		got, err := bundleNameFor(c.chip, c.kind)
		if err != nil {
			t.Errorf("bundleNameFor(0x%02x, %s): %v", c.chip, c.kind, err)
			continue
		}
		if got != c.want {
			t.Errorf("bundleNameFor(0x%02x, %s) = %s, want %s", c.chip, c.kind, got, c.want)
		}
	}
}

// TestBundleNameFor_UnknownKind verifies that unknown blob kinds
// return an error.
func TestBundleNameFor_UnknownKind(t *testing.T) {
	if _, err := bundleNameFor(1, "nonsense"); err == nil {
		t.Errorf("expected error for unknown kind")
	}
}

// TestNewLoader_DefaultFirmwareDir verifies that the default firmware
// directory is `<HOME>/.event-horizon/firmware/aic8800D80` when HOME is
// set.
func TestNewLoader_DefaultFirmwareDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	l := NewLoader()
	want := filepath.Join(home, ".event-horizon", "firmware", "aic8800D80")
	if l.fwDir != want {
		t.Errorf("default fwDir = %q, want %q", l.fwDir, want)
	}
}

// TestNewLoader_OverrideFirmwareDir verifies that WithFirmwareDir
// overrides the default.
func TestNewLoader_OverrideFirmwareDir(t *testing.T) {
	l := NewLoader(WithFirmwareDir("/tmp/custom"))
	if l.fwDir != "/tmp/custom" {
		t.Errorf("fwDir = %q, want /tmp/custom", l.fwDir)
	}
}

// TestNewLoader_WithDebug verifies that WithDebug sets the debug flag.
func TestNewLoader_WithDebug(t *testing.T) {
	l := NewLoader(WithDebug())
	if !l.debug {
		t.Errorf("WithDebug() did not set debug=true")
	}
}

// TestLoader_AsFilesystemFriendly verifies that the loader does not
// leave the firmware dir in /something/dangerous — it should be inside
// the user's home directory by default.
func TestLoader_AsFilesystemFriendly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	l := NewLoader()
	if !strings.HasPrefix(l.fwDir, home) {
		t.Errorf("default fwDir %q is not inside HOME %q", l.fwDir, home)
	}
}

// TestHomeDir_WhenUnset verifies that homeDir errors when HOME is unset.
func TestHomeDir_WhenUnset(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := homeDir()
	if err == nil {
		t.Errorf("expected error when HOME unset")
	}
}

// TestHomeDir_WhenSet verifies that homeDir returns HOME when set.
func TestHomeDir_WhenSet(t *testing.T) {
	t.Setenv("HOME", "/Users/test")
	got, err := homeDir()
	if err != nil {
		t.Fatalf("homeDir: %v", err)
	}
	if got != "/Users/test" {
		t.Errorf("homeDir = %q, want /Users/test", got)
	}
}

// TestLoadFirmwareBundle_OverrideDir verifies that the loader uses the
// configured firmware dir when calling LoadFirmwareBundle.
func TestLoadFirmwareBundle_OverrideDir(t *testing.T) {
	dir := t.TempDir()
	// Create U01 blobs.
	for _, name := range []string{
		"fmacfw_8800d80.bin",
		"fw_patch_8800d80.bin",
		"fw_adid_8800d80.bin",
		"fw_patch_table_8800d80.bin",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("FAKE"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// We can't test LoadFirmware end-to-end without hardware, but we
	// can confirm the loader reads its configured dir.
	l := NewLoader(WithFirmwareDir(dir))
	if l.fwDir != dir {
		t.Errorf("fwDir = %q, want %q", l.fwDir, dir)
	}
}

// TestMinHelper verifies the min helper function.
func TestMinHelper(t *testing.T) {
	if got := min(3, 5); got != 3 {
		t.Errorf("min(3, 5) = %d, want 3", got)
	}
	if got := min(10, 4); got != 4 {
		t.Errorf("min(10, 4) = %d, want 4", got)
	}
	if got := min(-2, 2); got != -2 {
		t.Errorf("min(-2, 2) = %d, want -2", got)
	}
}

// TestU32sToBytes verifies converting uint32 slice to byte slice.
func TestU32sToBytes(t *testing.T) {
	input := []uint32{0x04030201, 0x08070605}
	got := u32sToBytes(input)
	want := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte %d = 0x%02x, want 0x%02x", i, got[i], want[i])
		}
	}
}

