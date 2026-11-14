package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// show printed the decoded tree and nothing about where it sits in the
// file (#259).

type jsonElement struct {
	Tag             string `json:"tag"`
	Keyword         string `json:"keyword"`
	VR              string `json:"vr"`
	VM              int    `json:"vm"`
	Value           *string
	TagOffset       int64  `json:"tagOffset"`
	VROffset        *int64 `json:"vrOffset"`
	LengthOffset    int64  `json:"lengthOffset"`
	LengthSize      int    `json:"lengthSize"`
	ValueOffset     int64  `json:"valueOffset"`
	ValueLength     int64  `json:"valueLength"`
	UndefinedLength bool   `json:"undefinedLength"`
	EndOffset       int64  `json:"endOffset"`
	Items           []struct {
		TagOffset int64         `json:"tagOffset"`
		EndOffset int64         `json:"endOffset"`
		Elements  []jsonElement `json:"elements"`
		Delimiter *struct {
			Tag       string `json:"tag"`
			TagOffset int64  `json:"tagOffset"`
		} `json:"delimiter"`
	} `json:"items"`
	Fragments []struct {
		TagOffset   int64 `json:"tagOffset"`
		ValueOffset int64 `json:"valueOffset"`
		ValueLength int64 `json:"valueLength"`
	} `json:"fragments"`
	Delimiter *struct {
		Tag       string `json:"tag"`
		TagOffset int64  `json:"tagOffset"`
	} `json:"delimiter"`
}

type jsonDocument struct {
	SchemaVersion int   `json:"schemaVersion"`
	Size          int64 `json:"size"`
	Preamble      bool  `json:"preamble"`
	Regions       []struct {
		Name      string `json:"name"`
		Offset    int64  `json:"offset"`
		EndOffset int64  `json:"endOffset"`
	} `json:"regions"`
	TransferSyntax struct {
		UID        string `json:"uid"`
		ExplicitVR bool   `json:"explicitVR"`
	} `json:"transferSyntax"`
	OffsetsRelativeTo string        `json:"offsetsRelativeTo"`
	FileMeta          []jsonElement `json:"fileMeta"`
	DataSet           []jsonElement `json:"dataSet"`
}

func showJSON(t *testing.T, path string) jsonDocument {
	t.Helper()
	var out bytes.Buffer
	if err := (&ShowCommand{out: &out}).Execute([]string{path, "--json"}); err != nil {
		t.Fatal(err)
	}
	var doc jsonDocument
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("--json did not print JSON alone: %v\n%s", err, out.String())
	}
	return doc
}

