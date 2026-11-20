package anonymize_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// TestDetectingTextFromTheFileFindsWhatDetectingItInMemoryFinds: for every
// image of pydicom's corpus, the regions a detector finds reading the frames
// from the file, one at a time, are those it finds in the image held in
// memory, with nothing filled and with regions already filled in the data set
// (CleanPixelData), which the detector then sees filled in both. Without the
// corpus (GODICOM_PYDICOM_DATA) it is skipped.
func TestDetectingTextFromTheFileFindsWhatDetectingItInMemoryFinds(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	entries, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	filled := []dataset.PixelRegion{{X: 0, Y: 0, Width: 30, Height: 12}, {X: 40, Y: 50, Width: 30, Height: 30}}
	compared, found := 0, 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(corpus, entry.Name())
		for _, regions := range [][]dataset.PixelRegion{nil, filled} {
			held, err := readDataset(t, path)
			if err != nil {
				continue
			}
			if _, ok := held.Get(tagPixelData); !ok {
				continue
			}
			if len(regions) > 0 && a.CleanPixelData(held, regions...) != nil {
				continue
			}
			want, wantErr := anonymize.DetectBurnedInText(held, nil)

			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
				filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: math.MaxUint32})
			if err != nil || df.Deflated || df.PixelDataElement() == nil {
				_ = f.Close()
				continue
			}
			inFile, err := df.GetDatasetWithValuesInFile()
			frames, framesErr := df.FrameReader(f)
			if err != nil || framesErr != nil {
				_ = f.Close()
				continue
			}
			if len(regions) > 0 {
				if err := a.CleanPixelData(inFile, regions...); err != nil {
					t.Errorf("%s: %v", entry.Name(), err)
					_ = f.Close()
					continue
				}
			}
			got, gotErr := anonymize.DetectBurnedInTextFrom(inFile, frames, nil)
			_ = f.Close()
			if (gotErr == nil) != (wantErr == nil) {
				t.Errorf("%s: from the file %v; in memory %v", entry.Name(), gotErr, wantErr)
				continue
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("%s, %d regions filled: from the file %v; in memory %v", entry.Name(), len(regions), got, want)
			}
			compared++
			found += len(want)
		}
	}
	if compared == 0 {
		t.Fatal("no image was compared")
	}
	t.Logf("%d images compared, %d regions found in them", compared, found)
}
