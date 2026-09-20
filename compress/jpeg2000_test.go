package compress_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// noiseFrame is the 32x32 8-bit codestream the codec package uses as a known
// answer. Reading it from there keeps one copy of the fixture.
func noiseFrame(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("jpeg2000", "testdata", "noise.j2k"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestJPEG2000CanDecompress(t *testing.T) {
	d := compress.NewJPEG2000Decompressor()

	for _, c := range []struct {
		name string
		data []byte
		want bool
	}{
		{"a codestream", []byte{0xFF, 0x4F, 0xFF, 0x51, 0, 0}, true},
		{"a JP2 file", []byte{0, 0, 0, 12, 'j', 'P', ' ', ' ', 0x0D, 0x0A, 0x87, 0x0A}, true},
		{"SOC without SIZ", []byte{0xFF, 0x4F, 0xFF, 0x90}, false},
		{"a JPEG frame", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0}, false},
		{"nothing", nil, false},
		{"two bytes", []byte{0xFF, 0x4F}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := d.CanDecompress(c.data); got != c.want {
				t.Fatalf("CanDecompress is %v, want %v", got, c.want)
			}
		})
	}
}

// TestJPEG2000FrameSignedness is the conversion that makes
// J2K_pixelrep_mismatch.dcm readable: a codestream that codes unsigned samples
// inside a data set that calls them signed.
//
// The fixture here is unsigned 8-bit noise, so roughly half its samples are at
// or above 128 and must come back negative when the data set says signed. The
// bytes do not change — only what they mean — which is what a disagreement
// between the two headers amounts to.
func TestJPEG2000FrameSignedness(t *testing.T) {
	d := compress.NewJPEG2000Decompressor()
	frame := noiseFrame(t)

	unsigned, err := d.DecompressFrame(frame, 8, 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := d.DecompressFrame(frame, 8, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsigned) != 32*32 || len(signed) != len(unsigned) {
		t.Fatalf("decoded %d and %d bytes, want %d", len(unsigned), len(signed), 32*32)
	}

	// At eight bits the two readings are the same bits, so the bytes match and
	// only their interpretation differs.
	negatives := 0
	for i := range unsigned {
		if unsigned[i] != signed[i] {
			t.Fatalf("sample %d differs between the two readings: %d and %d",
				i, unsigned[i], signed[i])
		}
		if int8(signed[i]) < 0 {
			negatives++
		}
	}
	if negatives == 0 {
		t.Fatal("no sample reads as negative, so this fixture proves nothing")
	}

	// Sixteen bits is where the conversion shows: a sample of 200 stored in a
	// 16-bit word is 200 unsigned and -56 signed at eight bits stored.
	wide, err := d.DecompressFrame(frame, 16, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(wide) != 32*32*2 {
		t.Fatalf("decoded %d bytes at 16 bits allocated, want %d", len(wide), 32*32*2)
	}
	for i := range unsigned {
		want := int16(int8(unsigned[i]))
		got := int16(uint16(wide[2*i]) | uint16(wide[2*i+1])<<8)
		if got != want {
			t.Fatalf("sample %d is %d at 16 bits, want %d", i, got, want)
		}
	}
}

// TestJPEG2000FrameRefusesImpossibleLayouts checks the bounds on what a data
// set may ask for. Bits Allocated comes from the file and is not the
// codestream's to agree with.
func TestJPEG2000FrameRefusesImpossibleLayouts(t *testing.T) {
	d := compress.NewJPEG2000Decompressor()
	frame := noiseFrame(t)

	for _, bits := range []int{0, 1, 7, 12, 24, 64} {
		if _, err := d.DecompressFrame(frame, bits, 8, 0); err == nil {
			t.Errorf("accepted Bits Allocated of %d", bits)
		}
	}
	if _, err := d.Decompress(nil); err == nil {
		t.Error("decoded an empty frame")
	}
	if _, err := d.Decompress([]byte{0xFF, 0x4F, 0xFF, 0x51}); err == nil {
		t.Error("decoded a codestream that stops after its first marker")
	}
}

// TestJPEG2000IsRegistered is the wiring: nothing is asked of the caller.
func TestJPEG2000IsRegistered(t *testing.T) {
	registry := compress.GetExternalRegistry()
	if !registry.IsExternalDecoderAvailable(compress.JPEG_2000) {
		t.Fatal("no JPEG 2000 decoder is registered")
	}
	decoder, err := registry.GetExternalDecoder(compress.JPEG_2000)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decoder.(*compress.JPEG2000Decompressor); !ok {
		t.Fatalf("the registered decoder is a %T, not this package's", decoder)
	}
}

// TestRegisteredDecoderTakesPrecedence is the promise made to anyone who had
// already supplied a JPEG 2000 decoder before this package bundled one —
// examples/jpeg2000 shells out to OpenJPEG, and code doing that must keep
// working exactly as it did.
//
// The frame below is a real codestream, so the bundled decoder would decode it
// successfully. That the substitute's answer comes back instead is what says
// registration still wins.
func TestRegisteredDecoderTakesPrecedence(t *testing.T) {
	registry := compress.GetExternalRegistry()
	t.Cleanup(func() {
		_ = registry.RegisterExternalDecoder(compress.JPEG_2000, compress.NewJPEG2000Decompressor())
	})

	frame := noiseFrame(t)
	bundled, err := registry.GetExternalDecoder(compress.JPEG_2000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bundled.Decompress(frame); err != nil {
		t.Fatalf("the bundled decoder cannot decode the fixture: %v", err)
	}

	if err := registry.RegisterExternalDecoder(compress.JPEG_2000, passthroughDecoder{}); err != nil {
		t.Fatal(err)
	}
	decoder, err := registry.GetExternalDecoder(compress.JPEG_2000)
	if err != nil {
		t.Fatal(err)
	}
	out, err := decoder.Decompress(frame)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(frame) {
		t.Fatalf("the registered decoder returned %d bytes, not the %d it was given: "+
			"the bundled decoder answered instead", len(out), len(frame))
	}
}
