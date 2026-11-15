package filereader

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/tag"
)

// A file meta group with no Transfer Syntax UID was read as implicit VR
// little endian without a word, so a guessed encoding could not be told from
// a declared one (#273).

// metaFile is a Part 10 file whose meta group holds the given elements, as
// tag and text value, before a data set.
func metaFile(meta [][2]string, dataSet []byte) []byte {
	var group bytes.Buffer
	for _, e := range meta {
		value := e[1]
		if len(value)%2 == 1 {
			value += "\x00"
		}
		var element uint16
		for _, c := range e[0] {
			element = element<<4 | uint16(strings.IndexRune("0123456789ABCDEF", c))
		}
		group.Write([]byte{0x02, 0x00, byte(element), byte(element >> 8), 'U', 'I'})
		_ = binary.Write(&group, binary.LittleEndian, uint16(len(value)))
		group.WriteString(value)
	}
	var out bytes.Buffer
	out.Write(make([]byte, 128))
	out.WriteString("DICM")
	out.Write([]byte{0x02, 0x00, 0x00, 0x00, 'U', 'L', 4, 0})
	_ = binary.Write(&out, binary.LittleEndian, uint32(group.Len()))
	out.Write(group.Bytes())
	out.Write(dataSet)
	return out.Bytes()
}

var (
	explicitName = []byte{0x10, 0x00, 0x10, 0x00, 'P', 'N', 4, 0, 'D', 'o', 'e', ' '}
	implicitName = []byte{0x10, 0x00, 0x10, 0x00, 4, 0, 0, 0, 'D', 'o', 'e', ' '}
)

func readBytes(t *testing.T, data []byte) *DICOMFile {
	t.Helper()
	df, err := ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatal(err)
	}
	return df
}

func warned(df *DICOMFile, about string) bool {
	for _, w := range df.Warnings {
		if strings.Contains(w, about) {
			return true
		}
	}
	return false
}

func nameOf(df *DICOMFile) string {
	for _, e := range df.DataElements {
		if e.Tag == tag.New(0x0010, 0x0010) {
			return string(e.Value)
		}
	}
	return ""
}

func TestMissingTransferSyntaxIsSaid(t *testing.T) {
	sopClass := [2]string{"0002", "1.2.840.10008.5.1.4.1.1.7"}

	// The data set says how it is encoded, whichever way that is.
	for _, tc := range []struct {
		name     string
		dataSet  []byte
		explicit bool
		encoding string
	}{
		{"explicit", explicitName, true, "explicit VR little endian"},
		{"implicit", implicitName, false, "implicit VR little endian"},
	} {
		df := readBytes(t, metaFile([][2]string{sopClass}, tc.dataSet))
		if df.ExplicitVR != tc.explicit || nameOf(df) != "Doe " {
			t.Errorf("%s: read as explicit=%v, name %q", tc.name, df.ExplicitVR, nameOf(df))
		}
		if !warned(df, "(0002,0010)") || !warned(df, tc.encoding) {
			t.Errorf("%s: the warnings do not name the missing element and the encoding used: %q", tc.name, df.Warnings)
		}
	}

	// A UID that is not a transfer syntax the standard registers says
	// nothing of the encoding either.
	df := readBytes(t, metaFile([][2]string{sopClass, {"0010", "1.2.3.4.5"}}, implicitName))
	if df.ExplicitVR || nameOf(df) != "Doe " {
		t.Errorf("unknown UID: read as explicit=%v, name %q", df.ExplicitVR, nameOf(df))
	}
	if !warned(df, "1.2.3.4.5") || !warned(df, "implicit VR little endian") {
		t.Errorf("unknown UID: the warnings are %q", df.Warnings)
	}

	// A declared syntax is taken at its word, and nothing is said.
	for _, syntax := range []string{"1.2.840.10008.1.2.1", "1.2.840.10008.1.2.4.70", "1.2.840.10008.1.2.4.203"} {
		df := readBytes(t, metaFile([][2]string{sopClass, {"0010", syntax}}, explicitName))
		if !df.ExplicitVR || warned(df, "Transfer Syntax") {
			t.Errorf("%s: explicit=%v, warnings %q", syntax, df.ExplicitVR, df.Warnings)
		}
	}
	if df := readBytes(t, metaFile([][2]string{sopClass, {"0010", "1.2.840.10008.1.2"}}, implicitName)); df.ExplicitVR || warned(df, "Transfer Syntax") {
		t.Errorf("implicit VR little endian: explicit=%v, warnings %q", df.ExplicitVR, df.Warnings)
	}
}

func TestPydicomFileWithNoTransferSyntax(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	data, err := os.ReadFile(filepath.Join(dir, "meta_missing_tsyntax.dcm"))
	if err != nil {
		t.Fatal(err)
	}
	df := readBytes(t, data)
	if !warned(df, "(0002,0010)") {
		t.Errorf("no warning for the missing Transfer Syntax UID: %q", df.Warnings)
	}
	// pydicom reads its 2 elements as implicit VR little endian.
	if df.ExplicitVR || len(df.DataElements) != 2 {
		t.Errorf("read as explicit=%v with %d elements", df.ExplicitVR, len(df.DataElements))
	}
}
