package dataset_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// lowPrecisionDataset builds a data set of 16 bits allocated and 8 stored
// whose one frame is the given codestream, under the given transfer syntax.
func lowPrecisionDataset(syntax string, rows, cols, samples int, frame []byte) *dataset.Dataset {
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID(syntax)
	addUS := func(element uint16, v int) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	addUS(0x0002, samples)
	addUS(0x0010, rows)
	addUS(0x0011, cols)
	addUS(0x0100, 16)
	addUS(0x0101, 8)
	addUS(0x0102, 7)
	addUS(0x0103, 0)
	photometric := "MONOCHROME2 "
	if samples == 3 {
		photometric = "RGB "
		addUS(0x0006, 0)
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0004), dataelem.CS, []byte(photometric)))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, encapsulate(frame)))
	return ds
}

// TestLowPrecisionFrameInSixteenBitImage proves that DecodedPixelData and
// DecompressInPlace give a 16-bit image whose frames are coded at 8 bits two
// bytes a sample, equal to dcmtk's decode of the same frames, for every
// transfer syntax that has a precision of its own.
//
// The JPEG decoders unpacked such a frame one byte a sample, so
// DecodedPixelData returned half the image with no error, and everything after
// it read those bytes as 16-bit samples. The frames are dcmtk's, coded from
// 8-bit images; see compress/testdata/samplewidth.
func TestLowPrecisionFrameInSixteenBitImage(t *testing.T) {
	cases := []struct {
		syntax     string
		stream     string
		rows, cols int
		samples    int
		tolerance  int
	}{
		{"1.2.840.10008.1.2.4.80", "mono8_jpegls.jls", 37, 41, 1, 0},
		{"1.2.840.10008.1.2.4.81", "mono8_jpegls_near.jls", 37, 41, 1, 0},
		{"1.2.840.10008.1.2.4.80", "rgb8_jpegls.jls", 21, 19, 3, 0},
		{"1.2.840.10008.1.2.4.70", "mono8_sv1.jpg", 37, 41, 1, 0},
		{"1.2.840.10008.1.2.4.57", "rgb8_sv1.jpg", 21, 19, 3, 0},
		{"1.2.840.10008.1.2.4.50", "mono8_baseline.jpg", 37, 41, 1, 1},
		{"1.2.840.10008.1.2.4.51", "mono8_extended.jpg", 37, 41, 1, 1},
		{"1.2.840.10008.1.2.4.90", "mono8_j2k.j2k", 37, 41, 1, 0},
	}
	dir := filepath.Join("..", "compress", "testdata", "samplewidth")
	for _, c := range cases {
		t.Run(c.syntax+" "+c.stream, func(t *testing.T) {
			frame, err := os.ReadFile(filepath.Join(dir, c.stream))
			if err != nil {
				t.Fatalf("read frame: %v", err)
			}
			want, err := os.ReadFile(filepath.Join(dir, c.stream[:strings.LastIndex(c.stream, ".")]+"_expected.raw"))
			if err != nil {
				t.Fatalf("read expected samples: %v", err)
			}
			need := c.rows * c.cols * c.samples * 2

			ds := lowPrecisionDataset(c.syntax, c.rows, c.cols, c.samples, frame)
			got, err := ds.DecodedPixelData()
			if err != nil {
				t.Fatalf("DecodedPixelData: %v", err)
			}
			if len(got) != need {
				t.Fatalf("DecodedPixelData gave %d bytes; the image is %dx%dx%d at 16 bits allocated, %d bytes",
					len(got), c.rows, c.cols, c.samples, need)
			}
			for i, w := range want {
				g := int(binary.LittleEndian.Uint16(got[i*2:]))
				if d := g - int(w); d > c.tolerance || -d > c.tolerance {
					t.Fatalf("sample %d is %d, dcmtk decodes %d", i, g, w)
				}
			}

			if err := ds.DecompressInPlace(); err != nil {
				t.Fatalf("DecompressInPlace: %v", err)
			}
			native, err := ds.DecodedPixelData()
			if err != nil || len(native) != need {
				t.Fatalf("after DecompressInPlace: %d bytes, err %v; want %d", len(native), err, need)
			}
		})
	}
}

// TestRLEOfNarrowSegmentsInSixteenBitImageIsRefused proves that an RLE frame
// with one segment a sample, which is 8-bit data, is refused in a data set of
// 16 bits allocated. RLE has no precision of its own: PS3.5 G.2 counts its
// segments by the bytes of Bits Allocated, so the mismatch is a malformed frame
// and not a narrower one.
func TestRLEOfNarrowSegmentsInSixteenBitImageIsRefused(t *testing.T) {
	samples := make([]byte, 4*4)
	for i := range samples {
		samples[i] = byte(i * 9)
	}
	frame, err := compress.NewRLECompressor().CompressFrame(samples, 1, 8)
	if err != nil {
		t.Fatalf("CompressFrame: %v", err)
	}
	ds := lowPrecisionDataset("1.2.840.10008.1.2.5", 4, 4, 1, frame)
	got, err := ds.DecodedPixelData()
	if err == nil {
		t.Fatalf("DecodedPixelData gave %d bytes; want a refusal", len(got))
	}
	if !strings.Contains(err.Error(), "segments") {
		t.Errorf("the refusal does not name the segments: %v", err)
	}
}

// plainDecoder hides every method but Decompress of the decoder it wraps, as a
// caller's own decoder registered without DecompressFrame would.
type plainDecoder struct{ inner compress.Decompressor }

func (p plainDecoder) Decompress(data []byte) ([]byte, error) { return p.inner.Decompress(data) }
func (p plainDecoder) CanDecompress(data []byte) bool         { return p.inner.CanDecompress(data) }

// TestShortFrameIsAnError proves that a decoded frame shorter than the image
// is an error from DecodedPixelData, not half a picture. A decoder the caller
// registers without DecompressFrame unpacks at the codestream's width, which
// for an 8-bit frame in a 16-bit image is half of it.
func TestShortFrameIsAnError(t *testing.T) {
	registry := compress.GetExternalRegistry()
	if err := registry.RegisterExternalDecoder(compress.JPEG_LS, plainDecoder{compress.NewJPEGLSDecompressor()}); err != nil {
		t.Fatalf("RegisterExternalDecoder: %v", err)
	}
	defer func() { _ = registry.RegisterExternalDecoder(compress.JPEG_LS, compress.NewJPEGLSDecompressor()) }()

	frame, err := os.ReadFile(filepath.Join("..", "compress", "testdata", "samplewidth", "mono8_jpegls.jls"))
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	ds := lowPrecisionDataset("1.2.840.10008.1.2.4.80", 37, 41, 1, frame)
	got, err := ds.DecodedPixelData()
	if err == nil {
		t.Fatalf("DecodedPixelData gave %d bytes of a %d-byte image with no error", len(got), 37*41*2)
	}
	if !strings.Contains(err.Error(), "decodes to 1517 bytes and the image needs 3034") {
		t.Errorf("the error does not give the sizes: %v", err)
	}
}
