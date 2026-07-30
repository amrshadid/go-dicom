package sr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sr"
)

// A Structured Report is a tree, and the tree is the document. Everything the
// report says lives in the content items, each with a value type saying what it
// carries, a coded concept name saying what it means, and a relationship to its
// parent.
//
// This package could not read one. Its own model — Finding, Observation,
// ReportTemplate — is a different thing: reading pydicom's test-SR.dcm, which
// holds 28 content items, it found no findings, no conclusions, no observations
// and no references. What it wrote was not conformant either, putting the
// number of findings into Concept Name Code Sequence, whose value
// representation is SQ.
//
// The counts and structures below are pydicom's reading of the same files.

func readTree(t *testing.T, name string) *sr.ContentItem {
	t.Helper()

	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	f, err := os.Open(filepath.Join(corpus, name))
	if err != nil {
		t.Skipf("%s is not present: %v", name, err)
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	root, err := sr.ReadContentTree(df.GetDataset())
	if err != nil {
		t.Fatalf("ReadContentTree: %v", err)
	}
	return root
}

// TestContentTreeSizes covers the three SR fixtures against pydicom's counts.
func TestContentTreeSizes(t *testing.T) {
	tests := []struct {
		file  string
		items int
	}{
		{"test-SR.dcm", 29},
		{"reportsi.dcm", 9},
		{"reportsi_with_empty_number_tags.dcm", 9},
	}

	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			root := readTree(t, tc.file)
			if got := root.Count(); got != tc.items {
				t.Errorf("the tree holds %d items, want %d", got, tc.items)
			}
			if root.ValueType != sr.ValueTypeContainer {
				t.Errorf("the root is %s, want CONTAINER", root.ValueType)
			}
			if root.RelationshipType != "" {
				t.Errorf("the root has relationship %q; the root relates to nothing",
					root.RelationshipType)
			}
		})
	}
}

// TestContentValuesAreRead covers each value type the fixture uses.
func TestContentValuesAreRead(t *testing.T) {
	root := readTree(t, "test-SR.dcm")

	var texts, codes, nums, uidrefs int
	var measurement *sr.Measurement
	root.Walk(func(item *sr.ContentItem) bool {
		switch item.ValueType {
		case sr.ValueTypeText:
			if item.Text != "" {
				texts++
			}
		case sr.ValueTypeCode:
			if item.Code != nil && item.Code.Meaning != "" {
				codes++
			}
		case sr.ValueTypeNum:
			if item.Measurement != nil {
				nums++
				measurement = item.Measurement
			}
		case sr.ValueTypeUIDRef:
			if item.UID != "" {
				uidrefs++
			}
		}
		return true
	})

	if texts == 0 {
		t.Error("no TEXT item carried a value")
	}
	if codes == 0 {
		t.Error("no CODE item carried a coded value")
	}
	if uidrefs == 0 {
		t.Error("no UIDREF item carried a UID")
	}
	if nums == 0 {
		t.Fatal("no NUM item carried a measurement")
	}

	// A number without its units is not a measurement. They live one level down
	// from the item, inside Measured Value Sequence.
	if measurement.Value == "" {
		t.Error("a NUM item has no numeric value")
	}
	if measurement.Units == nil || measurement.Units.Value == "" {
		t.Error("a NUM item has no units; the number on its own means nothing")
	}
}

// TestByReferenceRelationships covers the items that carry no value at all.
//
// PS3.3 C.17.3.1 lets an item relate to another by its position rather than by
// repeating its content: it has a relationship type and a Referenced Content
// Item Identifier and nothing else. A reader expecting every item to have a
// value type does not merely miss these, it fails on them — pydicom's own dump
// raises AttributeError on the two in test-SR.dcm.
//
// The identifier's value representation is UL, so the ordinals are four-byte
// binary values rather than the backslash-separated text most multi-valued
// attributes use. Read as text they are unparseable, and the reference comes
// back empty — which is indistinguishable from an item that has no reference.
func TestByReferenceRelationships(t *testing.T) {
	root := readTree(t, "test-SR.dcm")

	var refs [][]int
	var withRelationship int
	root.Walk(func(item *sr.ContentItem) bool {
		if len(item.ReferencedContentItem) > 0 {
			refs = append(refs, item.ReferencedContentItem)
			if item.RelationshipType != "" {
				withRelationship++
			}
		}
		return true
	})

	if len(refs) != 2 {
		t.Fatalf("found %d by-reference items, want 2", len(refs))
	}
	if withRelationship != 2 {
		t.Errorf("%d of the by-reference items carry a relationship type, want 2", withRelationship)
	}
	// pydicom reads these as [1 3 2] and [1 2 2 1]: paths from the root as
	// one-based ordinals.
	want := map[string]bool{"[1 3 2]": true, "[1 2 2 1]": true}
	for _, ref := range refs {
		key := "["
		for i, n := range ref {
			if i > 0 {
				key += " "
			}
			key += string(rune('0' + n))
		}
		key += "]"
		if !want[key] {
			t.Errorf("a by-reference item points at %v, which is neither of the two "+
				"pydicom reads", ref)
		}
	}
}

