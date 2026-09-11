package firmware

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// synthLoader builds a fake aicloadfw.Sys: junk, then an image whose
// self-described end address yields imgLen bytes, then more junk.
func synthLoader(imgLen int, copies int) []byte {
	img := make([]byte, imgLen)
	for i := range img {
		img[i] = byte(i * 7)
	}
	binary.LittleEndian.PutUint32(img[0:4], CarveSigSP)
	binary.LittleEndian.PutUint32(img[4:8], CarveSigReset)
	binary.LittleEndian.PutUint32(img[CarveEndAddrOff:], CarveLoadAddr+uint32(imgLen))

	var b bytes.Buffer
	b.Write(bytes.Repeat([]byte{0xAB}, 1000))
	for i := 0; i < copies; i++ {
		b.Write(img)
		b.Write(bytes.Repeat([]byte{0xCD}, 500))
	}
	return b.Bytes()
}

func TestCarve_HappyPath(t *testing.T) {
	loader := synthLoader(CarveExpectedLen, 1)
	res, err := CarveFromLoader(loader)
	if err != nil {
		t.Fatal(err)
	}
	if res.Offset != 1000 {
		t.Fatalf("offset = %d, want 1000", res.Offset)
	}
	if res.Size != CarveExpectedLen || len(res.Image) != CarveExpectedLen {
		t.Fatalf("size = %d, want %d", res.Size, CarveExpectedLen)
	}
	if !bytes.Equal(res.Image, loader[1000:1000+CarveExpectedLen]) {
		t.Fatal("carved bytes differ from the embedded image")
	}
}

func TestCarve_NoSignature(t *testing.T) {
	_, err := CarveFromLoader(bytes.Repeat([]byte{0x00}, 4096))
	if !errors.Is(err, ErrNoSignature) {
		t.Fatalf("err = %v, want ErrNoSignature", err)
	}
}

func TestCarve_Ambiguous(t *testing.T) {
	_, err := CarveFromLoader(synthLoader(CarveExpectedLen, 2))
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("err = %v, want ErrAmbiguous", err)
	}
}

func TestCarve_ImplausibleLength(t *testing.T) {
	// A signature whose length word points far beyond anything sane.
	loader := synthLoader(CarveExpectedLen, 1)
	binary.LittleEndian.PutUint32(loader[1000+CarveEndAddrOff:], CarveLoadAddr+10_000_000)
	if _, err := CarveFromLoader(loader); err == nil {
		t.Fatal("expected an error for an implausible length")
	}
}

func TestCarve_Truncated(t *testing.T) {
	loader := synthLoader(CarveExpectedLen, 1)
	if _, err := CarveFromLoader(loader[:1000+CarveExpectedLen-10]); err == nil {
		t.Fatal("expected an error when the image does not fit")
	}
}
