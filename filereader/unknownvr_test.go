package filereader_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// TestKnownTagEncodedAsUNTakesItsDictionaryVR covers #117. An intermediary that
// does not know a tag re-encodes it as UN, and PS3.5 6.2.2 Note 2 says a
// receiver that does know it may read the value as Implicit VR Little Endian.
// The VR was kept as UN instead, so the typed accessors refused the value and a
// UN-encoded sequence was never parsed into items: pydicom's rtdose_rle.dcm has
// 35 such elements.
func TestKnownTagEncodedAsUNTakesItsDictionaryVR(t *testing.T) {
	// A sequence encoded as UN holds implicit VR items (Note 5).
	var item bytes.Buffer
	item.Write(implicitElement(0x0008, 0x1150, []byte("1.2.840.10008.5.1.4.1.1.481.5\x00")))
	var items bytes.Buffer
	_ = binary.Write(&items, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&items, binary.LittleEndian, uint16(0xE000))
	_ = binary.Write(&items, binary.LittleEndian, uint32(item.Len()))
	items.Write(item.Bytes())

	var ds bytes.Buffer
	ds.Write(explicitElement(0x0008, 0x0005, "CS", []byte("ISO_IR 100 ")))
	ds.Write(unElement(0x0008, 0x0012, []byte("20030903"))) // Instance Creation Date, DA
	ds.Write(unElement(0x0010, 0x0010, []byte("Doe^John"))) // Patient's Name, PN
	ds.Write(unElement(0x300C, 0x0002, items.Bytes()))      // Referenced RT Plan Sequence, SQ
	ds.Write(unElement(0x0009, 0x0010, []byte("ACME 1.0"))) // private creator: LO by definition
	ds.Write(unElement(0x0009, 0x1001, []byte{0x01, 0x02})) // private: nothing knows it

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))
	got := df.GetDataset()

	for _, tc := range []struct {
		tg   tag.Tag
		vr   string
		text string
	}{
		{tag.New(0x0008, 0x0012), "DA", "20030903"},
		{tag.New(0x0010, 0x0010), "PN", "Doe^John"},
	} {
		elem, ok := got.Get(tc.tg)
		if !ok {
			t.Errorf("%s was lost", tc.tg)
			continue
		}
		if string(elem.GetVR()) != tc.vr {
			t.Errorf("%s VR = %q, want %s from the dictionary", tc.tg, elem.GetVR(), tc.vr)
		}
		if got := strings.TrimRight(string(elem.GetValue().([]byte)), " \x00"); got != tc.text {
			t.Errorf("%s value = %q, want %q", tc.tg, got, tc.text)
		}
	}

	seq, err := got.GetSequence(tag.New(0x300C, 0x0002))
	if err != nil {
		t.Fatalf("the UN-encoded sequence was not parsed: %v", err)
	}
	if seq.Length() != 1 {
		t.Fatalf("the sequence has %d items, want 1", seq.Length())
	}
	raw, _ := seq.Get(0)
	if _, ok := raw.(*dataset.Dataset).Get(tag.New(0x0008, 0x1150)); !ok {
		t.Error("the item's Referenced SOP Class UID was lost")
	}

	if elem, ok := got.Get(tag.New(0x0009, 0x0010)); !ok || elem.GetVR() != "LO" {
		t.Errorf("the private creator is %q, want LO (PS3.5 7.8.1)", elem.GetVR())
	}

	// A private tag nothing knows stays UN: there is no dictionary entry to
	// take a VR from, and guessing would describe bytes as something they may
	// not be.
	if elem, ok := got.Get(tag.New(0x0009, 0x1001)); !ok || elem.GetVR() != "UN" {
		t.Errorf("the unknown private tag is %q, want UN", elem.GetVR())
	}
}

// unElement encodes an element with VR UN, which uses the long form header.
func unElement(group, element uint16, value []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, group)
	_ = binary.Write(&buf, binary.LittleEndian, element)
	buf.WriteString("UN")
	buf.Write([]byte{0x00, 0x00})
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(value)))
	buf.Write(value)
	return buf.Bytes()
}