// sequenceFile is an Explicit VR Little Endian file holding a sequence
// closed by delimiters and encapsulated pixel data of two fragments.
func sequenceFile(t *testing.T) string {
	t.Helper()
	le32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	const syntax = "1.2.840.10008.1.2.4.70\x00"

	var meta bytes.Buffer
	meta.Write([]byte{0x02, 0x00, 0x10, 0x00, 'U', 'I', byte(len(syntax)), 0})
	meta.WriteString(syntax)

	var f bytes.Buffer
	f.Write(make([]byte, 128))
	f.WriteString("DICM")
	f.Write([]byte{0x02, 0x00, 0x00, 0x00, 'U', 'L', 4, 0})
	f.Write(le32(uint32(meta.Len())))
	f.Write(meta.Bytes())

	f.Write([]byte{0x08, 0x00, 0x40, 0x11, 'S', 'Q', 0, 0})
	f.Write(le32(0xFFFFFFFF))
	f.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
	f.Write(le32(0xFFFFFFFF))
	f.Write([]byte{0x08, 0x00, 0x50, 0x11, 'U', 'I', 4, 0, '1', '.', '2', 0})
	f.Write([]byte{0xFE, 0xFF, 0x0D, 0xE0, 0, 0, 0, 0})
	f.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})
	f.Write([]byte{0x28, 0x00, 0x10, 0x00, 'U', 'S', 2, 0, 16, 0})
	f.Write([]byte{0xE0, 0x7F, 0x10, 0x00, 'O', 'B', 0, 0})
	f.Write(le32(0xFFFFFFFF))
	f.Write([]byte{0xFE, 0xFF, 0x00, 0xE0, 0, 0, 0, 0})
	f.Write([]byte{0xFE, 0xFF, 0x00, 0xE0, 2, 0, 0, 0, 0xAA, 0xBB})
	f.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})

	path := filepath.Join(t.TempDir(), "seq.dcm")
	if err := os.WriteFile(path, f.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShowJSONSaysWhereEverythingIs(t *testing.T) {
	path := sequenceFile(t)
	data, _ := os.ReadFile(path)
	doc := showJSON(t, path)

	if doc.SchemaVersion != 1 || doc.Size != int64(len(data)) || !doc.Preamble || doc.OffsetsRelativeTo != "file" {
		t.Errorf("the header is %+v", doc)
	}
	want := []string{"preamble", "prefix", "fileMeta", "dataSet"}
	at := int64(0)
	for i, region := range doc.Regions {
		if i >= len(want) || region.Name != want[i] || region.Offset != at {
			t.Fatalf("region %d is %+v, want %s from %d", i, region, want[i], at)
		}
		at = region.EndOffset
	}
	if at != int64(len(data)) || len(doc.Regions) != 4 {
		t.Errorf("the regions end at %d of %d", at, len(data))
	}
	if doc.FileMeta[0].Tag != "(0002,0000)" || doc.FileMeta[0].TagOffset != 132 {
		t.Errorf("the meta group starts with %+v", doc.FileMeta[0])
	}

	seq := doc.DataSet[0]
	if seq.VR != "SQ" || !seq.UndefinedLength || seq.VROffset == nil || *seq.VROffset != seq.TagOffset+4 ||
		seq.LengthSize != 4 || seq.ValueOffset != seq.TagOffset+12 {
		t.Fatalf("the sequence is %+v", seq)
	}
	if seq.Delimiter == nil || seq.Delimiter.Tag != "(FFFE,E0DD)" || seq.EndOffset != seq.Delimiter.TagOffset+8 {
		t.Errorf("the sequence's delimiter is %+v, the sequence ends at %d", seq.Delimiter, seq.EndOffset)
	}
	item := seq.Items[0]
	if item.TagOffset != seq.ValueOffset || item.Delimiter == nil || item.Delimiter.Tag != "(FFFE,E00D)" {
		t.Errorf("the item is %+v", item)
	}
	inner := item.Elements[0]
	if inner.Keyword != "ReferencedSOPClassUID" || inner.Value == nil || *inner.Value != "1.2" ||
		string(data[inner.ValueOffset:inner.ValueOffset+3]) != "1.2" || inner.LengthSize != 2 {
		t.Errorf("the nested element is %+v", inner)
	}

	rows := doc.DataSet[1]
	if rows.Value == nil || *rows.Value != "16" || rows.VM != 1 || rows.ValueLength != 2 {
		t.Errorf("Rows is %+v", rows)
	}

	pixels := doc.DataSet[2]
	if pixels.Value != nil || len(pixels.Fragments) != 2 || pixels.Fragments[1].ValueLength != 2 ||
		!bytes.Equal(data[pixels.Fragments[1].ValueOffset:pixels.Fragments[1].ValueOffset+2], []byte{0xAA, 0xBB}) {
		t.Errorf("the pixel data is %+v", pixels)
	}
	if pixels.EndOffset != int64(len(data)) {
		t.Errorf("the last element ends at %d of %d", pixels.EndOffset, len(data))
	}
}

func TestShowJSONOfImplicitVRHasNoVROffset(t *testing.T) {
	dir := t.TempDir()
	implicit := showJSON(t, writeInstance(t, dir, "i.dcm", "1.2.840.10008.5.1.4.1.1.2", "1.2.3", "1.2.840.10008.1.2", nil))
	explicit := showJSON(t, writeInstance(t, dir, "e.dcm", "1.2.840.10008.5.1.4.1.1.2", "1.2.3", "1.2.840.10008.1.2.1", nil))

	if implicit.TransferSyntax.ExplicitVR || !explicit.TransferSyntax.ExplicitVR {
		t.Fatalf("the syntaxes are %+v and %+v", implicit.TransferSyntax, explicit.TransferSyntax)
	}
	for i, e := range implicit.DataSet {
		if e.VROffset != nil || e.LengthSize != 4 || e.ValueOffset != e.TagOffset+8 {
			t.Errorf("implicit %s is %+v", e.Tag, e)
		}
		// The same object either way.
		if other := explicit.DataSet[i]; other.Tag != e.Tag || other.VR != e.VR || other.VROffset == nil ||
			(e.Value != nil) != (other.Value != nil) || (e.Value != nil && *e.Value != *other.Value) {
			t.Errorf("%s differs between the two encodings: %+v and %+v", e.Tag, e, other)
		}
	}
	// The meta group is explicit whatever the data set is.
	if implicit.FileMeta[0].VROffset == nil {
		t.Error("the meta group of an implicit file has no VR offsets")
	}
}

func TestShowJSONDescribesTheWholeFile(t *testing.T) {
	path := sequenceFile(t)
	if err := (&ShowCommand{out: &bytes.Buffer{}}).Execute([]string{path + "::Rows", "--json"}); err == nil {
		t.Error("--json with one element was accepted")
	}
}
