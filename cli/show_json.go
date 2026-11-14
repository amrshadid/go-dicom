package cli

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// showSchemaVersion is the version of the document `show --json` prints. It
// goes up when a field is renamed, removed or changes meaning; a field added
// does not change it.
const showSchemaVersion = 1

// showDocument is a file as `show --json` describes it: what it holds and
// where each part of it sits.
type showDocument struct {
	SchemaVersion int    `json:"schemaVersion"`
	File          string `json:"file"`
	Size          int64  `json:"size"`
	// Preamble is false for a bare data set, which has no preamble, prefix
	// or file meta group.
	Preamble       bool         `json:"preamble"`
	Regions        []showRegion `json:"regions"`
	TransferSyntax showSyntax   `json:"transferSyntax"`
	// OffsetsRelativeTo is "file", or "inflatedDataSet" for the elements of
	// a deflated data set, which are read after inflating it.
	OffsetsRelativeTo string        `json:"offsetsRelativeTo"`
	FileMeta          []showElement `json:"fileMeta"`
	DataSet           []showElement `json:"dataSet"`
	Warnings          []string      `json:"warnings"`
}

// showRegion is a part of a Part 10 file, from offset up to endOffset.
type showRegion struct {
	Name      string `json:"name"`
	Offset    int64  `json:"offset"`
	EndOffset int64  `json:"endOffset"`
}

// showSyntax is how the data set is encoded.
type showSyntax struct {
	UID          string `json:"uid"`
	ExplicitVR   bool   `json:"explicitVR"`
	LittleEndian bool   `json:"littleEndian"`
	Deflated     bool   `json:"deflated"`
}

// showElement is a data element and its bytes.
type showElement struct {
	Tag     string `json:"tag"`
	Keyword string `json:"keyword"`
	Name    string `json:"name"`
	VR      string `json:"vr"`
	VM      int    `json:"vm"`
	// Value is the value as text. It is absent for a sequence and for
	// binary values, which are given by their position alone.
	Value *string `json:"value,omitempty"`

	TagOffset int64 `json:"tagOffset"`
	// VROffset is null when the element has no VR in the file.
	VROffset     *int64 `json:"vrOffset"`
	LengthOffset int64  `json:"lengthOffset"`
	LengthSize   int    `json:"lengthSize"`
	ValueOffset  int64  `json:"valueOffset"`
	// ValueLength counts the bytes from valueOffset to the end of the
	// content. A delimiter that closes it is not part of it.
	ValueLength     int64 `json:"valueLength"`
	UndefinedLength bool  `json:"undefinedLength"`
	EndOffset       int64 `json:"endOffset"`

	Items     []showItem     `json:"items,omitempty"`
	Fragments []showFragment `json:"fragments,omitempty"`
	Delimiter *showDelimiter `json:"delimiter,omitempty"`
}

// showItem is an item of a sequence.
type showItem struct {
	Tag             string         `json:"tag"`
	TagOffset       int64          `json:"tagOffset"`
	LengthOffset    int64          `json:"lengthOffset"`
	LengthSize      int            `json:"lengthSize"`
	ValueOffset     int64          `json:"valueOffset"`
	ValueLength     int64          `json:"valueLength"`
	UndefinedLength bool           `json:"undefinedLength"`
	EndOffset       int64          `json:"endOffset"`
	Elements        []showElement  `json:"elements"`
	Delimiter       *showDelimiter `json:"delimiter,omitempty"`
}

// showFragment is an item of encapsulated pixel data.
type showFragment struct {
	Tag         string `json:"tag"`
	TagOffset   int64  `json:"tagOffset"`
	ValueOffset int64  `json:"valueOffset"`
	ValueLength int64  `json:"valueLength"`
	EndOffset   int64  `json:"endOffset"`
}

// showDelimiter is an Item or Sequence Delimitation Item: a tag and a
// 4-byte length of zero.
type showDelimiter struct {
	Tag       string `json:"tag"`
	TagOffset int64  `json:"tagOffset"`
	EndOffset int64  `json:"endOffset"`
}

// writeShowJSON reads path and writes its description to out.
func writeShowJSON(out io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return err
	}

	doc := showDocument{
		SchemaVersion:     showSchemaVersion,
		File:              path,
		Size:              info.Size(),
		Preamble:          df.HasPreamble,
		OffsetsRelativeTo: "file",
		FileMeta:          []showElement{},
		Warnings:          append([]string{}, df.Warnings...),
		TransferSyntax: showSyntax{
			ExplicitVR: df.ExplicitVR, LittleEndian: df.IsLittleEndian, Deflated: df.Deflated,
		},
	}
	if df.FileMetaInfo != nil {
		doc.TransferSyntax.UID = df.FileMetaInfo.TransferSyntaxUID
	}
	if df.HasPreamble {
		doc.Regions = append(doc.Regions,
			showRegion{"preamble", 0, 128},
			showRegion{"prefix", 128, 132},
			showRegion{"fileMeta", 132, df.DataSetOffset})
		if df.MetaGroupLength != nil {
			doc.FileMeta = append(doc.FileMeta, describeElement(df.MetaGroupLength))
		}
		doc.FileMeta = append(doc.FileMeta, describeElements(df.MetaElements)...)
	}
	doc.Regions = append(doc.Regions, showRegion{"dataSet", df.DataSetOffset, info.Size()})
	if df.Deflated {
		doc.OffsetsRelativeTo = "inflatedDataSet"
	}
	doc.DataSet = describeElements(df.DataElements)

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(doc)
}

