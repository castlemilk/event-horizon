package firmware

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// The chip_id=0x07 firmware image embedded in the vendor's Windows boot-ROM
// loader (aicloadfw.Sys) is located by its first two words: the initial
// stack pointer and the reset vector of an ARM image loaded at 0x120000.
// The image's own length is self-describing: the u32 at offset 0x454 holds
// the end address, so size = end - load address.
//
// The recipe is from docs/HANDOVER-aic8800d80.md §2 and was verified
// against the blob that associates on hardware.
const (
	CarveSigSP       uint32 = 0x001A0000 // initial stack pointer
	CarveSigReset    uint32 = 0x001201A5 // reset vector (Thumb bit set)
	CarveLoadAddr    uint32 = 0x00120000 // where the boot ROM places the image
	CarveEndAddrOff         = 0x454      // offset of the u32 end-address word
	CarveExpectedLen        = 324848     // bytes, the win10_x64 variant
	CarvedName              = "fmacfw_8800d80_u02_ipc.bin"
)

// ErrNoSignature means the loader image did not contain the two-word
// signature; ErrAmbiguous means it contained it more than once.
var (
	ErrNoSignature = errors.New("firmware signature not found")
	ErrAmbiguous   = errors.New("firmware signature found more than once")
)

// CarveResult describes a successful carve.
type CarveResult struct {
	Offset int    // byte offset of the image inside the loader
	Size   int    // bytes carved
	Image  []byte // the image itself
}

// CarveFromLoader extracts the fmacfw image from the contents of
// aicloadfw.Sys. It refuses to guess: exactly one signature hit is
// required, the self-described length must be sane, and the image must
// fit inside the input.
func CarveFromLoader(loader []byte) (*CarveResult, error) {
	sig := make([]byte, 8)
	binary.LittleEndian.PutUint32(sig[0:4], CarveSigSP)
	binary.LittleEndian.PutUint32(sig[4:8], CarveSigReset)

	first := bytes.Index(loader, sig)
	if first < 0 {
		return nil, ErrNoSignature
	}
	if bytes.Index(loader[first+1:], sig) >= 0 {
		return nil, ErrAmbiguous
	}
	if first+CarveEndAddrOff+4 > len(loader) {
		return nil, fmt.Errorf("signature at 0x%x but the loader is too short to hold the length word", first)
	}
	end := binary.LittleEndian.Uint32(loader[first+CarveEndAddrOff:])
	if end <= CarveLoadAddr {
		return nil, fmt.Errorf("end address 0x%08x is not above the load address 0x%08x", end, CarveLoadAddr)
	}
	size := int(end - CarveLoadAddr)
	if first+size > len(loader) {
		return nil, fmt.Errorf("image claims %d bytes but only %d remain after the signature", size, len(loader)-first)
	}
	// Anything wildly off the known length is a different image, not this
	// one. Allow a little slack so a future vendor build still carves.
	if size < CarveExpectedLen/2 || size > CarveExpectedLen*2 {
		return nil, fmt.Errorf("image length %d bytes is not plausible (expected about %d)", size, CarveExpectedLen)
	}
	img := make([]byte, size)
	copy(img, loader[first:first+size])
	return &CarveResult{Offset: first, Size: size, Image: img}, nil
}

// CarveFromLoaderFile is CarveFromLoader over a file on disk.
func CarveFromLoaderFile(path string) (*CarveResult, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return CarveFromLoader(b)
}
