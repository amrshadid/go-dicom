package filereader_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// A file that declares explicit VR and holds elements written without one
// is read, each such element as implicit. That it happened was said only in
// the library's log, which the commands silence, so nothing a program reads
// said the file is not conformant (#305).

// mixedDataset is an explicit VR data set with the given number of elements
// written without a VR after one written with it.
func mixedDataset(implicit int) []byte {
	var out bytes.Buffer
	le := binary.LittleEndian
	// (0008,0060) Modality, explicit: tag, VR, 2-byte length, value.
	_ = binary.Write(&out, le, [2]uint16{0x0008, 0x0060})
	out.WriteString("CS")
	_ = binary.Write(&out, le, uint16(2))
	out.WriteString("CT")
	// (0010,00x0) and on, implicit: tag, 4-byte length, value.
	elements := [][2]uint16{{0x0010, 0x0010}, {0x0010, 0x0020}, {0x0010, 0x0040}}
	for i := 0; i < implicit; i++ {
		_ = binary.Write(&out, le, elements[i])
		_ = binary.Write(&out, le, uint32(4))
		out.WriteString("ABCD")
	}
	return out.Bytes()
}

func readMixed(t *testing.T, implicit int) *filereader.DICOMFile {
	t.Helper()
	data := buildFile("1.2.840.10008.1.2.1", mixedDataset(implicit), false)
	file, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	return file
}

// TestElementsWithoutAVRAreWarnedOfOnce checks the warning: one line however
// many elements, with their number and the first of them, and the elements
// read all the same.
func TestElementsWithoutAVRAreWarnedOfOnce(t *testing.T) {
	file := readMixed(t, 3)

	var found []string
	for _, warning := range file.Warnings {
		if strings.Contains(warning, "without a VR") {
			found = append(found, warning)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d warnings about elements without a VR, want 1: %v", len(found), file.Warnings)
	}
	if !strings.HasPrefix(found[0], "3 element(s)") || !strings.Contains(found[0], "(0010,0010)") {
		t.Errorf("the warning is %q", found[0])
	}

	if len(file.DataElements) != 4 {
		t.Fatalf("%d elements read, want 4", len(file.DataElements))
	}
	name := file.DataElements[1]
	if name.VR != "PN" || string(name.Value) != "ABCD" {
		t.Errorf("the first element without a VR was read as %s %q", name.VR, name.Value)
	}
}

// TestAConformantFileHasNoSuchWarning checks that a file with every VR
// written is not warned about.
func TestAConformantFileHasNoSuchWarning(t *testing.T) {
	for _, warning := range readMixed(t, 0).Warnings {
		if strings.Contains(warning, "without a VR") {
			t.Errorf("a conformant file is warned about: %s", warning)
		}
	}
}

// TestInsideAnUnknownSequenceNoVRIsExpected checks the case that is not a
// fault: an element of VR UN and undefined length holds a sequence in
// implicit VR by rule (PS3.5 6.2.2 Note 5), so the elements in it have no
// VR and the file is conformant. pydicom does not warn of it either.
func TestInsideAnUnknownSequenceNoVRIsExpected(t *testing.T) {
	var ds bytes.Buffer
	le := binary.LittleEndian
	// (0008,1115) Referenced Series Sequence as UN, undefined length.
	_ = binary.Write(&ds, le, [2]uint16{0x0008, 0x1115})
	ds.WriteString("UN")
	_ = binary.Write(&ds, le, uint16(0))
	_ = binary.Write(&ds, le, uint32(0xFFFFFFFF))
	// One item of undefined length holding (0020,000E), implicit.
	_ = binary.Write(&ds, le, [2]uint16{0xFFFE, 0xE000})
	_ = binary.Write(&ds, le, uint32(0xFFFFFFFF))
	_ = binary.Write(&ds, le, [2]uint16{0x0020, 0x000E})
	_ = binary.Write(&ds, le, uint32(4))
	ds.WriteString("1.2\x00")
	_ = binary.Write(&ds, le, [2]uint16{0xFFFE, 0xE00D})
	_ = binary.Write(&ds, le, uint32(0))
	_ = binary.Write(&ds, le, [2]uint16{0xFFFE, 0xE0DD})
	_ = binary.Write(&ds, le, uint32(0))

	data := buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false)
	file, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	for _, warning := range file.Warnings {
		if strings.Contains(warning, "without a VR") {
			t.Errorf("a UN sequence is warned about: %s", warning)
		}
	}
	if len(file.DataElements) != 1 || len(file.DataElements[0].Items) != 1 {
		t.Fatalf("the sequence was not read: %d elements", len(file.DataElements))
	}
}
