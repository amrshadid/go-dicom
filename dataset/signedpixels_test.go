package dataset_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// sampleDataset is one image of a single 16-bit sample, with Bits Stored and
// Pixel Representation as given.
func sampleDataset(stored int, representation uint16, raw uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")

	addUS := func(group, element, v uint16) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		_ = ds.Add(dataelem.NewDataElement(tag.New(group, element), dataelem.US, b))
	}
	addUS(0x0028, 0x0002, 1)
	addUS(0x0028, 0x0010, 1)
	addUS(0x0028, 0x0011, 1)
	addUS(0x0028, 0x0100, 16)
	addUS(0x0028, 0x0101, uint16(stored))
	addUS(0x0028, 0x0102, uint16(stored-1))
	addUS(0x0028, 0x0103, representation)

	px := make([]byte, 2)
	binary.LittleEndian.PutUint16(px, raw)
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, px))
	return ds
}

// TestPixelArrayInterpretedSignExtendsFromBitsStored is the case that decides
// whether this was implemented or merely typed.
//
// The sign bit sits at Bits Stored, not at Bits Allocated. A 13-bit sample in a
// 16-bit word stored as 0x1830 is -2000; a Go conversion of the whole word,
// int16(0x1830), gives 6192 and is wrong. Both readings agree whenever Bits
// Stored equals Bits Allocated, which is most files — so a test built only on
// those would pass either way.
func TestPixelArrayInterpretedSignExtendsFromBitsStored(t *testing.T) {
	for _, c := range []struct {
		name   string
		stored int
		raw    uint16
		want   int16
	}{
		// 13 significant bits: bit 12 is the sign, and the word's upper bits
		// are not part of the number.
		{"13 bits, negative", 13, 0x1830, -2000},
		{"13 bits, positive", 13, 0x0BB8, 3000},
		{"13 bits, the most negative", 13, 0x1000, -4096},
		{"13 bits, the largest positive", 13, 0x0FFF, 4095},
		// Where the widths agree, the plain reading is also correct.
		{"16 bits, negative", 16, 0xF830, -2000},
		{"16 bits, positive", 16, 0x0BB8, 3000},
	} {
		t.Run(c.name, func(t *testing.T) {
			arr, err := sampleDataset(c.stored, 1, c.raw).PixelArrayInterpreted()
			if err != nil {
				t.Fatal(err)
			}
			samples, ok := arr.([][][]int16)
			if !ok {
				t.Fatalf("returned a %T, want [][][]int16", arr)
			}
			if got := samples[0][0][0]; got != c.want {
				t.Fatalf("stored 0x%04X with %d bits stored reads as %d, want %d",
					c.raw, c.stored, got, c.want)
			}
		})
	}
}

// TestPixelArrayInterpretedLeavesUnsignedAlone checks the promise that makes the
// call safe to use everywhere: for unsigned data it is PixelArray, in value and
// in type, so no caller has to branch on signedness.
func TestPixelArrayInterpretedLeavesUnsignedAlone(t *testing.T) {
	ds := sampleDataset(13, 0, 0x1830)

	interpreted, err := ds.PixelArrayInterpreted()
	if err != nil {
		t.Fatal(err)
	}
	samples, ok := interpreted.([][][]uint16)
	if !ok {
		t.Fatalf("returned a %T for unsigned data, want [][][]uint16", interpreted)
	}
	if got := samples[0][0][0]; got != 0x1830 {
		t.Fatalf("the sample reads as %d, want %d", got, 0x1830)
	}

	plain, err := ds.PixelArray()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.([][][]uint16); !ok {
		t.Fatalf("PixelArray returned a %T; the two calls have diverged", plain)
	}
}

// TestPixelArrayInterpretedAgainstCorpus reads two real signed files and
// requires the numbers pydicom reads.
//
// Both are JPEG 2000, which is deliberate: they exercise the whole chain from
// the codestream to the typed array, and J2K_pixelrep_mismatch.dcm is the file
// whose codestream calls its samples unsigned while the data set calls them
// signed, at 13 bits stored in 16 allocated.
func TestPixelArrayInterpretedAgainstCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for _, c := range []struct {
		file  string
		first int16 // pydicom's first sample
	}{
		{"693_J2KI.dcm", -2016},
		{"J2K_pixelrep_mismatch.dcm", -2000},
	} {
		t.Run(c.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, c.file))
			if err != nil {
				t.Skipf("%s is not in this corpus", c.file)
			}
			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
			if err != nil {
				t.Fatal(err)
			}

			arr, err := df.GetDataset().PixelArrayInterpreted()
			if err != nil {
				t.Fatal(err)
			}
			samples, ok := arr.([][][]int16)
			if !ok {
				t.Fatalf("returned a %T, want [][][]int16", arr)
			}
			if got := samples[0][0][0]; got != c.first {
				t.Fatalf("the first sample is %d, pydicom reads %d", got, c.first)
			}
		})
	}
}
