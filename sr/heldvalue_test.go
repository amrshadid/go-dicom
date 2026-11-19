package sr_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/internal/heldvalue"
	"github.com/amrshadid/go-dicom/sr"
)

// describeTree writes every modeled field of every item, one line each.
func describeTree(item *sr.ContentItem, depth int, out *strings.Builder) {
	fmt.Fprintf(out, "%s%s %s %v text=%q code=%v num=%+v dt=%q uid=%q pn=%q ref=%+v cont=%q byref=%v\n",
		strings.Repeat(" ", depth), item.ValueType, item.RelationshipType, item.ConceptName, item.Text,
		item.Code, item.Measurement, item.DateTime, item.UID, item.PersonName, item.Reference,
		item.ContinuityOfContent, item.ReferencedContentItem)
	for _, child := range item.Children {
		describeTree(child, depth+1, out)
	}
}

// TestAReportHeldAsGoValuesReadsAsTheFile proves that ReadContentTree and
// FromDataset read the same report from pydicom's SR files as read, and from
// the same data sets rebuilt with string, []string and number values.
//
// Text values and the by-reference ordinals were read only when held as
// []byte, so a report built in memory lost every text and every
// by-reference relationship, and FromDataset read a []string as its first
// value and a string untrimmed (#420).
func TestAReportHeldAsGoValuesReadsAsTheFile(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for _, name := range []string{"test-SR.dcm", "reportsi.dcm", "reportsi_with_empty_number_tags.dcm"} {
		f, err := os.Open(filepath.Join(corpus, name))
		if err != nil {
			t.Skipf("%s is not present: %v", name, err)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
		_ = f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		fromFile, held := df.GetDataset(), heldvalue.AsGoValues(df.GetDataset())

		wantTree, err := sr.ReadContentTree(fromFile)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		gotTree, err := sr.ReadContentTree(held)
		if err != nil {
			t.Fatalf("%s held as Go values: %v", name, err)
		}
		var want, got strings.Builder
		describeTree(wantTree, 0, &want)
		describeTree(gotTree, 0, &got)
		if want.String() != got.String() {
			t.Errorf("%s: the content tree differs.\nfrom the file:\n%s\nheld as Go values:\n%s", name, want.String(), got.String())
		}

		wantReport, err := sr.FromDataset(fromFile)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		gotReport, err := sr.FromDataset(held)
		if err != nil {
			t.Fatalf("%s held as Go values: %v", name, err)
		}
		// CreationTime is when FromDataset ran, so it is left out.
		summary := func(r *sr.StructuredReport) string {
			return fmt.Sprintf("%q %q %q %q %q %q %+v", r.SOPInstanceUID, r.SOPClassUID, r.PatientName,
				r.PatientID, r.StudyInstanceUID, r.SeriesInstanceUID, *r.ReportContent)
		}
		if w, g := summary(wantReport), summary(gotReport); w != g {
			t.Errorf("%s: FromDataset differs.\nfrom the file:     %s\nheld as Go values: %s", name, w, g)
		}
	}
}
