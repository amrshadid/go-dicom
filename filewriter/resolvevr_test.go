package filewriter_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

const (
	implicitVRLittleEndian = "1.2.840.10008.1.2"
	explicitVRLittleEndian = "1.2.840.10008.1.2.1"
)

// writeAsSyntax writes elements as a Part 10 file in the given transfer syntax.
func writeAsSyntax(t *testing.T, elements []*filewriter.DataElement, syntax string) []byte {
	t.Helper()

	out := &growBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          syntax,
	})
	for _, e := range elements {
		if err := w.AddDataElement(e); err != nil {
			t.Fatalf("AddDataElement %s: %v", e.Tag, err)
		}
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return out.Bytes()
}

// readBytes reads a Part 10 file held in memory.
func readBytes(t *testing.T, raw []byte) *dataset.Dataset {
	t.Helper()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	return df.GetDataset()
}

func uint16Bytes(values ...uint16) []byte {
	out := make([]byte, 2*len(values))
	for i, v := range values {
		binary.LittleEndian.PutUint16(out[2*i:], v)
	}
	return out
}

// TestImplicitToExplicitKeepsEveryElement is the defect in #118.
//
// A data set read from an Implicit VR file holds the dictionary's VR, which for
// Pixel Data is "OB or OW" and for Smallest Image Pixel Value "US or SS".
// Written as Explicit VR, those eight characters went into the two-byte VR
// field and the six bytes after it, the length was read from "r OW", and Pixel
// Data and everything after it was gone. MR_small_implicit.dcm lost 4 of its 72
// elements, Pixel Data among them.
//
// The item in the Modality LUT Sequence carries a "US or SS" of its own and no
// Pixel Representation: it takes the image's, which here is signed.
func TestImplicitToExplicitKeepsEveryElement(t *testing.T) {
	lutItem := dataset.NewDataset()
	_ = lutItem.Add(dataelem.NewDataElement(tag.New(0x0028, 0x3002), dataelem.SS, uint16Bytes(4, 0xFFF0, 16)))
	_ = lutItem.Add(dataelem.NewDataElement(tag.New(0x0028, 0x3006), dataelem.OW, uint16Bytes(1, 2, 3, 4)))
	lut := sequence.New()
	_ = lut.Append(lutItem)

	source := dataset.NewDataset()
	_ = source.Add(dataelem.NewDataElement(tag.New(0x0019, 0x0010), dataelem.LO, []byte("ACME 1.0")))
	_ = source.Add(dataelem.NewDataElement(tag.New(0x0019, 0x1001), dataelem.UN, []byte{1, 2, 3, 4}))
	_ = source.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.US, uint16Bytes(16)))
	_ = source.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0103), dataelem.US, uint16Bytes(1)))
	_ = source.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0106), dataelem.SS, uint16Bytes(0xFFFB)))
	_ = source.AddSequence(tag.New(0x0028, 0x3000), lut)
	_ = source.Add(dataelem.NewDataElement(tag.New(0x6000, 0x3000), dataelem.OW, []byte{0xAA, 0x55}))
	_ = source.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, uint16Bytes(1, 2, 3, 40000)))
	_ = source.Add(dataelem.NewDataElement(tag.New(0x0009, 0x1002), dataelem.OB, []byte{9, 9}))

	implicit := readBytes(t, writeAsSyntax(t, filewriter.ElementsFromDataset(source), implicitVRLittleEndian))

	// The reader has done what makes this hard: the VRs are the dictionary's.
	if elem, _ := implicit.Get(tag.New(0x7FE0, 0x0010)); elem == nil || !strings.Contains(string(elem.GetVR()), " or ") {
		t.Fatalf("the implicit read did not produce an ambiguous Pixel Data VR, so this tests nothing")
	}

	explicit := readBytes(t, writeAsSyntax(t, filewriter.ElementsFromDataset(implicit), explicitVRLittleEndian))

	want := map[tag.Tag]dataelem.VR{
		tag.New(0x0019, 0x0010): dataelem.LO,
		tag.New(0x0019, 0x1001): dataelem.UN,
		tag.New(0x0028, 0x0100): dataelem.US,
		tag.New(0x0028, 0x0103): dataelem.US,
		tag.New(0x0028, 0x0106): dataelem.SS,
		tag.New(0x0028, 0x3000): dataelem.SQ,
		tag.New(0x6000, 0x3000): dataelem.OW,
		tag.New(0x7FE0, 0x0010): dataelem.OW,
		tag.New(0x0009, 0x1002): dataelem.UN, // private, no creator
	}
	if got := explicit.Length(); got != len(want) {
		t.Errorf("the rewritten file holds %d elements, want %d", got, len(want))
	}
	for tg, vr := range want {
		elem, ok := explicit.Get(tg)
		if !ok {
			t.Errorf("%s was lost", tg)
			continue
		}
		if elem.GetVR() != vr {
			t.Errorf("%s VR = %q, want %s", tg, elem.GetVR(), vr)
		}
		if vr == dataelem.SQ {
			continue
		}
		before, _ := implicit.Get(tg)
		if !bytes.Equal(elem.GetValue().([]byte), before.GetValue().([]byte)) {
			t.Errorf("%s value = % x, want % x", tg, elem.GetValue(), before.GetValue())
		}
	}

	seq, err := explicit.GetSequence(tag.New(0x0028, 0x3000))
	if err != nil || seq.Length() != 1 {
		t.Fatalf("the Modality LUT Sequence did not survive: %v", err)
	}
	raw, _ := seq.Get(0)
	item := raw.(*dataset.Dataset)
	for tg, vr := range map[tag.Tag]dataelem.VR{
		tag.New(0x0028, 0x3002): dataelem.SS, // Pixel Representation from the image
		tag.New(0x0028, 0x3006): dataelem.OW, // four entries, so not US
	} {
		elem, ok := item.Get(tg)
		if !ok {
			t.Errorf("item %s was lost", tg)
			continue
		}
		if elem.GetVR() != vr {
			t.Errorf("item %s VR = %q, want %s", tg, elem.GetVR(), vr)
		}
	}
}

