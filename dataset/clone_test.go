package dataset_test

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

var (
	tagCodeValue       = tag.New(0x0008, 0x0100)
	tagContentSequence = tag.New(0x0040, 0xA730)
	tagPatientName     = tag.New(0x0010, 0x0010)
)

// nestedDataset builds a data set with a sequence two levels deep, so a clone
// that copies only the top level is distinguishable from one that recurses.
func nestedDataset(t *testing.T) *dataset.Dataset {
	t.Helper()

	inner := dataset.NewDataset()
	if err := inner.Add(dataelem.NewDataElement(tagCodeValue, dataelem.SH, []byte("INNER   "))); err != nil {
		t.Fatalf("add inner: %v", err)
	}

	innerSeq := sequence.New()
	if err := innerSeq.Append(inner); err != nil {
		t.Fatalf("append inner: %v", err)
	}

	middle := dataset.NewDataset()
	if err := middle.Add(dataelem.NewDataElement(tagCodeValue, dataelem.SH, []byte("MIDDLE  "))); err != nil {
		t.Fatalf("add middle: %v", err)
	}
	if err := middle.AddSequence(tagContentSequence, innerSeq); err != nil {
		t.Fatalf("add inner sequence: %v", err)
	}

	outerSeq := sequence.New()
	if err := outerSeq.Append(middle); err != nil {
		t.Fatalf("append middle: %v", err)
	}

	ds := dataset.NewDataset()
	if err := ds.Add(dataelem.NewDataElement(tagPatientName, dataelem.PN, []byte("Doe^John"))); err != nil {
		t.Fatalf("add patient name: %v", err)
	}
	if err := ds.AddSequence(tagContentSequence, outerSeq); err != nil {
		t.Fatalf("add outer sequence: %v", err)
	}
	return ds
}

// itemAt walks into a sequence and returns the nested data set at an index.
func itemAt(t *testing.T, ds *dataset.Dataset, tg tag.Tag, index int) *dataset.Dataset {
	t.Helper()

	seq, err := ds.GetSequence(tg)
	if err != nil {
		t.Fatalf("GetSequence %s: %v", tg, err)
	}
	raw, err := seq.Get(index)
	if err != nil {
		t.Fatalf("sequence item %d: %v", index, err)
	}
	nested, ok := raw.(*dataset.Dataset)
	if !ok {
		t.Fatalf("sequence item %d is %T, want *dataset.Dataset", index, raw)
	}
	return nested
}

func codeValue(t *testing.T, ds *dataset.Dataset) string {
	t.Helper()

	elem, ok := ds.Get(tagCodeValue)
	if !ok {
		t.Fatal("code value missing")
	}
	return string(elem.GetValue().([]byte))
}

// TestCloneIsDeepThroughSequences verifies a clone shares no nested state with
// its original.
//
// Clone documented itself as a deep copy — "Modifications to the cloned dataset
// do not affect the original" — while copying only []byte values. A sequence
// was shared by reference, so both data sets pointed at the same nested items
// and editing either edited both. That is the one thing a caller clones to
// avoid, and it stayed invisible until something wrote to a shared item.
func TestCloneIsDeepThroughSequences(t *testing.T) {
	orig := nestedDataset(t)
	clone := orig.Clone()

	// Mutate the original at each depth.
	itemAt(t, orig, tagContentSequence, 0).
		Add(dataelem.NewDataElement(tagCodeValue, dataelem.SH, []byte("CHANGED1")))
	itemAt(t, itemAt(t, orig, tagContentSequence, 0), tagContentSequence, 0).
		Add(dataelem.NewDataElement(tagCodeValue, dataelem.SH, []byte("CHANGED2")))

	if got := codeValue(t, itemAt(t, clone, tagContentSequence, 0)); got != "MIDDLE  " {
		t.Errorf("depth 1 of the clone changed with the original: %q, want %q", got, "MIDDLE  ")
	}
	if got := codeValue(t, itemAt(t, itemAt(t, clone, tagContentSequence, 0), tagContentSequence, 0)); got != "INNER   " {
		t.Errorf("depth 2 of the clone changed with the original: %q, want %q", got, "INNER   ")
	}
}

// TestCloneIsDeepInBothDirections covers the direction the documentation states
// literally. Sharing is symmetric, so a test that only mutates one side can
// pass against an implementation that copies lazily.
func TestCloneIsDeepInBothDirections(t *testing.T) {
	orig := nestedDataset(t)
	clone := orig.Clone()

	itemAt(t, clone, tagContentSequence, 0).
		Add(dataelem.NewDataElement(tagCodeValue, dataelem.SH, []byte("FROMCLNE")))

	if got := codeValue(t, itemAt(t, orig, tagContentSequence, 0)); got != "MIDDLE  " {
		t.Errorf("the original changed when the clone was modified: %q, want %q", got, "MIDDLE  ")
	}
}

// TestCloneCopiesByteValues guards the part that already worked, so a rewrite
// of cloneValue cannot lose it.
func TestCloneCopiesByteValues(t *testing.T) {
	orig := dataset.NewDataset()
	value := []byte("Doe^John")
	if err := orig.Add(dataelem.NewDataElement(tagPatientName, dataelem.PN, value)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	clone := orig.Clone()

	// Mutate the backing array the original was given.
	value[0] = 'X'

	elem, ok := clone.Get(tagPatientName)
	if !ok {
		t.Fatal("patient name missing from the clone")
	}
	if got := elem.GetValue().([]byte); !bytes.Equal(got, []byte("Doe^John")) {
		t.Errorf("clone shares the caller's byte slice: %q", got)
	}
}

// TestCloneCarriesTransferSyntax verifies the clone knows how its own values
// are encoded.
//
// The transfer syntax is not an element; it says how the values were encoded.
// Clone dropped it, so a cloned data set could not tell whether its pixel data
// was raw or encapsulated, and pixel access fell back to inferring the codec
// from structure — a silent downgrade rather than an error.
func TestCloneCarriesTransferSyntax(t *testing.T) {
	orig := dataset.NewDataset()
	orig.SetTransferSyntaxUID("1.2.840.10008.1.2.5")

	if got := orig.Clone().TransferSyntaxUID(); got != "1.2.840.10008.1.2.5" {
		t.Errorf("clone transfer syntax = %q, want the original's", got)
	}
}

// TestCloneOfEmptyDataset covers the degenerate case, since the loop and the
// sequence walk both have to tolerate having nothing to do.
func TestCloneOfEmptyDataset(t *testing.T) {
	clone := dataset.NewDataset().Clone()
	if clone == nil {
		t.Fatal("Clone returned nil for an empty data set")
	}
	if n := clone.Length(); n != 0 {
		t.Errorf("clone of an empty data set has %d elements", n)
	}
}

// TestCloneOfEmptySequence covers a sequence with no items, which is legal and
// common for an optional attribute.
func TestCloneOfEmptySequence(t *testing.T) {
	orig := dataset.NewDataset()
	if err := orig.AddSequence(tagContentSequence, sequence.New()); err != nil {
		t.Fatalf("AddSequence: %v", err)
	}

	seq, err := orig.Clone().GetSequence(tagContentSequence)
	if err != nil {
		t.Fatalf("the clone lost the empty sequence: %v", err)
	}
	if seq.Length() != 0 {
		t.Errorf("cloned empty sequence has %d items", seq.Length())
	}
}
