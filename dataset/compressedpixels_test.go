package dataset_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// encapsulate wraps compressed frames in the item structure PixelData carries
// under a compressed transfer syntax: an empty Basic Offset Table, then one
// item per frame.
func encapsulate(frames ...[]byte) []byte {
	var buf bytes.Buffer
	item := func(payload []byte) {
		_ = binary.Write(&buf, binary.LittleEndian, uint16(0xFFFE))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(0xE000))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(payload)))
		buf.Write(payload)
	}
	item(nil)
	for _, f := range frames {
		item(f)
	}
	return buf.Bytes()
}

// rleDataset builds a data set holding RLE-compressed pixel data.
func rleDataset(t *testing.T, rows, cols, samples, bits, frameCount int, pixels [][]byte) *dataset.Dataset {
	t.Helper()

	compressed := make([][]byte, 0, len(pixels))
	for i, raw := range pixels {
		frame, err := compress.NewRLECompressor().CompressFrame(raw, samples, bits)
		if err != nil {
			t.Fatalf("CompressFrame(frame %d): %v", i, err)
		}
		compressed = append(compressed, frame)
	}

	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.5") // RLE Lossless

	addUS := func(group, element uint16, v uint16) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		_ = ds.Add(dataelem.NewDataElement(tag.New(group, element), dataelem.US, b))
	}
	addUS(0x0028, 0x0002, uint16(samples)) // SamplesPerPixel
	addUS(0x0028, 0x0010, uint16(rows))    // Rows
	addUS(0x0028, 0x0011, uint16(cols))    // Columns
	addUS(0x0028, 0x0100, uint16(bits))    // BitsAllocated
	addUS(0x0028, 0x0101, uint16(bits))    // BitsStored
	addUS(0x0028, 0x0102, uint16(bits-1))  // HighBit
	addUS(0x0028, 0x0103, 0)               // PixelRepresentation

	// NumberOfFrames is IS, padded to an even length with a trailing space.
	frames := []byte(itoa(frameCount))
	if len(frames)%2 != 0 {
		frames = append(frames, ' ')
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0008), dataelem.IS, frames))

	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, encapsulate(compressed...)))
	return ds
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestPixelArrayDecodesRLE verifies compressed pixel data is decompressed
// rather than handed to the sample parsers as though it were raw.
//
// PixelArray used to read PixelData directly whatever the transfer syntax, so
// every compressed file failed with "insufficient pixel data at frame 0, row N,
// col M" — the parser was walking an RLE stream expecting samples.
func TestPixelArrayDecodesRLE(t *testing.T) {
	const rows, cols = 4, 4

	raw := make([]byte, rows*cols*2)
	for i := 0; i < rows*cols; i++ {
		binary.LittleEndian.PutUint16(raw[i*2:], uint16(1000+i))
	}

	ds := rleDataset(t, rows, cols, 1, 16, 1, [][]byte{raw})

	got, err := ds.PixelArray()
	if err != nil {
		t.Fatalf("PixelArray: %v", err)
	}

	frames, ok := got.([][][]uint16)
	if !ok {
		t.Fatalf("PixelArray returned %T, want [][][]uint16", got)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}

	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			want := uint16(1000 + r*cols + c)
			if frames[0][r][c] != want {
				t.Errorf("pixel (%d,%d) = %d, want %d", r, c, frames[0][r][c], want)
			}
		}
	}
}

// TestPixelArrayDecodesMultiFrameRLE covers more than one compressed frame,
// which is the case that could not work at all while the reader discarded the
// encapsulation structure.
func TestPixelArrayDecodesMultiFrameRLE(t *testing.T) {
	const rows, cols = 2, 2

	frameA := []byte{0x10, 0x11, 0x12, 0x13}
	frameB := []byte{0x20, 0x21, 0x22, 0x23}

	ds := rleDataset(t, rows, cols, 1, 8, 2, [][]byte{frameA, frameB})

	got, err := ds.PixelArray()
	if err != nil {
		t.Fatalf("PixelArray: %v", err)
	}

	frames, ok := got.([][][]uint8)
	if !ok {
		t.Fatalf("PixelArray returned %T, want [][][]uint8", got)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2 — the second frame was dropped", len(frames))
	}

	for f, want := range [][]byte{frameA, frameB} {
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				if got := frames[f][r][c]; got != want[r*cols+c] {
					t.Errorf("frame %d pixel (%d,%d) = %#x, want %#x", f, r, c, got, want[r*cols+c])
				}
			}
		}
	}
}

