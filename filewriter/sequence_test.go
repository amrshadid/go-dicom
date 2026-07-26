package filewriter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// writeAndRead writes elements to a temporary file and parses it back, which is
// the only check that matters for a writer: whether a reader can recover what
// was written.
func writeAndRead(t *testing.T, elements []*filewriter.DataElement) *filereader.DICOMFile {
	t.Helper()

	path := filepath.Join(t.TempDir(), "seq.dcm")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.2",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})
	for _, e := range elements {
		if err := w.AddDataElement(e); err != nil {
			t.Fatalf("AddDataElement %s: %v", e.Tag, err)
		}
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f.Close()

	rf, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rf.Close()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(rf))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	if len(df.Warnings) != 0 {
		t.Errorf("parse warnings: %v", df.Warnings)
	}
	return df
}

// TestWriteSequenceRoundTrip verifies a sequence written to disk parses back
// with its items and their values intact. Until this release the writer dropped
// sequences entirely, so a file could be read but not rewritten.
func TestWriteSequenceRoundTrip(t *testing.T) {
	seqTag := tag.New(0x0010, 0x1002) // OtherPatientIDsSequence

	df := writeAndRead(t, []*filewriter.DataElement{
		{Tag: tag.New(0x0010, 0x0010), VR: "PN", Value: []byte("Doe^John"), Length: 8},
		{
			Tag: seqTag,
			VR:  "SQ",
			Items: []*filewriter.SequenceItem{
				{Elements: []*filewriter.DataElement{
					{Tag: tag.New(0x0010, 0x0020), VR: "LO", Value: []byte("ABCD1234"), Length: 8},
					{Tag: tag.New(0x0010, 0x0022), VR: "CS", Value: []byte("TEXT"), Length: 4},
				}},
				{Elements: []*filewriter.DataElement{
					{Tag: tag.New(0x0010, 0x0020), VR: "LO", Value: []byte("1234ABCD"), Length: 8},
					{Tag: tag.New(0x0010, 0x0022), VR: "CS", Value: []byte("TEXT"), Length: 4},
				}},
			},
		},
	})

	ds := df.GetDataset()
	seq, err := ds.GetSequence(seqTag)
	if err != nil {
		t.Fatalf("GetSequence: %v", err)
	}
	if seq.Length() != 2 {
		t.Fatalf("sequence has %d items, want 2", seq.Length())
	}

	want := []string{"ABCD1234", "1234ABCD"}
	for i := 0; i < seq.Length(); i++ {
		item, err := seq.Get(i)
		if err != nil {
			t.Fatalf("item %d: %v", i, err)
		}
		child, ok := item.(*dataset.Dataset)
		if !ok {
			t.Fatalf("item %d is %T, want *dataset.Dataset", i, item)
		}
		elem, ok := child.Get(tag.New(0x0010, 0x0020))
		if !ok {
			t.Fatalf("item %d: nested element missing", i)
		}
		if got := string(elem.GetValue().([]byte)); got != want[i] {
			t.Errorf("item %d = %q, want %q", i, got, want[i])
		}
	}
}

