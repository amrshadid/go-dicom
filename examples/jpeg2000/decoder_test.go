package jpeg2000_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/examples/jpeg2000"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// The point of this test is not the decoder — openjpeg is the reference
// implementation and does not need verifying here. It is that registering a
// decoder actually makes JPEG 2000 pixel data decode through the ordinary
// accessors.
//
// go-dicom documents that escape hatch and, until this existed, never exercised
// it. A registry that quietly failed to consult its decoder would leave every
// caller who followed the documentation with an error they could not explain.

// TestDecoderMatchesPydicom decodes a codestream and compares every sample with
// pydicom's reading of the same instance.
//
// It is here because the decoder was wrong in a way that looked right: netpbm has
// no signed form, so openjpeg writes signed components with half the range added
// to every sample. Left uncorrected, a signed CT frame comes back uniformly 32768
// too high — every value wrong, the image still looking like an image, and
// nothing short of this comparison noticing.
func TestDecoderMatchesPydicom(t *testing.T) {
	stream, err := os.ReadFile("testdata/signed_16bit.j2k")
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	want, err := os.ReadFile("testdata/signed_16bit_expected.raw")
	if err != nil {
		t.Skipf("expected pixels missing: %v", err)
	}

	dec, err := jpeg2000.NewDecoder()
	if err != nil {
		t.Skipf("openjpeg is not installed: %v", err)
	}

	got, err := dec.Decompress(stream)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d bytes, want %d", len(got), len(want))
	}
	for i := 0; i+1 < len(got); i += 2 {
		g := int16(binary.LittleEndian.Uint16(got[i:]))
		w := int16(binary.LittleEndian.Uint16(want[i:]))
		if g != w {
			t.Fatalf("sample %d = %d, pydicom reads %d", i/2, g, w)
		}
	}
}

// TestRegisteredDecoderIsUsedForJPEG2000 registers the reference decoder and
// reads a JPEG 2000 instance through PixelArrayBySample.
func TestRegisteredDecoderIsUsedForJPEG2000(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("GODICOM_PYDICOM_DATA is not set")
	}

	dec, err := jpeg2000.NewDecoder()
	if err != nil {
		t.Skipf("openjpeg is not installed: %v", err)
	}
	if err := compress.GetExternalRegistry().RegisterExternalDecoder(compress.JPEG_2000, dec); err != nil {
		t.Fatalf("RegisterExternalDecoder: %v", err)
	}

	for _, tc := range []struct {
		file       string
		wantValues int
	}{
		{"693_J2KR.dcm", 512 * 512},
		{"693_J2KI.dcm", 512 * 512},
	} {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(corpus, tc.file)
			f, err := os.Open(path)
			if err != nil {
				t.Skipf("%s is not in the corpus", tc.file)
			}
			defer func() { _ = f.Close() }()

			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatalf("parsing %s: %v", tc.file, err)
			}

			arr, err := df.GetDataset().PixelArrayBySample()
			if err != nil {
				t.Fatalf("a registered JPEG 2000 decoder did not reach the pixel accessor: %v", err)
			}

			var count int
			var walk func(reflect.Value)
			walk = func(v reflect.Value) {
				if v.Kind() == reflect.Slice {
					for i := 0; i < v.Len(); i++ {
						walk(v.Index(i))
					}
					return
				}
				count++
			}
			walk(reflect.ValueOf(arr))

			if count != tc.wantValues {
				t.Errorf("decoded %d samples, want %d", count, tc.wantValues)
			}
		})
	}
}

// TestDecoderRejectsWhatIsNotJPEG2000 checks the framing test, which is what
// keeps a registered decoder from claiming another codec's frames.
func TestDecoderRejectsWhatIsNotJPEG2000(t *testing.T) {
	dec := &jpeg2000.Decoder{}

	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{"raw codestream", []byte{0xFF, 0x4F, 0xFF, 0x51}, true},
		{"JP2 container", append([]byte{0, 0, 0, 12}, []byte("jP  \r\n\x87\n")...), true},
		{"a JPEG", []byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x0B}, false},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dec.CanDecompress(tc.data); got != tc.want {
				t.Errorf("CanDecompress(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
