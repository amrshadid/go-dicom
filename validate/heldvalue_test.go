package validate_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/internal/heldvalue"
	"github.com/amrshadid/go-dicom/validate"
)

// findings lists every finding of a report, "not checked" included, in order.
func findings(r *validate.Report) []string {
	out := make([]string, len(r.Findings))
	for i, f := range r.Findings {
		out[i] = fmt.Sprintf("%s %s %s: %s", f.Severity, f.Path, f.Rule, f.Message)
	}
	return out
}

// TestAValueHeldAsAGoValueIsCheckedAsTheFile proves that a data set built in
// memory with string, []string and number values gets the same report as the
// same data set read from its file, over pydicom's corpus.
//
// isEmpty and text knew []byte and string, and valuesOf the same: a Modality
// or an enumerated value held as a []string, or Rows held as a uint16, read
// as no value, so a present attribute was reported absent or its value
// unchecked (#420).
func TestAValueHeldAsAGoValueIsCheckedAsTheFile(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	compared := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
		if err != nil {
			continue
		}
		fromFile, err := validate.Dataset(df.GetDataset())
		if err != nil {
			continue // refused by name for its SOP class, which TestEveryCorpusFileIsChecked covers
		}
		inMemory, err := validate.Dataset(heldvalue.AsGoValues(df.GetDataset()))
		if err != nil {
			t.Errorf("%s: the file is checked, the data set held as Go values is refused: %v", filepath.Base(path), err)
			continue
		}
		want, got := findings(fromFile), findings(inMemory)
		for i := 0; i < len(want) || i < len(got); i++ {
			var w, g string
			if i < len(want) {
				w = want[i]
			}
			if i < len(got) {
				g = got[i]
			}
			if w != g {
				t.Errorf("%s: the file gives %q, the data set held as Go values %q", filepath.Base(path), w, g)
				break
			}
		}
		compared++
	}
	if compared < 60 {
		t.Errorf("only %d corpus files compared", compared)
	}
}
