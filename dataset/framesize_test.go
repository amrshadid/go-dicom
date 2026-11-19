package dataset_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// framedDataset builds a data set of the given image whose one frame is the
// given codestream, under the given transfer syntax.
func framedDataset(syntax string, rows, cols, samples, bitsAllocated int, frame []byte) *dataset.Dataset {
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
	addUS(0x0100, bitsAllocated)
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

// bytesAllocatedBy reports how many bytes fn allocated, as the JPEG 2000
// package's allocation tests measure it.
func bytesAllocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// everyDecodePath runs each way a data set's frame is decoded, and returns
// what each said: decoded pixel data, a frame decoded alone (DICOMweb frames),
// a frame rendered (rendering, convert, DICOMweb rendered) and the data set
// decompressed in place (decompress, redaction, a send that transcodes,
// DICOMweb retrieve). A path that decoded the frame reports nil.
func everyDecodePath(t *testing.T, ds *dataset.Dataset, frame []byte) map[string]error {
	t.Helper()
	errs := map[string]error{}
	_, errs["DecodedPixelData"] = ds.DecodedPixelData()
	_, errs["DecodeFrame"] = ds.DecodeFrame(frame)
	_, errs["RenderFrame"] = ds.RenderFrame(0, nil)
	errs["DecompressInPlace"] = ds.DecompressInPlace()
	return errs
}

// TestFrameOfAnotherImageIsRefused proves that a frame whose codestream is
// not the image the data set describes is refused, by every path that decodes
// one, naming both: its columns, rows and components and the data set's.
//
// The frames are the dcmtk and OpenJPEG codestreams of
// compress/testdata/samplewidth, 41 columns by 37 rows (19 by 21 in color),
// one for each codec that states a size in its header. Each data set changes
// only Rows, Columns or Samples per Pixel. The transposed one and the one with
// a sample a pixel and three times the columns are the image's length exactly,
// so nothing but the size in the header can tell them from the right image:
// before this they decoded, and the samples were laid out as a picture the
// frame is not. The frames larger than their data set's image were decoded at
// their own size and kept whole.
func TestFrameOfAnotherImageIsRefused(t *testing.T) {
	codestreams := []struct {
		syntax  string
		stream  string
		samples int
	}{
		{"1.2.840.10008.1.2.4.80", "mono8_jpegls.jls", 1},
		{"1.2.840.10008.1.2.4.81", "mono8_jpegls_near.jls", 1},
		{"1.2.840.10008.1.2.4.80", "rgb8_jpegls.jls", 3},
		{"1.2.840.10008.1.2.4.70", "mono8_sv1.jpg", 1},
		{"1.2.840.10008.1.2.4.57", "rgb8_sv1.jpg", 3},
		{"1.2.840.10008.1.2.4.50", "mono8_baseline.jpg", 1},
		{"1.2.840.10008.1.2.4.51", "mono8_extended.jpg", 1},
		{"1.2.840.10008.1.2.4.90", "mono8_j2k.j2k", 1},
	}
	dir := filepath.Join("..", "compress", "testdata", "samplewidth")
	for _, c := range codestreams {
		frame, err := os.ReadFile(filepath.Join(dir, c.stream))
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		// The frames are 41 columns by 37 rows, except the color ones, 19 by 21.
		rows, cols := 37, 41
		if c.samples == 3 {
			rows, cols = 21, 19
		}
		images := []struct {
			name                   string
			rows, columns, samples int
		}{
			{"transposed", cols, rows, c.samples},
			{"fewer rows", rows - 1, cols, c.samples},
			{"more columns", rows, cols + 1, c.samples},
		}
		if c.samples == 3 {
			images = append(images, struct {
				name                   string
				rows, columns, samples int
			}{"one sample a pixel, three times the columns", rows, cols * 3, 1})
		} else {
			images = append(images, struct {
				name                   string
				rows, columns, samples int
			}{"three samples a pixel", rows, cols, 3})
		}
		for _, img := range images {
			t.Run(c.stream+" "+img.name, func(t *testing.T) {
				header := fmt.Sprintf("its codestream is %dx%d with %d", cols, rows, c.samples)
				declared := fmt.Sprintf("Columns, Rows and Samples per Pixel are %d, %d and %d",
					img.columns, img.rows, img.samples)
				ds := framedDataset(c.syntax, img.rows, img.columns, img.samples, 16, frame)
				for path, err := range everyDecodePath(t, ds, frame) {
					if !errors.Is(err, compress.ErrFrameMismatch) {
						t.Errorf("%s: %v; want the frame refused as another image", path, err)
						continue
					}
					if !strings.Contains(err.Error(), header) || !strings.Contains(err.Error(), declared) {
						t.Errorf("%s: %q does not name %q and %q", path, err, header, declared)
					}
				}
			})
		}
	}
}

// TestLargeFrameInASmallImageCostsTheSmallImage proves that a 4096x4096
// JPEG 2000 frame in a data set of 64x64 is refused before it is decoded, so
// what the file costs is held to the image its data set declares.
//
// black4096.j2k is opj_compress's codestream of a black 4096x4096 image at 16
// bits, 195 bytes. In such a data set it made a file of under 2 KB that
// DecompressInPlace decoded to 32 MiB (300 MB resident) and wrote out under
// Rows and Columns of 64. A JPEG 2000 codestream can describe a large image in a
// few hundred bytes honestly, so the codec cannot tell this frame from a real
// one; the data set can.
func TestLargeFrameInASmallImageCostsTheSmallImage(t *testing.T) {
	frame, err := os.ReadFile(filepath.Join("testdata", "framesize", "black4096.j2k"))
	if err != nil {
		t.Fatal(err)
	}
	ds := framedDataset("1.2.840.10008.1.2.4.90", 64, 64, 1, 16, frame)
	var decodeErr error
	allocated := bytesAllocatedBy(func() { _, decodeErr = ds.DecodedPixelData() })
	if !errors.Is(decodeErr, compress.ErrFrameMismatch) ||
		!strings.Contains(decodeErr.Error(), "4096x4096") {
		t.Fatalf("DecodedPixelData: %v; want the 4096x4096 frame refused", decodeErr)
	}
	if allocated > 1<<20 {
		t.Errorf("refusing the frame allocated %d bytes; the decode it replaces allocates hundreds of megabytes",
			allocated)
	}
	t.Logf("refused with %d bytes allocated", allocated)
}

// TestRLEFrameIsHeldToRowsTimesColumns proves that an RLE frame, which states
// no size of its own, is held to Rows times Columns by the length of its
// segments, and that a segment longer than that is refused before it is
// decoded to the end: 16 million pixels of zeros, 262 KB coded, in a data set
// of 64x64.
func TestRLEFrameIsHeldToRowsTimesColumns(t *testing.T) {
	big, err := compress.NewRLECompressor().CompressFrame(make([]byte, 4096*4096), 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	ds := framedDataset("1.2.840.10008.1.2.5", 64, 64, 1, 8, big)
	var decodeErr error
	allocated := bytesAllocatedBy(func() { _, decodeErr = ds.DecodedPixelData() })
	if !errors.Is(decodeErr, compress.ErrFrameMismatch) ||
		!strings.Contains(decodeErr.Error(), "Rows times Columns is 4096") {
		t.Fatalf("DecodedPixelData: %v; want the frame refused for its segment's length", decodeErr)
	}
	// The 262 KB frame is copied out of Pixel Data on the way, more than once;
	// what must not be allocated is the 16 MiB it expands to.
	if allocated > 4<<20 {
		t.Errorf("refusing the frame allocated %d bytes", allocated)
	}

	// A frame of fewer pixels than the image is refused in the same words.
	small, err := compress.NewRLECompressor().CompressFrame(make([]byte, 60), 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	ds = framedDataset("1.2.840.10008.1.2.5", 8, 8, 1, 8, small)
	if _, err := ds.DecodedPixelData(); !errors.Is(err, compress.ErrFrameMismatch) ||
		!strings.Contains(err.Error(), "holds 60 bytes") {
		t.Errorf("a 60-pixel frame in an 8x8 image: %v", err)
	}
}

// TestRLESegmentPaddedAsALiteralDecodes proves that a segment of an odd
// number of pixels that decodes to one byte more, its pad byte written as a
// literal (PS3.5 G.3.1 pads every segment to an even length), still decodes,
// to the image without the pad, and that two bytes more is refused. pydicom
// accepts the first with a warning.
func TestRLESegmentPaddedAsALiteralDecodes(t *testing.T) {
	segment := func(literal []byte) []byte {
		frame := make([]byte, 64)
		binary.LittleEndian.PutUint32(frame[0:], 1)
		binary.LittleEndian.PutUint32(frame[4:], 64)
		frame = append(frame, byte(len(literal)-1))
		return append(frame, literal...)
	}
	pixels := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
	ds := framedDataset("1.2.840.10008.1.2.5", 3, 3, 1, 8, segment(append(pixels, 0)))
	got, err := ds.DecodedPixelData()
	if err != nil || !bytes.Equal(got, pixels) {
		t.Errorf("a 3x3 segment with its pad as a literal: %v, %v; want %v", got, err, pixels)
	}
	ds = framedDataset("1.2.840.10008.1.2.5", 3, 3, 1, 8, segment(append(pixels, 0, 0)))
	if _, err := ds.DecodedPixelData(); !errors.Is(err, compress.ErrFrameMismatch) {
		t.Errorf("a 3x3 segment of 11 bytes: %v; want it refused", err)
	}
}

// longDecoder hands back a frame of a byte more than the one it decodes, as a
// caller's decoder might for a frame of another image, and has no FrameHeader.
type longDecoder struct{ plainDecoder }

func (l longDecoder) Decompress(data []byte) ([]byte, error) {
	out, err := l.plainDecoder.Decompress(data)
	return append(out, 0), err
}

// TestLongFrameIsAnError proves that a decoded frame longer than the image is
// refused, as a shorter one is: this is what holds a decoder registered
// without FrameHeader to the data set's image.
func TestLongFrameIsAnError(t *testing.T) {
	frame, err := os.ReadFile(filepath.Join("..", "compress", "testdata", "samplewidth", "mono8_jpegls.jls"))
	if err != nil {
		t.Fatal(err)
	}
	registry := compress.GetExternalRegistry()
	if err := registry.RegisterExternalDecoder(compress.JPEG_LS,
		longDecoder{plainDecoder{compress.NewJPEGLSDecompressor()}}); err != nil {
		t.Fatalf("RegisterExternalDecoder: %v", err)
	}
	defer func() { _ = registry.RegisterExternalDecoder(compress.JPEG_LS, compress.NewJPEGLSDecompressor()) }()

	ds := framedDataset("1.2.840.10008.1.2.4.80", 37, 41, 1, 8, frame)
	if got, err := ds.DecodedPixelData(); err == nil ||
		!strings.Contains(err.Error(), "decodes to 1518 bytes and the image needs 1517") {
		t.Errorf("DecodedPixelData: %d bytes, %v; want the long frame refused", len(got), err)
	}
}
