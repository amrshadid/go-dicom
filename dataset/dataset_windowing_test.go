package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestGetWindowingParameters tests extracting windowing parameters
func TestGetWindowingParameters(t *testing.T) {
	ds := dataset.NewDataset()

	// Add windowing parameters
	centerElem := dataelem.NewDataElement(tag.New(0x0028, 0x1050), dataelem.LO, []byte("40"))
	widthElem := dataelem.NewDataElement(tag.New(0x0028, 0x1051), dataelem.LO, []byte("400"))
	ds.Add(centerElem)
	ds.Add(widthElem)

	params := ds.GetWindowingParameters()

	if params.Center != 40 {
		t.Errorf("Center = %d, want 40", params.Center)
	}
	if params.Width != 400 {
		t.Errorf("Width = %d, want 400", params.Width)
	}
}

// TestGetWindowingParametersDefault tests default windowing parameters
func TestGetWindowingParametersDefault(t *testing.T) {
	ds := dataset.NewDataset()

	params := ds.GetWindowingParameters()

	// Should have default CT windowing
	if params.Center != 40 {
		t.Errorf("Default Center = %d, want 40", params.Center)
	}
	if params.Width != 400 {
		t.Errorf("Default Width = %d, want 400", params.Width)
	}
}

// TestApplyWindowing tests windowing transformation
func TestApplyWindowing(t *testing.T) {
	ds := dataset.NewDataset()

	// Add pixel data info
	bitsAllocElem := dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.LO, []byte{16, 0})
	bitsStoredElem := dataelem.NewDataElement(tag.New(0x0028, 0x0101), dataelem.LO, []byte{16, 0})
	rowsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.LO, []byte{4, 0})
	colsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0011), dataelem.LO, []byte{4, 0})
	samplesElem := dataelem.NewDataElement(tag.New(0x0028, 0x0002), dataelem.LO, []byte{1, 0})

	ds.Add(bitsAllocElem)
	ds.Add(bitsStoredElem)
	ds.Add(rowsElem)
	ds.Add(colsElem)
	ds.Add(samplesElem)

	// Create 4x4x16-bit pixel data
	pixelBytes := make([]byte, 4*4*2)
	for i := 0; i < 32; i += 2 {
		pixelBytes[i] = byte(i / 2)
		pixelBytes[i+1] = 0
	}

	pixelElem := dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, pixelBytes)
	ds.Add(pixelElem)

	// Apply windowing
	result, err := ds.ApplyWindowing(40, 400)
	if err != nil {
		t.Fatalf("ApplyWindowing error = %v", err)
	}

	if len(result) == 0 {
		t.Fatal("ApplyWindowing returned empty result")
	}

	if len(result[0]) != 4 {
		t.Errorf("Result rows = %d, want 4", len(result[0]))
	}

	if len(result[0][0]) != 4 {
		t.Errorf("Result cols = %d, want 4", len(result[0][0]))
	}
}

// TestApplyWindowingWithParams tests windowing with stored parameters
func TestApplyWindowingWithParams(t *testing.T) {
	ds := dataset.NewDataset()

	// Add windowing parameters
	centerElem := dataelem.NewDataElement(tag.New(0x0028, 0x1050), dataelem.LO, []byte("100"))
	widthElem := dataelem.NewDataElement(tag.New(0x0028, 0x1051), dataelem.LO, []byte("300"))
	ds.Add(centerElem)
	ds.Add(widthElem)

	// Add pixel data
	bitsAllocElem := dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.LO, []byte{16, 0})
	rowsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.LO, []byte{2, 0})
	colsElem := dataelem.NewDataElement(tag.New(0x0028, 0x0011), dataelem.LO, []byte{2, 0})
	samplesElem := dataelem.NewDataElement(tag.New(0x0028, 0x0002), dataelem.LO, []byte{1, 0})

	ds.Add(bitsAllocElem)
	ds.Add(rowsElem)
	ds.Add(colsElem)
	ds.Add(samplesElem)

	pixelBytes := make([]byte, 8)
	for i := 0; i < 8; i += 2 {
		pixelBytes[i] = 100
		pixelBytes[i+1] = 0
	}
	pixelElem := dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, pixelBytes)
	ds.Add(pixelElem)

	// Apply windowing with dataset params
	result, err := ds.ApplyWindowingWithParams()
	if err != nil {
		t.Fatalf("ApplyWindowingWithParams error = %v", err)
	}

	if result == nil {
		t.Fatal("ApplyWindowingWithParams returned nil")
	}
}
