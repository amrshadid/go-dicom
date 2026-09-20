package filereader_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// sequenceItem frames a body as a defined-length item, declaring extra bytes it
// does not have when overrun is non-zero.
func sequenceItem(body []byte, overrun uint32) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xE000))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(body))+overrun)
	buf.Write(body)
	return buf.Bytes()
}

// TestItemOverrunningItsSequenceIsKept covers #122. A directory record whose
// declared length runs past the end of its sequence still holds complete
// elements: only the length field is wrong. Dropping it lost an IMAGE record
// from pydicom's DICOMDIR-nooffset, 51 of 52, and DICOMFile.Warnings said
// nothing, because data set warnings were copied to the file before the data
// set was parsed.
func TestItemOverrunningItsSequenceIsKept(t *testing.T) {
	first := explicitElement(0x0008, 0x0100, "SH", []byte("CODE01  "))
	second := explicitElement(0x0008, 0x0100, "SH", []byte("CODE02  "))

	items := append(sequenceItem(first, 0), sequenceItem(second, 24)...)
	var seq bytes.Buffer
	_ = binary.Write(&seq, binary.LittleEndian, uint16(0x0004))
	_ = binary.Write(&seq, binary.LittleEndian, uint16(0x1220)) // Directory Record Sequence
	seq.WriteString("SQ")
	seq.Write([]byte{0x00, 0x00})
	_ = binary.Write(&seq, binary.LittleEndian, uint32(len(items)))
	seq.Write(items)

	var ds bytes.Buffer
	ds.Write(explicitElement(0x0008, 0x0060, "CS", []byte("MR")))
	ds.Write(seq.Bytes())
	// An element after the sequence, which must still be read from the right
	// offset: the sequence ends where it said it would, not where the item
	// claimed to.
	ds.Write(explicitElement(0x0010, 0x0010, "PN", []byte("Doe^John")))

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))

	if len(df.DataElements) != 3 {
		t.Fatalf("read %d top-level elements, want 3", len(df.DataElements))
	}
	sequence := df.DataElements[1]
	if len(sequence.Items) != 2 {
		t.Fatalf("the sequence has %d items, want 2: the overrunning one was dropped",
			len(sequence.Items))
	}
	if got := string(sequence.Items[1].Elements[0].Value); got != "CODE02  " {
		t.Errorf("the second item holds %q, want CODE02", got)
	}
	if got := string(df.DataElements[2].Value); got != "Doe^John" {
		t.Errorf("the element after the sequence reads %q", got)
	}
	if !hasWarning(df.Warnings, "overrun") && !hasWarning(df.Warnings, "left in the sequence") {
		t.Errorf("nothing in DICOMFile.Warnings says the item overran: %v", df.Warnings)
	}
}

// TestItemHeaderAtTheSequenceEndIsNotAnItem: a header with nothing left to read
// is not an item, and recording an empty one would invent a record the file
// does not contain.
func TestItemHeaderAtTheSequenceEndIsNotAnItem(t *testing.T) {
	first := explicitElement(0x0008, 0x0100, "SH", []byte("CODE01  "))
	items := sequenceItem(first, 0)
	// A second item header, with its 8 bytes inside the sequence and a body
	// that is not.
	var header bytes.Buffer
	_ = binary.Write(&header, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&header, binary.LittleEndian, uint16(0xE000))
	_ = binary.Write(&header, binary.LittleEndian, uint32(16))
	items = append(items, header.Bytes()...)

	var seq bytes.Buffer
	_ = binary.Write(&seq, binary.LittleEndian, uint16(0x0004))
	_ = binary.Write(&seq, binary.LittleEndian, uint16(0x1220))
	seq.WriteString("SQ")
	seq.Write([]byte{0x00, 0x00})
	_ = binary.Write(&seq, binary.LittleEndian, uint32(len(items)))
	seq.Write(items)

	var ds bytes.Buffer
	ds.Write(seq.Bytes())
	ds.Write(explicitElement(0x0010, 0x0010, "PN", []byte("Doe^John")))

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))
	if len(df.DataElements[0].Items) != 1 {
		t.Fatalf("the sequence has %d items, want 1: an empty item was recorded",
			len(df.DataElements[0].Items))
	}
	if len(df.Warnings) == 0 {
		t.Error("nothing warned that a header sat at the end of the sequence")
	}
}

// TestDICOMDIRNoOffsetKeepsEveryRecord is the corpus case: pydicom's
// DICOMDIR-nooffset must read the same instances as DICOMDIR, which is what
// pydicom's own test asserts.
func TestDICOMDIRNoOffsetKeepsEveryRecord(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	count := func(name string) (int, []string) {
		raw, err := os.ReadFile(filepath.Join(dir, "dicomdirtests", name))
		if err != nil {
			raw, err = os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Skipf("%s is not in this corpus", name)
			}
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, elem := range df.DataElements {
			if elem.Tag.Group() == 0x0004 && elem.Tag.Element() == 0x1220 {
				var types []string
				for _, item := range elem.Items {
					for _, e := range item.Elements {
						if e.Tag.Group() == 0x0004 && e.Tag.Element() == 0x1430 {
							types = append(types, strings.TrimSpace(string(e.Value)))
						}
					}
				}
				return len(elem.Items), types
			}
		}
		t.Fatalf("%s has no Directory Record Sequence", name)
		return 0, nil
	}

	wantRecords, wantTypes := count("DICOMDIR")
	gotRecords, gotTypes := count("DICOMDIR-nooffset")
	if gotRecords != wantRecords {
		t.Errorf("DICOMDIR-nooffset has %d records, DICOMDIR has %d", gotRecords, wantRecords)
	}
	if strings.Join(gotTypes, ",") != strings.Join(wantTypes, ",") {
		t.Errorf("the record types differ:\n nooffset: %v\n DICOMDIR: %v", gotTypes, wantTypes)
	}
}
