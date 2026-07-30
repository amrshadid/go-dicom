package charset_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// pydicom ships a set of files whose only purpose is to exercise character sets:
// Arabic, Greek, Hebrew, Japanese, Korean, Russian, Chinese and accented Latin,
// in single-byte, multi-byte and ISO 2022 escape-switched forms.
//
// Checking against them found a defect that only a double-byte set can show. The
// Person Name delimiters ^ and = were located by scanning bytes one at a time,
// and in JIS X 0208 the second byte of a character occupies the same range as
// printable ASCII. ま is 0x24 0x5E, and 0x5E is ^ — so the scan split a
// character in half and the decoder lost the rest of the string:
//
//	やまだ^たろう  decoded as  や?^$@^たろう
//
// The expectations below are pydicom's own decodes of the same files.
var pydicomCharsetNames = map[string]string{
	"chrArab.dcm":                "قباني^لنزار",
	"chrFren.dcm":                "Buc^Jérôme",
	"chrFrenMulti.dcm":           "Buc^Jérôme",
	"chrGerm.dcm":                "Äneas^Rüdiger",
	"chrGreek.dcm":               "Διονυσιος",
	"chrH31.dcm":                 "Yamada^Tarou=山田^太郎=やまだ^たろう",
	"chrH32.dcm":                 "ﾔﾏﾀﾞ^ﾀﾛｳ=山田^太郎=やまだ^たろう",
	"chrHbrw.dcm":                "שרון^דבורה",
	"chrI2.dcm":                  "Hong^Gildong=洪^吉洞=홍^길동",
	"chrJapMulti.dcm":            "やまだ^たろう",
	"chrJapMultiExplicitIR6.dcm": "やまだ^たろう",
	"chrKoreanMulti.dcm":         "김희중",
	"chrRuss.dcm":                "Люкceмбypг",
	"chrX1.dcm":                  "Wang^XiaoDong=王^小東",
	"chrX2.dcm":                  "Wang^XiaoDong=王^小东",
}

// charsetDir locates pydicom's charset fixtures, which sit beside the corpus
// GODICOM_PYDICOM_DATA points at.
func charsetDir(t *testing.T) string {
	t.Helper()

	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	dir := filepath.Join(filepath.Dir(corpus), "charset_files")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("pydicom's charset_files are not beside the corpus: %v", err)
	}
	return dir
}

// TestPersonNamesAgainstPydicomCharsetFiles decodes every charset fixture and
// compares the patient name with pydicom's reading of it.
func TestPersonNamesAgainstPydicomCharsetFiles(t *testing.T) {
	dir := charsetDir(t)

	for file, want := range pydicomCharsetNames {
		t.Run(file, func(t *testing.T) {
			path := filepath.Join(dir, file)
			f, err := os.Open(path)
			if err != nil {
				t.Skipf("%s is not present: %v", file, err)
			}
			defer func() { _ = f.Close() }()

			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatalf("ReadDICOMFile: %v", err)
			}

			var raw *filereader.DataElementValue
			for _, elem := range df.DataElements {
				if elem.Tag == 0x00100010 {
					raw = elem
					break
				}
			}
			if raw == nil {
				t.Fatal("the file has no Patient Name")
			}

			pn, err := df.DecodePersonName(raw)
			if err != nil {
				t.Fatalf("DecodePersonName: %v", err)
			}

			// Trailing padding is not part of the name: DICOM pads values to an
			// even length, and an absent trailing group leaves a separator.
			got := trimName(pn.String())
			if got != trimName(want) {
				t.Errorf("patient name is %q, pydicom reads %q", got, want)
			}
		})
	}
}

// trimName removes the padding and empty trailing groups a stored value carries.
func trimName(s string) string {
	for len(s) > 0 {
		switch s[len(s)-1] {
		case ' ', 0, '=', '^':
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}
