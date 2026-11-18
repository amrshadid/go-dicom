package dataset_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// captureLog collects what the library logs while fn runs. The library's logger
// is global, so the tests using this do not run in parallel.
func captureLog(fn func()) string {
	var buf bytes.Buffer
	saved := config.Logger
	config.SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	defer config.SetLogger(saved)
	fn()
	return buf.String()
}

// lutDataVRs walks a data set and its sequence items and reports the VR of each
// LUT Data (0028,3006) it finds.
func lutDataVRs(ds *dataset.Dataset, into map[dataelem.VR]int) {
	for _, elem := range ds.GetAll() {
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			for _, item := range seq.Items() {
				if inner, ok := item.(*dataset.Dataset); ok {
					lutDataVRs(inner, into)
				}
			}
			continue
		}
		if elem.MustTag() == tag.New(0x0028, 0x3006) {
			into[elem.GetVR()]++
		}
	}
}

// TestReadingLUTDataIsNotReportedAsAVRMismatch proves that reading the files
// with Modality, VOI and Presentation LUTs, whose LUT Data is US in some and OW
// in others, logs no VR mismatch. Before, each LUT Data logged "VR mismatch for
// (0028,3006): expected US or OW, got ..." whatever its VR.
func TestReadingLUTDataIsNotReportedAsAVRMismatch(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "lut", "*.dcm"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no LUT test files: %v", err)
	}
	seen := map[dataelem.VR]int{}
	for _, path := range files {
		var ds *dataset.Dataset
		logged := captureLog(func() {
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			ds = df.GetDataset()
		})
		if strings.Contains(logged, "VR mismatch") {
			t.Errorf("%s logged:\n%s", filepath.Base(path), logged)
		}
		lutDataVRs(ds, seen)
	}
	if seen["US"] == 0 || seen["OW"] == 0 {
		t.Errorf("the files hold LUT Data as %v; the test needs it as both US and OW", seen)
	}
}

// TestAddingMultiVRElementsLogsNoMismatch proves the check made when an element
// is added accepts every VR the dictionary offers for LUT Data and Pixel Data,
// and still reports a VR it does not offer. (Gray Lookup Table Data is retired,
// which the check reports before it looks at the VR.)
func TestAddingMultiVRElementsLogsNoMismatch(t *testing.T) {
	lutData, pixelData := tag.New(0x0028, 0x3006), tag.New(0x7FE0, 0x0010)
	add := func(tg tag.Tag, vr dataelem.VR) string {
		return captureLog(func() {
			_ = dataset.NewDataset().Add(dataelem.NewDataElement(tg, vr, []byte{0, 0}))
		})
	}
	for _, tc := range []struct {
		tg  tag.Tag
		vr  dataelem.VR
		bad bool
	}{
		{lutData, dataelem.US, false}, {lutData, dataelem.OW, false}, {lutData, dataelem.LO, true},
		{pixelData, dataelem.OB, false}, {pixelData, dataelem.OW, false}, {pixelData, dataelem.US, true},
	} {
		got := strings.Contains(add(tc.tg, tc.vr), "VR mismatch")
		if got != tc.bad {
			t.Errorf("%s as %s: mismatch logged = %v, want %v", tc.tg, tc.vr, got, tc.bad)
		}
	}
}
