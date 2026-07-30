package dataset_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// TestPixelsAgainstWholePydicomCorpus compares every decoded sample of every
// corpus file with pydicom's decode of the same file.
//
// The older corpus test checks the first eight values of a curated list, which
// is why three defects lived through it: color planes not interleaved, 32-bit
// big-endian samples half-swapped, and grayscale JPEG frames emitted at triple
// length. All three produced an image of the right size, and two of them were
// correct for the first eight values.
//
// Lossless syntaxes are compared by digest, so any deviation anywhere fails.
// Lossy ones cannot be — two conforming JPEG decoders need not agree exactly —
// so they are compared on sample count and mean, which a decoder that repeats
// or drops samples cannot satisfy.
func TestPixelsAgainstWholePydicomCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	var compared, skipped int
	for _, want := range pydicomPixelExpectations {
		t.Run(want.file, func(t *testing.T) {
			path := filepath.Join(dir, want.file)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s is not in this corpus", want.file)
			}

			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer func() { _ = f.Close() }()

			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatalf("ReadDICOMFile: %v", err)
			}

			arr, err := df.GetDataset().PixelArrayBySample()
			if err != nil {
				// A syntax with no decoder is a documented limitation, not a
				// failure — but it must not be one pydicom handled silently.
				skipped++
				t.Skipf("no decoder: %v", err)
			}

			samples, width, signed := flattenSamples(arr)
			if len(samples) != want.n {
				t.Fatalf("decoded %d samples, pydicom reads %d", len(samples), want.n)
			}

			var sum float64
			buf := make([]byte, 8)
			digest := sha256.New()
			for _, s := range samples {
				binary.LittleEndian.PutUint64(buf, s)
				_, _ = digest.Write(buf[:width])
				sum += float64(signedValue(s, width, signed))
			}
			mean := sum / float64(len(samples))

			if want.digest != "" {
				if got := hex.EncodeToString(digest.Sum(nil))[:16]; got != want.digest {
					t.Fatalf("samples differ from pydicom's (digest %s, want %s)", got, want.digest)
				}
				compared++
				return
			}

			// Lossy: the mean is what a repeated or dropped sample moves.
			if math.Abs(mean-want.mean) > 1.0 {
				t.Fatalf("mean sample value %.3f, pydicom reads %.3f", mean, want.mean)
			}
			compared++
		})
	}
	t.Logf("compared %d files against pydicom, skipped %d for want of a decoder", compared, skipped)
}

// flattenSamples returns the samples, the byte width of each, and whether the
// accessor gave them as signed.
func flattenSamples(arr interface{}) ([]uint64, int, bool) {
	var out []uint64
	width := 1
	signed := false

	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if v.Kind() == reflect.Slice {
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
			return
		}
		switch v.Kind() {
		case reflect.Uint8, reflect.Int8:
			width = 1
		case reflect.Uint16, reflect.Int16:
			width = 2
		case reflect.Uint32, reflect.Int32:
			width = 4
		default:
			width = 8
		}
		if v.CanInt() {
			signed = true
			out = append(out, uint64(v.Int()))
		} else {
			out = append(out, v.Uint())
		}
	}
	walk(reflect.ValueOf(arr))
	return out, width, signed
}

// signedValue interprets the low width bytes of s, sign-extending only when the
// pixel data is signed.
//
// Doing it unconditionally reads an unsigned 210 as -46, which moves the mean by
// more than any decoding difference would and makes the comparison useless.
func signedValue(s uint64, width int, signed bool) int64 {
	if !signed {
		return int64(s)
	}
	switch width {
	case 1:
		return int64(int8(s))
	case 2:
		return int64(int16(s))
	case 4:
		return int64(int32(s))
	}
	return int64(s)
}
