package dataset_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// The expectations below come from pydicom, not from this library. They are the
// point of the file: the unit tests elsewhere build fixtures with this library's
// own encoder, so they cannot detect the two halves agreeing on a wrong format —
// which is how every serious defect in this project has hidden. These values
// were produced by reading each file with pydicom and printing
// pixel_array.tobytes(), so a change that breaks conformance fails here even if
// it round-trips internally.
//
// The corpus is not vendored. Set GODICOM_PYDICOM_DATA to pydicom's test_files
// directory to run these; CI does so in the interoperability job.
var pydicomExpectations = []struct {
	file       string
	frames     int
	rows, cols int
	samples    int
	bits       int
	// first8 is the first eight values of frame 0, in pydicom's order.
	first8 []int
}{
	{
		// RLE Lossless, 16-bit grayscale. The file that exposed the decoder
		// returning 8736 bytes where 8192 is correct.
		file: "MR_small_RLE.dcm", frames: 1, rows: 64, cols: 64, samples: 1, bits: 16,
		first8: []int{905, 1019, 1227, 1259, 761, 404, 639, 914},
	},
	{
		// The same image uncompressed. Both must decode to the same pixels,
		// which is a stronger check than either on its own.
		file: "MR_small.dcm", frames: 1, rows: 64, cols: 64, samples: 1, bits: 16,
		first8: []int{905, 1019, 1227, 1259, 761, 404, 639, 914},
	},
	{
		// RLE, 8-bit RGB, two frames. Multi-frame compressed data could not be
		// split at all while the reader discarded the encapsulation structure.
		file: "SC_rgb_rle_2frame.dcm", frames: 2, rows: 100, cols: 100, samples: 3, bits: 8,
		first8: []int{255, 0, 0, 255, 0, 0, 255, 0},
	},
	{
		// Deflated data set, uncompressed pixels.
		file: "image_dfl.dcm", frames: 1, rows: 512, cols: 512, samples: 1, bits: 8,
		first8: []int{213, 213, 213, 213, 213, 213, 213, 213},
	},
	{
		// Explicit VR Big Endian, to keep byte-order normalisation honest.
		file: "MR_small_bigendian.dcm", frames: 1, rows: 64, cols: 64, samples: 1, bits: 16,
		first8: []int{905, 1019, 1227, 1259, 761, 404, 639, 914},
	},
}

// TestAgainstPydicomCorpus decodes real files and compares the result with what
// pydicom reads from the same files.
func TestAgainstPydicomCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for _, tc := range pydicomExpectations {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("%s not present in the corpus", tc.file)
			}

			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer f.Close()

			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatalf("ReadDICOMFile: %v", err)
			}
			ds := df.GetDataset()

			info, err := ds.GetPixelDataInfo()
			if err != nil {
				t.Fatalf("GetPixelDataInfo: %v", err)
			}
			if info.Rows != tc.rows || info.Columns != tc.cols {
				t.Errorf("dimensions %dx%d, want %dx%d", info.Rows, info.Columns, tc.rows, tc.cols)
			}
			if info.SamplesPerPixel != tc.samples {
				t.Errorf("SamplesPerPixel = %d, want %d", info.SamplesPerPixel, tc.samples)
			}
			if info.BitsAllocated != tc.bits {
				t.Errorf("BitsAllocated = %d, want %d", info.BitsAllocated, tc.bits)
			}
			if info.NumberOfFrames != tc.frames {
				t.Errorf("NumberOfFrames = %d, want %d", info.NumberOfFrames, tc.frames)
			}

			pixels, err := ds.PixelArray()
			if err != nil {
				t.Fatalf("PixelArray: %v", err)
			}

			got := firstValues(t, pixels, len(tc.first8))
			for i, want := range tc.first8 {
				if got[i] != want {
					t.Errorf("value %d = %d, want %d (pydicom)\n  got  %v\n  want %v",
						i, got[i], want, got, tc.first8)
					break
				}
			}
		})
	}
}

// firstValues pulls the leading values out of whichever concrete type
// PixelArray returned.
func firstValues(t *testing.T, pixels interface{}, n int) []int {
	t.Helper()

	out := make([]int, 0, n)
	switch v := pixels.(type) {
	case [][][]uint8:
		for _, row := range v[0] {
			for _, s := range row {
				if len(out) == n {
					return out
				}
				out = append(out, int(s))
			}
		}
	case [][][]uint16:
		for _, row := range v[0] {
			for _, s := range row {
				if len(out) == n {
					return out
				}
				out = append(out, int(s))
			}
		}
	case [][][]uint32:
		for _, row := range v[0] {
			for _, s := range row {
				if len(out) == n {
					return out
				}
				out = append(out, int(s))
			}
		}
	default:
		t.Fatalf("unexpected pixel type %T", pixels)
	}
	return out
}