// TestAHandBuiltElementCannotCorruptTheFile covers a DataElement that never
// went through ElementsFromDataset. Its VR is written into a two-byte field as
// given, so one that is not two characters used to shift everything after it.
func TestAHandBuiltElementCannotCorruptTheFile(t *testing.T) {
	for _, vr := range []string{"", "OB or OW", "X"} {
		t.Run(vr, func(t *testing.T) {
			elements := []*filewriter.DataElement{
				{Tag: tag.New(0x0009, 0x1001), VR: vr, Value: []byte{1, 2, 3, 4}, Length: 4},
				{Tag: tag.New(0x0010, 0x0010), VR: "PN", Value: []byte("Doe^John"), Length: 8},
			}
			back := readBytes(t, writeAsSyntax(t, elements, explicitVRLittleEndian))

			odd, ok := back.Get(tag.New(0x0009, 0x1001))
			if !ok {
				t.Fatal("the element was lost")
			}
			if odd.GetVR() != dataelem.UN {
				t.Errorf("VR = %q, want UN", odd.GetVR())
			}
			name, ok := back.Get(tag.New(0x0010, 0x0010))
			if !ok {
				t.Fatal("the element after it was lost")
			}
			if got := string(name.GetValue().([]byte)); got != "Doe^John" {
				t.Errorf("the element after it reads %q", got)
			}
		})
	}
}

// TestRewriteAsExplicitAgainstWholePydicomCorpus writes every uncompressed
// corpus file as Explicit VR Little Endian and reads it back. Every element
// read from the original must come back with the same value.
//
// Before #118 was fixed, six files lost elements here, MR_small_implicit.dcm its
// Pixel Data among them. Nothing is tolerated. The interop round trip tolerated
// two unreadable rewrites, and both had this defect.
func TestRewriteAsExplicitAgainstWholePydicomCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	if len(paths) == 0 {
		t.Fatalf("no .dcm files in %s", dir)
	}

	uncompressed := map[string]bool{
		"1.2.840.10008.1.2": true, "1.2.840.10008.1.2.1": true, "1.2.840.10008.1.2.2": true,
	}
	var rewritten int
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
			if err != nil {
				t.Skipf("not readable as supplied: %v", err)
			}
			if df.FileMetaInfo == nil || !uncompressed[df.FileMetaInfo.TransferSyntaxUID] {
				t.Skip("not an uncompressed syntax")
			}
			original := df.GetDataset()

			back := readBytes(t, writeAsSyntax(t, filewriter.ElementsFromDataset(original), explicitVRLittleEndian))
			rewritten++
			sameElements(t, "", original, back)
		})
	}
	t.Logf("%d files rewritten and compared", rewritten)
}

// sameElements reports every element of want that got lacks or holds a
// different value, descending into sequences.
func sameElements(t *testing.T, path string, want, got *dataset.Dataset) {
	t.Helper()

	for _, elem := range want.GetAll() {
		tg, ok := elem.Tag()
		if !ok || tg.Group() == 0x0002 {
			continue
		}
		where := path + tg.String()
		other, ok := got.Get(tg)
		if !ok {
			t.Errorf("%s (%s) was lost", where, elem.GetVR())
			continue
		}
		if seq, isSeq := elem.GetValue().(*sequence.Sequence); isSeq {
			otherSeq, _ := other.GetValue().(*sequence.Sequence)
			if otherSeq == nil || otherSeq.Length() != seq.Length() {
				t.Errorf("%s: sequence items changed", where)
				continue
			}
			for i := 0; i < seq.Length(); i++ {
				a, _ := seq.Get(i)
				b, _ := otherSeq.Get(i)
				ad, _ := a.(*dataset.Dataset)
				bd, _ := b.(*dataset.Dataset)
				if ad != nil && bd != nil {
					sameElements(t, where+"/", ad, bd)
				}
			}
			continue
		}
		a, _ := elem.GetValue().([]byte)
		b, _ := other.GetValue().([]byte)
		if !bytes.Equal(bytes.TrimRight(a, " \x00"), bytes.TrimRight(b, " \x00")) {
			t.Errorf("%s (%s → %s) value changed", where, elem.GetVR(), other.GetVR())
		}
	}
}
