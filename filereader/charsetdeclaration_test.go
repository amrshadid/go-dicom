package filereader_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// latin1Name is "Müller^Jürgen" as a modality with no character set declaration
// writes it: one byte per character.
var latin1Name = []byte{'M', 0xFC, 'l', 'l', 'e', 'r', '^', 'J', 0xFC, 'r', 'g', 'e', 'n'}

// TestUndeclaredLatin1TextIsDecoded covers #115. A file that declares no
// character set, or declares an empty one, and holds non-ASCII text is
// non-conformant (PS3.5 6.1.2.3 makes the default repertoire ASCII) and common
// from older equipment. The bytes were left as they were found and the data set
// was then labeled ISO_IR 192, so it asserted UTF-8 over Latin-1: a write-back
// produced "M?ller^J?rgen" in pydicom. An unrecognized term such as ISO_IR 999
// already fell back to Latin-1 and decoded correctly; only the empty and absent
// cases did not.
func TestUndeclaredLatin1TextIsDecoded(t *testing.T) {
	tests := map[string]struct{ charset []byte }{
		"declared empty":    {[]byte("  ")},
		"absent":            {nil},
		"unrecognized term": {[]byte("ISO_IR 999 ")},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var ds bytes.Buffer
			if tc.charset != nil {
				ds.Write(explicitElement(0x0008, 0x0005, "CS", tc.charset))
			}
			ds.Write(explicitElement(0x0010, 0x0010, "PN", append(append([]byte{}, latin1Name...), ' ')))

			df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))
			got := df.GetDataset()

			name, err := got.GetPersonName(tag.New(0x0010, 0x0010))
			if err != nil {
				t.Fatalf("GetPersonName: %v", err)
			}
			if want := "Müller^Jürgen"; strings.TrimSpace(name.String()) != want {
				t.Errorf("patient name = %q, want %q", name.String(), want)
			}
			assertDeclarationMatchesBytes(t, got)

			// The caller is told a guess was made, since Latin-1 is a guess:
			// the file said nothing, and some other single-byte set is possible.
			if !hasWarning(df.Warnings, "Character Set") {
				t.Errorf("no warning that undeclared text was decoded as Latin-1: %v", df.Warnings)
			}
		})
	}
}

// TestNoDataSetDeclaresUTF8OverBytesThatAreNot is the property the fix is for,
// over pydicom's whole corpus: a data set says ISO_IR 192 only when its text
// really is UTF-8. Before, the declaration was written unconditionally, so a
// file that declared no character set and held Latin-1 was labeled UTF-8 over
// bytes nobody had converted.
func TestNoDataSetDeclaresUTF8OverBytesThatAreNot(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	if len(paths) == 0 {
		t.Fatalf("no .dcm files in %s", dir)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
			if err != nil {
				t.Skipf("not readable: %v", err)
			}
			assertDeclarationMatchesBytes(t, df.GetDataset())
		})
	}
}

// TestAsciiWithAnEmptyDeclarationIsUnchanged: the common case must not grow a
// warning. ASCII is valid UTF-8, so the data set may say ISO_IR 192.
func TestAsciiWithAnEmptyDeclarationIsUnchanged(t *testing.T) {
	var ds bytes.Buffer
	ds.Write(explicitElement(0x0008, 0x0005, "CS", []byte("  ")))
	ds.Write(explicitElement(0x0010, 0x0010, "PN", []byte("Doe^John")))

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))
	assertDeclarationMatchesBytes(t, df.GetDataset())
	if hasWarning(df.Warnings, "Character Set") {
		t.Errorf("an all-ASCII file warned about its character set: %v", df.Warnings)
	}
}

// assertDeclarationMatchesBytes checks the promise this fix is about: a data set
// declares ISO_IR 192 only when every text value in it is valid UTF-8, at every
// level, since a sequence item may declare its own character set.
func assertDeclarationMatchesBytes(t *testing.T, ds *dataset.Dataset) {
	t.Helper()

	declaresUTF8 := false
	if elem, ok := ds.Get(tag.New(0x0008, 0x0005)); ok {
		if raw, ok := elem.GetValue().([]byte); ok {
			declaresUTF8 = strings.TrimSpace(string(raw)) == "ISO_IR 192"
		}
	}
	for _, elem := range ds.GetAll() {
		tg, ok := elem.Tag()
		if !ok {
			continue
		}
		raw, ok := elem.GetValue().([]byte)
		if !ok || !dataelem.IsTextVR(elem.GetVR()) || utf8.Valid(raw) {
			continue
		}
		if declaresUTF8 {
			t.Errorf("%s holds bytes that are not UTF-8 while the data set declares ISO_IR 192", tg)
		}
	}
}

func hasWarning(warnings []string, substring string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substring) {
			return true
		}
	}
	return false
}
