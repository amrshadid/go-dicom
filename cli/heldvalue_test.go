package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/fileset"
	"github.com/amrshadid/go-dicom/internal/heldvalue"
	"github.com/amrshadid/go-dicom/tag"
)

// TestCommandsReadAValueHeldAsAGoValueAsTheFile proves that the value readers
// the commands share answer the same for pydicom's corpus files as read and
// for the same data sets rebuilt with string, []string and number values:
// text and numbers for nifti, sr and anonymize, the bytes create copies, the
// non-ASCII test that decides create's character set, dicomweb's value
// column, worklist's answer lines and the dicomdir record keys.
//
// Each asserted []byte (or knew []byte and string), so a value held any other
// way read as absent, or as Go's %v of it (#420).
func TestCommandsReadAValueHeldAsAGoValueAsTheFile(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for _, name := range []string{"CT_small.dcm", "MR_small.dcm", "test-SR.dcm", "rtplan.dcm"} {
		f, err := os.Open(filepath.Join(corpus, name))
		if err != nil {
			t.Skipf("%s is not present: %v", name, err)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
		_ = f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		file := df.GetDataset()
		// Text outside ASCII, which create's character set test looks for.
		_ = file.Add(dataelem.NewDataElement(tag.New(0x0010, 0x4000), dataelem.LT, []byte("Gérard ")))
		held := heldvalue.AsGoValues(file)

		for _, elem := range file.GetAll() {
			tg, _ := elem.Tag()
			heldElem, _ := held.Get(tg)
			for what, pair := range map[string][2]any{
				"niftiString":       {niftiString(file, tg), niftiString(held, tg)},
				"niftiUint16Value":  {niftiUint16Value(file, tg), niftiUint16Value(held, tg)},
				"dsString (sr)":     {dsString(file, tg), dsString(held, tg)},
				"datasetText":       {datasetText(file, tg), datasetText(held, tg)},
				"rawBytes (create)": {string(rawBytes(file, tg)), string(rawBytes(held, tg))},
				"webValue":          {webValue(elem), webValue(heldElem)},
			} {
				if pair[0] != pair[1] {
					t.Errorf("%s %s %s: %q from the file, %q held as a Go value", name, tg, what, pair[0], pair[1])
				}
			}
		}

		if want, got := nonASCIIText(file), nonASCIIText(held); want != got || !want {
			t.Errorf("%s nonASCIIText: %v from the file, %v held as Go values; want true for both", name, want, got)
		}

		var want, got bytes.Buffer
		printDataSet(&want, file, "")
		printDataSet(&got, held, "")
		if want.String() != got.String() {
			t.Errorf("%s worklist lines differ:\nfrom the file:\n%s\nheld as Go values:\n%s", name, want.String(), got.String())
		}

		reached := 0
		wantRecord := describeRecord(&fileset.Record{Type: "IMAGE", Dataset: file}, &reached)
		gotRecord := describeRecord(&fileset.Record{Type: "IMAGE", Dataset: held}, &reached)
		if !reflect.DeepEqual(wantRecord, gotRecord) {
			t.Errorf("%s dicomdir record keys: %d from the file, %d held as Go values",
				name, len(wantRecord.Attributes), len(gotRecord.Attributes))
		}
	}
}
