package cli

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShowJSONCarriesATagOrderWarningWithinTheSchema proves a data set whose
// tags repeat and decrease puts the reader's warning in the `warnings` array
// of `show --json`, as strings, and that the document still matches
// docs/show-json.schema.json at schemaVersion 1: a new warning is a new
// string in an array of strings, not a new property.
func TestShowJSONCarriesATagOrderWarningWithinTheSchema(t *testing.T) {
	element := func(group, elem uint16, vr, value string) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.LittleEndian, [2]uint16{group, elem})
		b.WriteString(vr)
		_ = binary.Write(&b, binary.LittleEndian, uint16(len(value)))
		b.WriteString(value)
		return b.Bytes()
	}
	var meta bytes.Buffer
	meta.Write(element(0x0002, 0x0010, "UI", "1.2.840.10008.1.2.1\x00"))
	var file bytes.Buffer
	file.Write(make([]byte, 128))
	file.WriteString("DICM")
	group := make([]byte, 4)
	binary.LittleEndian.PutUint32(group, uint32(meta.Len()))
	file.Write(element(0x0002, 0x0000, "UL", string(group)))
	file.Write(meta.Bytes())
	file.Write(element(0x0010, 0x0020, "LO", "AB"))
	file.Write(element(0x0010, 0x0020, "LO", "CD")) // repeated
	file.Write(element(0x0008, 0x0070, "LO", "EF")) // decreasing

	path := filepath.Join(t.TempDir(), "unordered.dcm")
	if err := os.WriteFile(path, file.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeShowJSON(&out, path); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := decodeJSON(out.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if problems := loadSchema(t, "show-json.schema.json").check(document, true); len(problems) > 0 {
		t.Errorf("the document does not match the schema:\n%s", strings.Join(problems, "\n"))
	}
	warnings, _ := document["warnings"].([]any)
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want the repeat and the decrease", warnings)
	}
	if first, _ := warnings[0].(string); !strings.Contains(first, "repeats") || !strings.Contains(first, "(0010,0020)") {
		t.Errorf("first warning is %q", warnings[0])
	}
	if second, _ := warnings[1].(string); !strings.Contains(second, "(0008,0070) at offset") || !strings.Contains(second, "follows (0010,0020)") {
		t.Errorf("second warning is %q", warnings[1])
	}
}