// TestNumberOfFramesTolerantOfPadding is the regression for the padding bug
// that made multi-frame images look single-frame.
//
// NumberOfFrames is IS, and PS3.5 §6.2 pads a value to an even length with a
// trailing space. "2 " failed strconv.Atoi, so the count silently stayed at its
// default of 1 — and for compressed data that means every frame after the first
// is discarded with no error at all.
func TestNumberOfFramesTolerantOfPadding(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  int
	}{
		{"padded to an even length", "2 ", 2},
		{"already even", "12", 12},
		{"NUL padded", "3\x00", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := dataset.NewDataset()
			addUS := func(group, element, v uint16) {
				b := make([]byte, 2)
				binary.LittleEndian.PutUint16(b, v)
				_ = ds.Add(dataelem.NewDataElement(tag.New(group, element), dataelem.US, b))
			}
			addUS(0x0028, 0x0010, 2)
			addUS(0x0028, 0x0011, 2)
			addUS(0x0028, 0x0100, 8)
			addUS(0x0028, 0x0002, 1)
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0008), dataelem.IS, []byte(tc.value)))

			info, err := ds.GetPixelDataInfo()
			if err != nil {
				t.Fatalf("GetPixelDataInfo: %v", err)
			}
			if info.NumberOfFrames != tc.want {
				t.Errorf("NumberOfFrames = %d, want %d (from %q)", info.NumberOfFrames, tc.want, tc.value)
			}
		})
	}
}

// TestUncompressedPixelDataStillWorks guards the other branch: a data set whose
// transfer syntax is uncompressed must not be routed through a decoder.
func TestUncompressedPixelDataStillWorks(t *testing.T) {
	const rows, cols = 2, 2
	raw := []byte{0x01, 0x02, 0x03, 0x04}

	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	addUS := func(group, element, v uint16) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		_ = ds.Add(dataelem.NewDataElement(tag.New(group, element), dataelem.US, b))
	}
	addUS(0x0028, 0x0010, rows)
	addUS(0x0028, 0x0011, cols)
	addUS(0x0028, 0x0100, 8)
	addUS(0x0028, 0x0002, 1)
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, raw))

	got, err := ds.PixelArray()
	if err != nil {
		t.Fatalf("PixelArray: %v", err)
	}
	frames, ok := got.([][][]uint8)
	if !ok {
		t.Fatalf("PixelArray returned %T, want [][][]uint8", got)
	}
	if frames[0][0][0] != 0x01 || frames[0][1][1] != 0x04 {
		t.Errorf("uncompressed pixels changed: %v", frames[0])
	}
}

// TestPixelArrayBySampleShape verifies color samples get their own dimension,
// which is the shape PixelDataShape has always reported.
//
// PixelArray flattens them into the column dimension, so a 2x2 RGB frame comes
// back from it as 2 rows of 6 values. The values are right and their order is
// right; only the shape disagrees with what the library says elsewhere about
// itself.
func TestPixelArrayBySampleShape(t *testing.T) {
	const rows, cols, samples = 2, 2, 3

	// Pixel (r,c) has samples 10*n, 10*n+1, 10*n+2 where n is its index.
	raw := make([]byte, rows*cols*samples)
	for i := 0; i < rows*cols; i++ {
		for s := 0; s < samples; s++ {
			raw[i*samples+s] = byte(10*i + s)
		}
	}

	ds := rleDataset(t, rows, cols, samples, 8, 1, [][]byte{raw})

	shape, err := ds.PixelDataShape()
	if err != nil {
		t.Fatalf("PixelDataShape: %v", err)
	}
	want := []int{1, rows, cols, samples}
	if len(shape) != len(want) {
		t.Fatalf("PixelDataShape = %v, want %v", shape, want)
	}

	got, err := ds.PixelArrayBySample()
	if err != nil {
		t.Fatalf("PixelArrayBySample: %v", err)
	}
	frames, ok := got.([][][][]uint8)
	if !ok {
		t.Fatalf("PixelArrayBySample returned %T, want [][][][]uint8", got)
	}

	// The array's own dimensions must equal what PixelDataShape promised.
	if len(frames) != shape[0] || len(frames[0]) != shape[1] ||
		len(frames[0][0]) != shape[2] || len(frames[0][0][0]) != shape[3] {
		t.Fatalf("array shape [%d %d %d %d] contradicts PixelDataShape %v",
			len(frames), len(frames[0]), len(frames[0][0]), len(frames[0][0][0]), shape)
	}

	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := r*cols + c
			for s := 0; s < samples; s++ {
				if want := uint8(10*i + s); frames[0][r][c][s] != want {
					t.Errorf("pixel (%d,%d) sample %d = %d, want %d",
						r, c, s, frames[0][r][c][s], want)
				}
			}
		}
	}
}

// TestPixelArrayBySampleGrayscaleUnchanged verifies single-sample data is not
// given a spurious fourth dimension.
func TestPixelArrayBySampleGrayscaleUnchanged(t *testing.T) {
	raw := []byte{0x01, 0x02, 0x03, 0x04}
	ds := rleDataset(t, 2, 2, 1, 8, 1, [][]byte{raw})

	got, err := ds.PixelArrayBySample()
	if err != nil {
		t.Fatalf("PixelArrayBySample: %v", err)
	}
	if _, ok := got.([][][]uint8); !ok {
		t.Errorf("grayscale returned %T, want [][][]uint8 — the same as PixelArray", got)
	}
}
