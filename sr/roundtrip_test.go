package sr_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/sr"
	"github.com/amrshadid/go-dicom/tag"
)

// The sr package did not round-trip a document, and the writer the README
// shows wrote files dcmtk refused (#216).

var contentSequence = tag.New(0x0040, 0xA730)

// flatten lists every value under ds by its path, so two trees compare value
// by value rather than by whatever a printed form happens to show.
func flatten(ds *dataset.Dataset, path string, out map[string]string) {
	for _, elem := range ds.ElementsInTagOrder() {
		t, _ := elem.Tag()
		key := fmt.Sprintf("%s/%s", path, t)
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			for i, item := range seq.Items() {
				flatten(item.(*dataset.Dataset), fmt.Sprintf("%s[%d]", key, i), out)
			}
			out[key] = fmt.Sprintf("%d items", seq.Length())
			continue
		}
		out[key] = fmt.Sprintf("%s %x", elem.GetVR(), elem.GetValue())
	}
}

// contentOf flattens the Content Sequence of ds, which is where a tree lives.
func contentOf(ds *dataset.Dataset) map[string]string {
	out := map[string]string{}
	content := dataset.NewDataset()
	if elem, ok := ds.Get(contentSequence); ok {
		_ = content.Add(elem)
	}
	flatten(content, "", out)
	return out
}

// TestContentTreeRoundTripKeepsEverything: written back, a tree lost every
// attribute it did not model, among them the GraphicType and GraphicData of
// an SCOORD, which dcmtk then refused to read.
func TestContentTreeRoundTripKeepsEverything(t *testing.T) {
	root := readTree(t, "test-SR.dcm")
	written := dataset.NewDataset()
	if err := sr.WriteContentTree(written, root); err != nil {
		t.Fatalf("WriteContentTree: %v", err)
	}

	want, got := contentOf(root.Dataset), contentOf(written)
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s: %q, want %q", key, got[key], value)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("%s was written but not read", key)
		}
	}
	if len(want) == 0 {
		t.Fatal("test-SR.dcm has no content")
	}
}

// TestChangedValueIsWrittenFromTheField: keeping what was read must not keep
// a value the caller changed.
func TestChangedValueIsWrittenFromTheField(t *testing.T) {
	root := readTree(t, "test-SR.dcm")
	var text *sr.ContentItem
	root.Walk(func(item *sr.ContentItem) bool {
		if text == nil && item.ValueType == sr.ValueTypeText {
			text = item
		}
		return text == nil
	})
	if text == nil {
		t.Fatal("test-SR.dcm has no TEXT item")
	}
	text.Text = "Changed"

	written := dataset.NewDataset()
	if err := sr.WriteContentTree(written, root); err != nil {
		t.Fatalf("WriteContentTree: %v", err)
	}
	reread, err := sr.ReadContentTree(written)
	if err != nil {
		t.Fatalf("ReadContentTree: %v", err)
	}
	found := false
	reread.Walk(func(item *sr.ContentItem) bool {
		found = found || item.Text == "Changed"
		return true
	})
	if !found {
		t.Error("the changed text was not written")
	}
}

func quickStartReport(sopClass string) *sr.StructuredReport {
	report := sr.NewStructuredReport("2.25.1234567890", sopClass)
	report.PatientName = "Smith^Jane"
	report.PatientID = "P1"
	report.StudyInstanceUID = "2.25.11"
	report.SeriesInstanceUID = "2.25.12"
	report.CreationTime = time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC)
	_ = report.AddFinding(sr.Finding{ID: "F001", Description: "Nodule in right upper lobe",
		Severity: "MAJOR", Location: "Right lung", Confidence: 0.87})
	_ = report.AddFinding(sr.Finding{ID: "F002", Description: "Small effusion",
		Severity: "MINOR", Location: "Left pleura", Confidence: 0.6})
	_ = report.AddObservation(sr.Observation{Code: "NODULE_SIZE", Value: "12", Unit: "mm"})
	_ = report.AddConclusion("Recommend follow-up imaging")
	_ = report.AddConclusion("Compare with prior")
	report.ReportContent.References = append(report.ReportContent.References, sr.ReportReference{
		ReferencedSOPClassUID: "1.2.840.10008.5.1.4.1.1.2", ReferencedSOPInstanceUID: "2.25.13", ReferenceType: "IMAGE",
	})
	return report
}

