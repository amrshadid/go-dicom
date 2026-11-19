package filewriter_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// readCorpusFile reads a file of pydicom's corpus whole, skipping the test when
// the corpus is not configured.
func readCorpusFile(t *testing.T, name string) *filereader.DICOMFile {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return df
}

// readWritten parses a written file.
func readWritten(t *testing.T, written []byte) *dataset.Dataset {
	t.Helper()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(written)))
	if err != nil {
		t.Fatalf("reading the written file: %v", err)
	}
	return df.GetDataset()
}

// itemOf returns item i of the sequence t in ds, failing when t is not a
// sequence there: the check that a reader learned it holds items.
func itemOf(t *testing.T, ds *dataset.Dataset, seqTag tag.Tag, i int) *dataset.Dataset {
	t.Helper()
	seq, err := ds.GetSequence(seqTag)
	if err != nil {
		t.Fatalf("%s is not read back as a sequence: %v", seqTag, err)
	}
	item, err := seq.Get(i)
	if err != nil {
		t.Fatalf("%s has no item %d: %v", seqTag, i, err)
	}
	return item.(*dataset.Dataset)
}

// lengthAfter returns the 4-byte length that follows the first occurrence of
// t's tag in an Implicit VR Little Endian encoding, from the given offset.
func lengthAfter(t *testing.T, data []byte, from int, seqTag tag.Tag) (uint32, int) {
	t.Helper()
	at := bytes.Index(data[from:], seqTag.ToBytes(true))
	if at < 0 {
		t.Fatalf("%s is not in the written bytes after offset %d", seqTag, from)
	}
	at += from
	return binary.LittleEndian.Uint32(data[at+4 : at+8]), at + 8
}

// A private sequence holding a private sequence (nested_priv_SQ.dcm, Implicit
// VR) written back in Implicit VR is still two sequences to a reader. Nothing
// in Implicit VR names an element a sequence, and the dictionary has no entry
// for a private tag, so the only thing that says the element holds items is an
// undefined length (PS3.5 6.2.2 Note 5, CP-246). Written with its byte count,
// as it was, dcmtk, pydicom and this library's reader all read the outer
// sequence as 66 opaque bytes. Both sequences are checked to be written with
// undefined length and closed by a delimiter, and read back with their values.
func TestPrivateSequenceInImplicitVRIsWrittenWithUndefinedLength(t *testing.T) {
	df := readCorpusFile(t, "nested_priv_SQ.dcm")
	written := writeFile(t, filewriter.ElementsFromDataset(df.GetDataset()), implicitLittle, nil, true)

	outer := tag.New(0x0001, 0x0001)
	got := readWritten(t, written)
	inner := itemOf(t, itemOf(t, got, outer, 0), outer, 0)
	value, ok := inner.Get(outer)
	if !ok {
		t.Fatal("the value inside the nested sequence is missing")
	}
	if v, _ := value.GetValue().([]byte); string(v) != "Double Nested SQ" {
		t.Errorf("value inside the nested sequence = %q, want %q", v, "Double Nested SQ")
	}
	if _, ok := itemOf(t, got, outer, 0).Get(tag.New(0x0001, 0x0002)); !ok {
		t.Error("(0001,0002) after the nested sequence is missing")
	}

	// The data set starts after the file meta; both sequence headers there
	// state undefined length, and two Sequence Delimitation Items close them.
	body := bytes.Index(written, []byte("1.2.840.10008.1.2\x00")) + len("1.2.840.10008.1.2\x00")
	length, next := lengthAfter(t, written, body, outer)
	if length != 0xFFFFFFFF {
		t.Errorf("the outer private sequence has length %d, want undefined (0xFFFFFFFF)", length)
	}
	if length, _ = lengthAfter(t, written, next, outer); length != 0xFFFFFFFF {
		t.Errorf("the nested private sequence has length %d, want undefined (0xFFFFFFFF)", length)
	}
	delimiter := []byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0}
	if n := bytes.Count(written[body:], delimiter); n != 2 {
		t.Errorf("%d Sequence Delimitation Items written, want 2", n)
	}
}

