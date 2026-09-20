package jpeg2000_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"testing"

	"github.com/amrshadid/go-dicom/compress/jpeg2000"
)

// pydicomFrame is what pydicom decodes one fixture to.
//
// The samples are not committed. Five megabytes of arrays in a Go module is
// five megabytes every caller of the library downloads, and a digest is exactly
// as strong. dataset/pydicompixels_table_test.go already keeps the corpus at
// large this way; regenerate this table with scripts/jpeg2000-reference.py.
type pydicomFrame struct {
	file           string
	components     int
	stored         int
	representation int // (0028,0103) Pixel Representation
	samples        int
	digest         string  // sha256 of the samples as int32, little endian
	mean           float64 // for a file no decoder is required to match exactly
}

var pydicomJPEG2000 = []pydicomFrame{
	{"MR_small_jp2klossless.dcm", 1, 16, 1, 4096, "b8d9a6cee6ff2ea9", 518.881348},
	{"693_J2KI.dcm", 1, 14, 1, 262144, "ef7e87ffc722f7f9", -8.322845},
	{"J2K_pixelrep_mismatch.dcm", 1, 13, 1, 262144, "22e412dd68b03a32", -658.436806},
	{"SC_rgb_gdcm_KY.dcm", 3, 8, 0, 30000, "89ad10bbda73de0d", 127.700000},
	{"GDCMJ2K_TextGBR.dcm", 3, 8, 0, 480000, "3afccfd017435a82", 124.435881},
	{"JPEG2000.dcm", 1, 16, 1, 262144, "aaa27310e46c0603", 13.458160},
}

// TestDecodeAgainstPydicom is the gate the whole decoder is built towards: the
// samples this package produces must be the samples pydicom produces.
//
// It is one comparison and it exercises everything. A wrong bit in the MQ
// decoder, a context assigned to the wrong neighbors, a packet length field
// read at the wrong width, a subband placed at the wrong offset, a lifting step
// with its sign reversed: each of them comes out as wrong pixels here, and for
// most of them nowhere else.
//
// A reversible codestream must match digest for digest. An irreversible one is
// not required to: the 9/7 filter is defined in real arithmetic, so two
// conformant decoders can land either side of a half, and ISO 15444-4 grades a
// decoder on how close it comes rather than on equality. For those the mean is
// checked instead — and, because a mean is a weak thing on its own, the 9/7
// filter has its own exact test against OpenJPEG in dwt_test.go.
func TestDecodeAgainstPydicom(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	frames := fixtureFrames(t, dir)

	for _, want := range pydicomJPEG2000 {
		t.Run(want.file, func(t *testing.T) {
			if _, ok := frames[want.file]; !ok {
				t.Skipf("%s is not in this corpus", want.file)
			}
			c := fixtureCodestream(t, dir, want.file)

			img, err := jpeg2000.Decode(c)
			if err != nil {
				t.Fatalf("decoding: %v", err)
			}
			if len(img.Components) != want.components {
				t.Fatalf("decoded %d components, pydicom has %d",
					len(img.Components), want.components)
			}
			if got := img.Width * img.Height * want.components; got != want.samples {
				t.Fatalf("decoded %d samples, pydicom has %d", got, want.samples)
			}

			// In the order pydicom returns them: the components of one pixel
			// together, not one plane after another.
			digest := sha256.New()
			var raw [4]byte
			sum := 0.0
			for pixel := 0; pixel < img.Width*img.Height; pixel++ {
				for comp := 0; comp < want.components; comp++ {
					v := asDICOMStores(img.Components[comp][pixel], img.Signed[comp], want)
					binary.LittleEndian.PutUint32(raw[:], uint32(v))
					digest.Write(raw[:])
					sum += float64(v)
				}
			}

			if mean := sum / float64(want.samples); math.Abs(mean-want.mean) > 0.01 {
				t.Errorf("the samples average %f, pydicom's %f", mean, want.mean)
			}
			if c.CodingFor(0).Wavelet == jpeg2000.Wavelet97Irreversible {
				return // an inexact filter: the mean above is the check
			}
			if got := hex.EncodeToString(digest.Sum(nil))[:16]; got != want.digest {
				t.Fatalf("the samples hash to %s, pydicom's to %s", got, want.digest)
			}
		})
	}
}

// asDICOMStores reads a decoded sample the way the DICOM data set says it
// should be read.
//
// A codestream states its own signedness, and it is allowed to disagree with
// the data set's Pixel Representation — J2K_pixelrep_mismatch.dcm is named for
// doing exactly that, coding unsigned samples that the data set calls signed.
// The data set is the authority, so an unsigned sample is read back as two's
// complement of Bits Stored. Stage 7 moves this into the library, where the
// DICOM attributes are in reach; here it belongs to the comparison.
func asDICOMStores(v int32, codestreamSigned bool, want pydicomFrame) int32 {
	if codestreamSigned || want.representation == 0 || want.stored <= 0 || want.stored > 31 {
		return v
	}
	if v >= 1<<uint(want.stored-1) {
		return v - 1<<uint(want.stored)
	}
	return v
}

// BenchmarkDecode measures a whole frame, which is what a caller pays: tier-2,
// tier-1, dequantization, the inverse wavelet and the level shift together.
func BenchmarkDecode(b *testing.B) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		b.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for _, name := range []string{
		"MR_small_jp2klossless.dcm", // 64x64, 5/3
		"J2K_pixelrep_mismatch.dcm", // 512x512, 5/3
		"JPEG2000.dcm",              // 256x1024, 9/7
		"GDCMJ2K_TextGBR.dcm",       // 400x400 RGB, 16 tiles, 6 layers
	} {
		b.Run(name, func(b *testing.B) {
			c := fixtureCodestream(b, dir, name)
			b.SetBytes(int64(int(c.Width) * int(c.Height) * len(c.Components)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := jpeg2000.Decode(c); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