// TestUNEncodedFileReadsLikeItsTwin is the corpus case: rtdose_rle.dcm is
// pydicom's fixture for this, every element UN, and rtdose.dcm is the same
// study written normally. The two must read the same.
func TestUNEncodedFileReadsLikeItsTwin(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	read := func(name string) *filereader.DICOMFile {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Skipf("%s is not in this corpus", name)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return df
	}

	plain, un := read("rtdose.dcm").GetDataset(), read("rtdose_rle.dcm")
	unDS := un.GetDataset()

	// Every element the plain file types, the UN file types the same way, with
	// the same text.
	for _, elem := range plain.GetAll() {
		tg, ok := elem.Tag()
		// Pixel Data is not comparable: rtdose.dcm is Implicit VR, so it carries
		// the dictionary's "OB or OW", while the RLE twin is explicit and says
		// which one. That ambiguity is the writer's to settle (#118).
		if !ok || elem.GetVR() == "SQ" || tg == tag.New(0x7FE0, 0x0010) {
			continue
		}
		other, ok := unDS.Get(tg)
		if !ok {
			t.Errorf("%s is missing from rtdose_rle.dcm", tg)
			continue
		}
		if other.GetVR() != elem.GetVR() {
			t.Errorf("%s reads as %q in rtdose_rle.dcm and %q in rtdose.dcm",
				tg, other.GetVR(), elem.GetVR())
		}
	}

	for _, ds := range []*dataset.Dataset{plain, unDS} {
		seq, err := ds.GetSequence(tag.New(0x300C, 0x0002))
		if err != nil || seq.Length() != 1 {
			t.Errorf("Referenced RT Plan Sequence: %v, %d items", err, seqLength(seq))
		}
	}

	// The 35 "VR mismatch ... got UN" warnings were the reader reporting a
	// problem it could have resolved.
	for _, w := range un.Warnings {
		if strings.Contains(w, "got UN") {
			t.Errorf("still warning about a VR it can resolve: %s", w)
		}
	}
}

func seqLength(s *sequence.Sequence) int {
	if s == nil {
		return 0
	}
	return s.Length()
}

// unElementBE is VR UN in an Explicit VR Big Endian data set: the tag and
// length use the file's byte order, but Note 2 says the value is little endian.
func unElementBE(group, element uint16, value []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, group)
	_ = binary.Write(&buf, binary.BigEndian, element)
	buf.WriteString("UN")
	buf.Write([]byte{0x00, 0x00})
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(value)))
	buf.Write(value)
	return buf.Bytes()
}

// TestUNInBigEndianFileIsNotSwapped covers the case #144 left as UN.
//
// Note 2's value is little endian even when the rest of the file is big
// endian. Replacing UN with US/SS and then swapping as if the value used the
// transfer syntax transposes the sample: Rows of 64 becomes 16384, and
// BitsAllocated of 16 becomes 4096. The value is read as little endian and
// the later swap is skipped.
func TestUNInBigEndianFileIsNotSwapped(t *testing.T) {
	rows := make([]byte, 2)
	binary.LittleEndian.PutUint16(rows, 64)
	bits := make([]byte, 2)
	binary.LittleEndian.PutUint16(bits, 16)
	ss := make([]byte, 2)
	binary.LittleEndian.PutUint16(ss, 0xFFFF) // -1 as SS

	var ds bytes.Buffer
	ds.Write(unElementBE(0x0028, 0x0010, rows)) // Rows, US
	ds.Write(unElementBE(0x0028, 0x0100, bits)) // Bits Allocated, US
	ds.Write(unElementBE(0x0028, 0x1041, ss))   // Pixel Intensity Relationship Sign, SS

	df := readFile(t, buildFile("1.2.840.10008.1.2.2", ds.Bytes(), false))
	got := df.GetDataset()

	for _, tc := range []struct {
		name string
		tg   tag.Tag
		vr   string
		want uint16
	}{
		{"Rows", tag.New(0x0028, 0x0010), "US", 64},
		{"BitsAllocated", tag.New(0x0028, 0x0100), "US", 16},
		{"PixelIntensityRelationshipSign", tag.New(0x0028, 0x1041), "SS", 0xFFFF},
	} {
		elem, ok := got.Get(tc.tg)
		if !ok {
			t.Errorf("%s was lost", tc.name)
			continue
		}
		if string(elem.GetVR()) != tc.vr {
			t.Errorf("%s VR = %q, want %s from the dictionary", tc.name, elem.GetVR(), tc.vr)
			continue
		}
		raw, ok := elem.GetValue().([]byte)
		if !ok || len(raw) < 2 {
			t.Errorf("%s value is %T %v, want 2 little-endian bytes", tc.name, elem.GetValue(), raw)
			continue
		}
		if got := binary.LittleEndian.Uint16(raw); got != tc.want {
			t.Errorf("%s = %d (bytes % X), want %d — the UN value was swapped as big endian",
				tc.name, got, raw, tc.want)
		}
	}
}
