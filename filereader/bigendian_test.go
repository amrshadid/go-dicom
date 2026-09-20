package filereader_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// TestExplicitVRBigEndian verifies a big endian file parses completely and that
// its numeric values mean the same thing as the little endian encoding of the
// same data.
//
// Two defects made this fail. The short-form value length was assembled little
// endian regardless of byte order, so parsing collapsed after one element. Once
// that was fixed, numeric values were still byte-swapped: BitsAllocated of 16
// read back as 4096, the same bits in the other order.
func TestExplicitVRBigEndian(t *testing.T) {
	le := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataset(binary.LittleEndian), false))
	be := readFile(t, buildFile("1.2.840.10008.1.2.2", explicitDataset(binary.BigEndian), false))

	if len(be.DataElements) != len(le.DataElements) {
		t.Fatalf("big endian parsed %d elements, little endian %d",
			len(be.DataElements), len(le.DataElements))
	}
	if len(be.DataElements) != 5 {
		t.Fatalf("parsed %d elements, want 5", len(be.DataElements))
	}

	// After parsing, a value must mean the same thing regardless of the file's
	// byte order — that is what lets everything downstream assume one order.
	beDS, leDS := be.GetDataset(), le.GetDataset()
	for _, tc := range []struct {
		name string
		tg   tag.Tag
	}{
		{"Rows", tag.New(0x0028, 0x0010)},
		{"Columns", tag.New(0x0028, 0x0011)},
		{"a UL value", tag.New(0x0018, 0x1063)},
		{"PatientName", tag.New(0x0010, 0x0010)},
	} {
		beElem, ok := beDS.Get(tc.tg)
		if !ok {
			t.Errorf("%s missing from the big endian file", tc.name)
			continue
		}
		leElem, _ := leDS.Get(tc.tg)
		if !bytes.Equal(beElem.GetValue().([]byte), leElem.GetValue().([]byte)) {
			t.Errorf("%s: big endian value % X, little endian % X — should match after parsing",
				tc.name, beElem.GetValue(), leElem.GetValue())
		}
	}
}

// TestBigEndianTextValuesAreNotSwapped guards the other half of normalisation:
// text and byte-oriented VRs carry no byte order and must be left alone.
func TestBigEndianTextValuesAreNotSwapped(t *testing.T) {
	be := readFile(t, buildFile("1.2.840.10008.1.2.2", explicitDataset(binary.BigEndian), false))

	elem, ok := be.GetDataset().Get(tag.New(0x0010, 0x0010))
	if !ok {
		t.Fatal("PatientName missing")
	}
	if got := string(elem.GetValue().([]byte)); got != "Doe^John" {
		t.Errorf("PatientName = %q, want %q — a text VR was byte-swapped", got, "Doe^John")
	}
}

// TestBigEndianWriteRoundTrip verifies a big endian file written from a parsed
// big endian file preserves its values.
//
// Reading normalises big endian values to little endian so the rest of the
// library can assume one order; writing must therefore convert back. Without
// the inverse, a file round-tripped through the library kept its big endian
// declaration but held little endian values — Rows of 64 read back as 16384.
//
// The file meta header is also always Explicit VR Little Endian whatever the
// data set's syntax; writing it in the data set's order produced a header whose
// first tag read back as (0200,0000).
func TestBigEndianWriteRoundTrip(t *testing.T) {
	original := buildFile("1.2.840.10008.1.2.2", explicitDataset(binary.BigEndian), false)
	parsed := readFile(t, original)

	// Write it back out, still declaring big endian.
	var out bytes.Buffer
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(&writeSeeker{&out}))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    parsed.FileMetaInfo.MediaStorageSOPClassUID,
		MediaStorageSOPInstanceUID: parsed.FileMetaInfo.MediaStorageSOPInstanceUID,
		TransferSyntaxUID:          "1.2.840.10008.1.2.2",
	})
	for _, e := range parsed.DataElements {
		if err := w.AddDataElement(&filewriter.DataElement{
			Tag: e.Tag, VR: e.VR, Value: e.Value, Length: uint32(len(e.Value)),
		}); err != nil {
			t.Fatalf("AddDataElement %s: %v", e.Tag, err)
		}
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Reading it back must reproduce the same values.
	back := readFile(t, out.Bytes())

	if len(back.DataElements) != len(parsed.DataElements) {
		t.Fatalf("round trip changed the element count: %d -> %d",
			len(parsed.DataElements), len(back.DataElements))
	}

	backDS, parsedDS := back.GetDataset(), parsed.GetDataset()
	for _, tg := range []tag.Tag{
		tag.New(0x0028, 0x0010), // Rows
		tag.New(0x0028, 0x0011), // Columns
		tag.New(0x0018, 0x1063), // a UL value
		tag.New(0x0010, 0x0010), // PatientName
	} {
		a, okA := parsedDS.Get(tg)
		b, okB := backDS.Get(tg)
		if !okA || !okB {
			t.Errorf("%s missing after the round trip", tg)
			continue
		}
		if !bytes.Equal(a.GetValue().([]byte), b.GetValue().([]byte)) {
			t.Errorf("%s changed: % X -> % X", tg, a.GetValue(), b.GetValue())
		}
	}
}