func describeElements(elements []*filereader.DataElementValue) []showElement {
	out := make([]showElement, 0, len(elements))
	for _, e := range elements {
		out = append(out, describeElement(e))
	}
	return out
}

func describeElement(e *filereader.DataElementValue) showElement {
	p := e.Position
	out := showElement{
		Tag:             e.Tag.String(),
		Keyword:         e.Tag.GetKeyword(),
		Name:            e.Tag.GetName(),
		VR:              e.VR,
		TagOffset:       p.Offset,
		LengthOffset:    p.LengthOffset,
		LengthSize:      p.LengthSize,
		ValueOffset:     p.ValueOffset,
		UndefinedLength: e.UndefinedLength,
		EndOffset:       p.End,
	}
	if p.ExplicitVR {
		vr := p.Offset + 4
		out.VROffset = &vr
	}
	contentEnd := p.End
	if p.DelimiterOffset != 0 {
		contentEnd = p.DelimiterOffset
		out.Delimiter = &showDelimiter{tag.SequenceDelimiterTag.String(), p.DelimiterOffset, p.DelimiterOffset + 8}
	}
	out.ValueLength = contentEnd - p.ValueOffset

	switch {
	case e.VR == "SQ" || e.Items != nil:
		out.VM = 1
		out.Items = make([]showItem, 0, len(e.Items))
		for _, item := range e.Items {
			described := showItem{
				Tag:             tag.ItemTag.String(),
				TagOffset:       item.Offset,
				LengthOffset:    item.Offset + 4,
				LengthSize:      4,
				ValueOffset:     item.Offset + 8,
				UndefinedLength: item.DeclaredLength == filereader.UndefinedLength,
				EndOffset:       item.End,
				Elements:        describeElements(item.Elements),
			}
			itemContentEnd := item.End
			if item.DelimiterOffset != 0 {
				itemContentEnd = item.DelimiterOffset
				described.Delimiter = &showDelimiter{tag.ItemDelimiterTag.String(), item.DelimiterOffset, item.DelimiterOffset + 8}
			}
			described.ValueLength = itemContentEnd - described.ValueOffset
			out.Items = append(out.Items, described)
		}
	case p.Fragments != nil:
		out.VM = 1
		for _, fragment := range p.Fragments {
			out.Fragments = append(out.Fragments, showFragment{
				tag.ItemTag.String(), fragment.Offset, fragment.ValueOffset,
				fragment.End - fragment.ValueOffset, fragment.End,
			})
		}
	default:
		value, vm, isText := displayValue(e.VR, e.Value)
		out.VM = vm
		if isText {
			out.Value = &value
		}
	}
	return out
}

// displayValue is a value as text, how many values it holds, and whether
// it is one to show at all: bulk binary is given by its position alone.
func displayValue(vr string, raw []byte) (text string, vm int, show bool) {
	if len(raw) == 0 {
		return "", 0, true
	}
	number := func(size int, format func([]byte) string) (string, int, bool) {
		if len(raw)%size != 0 {
			return "", 1, false
		}
		values := make([]string, 0, len(raw)/size)
		for i := 0; i < len(raw); i += size {
			values = append(values, format(raw[i:i+size]))
		}
		return strings.Join(values, `\`), len(values), true
	}
	le := binary.LittleEndian
	switch vr {
	case "OB", "OW", "OF", "OD", "OL", "OV", "UN":
		return "", 1, false
	case "US":
		return number(2, func(b []byte) string { return fmt.Sprint(le.Uint16(b)) })
	case "SS":
		return number(2, func(b []byte) string { return fmt.Sprint(int16(le.Uint16(b))) })
	case "UL":
		return number(4, func(b []byte) string { return fmt.Sprint(le.Uint32(b)) })
	case "SL":
		return number(4, func(b []byte) string { return fmt.Sprint(int32(le.Uint32(b))) })
	case "UV":
		return number(8, func(b []byte) string { return fmt.Sprint(le.Uint64(b)) })
	case "SV":
		return number(8, func(b []byte) string { return fmt.Sprint(int64(le.Uint64(b))) })
	case "FL":
		return number(4, func(b []byte) string { return fmt.Sprint(math.Float32frombits(le.Uint32(b))) })
	case "FD":
		return number(8, func(b []byte) string { return fmt.Sprint(math.Float64frombits(le.Uint64(b))) })
	case "AT":
		return number(4, func(b []byte) string { return tag.New(le.Uint16(b), le.Uint16(b[2:])).String() })
	}
	text = strings.TrimRight(decodeTextWithCharset(raw, vr), " \x00")
	switch vr {
	// One value whatever it holds: a backslash is text in these.
	case "LT", "ST", "UT", "UR":
		return text, 1, true
	}
	return text, strings.Count(text, `\`) + 1, true
}
