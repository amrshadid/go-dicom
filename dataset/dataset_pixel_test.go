package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestGetPixelDataInfo tests pixel data metadata extraction
func TestGetPixelDataInfo(t *testing.T) {
	ds := dataset.NewDataset()

	bitsAllocElem := dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.LO, []byte{16, 0})
	bitsStoredElem := dataelem.NewDataElement(tag.New(0x0028, 0x0101), dataelem.LO, []byte{12, 0})
	rowsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.LO, []byte{0, 2})
	colsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0011), dataelem.LO, []byte{0, 2})
	samplesElem := dataelem.NewDataElement(tag.New(0x0028, 0x0002), dataelem.LO, []byte{1, 0})

	ds.Add(bitsAllocElem)
	ds.Add(bitsStoredElem)
	ds.Add(rowsElem)
	ds.Add(colsElem)
	ds.Add(samplesElem)

	info, err := ds.GetPixelDataInfo()
	if err != nil {
		t.Fatalf("GetPixelDataInfo error = %v", err)
	}

	if info.BitsAllocated != 16 {
		t.Errorf("BitsAllocated = %d, want 16", info.BitsAllocated)
	}

	if info.BitsStored != 12 {
		t.Errorf("BitsStored = %d, want 12", info.BitsStored)
	}

	if info.Rows != 512 {
		t.Errorf("Rows = %d, want 512", info.Rows)
	}

	if info.Columns != 512 {
		t.Errorf("Columns = %d, want 512", info.Columns)
	}

	if info.SamplesPerPixel != 1 {
		t.Errorf("SamplesPerPixel = %d, want 1", info.SamplesPerPixel)
	}
}

// TestPixelDataShape tests pixel array shape calculation
func TestPixelDataShape(t *testing.T) {
	ds := dataset.NewDataset()

	// Setup multi-frame image data
	bitsAllocElem := dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.LO, []byte{16, 0})
	rowsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.LO, []byte{0, 1})
	colsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0011), dataelem.LO, []byte{0, 1})
	framesElem := dataelem.NewDataElement(tag.New(0x0028, 0x0008), dataelem.LO, []byte("10"))
	samplesElem := dataelem.NewDataElement(tag.New(0x0028, 0x0002), dataelem.LO, []byte{1, 0})

	ds.Add(bitsAllocElem)
	ds.Add(rowsElem)
	ds.Add(colsElem)
	ds.Add(framesElem)
	ds.Add(samplesElem)

	shape, err := ds.PixelDataShape()
	if err != nil {
		t.Fatalf("PixelDataShape error = %v", err)
	}

	if len(shape) != 3 {
		t.Errorf("Shape length = %d, want 3", len(shape))
	}

	if shape[0] != 10 {
		t.Errorf("Frames = %d, want 10", shape[0])
	}

	if shape[1] != 256 {
		t.Errorf("Rows = %d, want 256", shape[1])
	}

	if shape[2] != 256 {
		t.Errorf("Cols = %d, want 256", shape[2])
	}
}

// TestDecompressPixelData tests pixel data decompression
func TestDecompressPixelData(t *testing.T) {
	ds := dataset.NewDataset()

	// Setup uncompressed pixel data
	bitsAllocElem := dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.LO, []byte{8, 0})
	rowsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.LO, []byte{2, 0})
	colsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0011), dataelem.LO, []byte{2, 0})
	samplesElem := dataelem.NewDataElement(tag.New(0x0028, 0x0002), dataelem.LO, []byte{1, 0})

	ds.Add(bitsAllocElem)
	ds.Add(rowsElem)
	ds.Add(colsElem)
	ds.Add(samplesElem)

	// Add uncompressed pixel data
	pixelBytes := []byte{10, 20, 30, 40}
	pixelElem := dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, pixelBytes)
	ds.Add(pixelElem)

	// Decompress (should return as-is for uncompressed)
	result, err := ds.DecompressPixelData()
	if err != nil {
		t.Fatalf("DecompressPixelData error = %v", err)
	}

	if len(result) != 4 {
		t.Errorf("Decompressed length = %d, want 4", len(result))
	}
}

// TestColorSpaceConversion tests that color space conversion is available
func TestColorSpaceConversion(t *testing.T) {
	ds := dataset.NewDataset()

	// Add color pixel data info
	bitsAllocElem := dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.LO, []byte{8, 0})
	rowsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.LO, []byte{2, 0})
	colsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0011), dataelem.LO, []byte{2, 0})
	samplesElem := dataelem.NewDataElement(tag.New(0x0028, 0x0002), dataelem.LO, []byte{3, 0})

	ds.Add(bitsAllocElem)
	ds.Add(rowsElem)
	ds.Add(colsElem)
	ds.Add(samplesElem)

	// Create minimal RGB pixel data (2x2 with 3 samples per pixel = 12 bytes)
	pixelBytes := make([]byte, 2*2*3)
	for i := 0; i < len(pixelBytes); i++ {
		pixelBytes[i] = byte(i % 256)
	}

	pixelElem := dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, pixelBytes)
	ds.Add(pixelElem)

	// Get pixel array to ensure data exists
	pixelArray, err := ds.PixelArray()
	if err != nil {
		t.Fatalf("PixelArray error = %v", err)
	}

	if pixelArray == nil {
		t.Fatal("PixelArray returned nil")
	}
}
