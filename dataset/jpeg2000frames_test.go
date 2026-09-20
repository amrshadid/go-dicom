package dataset_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestPixelArrayDecodesMultiFrameJPEG2000 decodes a JPEG 2000 image of more
// than one frame.
//
// No file in pydicom's corpus is a multi-frame JPEG 2000 — every one of its
// seven is a single frame — so decoding the corpus says nothing about what
// happens when a second fragment arrives. The frame splitting itself is shared
// with every other encapsulated codec and is exercised by the RLE and JPEG
// tests, but "it is the same code path" is an argument, not a test, and this
// costs two fragments to settle.
//
// The two frames are the same codestream, so each must decode to the same
// samples: a decoder that returned frame 0 twice, or that let state leak from
// one frame into the next, would pass a shape check and fail this one.
func TestPixelArrayDecodesMultiFrameJPEG2000(t *testing.T) {
	// The 32x32 8-bit codestream the codec package keeps as a known answer.
	frame, err := os.ReadFile(filepath.Join("..", "compress", "jpeg2000", "testdata", "noise.j2k"))
	if err != nil {
		t.Skipf("the JPEG 2000 fixture is not available: %v", err)
	}

	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.4.90") // JPEG 2000 Lossless

	addUS := func(group, element, v uint16) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		_ = ds.Add(dataelem.NewDataElement(tag.New(group, element), dataelem.US, b))
	}
	addUS(0x0028, 0x0002, 1)  // SamplesPerPixel
	addUS(0x0028, 0x0010, 32) // Rows
	addUS(0x0028, 0x0011, 32) // Columns
	addUS(0x0028, 0x0100, 8)  // BitsAllocated
	addUS(0x0028, 0x0101, 8)  // BitsStored
	addUS(0x0028, 0x0102, 7)  // HighBit
	addUS(0x0028, 0x0103, 0)  // PixelRepresentation
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0008), dataelem.IS, []byte("2 ")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB,
		encapsulate(frame, frame)))

	arr, err := ds.PixelArrayBySample()
	if err != nil {
		t.Fatalf("PixelArrayBySample: %v", err)
	}
	frames, ok := arr.([][][]uint8)
	if !ok {
		t.Fatalf("decoded a %T, want [][][]uint8", arr)
	}
	if len(frames) != 2 {
		t.Fatalf("decoded %d frames, want 2", len(frames))
	}

	for i, f := range frames {
		if len(f) != 32 || len(f[0]) != 32 {
			t.Fatalf("frame %d is %dx%d, want 32x32", i, len(f), len(f[0]))
		}
	}

	// The same codestream twice: the frames must agree, and must not be blank.
	nonZero := 0
	for row := range frames[0] {
		for col := range frames[0][row] {
			if frames[0][row][col] != frames[1][row][col] {
				t.Fatalf("frames differ at (%d, %d): %d and %d",
					row, col, frames[0][row][col], frames[1][row][col])
			}
			if frames[0][row][col] != 0 {
				nonZero++
			}
		}
	}
	if nonZero < 512 {
		t.Fatalf("only %d of 1024 samples are non-zero, so the frames may not "+
			"have decoded at all", nonZero)
	}
}
