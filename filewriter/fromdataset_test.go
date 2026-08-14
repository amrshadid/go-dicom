package filewriter_test

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// TestElementsFromDatasetCarriesNestedSequences is the reason this function is
// public.
//
// A sequence holds child data sets rather than a byte value, so a conversion
// that copies Value and ignores Items writes a file that looks complete with
// every nested item missing — the element is present, its length is zero, and
// nothing reports it. Every caller wanting to read a file and write it back had
// to get this right themselves.
func TestElementsFromDatasetCarriesNestedSequences(t *testing.T) {
	inner := dataset.NewDataset()
	_ = inner.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0100), dataelem.SH, []byte("INNER   ")))
	innerSeq := sequence.New()
	_ = innerSeq.Append(inner)

	middle := dataset.NewDataset()
	_ = middle.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0104), dataelem.LO, []byte("Finding ")))
	_ = middle.AddSequence(tag.New(0x0040, 0xA043), innerSeq)

	outerSeq := sequence.New()
	_ = outerSeq.Append(middle)

	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))
	_ = ds.AddSequence(tag.New(0x0040, 0xA730), outerSeq)

	elements := filewriter.ElementsFromDataset(ds)
	if len(elements) != 2 {
		t.Fatalf("got %d elements, want 2", len(elements))
	}

	var seqElem *filewriter.DataElement
	for _, e := range elements {
		if e.Tag == tag.New(0x0040, 0xA730) {
			seqElem = e
		}
	}
	if seqElem == nil {
		t.Fatal("the sequence element is missing")
	}
	if seqElem.VR != "SQ" {
		t.Errorf("VR = %q, want SQ", seqElem.VR)
	}
	if len(seqElem.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(seqElem.Items))
	}

	// And the nesting continues: the item holds its own sequence.
	item := seqElem.Items[0]
	if len(item.Elements) != 2 {
		t.Fatalf("the item holds %d elements, want 2", len(item.Elements))
	}
	var nested *filewriter.DataElement
	for _, e := range item.Elements {
		if e.Tag == tag.New(0x0040, 0xA043) {
			nested = e
		}
	}
	if nested == nil || len(nested.Items) != 1 {
		t.Fatal("the second level of nesting was lost")
	}
}

// TestReadWriteReadRoundTrip is the end-to-end claim: a file read and written
// back holds the same values, sequences included.
func TestReadWriteReadRoundTrip(t *testing.T) {
	// Build a file with a nested sequence and an ordinary value.
	inner := dataset.NewDataset()
	_ = inner.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0100), dataelem.SH, []byte("CODE01  ")))
	seq := sequence.New()
	_ = seq.Append(inner)

	original := dataset.NewDataset()
	_ = original.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))
	_ = original.AddSequence(tag.New(0x0040, 0xA730), seq)

	out := &growBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})
	for _, e := range filewriter.ElementsFromDataset(original) {
		if err := w.AddDataElement(e); err != nil {
			t.Fatalf("AddDataElement %s: %v", e.Tag, err)
		}
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(out.Bytes())))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	back := df.GetDataset()

	elem, ok := back.Get(tag.New(0x0010, 0x0010))
	if !ok {
		t.Fatal("the plain value did not survive the round trip")
	}
	if got := string(elem.GetValue().([]byte)); got != "Doe^John" {
		t.Errorf("value = %q, want Doe^John", got)
	}

	readSeq, err := back.GetSequence(tag.New(0x0040, 0xA730))
	if err != nil {
		t.Fatalf("the sequence did not survive the round trip: %v", err)
	}
	if readSeq.Length() != 1 {
		t.Fatalf("the sequence has %d items after the round trip, want 1", readSeq.Length())
	}
	raw, _ := readSeq.Get(0)
	child, ok := raw.(*dataset.Dataset)
	if !ok {
		t.Fatal("the sequence item is not a data set")
	}
	nested, ok := child.Get(tag.New(0x0008, 0x0100))
	if !ok {
		t.Fatal("the nested element was lost")
	}
	if got := string(nested.GetValue().([]byte)); got != "CODE01  " {
		t.Errorf("nested value = %q, want CODE01", got)
	}
}