// TestToDatasetIsAnSRDocument: ToDataset wrote (0002,0000) into the data set,
// the number of findings into Concept Name Code Sequence as IS, the first
// finding into Study Description, and nothing else of the report.
func TestToDatasetIsAnSRDocument(t *testing.T) {
	ds, err := quickStartReport("1.2.840.10008.5.1.4.1.1.88.33").ToDataset()
	if err != nil {
		t.Fatalf("ToDataset: %v", err)
	}
	for _, elem := range ds.GetAll() {
		if t2, _ := elem.Tag(); t2.Group() == 0x0002 {
			t.Errorf("%s is file meta and has no place in the data set", t2)
		}
	}
	for _, required := range []tag.Tag{
		tag.New(0x0008, 0x0060), tag.New(0x0040, 0xA491), tag.New(0x0040, 0xA493),
		tag.New(0x0008, 0x0023), tag.New(0x0008, 0x0033), tag.New(0x0040, 0xA372),
	} {
		if _, ok := ds.Get(required); !ok {
			t.Errorf("%s, which every SR document carries, is missing", required)
		}
	}
	root, err := sr.ReadContentTree(ds)
	if err != nil {
		t.Fatalf("ReadContentTree: %v", err)
	}
	if root.ConceptName == nil || root.ConceptName.Value != "18748-4" {
		t.Errorf("document title %v, want the coded title", root.ConceptName)
	}
	// Two findings, one observation, two conclusions and one reference.
	if len(root.Children) != 6 {
		t.Errorf("%d items under the root, want 6", len(root.Children))
	}
}

// TestWriteSRFileRoundTrip: ReadSRFile returned an empty report whatever the
// file held, and WriteSRFile kept the first of each list.
func TestWriteSRFileRoundTrip(t *testing.T) {
	for _, class := range []string{
		"1.2.840.10008.5.1.4.1.1.88.11", "1.2.840.10008.5.1.4.1.1.88.22", "1.2.840.10008.5.1.4.1.1.88.33",
	} {
		t.Run(class, func(t *testing.T) {
			report := quickStartReport(class)
			path := filepath.Join(t.TempDir(), "report.dcm")
			if err := sr.WriteSRFile(path, report); err != nil {
				t.Fatalf("WriteSRFile: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte("Compare with prior")) {
				t.Error("the second conclusion is not in the file")
			}

			back, err := sr.ReadSRFile(path)
			if err != nil {
				t.Fatalf("ReadSRFile: %v", err)
			}
			if back.PatientName != report.PatientName || back.SOPInstanceUID != report.SOPInstanceUID {
				t.Errorf("read back patient %q, instance %q", back.PatientName, back.SOPInstanceUID)
			}
			got, want := back.ReportContent, report.ReportContent
			if !reflect.DeepEqual(got.Findings, want.Findings) {
				t.Errorf("findings %+v, want %+v", got.Findings, want.Findings)
			}
			if !reflect.DeepEqual(got.Conclusions, want.Conclusions) {
				t.Errorf("conclusions %q, want %q", got.Conclusions, want.Conclusions)
			}
			if len(got.Observations) != 1 || got.Observations[0].Code != "NODULE_SIZE" ||
				got.Observations[0].Value != "12" || got.Observations[0].Unit != "mm" {
				t.Errorf("observations %+v", got.Observations)
			}
			if len(got.References) != 1 || got.References[0].ReferencedSOPInstanceUID != "2.25.13" {
				t.Errorf("references %+v", got.References)
			}
		})
	}
}