// TestRoundTrip writes a tree and reads it back.
func TestRoundTrip(t *testing.T) {
	original := &sr.ContentItem{
		ValueType:           sr.ValueTypeContainer,
		ConceptName:         &sr.Code{Value: "1111", SchemeDesignator: "TEST", Meaning: "Diagnosis"},
		ContinuityOfContent: "SEPARATE",
		Children: []*sr.ContentItem{
			{
				ValueType:        sr.ValueTypeText,
				RelationshipType: sr.RelationshipContains,
				ConceptName:      &sr.Code{Value: "2222", SchemeDesignator: "TEST", Meaning: "Finding"},
				Text:             "A mass of",
			},
			{
				ValueType:        sr.ValueTypeNum,
				RelationshipType: sr.RelationshipContains,
				ConceptName:      &sr.Code{Value: "3333", SchemeDesignator: "TEST", Meaning: "Diameter"},
				Measurement: &sr.Measurement{
					Value: "3",
					Units: &sr.Code{Value: "cm", SchemeDesignator: "UCUM", Meaning: "centimeter"},
				},
			},
			{
				ValueType:        sr.ValueTypeCode,
				RelationshipType: sr.RelationshipHasConceptMod,
				ConceptName:      &sr.Code{Value: "4444", SchemeDesignator: "TEST", Meaning: "Severity"},
				Code:             &sr.Code{Value: "5555", SchemeDesignator: "TEST", Meaning: "Severe"},
			},
			{
				RelationshipType:      sr.RelationshipInferredFrom,
				ReferencedContentItem: []int{1, 2},
			},
		},
	}

	ds := dataset.NewDataset()
	if err := sr.WriteContentTree(ds, original); err != nil {
		t.Fatalf("WriteContentTree: %v", err)
	}

	back, err := sr.ReadContentTree(ds)
	if err != nil {
		t.Fatalf("ReadContentTree: %v", err)
	}

	if back.Count() != original.Count() {
		t.Fatalf("the tree came back with %d items, want %d", back.Count(), original.Count())
	}
	if back.ConceptName == nil || back.ConceptName.Meaning != "Diagnosis" {
		t.Errorf("the root concept name is %v, want Diagnosis", back.ConceptName)
	}
	if len(back.Children) != 4 {
		t.Fatalf("the root has %d children, want 4", len(back.Children))
	}
	if back.Children[0].Text != "A mass of" {
		t.Errorf("the TEXT item reads %q", back.Children[0].Text)
	}
	m := back.Children[1].Measurement
	if m == nil || m.Value != "3" || m.Units == nil || m.Units.Value != "cm" {
		t.Errorf("the NUM item came back as %+v", m)
	}
	if c := back.Children[2].Code; c == nil || c.Meaning != "Severe" {
		t.Errorf("the CODE item came back as %v", c)
	}
	if ref := back.Children[3].ReferencedContentItem; len(ref) != 2 || ref[0] != 1 || ref[1] != 2 {
		t.Errorf("the by-reference item came back as %v, want [1 2]", ref)
	}
	if back.Children[3].RelationshipType != sr.RelationshipInferredFrom {
		t.Errorf("the by-reference item's relationship is %q", back.Children[3].RelationshipType)
	}
}

// TestRootCarriesNoRelationship covers the one structural rule about the root.
func TestRootCarriesNoRelationship(t *testing.T) {
	root := &sr.ContentItem{
		ValueType:        sr.ValueTypeContainer,
		RelationshipType: sr.RelationshipContains,
	}
	if err := sr.WriteContentTree(dataset.NewDataset(), root); err == nil {
		t.Error("a root with a relationship type was written; the root relates to nothing")
	}
}

// TestNonSRIsRefused covers a data set that is not a report.
func TestNonSRIsRefused(t *testing.T) {
	if _, err := sr.ReadContentTree(dataset.NewDataset()); err == nil {
		t.Error("a data set with no Value Type was read as a structured report")
	}
}