// TestElementsFromDatasetHandlesNilAndEmpty covers the degenerate inputs, since
// a conversion that panics on an empty data set is worse than one that is slow.
func TestElementsFromDatasetHandlesNilAndEmpty(t *testing.T) {
	if got := filewriter.ElementsFromDataset(nil); got != nil {
		t.Errorf("nil data set gave %d elements, want nil", len(got))
	}
	if got := filewriter.ElementsFromDataset(dataset.NewDataset()); len(got) != 0 {
		t.Errorf("empty data set gave %d elements", len(got))
	}
}

// A Dataset built in code holds string values, since dataelem.NewDataElement
// takes an interface{} and a string is the obvious thing to pass. ElementsFromDataset
// used to discard the second return of a type assertion:
//
//	value, _ := elem.GetValue().([]byte)
//
// so every string value became nil and was written with length 0. The file came
// back with every element present, correctly typed, and empty — the same silent
// shape this function's own comment warns about for sequences.
func TestElementsFromDatasetKeepsStringValues(t *testing.T) {
	ds := dataset.NewDataset()
	values := map[tag.Tag]struct {
		vr    dataelem.VR
		value string
	}{
		tag.New(0x0008, 0x0018): {dataelem.UI, "1.2.3.4"},
		tag.New(0x0010, 0x0010): {dataelem.PN, "SMITH^JOHN"},
		tag.New(0x0008, 0x0060): {dataelem.CS, "CT"},
		tag.New(0x0020, 0x0013): {dataelem.IS, "1"},
	}
	for t2, v := range values {
		_ = ds.Add(dataelem.NewDataElement(t2, v.vr, v.value))
	}

	elements := filewriter.ElementsFromDataset(ds)
	if len(elements) != len(values) {
		t.Fatalf("got %d elements, want %d", len(elements), len(values))
	}

	for _, elem := range elements {
		want, ok := values[elem.Tag]
		if !ok {
			t.Errorf("unexpected element %s", elem.Tag)
			continue
		}
		if got := string(elem.Value); got != want.value {
			t.Errorf("%s wrote %q, want %q", elem.Tag, got, want.value)
		}
		if elem.Length != uint32(len(want.value)) {
			t.Errorf("%s wrote length %d for a %d byte value",
				elem.Tag, elem.Length, len(want.value))
		}
	}
}

// []byte values must keep working, since that is what filereader produces and
// what every read-modify-write path holds.
func TestElementsFromDatasetKeepsByteValues(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("SMITH^JOHN")))

	elements := filewriter.ElementsFromDataset(ds)
	if len(elements) != 1 {
		t.Fatalf("got %d elements, want 1", len(elements))
	}
	if got := string(elements[0].Value); got != "SMITH^JOHN" {
		t.Errorf("wrote %q", got)
	}
}

// An empty value is legitimate — a type 2 attribute is sent present and empty —
// so it must be written rather than dropped as unrenderable.
func TestElementsFromDatasetKeepsEmptyValues(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0030), dataelem.DA, ""))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0040), dataelem.CS, nil))

	elements := filewriter.ElementsFromDataset(ds)
	if len(elements) != 2 {
		t.Fatalf("got %d elements, want 2 — an empty value is not the same as no element",
			len(elements))
	}
	for _, elem := range elements {
		if elem.Length != 0 {
			t.Errorf("%s wrote length %d for an empty value", elem.Tag, elem.Length)
		}
	}
}

// A value of a type that cannot be rendered is dropped with a warning rather than
// written as empty. Writing it empty is what the old code did, and it is the
// outcome with no signal attached.
func TestElementsFromDatasetDropsUnrenderableValues(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, 42))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, "CT"))

	elements := filewriter.ElementsFromDataset(ds)
	if len(elements) != 1 {
		t.Fatalf("got %d elements, want 1: the int value should be dropped, not written empty",
			len(elements))
	}
	if elements[0].Tag != tag.New(0x0008, 0x0060) {
		t.Errorf("the surviving element is %s, want the CS one", elements[0].Tag)
	}
}