// A private sequence read from an Explicit VR file, where the VR said SQ, is
// written with undefined length when the file is converted to Implicit VR:
// what the source's length was does not matter, what the next reader can learn
// does. UN_sequence.dcm holds (4453,100C) encoded as UN with undefined length,
// with standard sequences inside it.
func TestPrivateSequenceFromExplicitVRSurvivesConversionToImplicitVR(t *testing.T) {
	df := readCorpusFile(t, "UN_sequence.dcm")
	written := writeFile(t, filewriter.ElementsFromDataset(df.GetDataset()), implicitLittle, nil, true)

	private := tag.New(0x4453, 0x100C)
	referencedSeries := tag.New(0x0008, 0x1115)
	referencedInstance := tag.New(0x0008, 0x1199)
	got := readWritten(t, written)
	instance := itemOf(t, itemOf(t, itemOf(t, got, private, 0), referencedSeries, 0), referencedInstance, 0)
	if _, ok := instance.Get(tag.New(0x0008, 0x1155)); !ok {
		t.Error("Referenced SOP Instance UID deep inside the private sequence is missing")
	}
}

// A sequence built in memory under a private tag, with no source file at all,
// is written so in Implicit VR too, and an empty one reads back as an empty
// sequence rather than as an empty value. A standard sequence keeps its
// explicit length: the dictionary tells any reader what it is, so its bytes
// are what they were before.
func TestSequenceLengthInImplicitVRFollowsTheDictionary(t *testing.T) {
	private := tag.New(0x0029, 0x1010)
	empty := tag.New(0x0029, 0x1020)
	standard := tag.New(0x0008, 0x1140) // Referenced Image Sequence
	elements := []*filewriter.DataElement{
		{Tag: standard, VR: "SQ", Items: []*filewriter.SequenceItem{{Elements: []*filewriter.DataElement{
			{Tag: tag.New(0x0008, 0x1150), VR: "UI", Value: []byte("1.2.3\x00"), Length: 6},
		}}}},
		{Tag: tag.New(0x0029, 0x0010), VR: "LO", Value: []byte("VENDOR"), Length: 6},
		{Tag: private, VR: "SQ", Items: []*filewriter.SequenceItem{{Elements: []*filewriter.DataElement{
			{Tag: tag.New(0x0010, 0x0010), VR: "PN", Value: []byte("Inside^Private"), Length: 14},
		}}}},
		{Tag: empty, VR: "SQ"},
		{Tag: tag.New(0x0029, 0x1030), VR: "LO", Value: []byte("AFTER "), Length: 6},
	}
	written := writeFile(t, elements, implicitLittle, nil, true)

	body := bytes.Index(written, []byte("1.2.840.10008.1.2\x00")) + len("1.2.840.10008.1.2\x00")
	if length, _ := lengthAfter(t, written, body, standard); length != 22 {
		t.Errorf("the standard sequence has length %#x, want its byte count, 22", length)
	}
	if length, _ := lengthAfter(t, written, body, private); length != 0xFFFFFFFF {
		t.Errorf("the private sequence has length %#x, want undefined", length)
	}

	got := readWritten(t, written)
	if _, ok := itemOf(t, got, private, 0).Get(tag.New(0x0010, 0x0010)); !ok {
		t.Error("the element inside the private sequence is missing")
	}
	seq, err := got.GetSequence(empty)
	if err != nil {
		t.Fatalf("the empty private sequence is not read back as a sequence: %v", err)
	}
	if seq.Length() != 0 {
		t.Errorf("the empty private sequence has %d items", seq.Length())
	}
	if _, ok := got.Get(tag.New(0x0029, 0x1030)); !ok {
		t.Error("the element after the empty sequence is missing")
	}

	// Explicit VR states SQ, so there every sequence keeps its byte count.
	explicit := writeFile(t, elements, explicitLittle, nil, true)
	if bytes.Contains(explicit, []byte{0xFE, 0xFF, 0xDD, 0xE0}) {
		t.Error("a Sequence Delimitation Item was written in Explicit VR, where none is needed")
	}
}
