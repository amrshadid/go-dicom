package cli

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// show read every text value as Latin-1, whatever character set its file
// declared: a Greek or Arabic name came out as accented Latin letters
// (#270).

// charsetFile is an Explicit VR Little Endian file in ISO_IR 126 (Greek)
// holding a sequence item that declares ISO_IR 144 (Cyrillic) for itself.
func charsetFile(t *testing.T) string {
	t.Helper()
	short := func(group, elem uint16, vr string, value []byte) []byte {
		if len(value)%2 == 1 {
			value = append(value, ' ')
		}
		out := binary.LittleEndian.AppendUint16(nil, group)
		out = binary.LittleEndian.AppendUint16(out, elem)
		out = append(out, vr...)
		out = binary.LittleEndian.AppendUint16(out, uint16(len(value)))
		return append(out, value...)
	}
	const syntax = "1.2.840.10008.1.2.1\x00"
	var meta bytes.Buffer
	meta.Write(short(0x0002, 0x0010, "UI", []byte(syntax)))

	var f bytes.Buffer
	f.Write(make([]byte, 128))
	f.WriteString("DICM")
	f.Write([]byte{0x02, 0x00, 0x00, 0x00, 'U', 'L', 4, 0})
	f.Write(binary.LittleEndian.AppendUint32(nil, uint32(meta.Len())))
	f.Write(meta.Bytes())

	f.Write(short(0x0008, 0x0005, "CS", []byte("ISO_IR 126")))
	// An item of its own character set: "Люк" in ISO 8859-5.
	item := append(short(0x0008, 0x0005, "CS", []byte("ISO_IR 144")), short(0x0010, 0x0010, "PN", []byte{0xBB, 0xEE, 0xDA})...)
	f.Write([]byte{0x08, 0x00, 0x40, 0x11, 'S', 'Q', 0, 0})
	f.Write(binary.LittleEndian.AppendUint32(nil, uint32(8+len(item))))
	f.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
	f.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(item))))
	f.Write(item)
	// "Διονυσιος" in ISO 8859-7, after the item: the file's own set again.
	f.Write(short(0x0010, 0x0010, "PN", []byte{0xC4, 0xE9, 0xEF, 0xED, 0xF5, 0xF3, 0xE9, 0xEF, 0xF2}))
	// A UID is in the default repertoire whatever the file declares.
	f.Write(short(0x0020, 0x000D, "UI", []byte("1.2.3")))

	path := filepath.Join(t.TempDir(), "greek.dcm")
	if err := os.WriteFile(path, f.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShowDecodesTheDeclaredCharacterSet(t *testing.T) {
	path := charsetFile(t)
	doc := showJSON(t, path)

	var name, nested string
	for _, e := range doc.DataSet {
		switch e.Tag {
		case "(0010,0010)":
			name = *e.Value
		case "(0008,1140)":
			for _, inner := range e.Items[0].Elements {
				if inner.Tag == "(0010,0010)" {
					nested = *inner.Value
				}
			}
		}
	}
	if name != "Διονυσιος" {
		t.Errorf("--json shows the name as %q, want Διονυσιος", name)
	}
	if nested != "Люк" {
		t.Errorf("--json shows the item's name as %q, want Люк", nested)
	}

	// The text table, and what info and convert are built from.
	elements, err := readDICOMFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var shown []string
	for _, e := range elements {
		if e.Tag == "0010,0010" {
			// Padded to an even length, as it is in the file.
			shown = append(shown, strings.TrimSpace(formatValueIn(e.VR, e.Value, 100, e.Encodings)))
		}
	}
	if len(shown) != 2 || shown[0] != "Люк" || shown[1] != "Διονυσιος" {
		t.Errorf("the table shows the names as %q", shown)
	}
	if info := extractDICOMInfo(elements); info.PatientName != "Διονυσιος" && info.PatientName != "Люк" {
		t.Errorf("info shows the patient as %q", info.PatientName)
	}
}

// pydicom's character set files, each with the name pydicom reads from it.
func TestShowOfPydicomCharsetFiles(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for file, want := range map[string]string{
		"chrArab.dcm":  "قباني^لنزار",
		"chrFren.dcm":  "Buc^Jérôme",
		"chrGerm.dcm":  "Äneas^Rüdiger",
		"chrGreek.dcm": "Διονυσιος",
		"chrH31.dcm":   "Yamada^Tarou=山田^太郎=やまだ^たろう",
		"chrH32.dcm":   "ﾔﾏﾀﾞ^ﾀﾛｳ=山田^太郎=やまだ^たろう",
		"chrHbrw.dcm":  "שרון^דבורה",
		"chrI2.dcm":    "Hong^Gildong=洪^吉洞=홍^길동",
		"chrRuss.dcm":  "Люкceмбypг",
		// The file ends these two with an empty phonetic group, which is shown
		// as it is written; pydicom leaves it off when it prints the name.
		"chrX1.dcm":                  "Wang^XiaoDong=王^小東=",
		"chrX2.dcm":                  "Wang^XiaoDong=王^小东=",
		"chrKoreanMulti.dcm":         "김희중",
		"chrFrenMulti.dcm":           "Buc^Jérôme",
		"chrJapMulti.dcm":            "やまだ^たろう",
		"chrJapMultiExplicitIR6.dcm": "やまだ^たろう",
	} {
		path := filepath.Join(dir, "..", "charset_files", file)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		for _, e := range showJSON(t, path).DataSet {
			if e.Tag == "(0010,0010)" && (e.Value == nil || *e.Value != want) {
				t.Errorf("%s: the name is shown as %v, want %q", file, e.Value, want)
			}
		}
	}
}