// TestWriteNestedSequenceRoundTrip verifies a sequence inside a sequence
// survives, which is the structure used by structured reports and multi-frame
// functional groups.
func TestWriteNestedSequenceRoundTrip(t *testing.T) {
	outerTag := tag.New(0x0040, 0xA730) // ContentSequence
	innerTag := tag.New(0x0040, 0xA043) // ConceptNameCodeSequence

	df := writeAndRead(t, []*filewriter.DataElement{
		{
			Tag: outerTag,
			VR:  "SQ",
			Items: []*filewriter.SequenceItem{
				{Elements: []*filewriter.DataElement{
					{Tag: tag.New(0x0040, 0xA040), VR: "CS", Value: []byte("CONTAINER "), Length: 10},
					{
						Tag: innerTag,
						VR:  "SQ",
						Items: []*filewriter.SequenceItem{
							{Elements: []*filewriter.DataElement{
								{Tag: tag.New(0x0008, 0x0100), VR: "SH", Value: []byte("121071  "), Length: 8},
								{Tag: tag.New(0x0008, 0x0104), VR: "LO", Value: []byte("Finding "), Length: 8},
							}},
						},
					},
				}},
			},
		},
	})

	ds := df.GetDataset()
	outer, err := ds.GetSequence(outerTag)
	if err != nil {
		t.Fatalf("outer GetSequence: %v", err)
	}
	if outer.Length() != 1 {
		t.Fatalf("outer has %d items, want 1", outer.Length())
	}

	item, _ := outer.Get(0)
	child := item.(*dataset.Dataset)

	inner, err := child.GetSequence(innerTag)
	if err != nil {
		t.Fatalf("inner GetSequence: %v", err)
	}
	if inner.Length() != 1 {
		t.Fatalf("inner has %d items, want 1", inner.Length())
	}

	innerItem, _ := inner.Get(0)
	grandchild := innerItem.(*dataset.Dataset)
	elem, ok := grandchild.Get(tag.New(0x0008, 0x0104))
	if !ok {
		t.Fatal("the twice-nested element is missing")
	}
	if got := string(elem.GetValue().([]byte)); got != "Finding " {
		t.Errorf("nested value = %q, want %q", got, "Finding ")
	}
}

// TestWriteEmptySequence verifies a sequence with no items is legal and
// round-trips, which is common for optional attributes.
func TestWriteEmptySequence(t *testing.T) {
	seqTag := tag.New(0x0040, 0xA730)

	df := writeAndRead(t, []*filewriter.DataElement{
		{Tag: seqTag, VR: "SQ", Items: nil},
		// An element after the empty sequence catches a length written wrongly:
		// a bad sequence length would swallow or misalign it.
		{Tag: tag.New(0x0010, 0x0010), VR: "PN", Value: []byte("After^Empty"), Length: 11},
	})

	ds := df.GetDataset()
	if !ds.HasSequence(seqTag) {
		t.Error("the empty sequence is missing")
	}

	elem, ok := ds.Get(tag.New(0x0010, 0x0010))
	if !ok {
		t.Fatal("the element following the empty sequence is missing")
	}
	// The odd-length value is padded to even on write.
	if got := string(elem.GetValue().([]byte)); got != "After^Empty " {
		t.Errorf("following element = %q, want %q", got, "After^Empty ")
	}
}

// TestWriteSequenceItemsPadOddValues verifies values inside a sequence item are
// padded to even length like any other, since an odd one misaligns the rest of
// the item.
func TestWriteSequenceItemsPadOddValues(t *testing.T) {
	seqTag := tag.New(0x0010, 0x1002)

	df := writeAndRead(t, []*filewriter.DataElement{
		{
			Tag: seqTag,
			VR:  "SQ",
			Items: []*filewriter.SequenceItem{
				{Elements: []*filewriter.DataElement{
					// 3 characters: odd, must be padded.
					{Tag: tag.New(0x0010, 0x0020), VR: "LO", Value: []byte("ODD"), Length: 3},
					{Tag: tag.New(0x0010, 0x0022), VR: "CS", Value: []byte("TEXT"), Length: 4},
				}},
			},
		},
	})

	seq, err := df.GetDataset().GetSequence(seqTag)
	if err != nil {
		t.Fatalf("GetSequence: %v", err)
	}
	item, _ := seq.Get(0)
	child := item.(*dataset.Dataset)

	// Both elements must be present: if the odd value went unpadded, the second
	// would be misaligned or lost.
	if child.Length() != 2 {
		t.Fatalf("item holds %d elements, want 2 — an unpadded value misaligned it", child.Length())
	}
	elem, _ := child.Get(tag.New(0x0010, 0x0020))
	if got := elem.GetValue().([]byte); len(got)%2 != 0 {
		t.Errorf("nested value length %d is odd", len(got))
	}
}
