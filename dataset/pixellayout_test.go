package dataset_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// Three ways pixel data came back wrong, all found by decoding pydicom's whole
// corpus and comparing every sample rather than by any test here.
//
// They share a shape: each produced an image of the right size that looked like
// an image, so nothing downstream could tell. The expectations below are
// pydicom's own decodes, so neither side of the comparison comes from this
// library.

func readLayoutFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/pixellayout/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// flatten reads a file's pixel data and returns the samples as little-endian
// bytes at the given width.
func flatten(t *testing.T, path string, width int) []byte {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	arr, err := df.GetDataset().PixelArrayBySample()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	var out []byte
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if v.Kind() == reflect.Slice {
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
			return
		}
		var u uint64
		if v.CanInt() {
			u = uint64(v.Int())
		} else {
			u = v.Uint()
		}
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, u)
		out = append(out, buf[:width]...)
	}
	walk(reflect.ValueOf(arr))
	return out
}

// TestGrayscaleJPEGIsNotTripled covers a JPEG decoder that emitted three bytes
// per pixel whatever the image held.
//
// A grayscale frame came back at triple length with every value repeated, and
// the accessors — sizing from SamplesPerPixel — kept the first third. The result
// was the first third of the rows with each pixel smeared across three, at
// exactly the expected size.
//
// pydicom's corpus has no grayscale baseline JPEG, which is why this survived
// every comparison against it.
func TestGrayscaleJPEGIsNotTripled(t *testing.T) {
	stream := readLayoutFixture(t, "gray_baseline.jpg")
	want := readLayoutFixture(t, "gray_baseline_expected.raw")

	got, err := compress.NewJPEGDecompressor().Decompress(stream)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if len(got) == len(want)*3 {
		t.Fatalf("decoded %d bytes for a %d-sample grayscale frame: every pixel was tripled",
			len(got), len(want))
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, want %d", len(got), len(want))
	}

	// Values are compared within the inverse DCT tolerance rather than exactly:
	// T.83 defines the IDCT to an accuracy bound, and Go's implementation is not
	// pillow's. The tripling this test exists for is not a rounding difference —
	// it repeats whole samples, so it fails the length check above and would show
	// here as differences of any size across most of the frame.
	worst, differing := 0, 0
	for i := range got {
		d := int(got[i]) - int(want[i])
		if d < 0 {
			d = -d
		}
		if d > 0 {
			differing++
		}
		if d > worst {
			worst = d
		}
	}
	if worst > 1 {
		t.Errorf("a sample differs from pydicom by %d, more than inverse DCT tolerance", worst)
	}
	if differing*100 > len(got)*10 {
		t.Errorf("%d of %d samples differ (%.1f%%), too many for rounding alone",
			differing, len(got), 100*float64(differing)/float64(len(got)))
	}
}

// TestPlanarConfigurationIsHonored covers color data stored plane by plane.
//
// PlanarConfiguration was a field on PixelDataInfo that nothing ever read from
// the data set, so it was always zero. A color-by-plane image came back with
// its channels scrambled: the first pixel reading as the first three reds.
func TestPlanarConfigurationIsHonored(t *testing.T) {
	want := readLayoutFixture(t, "planar_bigendian_expected.raw")
	got := flatten(t, "testdata/pixellayout/planar_bigendian.dcm", 1)

	if len(got) != len(want) {
		t.Fatalf("decoded %d samples, want %d", len(got), len(want))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("first pixel is %v, pydicom reads %v — the color planes were not interleaved",
			got[:3], want[:3])
	}
}

// TestWideBigEndianSamplesAreFullySwapped covers 32-bit pixel data in an
// Explicit VR Big Endian file.
//
// Byte order is reversed on read according to the VR, and pixel data is OW —
// two-byte words. With BitsAllocated of 32 each sample had its halves swapped
// but not its whole width, so an RT Dose of 1249000 read back as 250085395:
// plausible-looking numbers, uniformly wrong.
func TestWideBigEndianSamplesAreFullySwapped(t *testing.T) {
	want := readLayoutFixture(t, "dose_bigendian32_expected.raw")
	got := flatten(t, "testdata/pixellayout/dose_bigendian32.dcm", 4)

	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, want %d", len(got), len(want))
	}
	if !bytes.Equal(got, want) {
		g := binary.LittleEndian.Uint32(got)
		w := binary.LittleEndian.Uint32(want)
		t.Fatalf("first dose value is %d, pydicom reads %d", g, w)
	}
}

// TestPhotometricInterpretationDecidesColorSpace checks a YBR instance keeps
// its color space.
//
// Photometric Interpretation describes the samples as stored. Converting a
// YBR_FULL frame to RGB while the attribute still says YBR leaves a reader to
// apply the conversion a second time, and the values no longer match what
// pydicom or any other reader would report.
func TestPhotometricInterpretationDecidesColorSpace(t *testing.T) {
	stream := readLayoutFixture(t, "gray_baseline.jpg")

	// A grayscale frame has no color transform either way, so this only checks
	// the constructor does not disturb it.
	rgb, err := compress.NewJPEGDecompressorForPhotometric("MONOCHROME2").Decompress(stream)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	ybr, err := compress.NewJPEGDecompressorForPhotometric("YBR_FULL").Decompress(stream)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if !bytes.Equal(rgb, ybr) {
		t.Error("the photometric interpretation changed a grayscale frame, which has no color transform")
	}

	// And that the flag is what selects the behavior.
	if d := compress.NewJPEGDecompressorForPhotometric("YBR_FULL_422"); !d.KeepYCbCr {
		t.Error("a YBR photometric interpretation did not keep the YCbCr samples")
	}
	if d := compress.NewJPEGDecompressorForPhotometric("RGB"); d.KeepYCbCr {
		t.Error("an RGB photometric interpretation kept the YCbCr samples")
	}
}
