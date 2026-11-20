// Package dicomjson writes the DICOM JSON Model of PS3.18 Annex F from a data
// set given as plain attributes.
//
// It is the one implementation behind dataset.Dataset.ToDICOMJSON and
// attributes.DICOMJSON. The first converts a data set it has built, the second
// the elements a file was read as, and neither may link what the other needs:
// the data set package brings every image codec with it, and a program that
// only wants the JSON of a header should not carry them. So this package
// imports nothing but tag and the VR rules, and both callers give it their data
// set in the one shape it reads, Item.
//
// The model is the interchange format: what DICOMweb serves, what pydicom's
// to_json and from_json produce and consume, and what dcm4che reads. It is not
// a convenience rendering of a data set — its shape is specified, and a
// consumer that expects it will not accept anything else.
//
// The specified encoding is per VR:
//
//   - DS, IS, FL, FD, SL, SS, UL, US, SV, UV are JSON numbers, not strings.
//   - PN is an object with Alphabetic, Ideographic and Phonetic members, taken
//     from the three groups a stored person name separates with "=".
//   - AT is a string of eight hexadecimal digits.
//   - OB, OD, OF, OL, OV, OW and UN are base64 in InlineBinary.
//   - SQ holds an array of nested objects.
//   - Everything else is a string, and a multi-valued one becomes an array.
//
// An element with no value has no Value member at all, rather than an empty
// array — the standard distinguishes them, and a consumer checking for the key
// is entitled to rely on it.
package dicomjson

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/internal/vr"
	"github.com/amrshadid/go-dicom/tag"
)

// Element is one element in the DICOM JSON Model.
//
// Exactly one of Value, InlineBinary and BulkDataURI is present, or none when
// the element is empty.
type Element struct {
	VR string `json:"vr"`
	// Value is nil for an element with no value and non-nil for one with a
	// value, which for a sequence includes one with no items. A sequence that
	// is present and empty is not the same as an absent value, and a consumer
	// reading the two apart is entitled to see the difference.
	Value        []any  `json:"Value,omitempty"`
	InlineBinary string `json:"InlineBinary,omitempty"`
	BulkDataURI  string `json:"BulkDataURI,omitempty"`
}

// MarshalJSON writes the element, keeping an empty Value that is present.
//
// The struct tag alone cannot: omitempty drops an empty slice and a nil one
// alike, which would turn a sequence with no items into an element with no
// value.
func (e Element) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, 2)
	out["vr"] = e.VR
	switch {
	case e.InlineBinary != "":
		out["InlineBinary"] = e.InlineBinary
	case e.BulkDataURI != "":
		out["BulkDataURI"] = e.BulkDataURI
	case e.Value != nil:
		out["Value"] = e.Value
	}
	return json.Marshal(out)
}

// Attribute is one element of a data set as the writer reads it.
type Attribute struct {
	Tag tag.Tag
	// VR is the VR the element carries, "" for none. It is settled to the one
	// the element is written with before use (see Convert).
	VR string
	// Value is the element's bytes, text already in UTF-8. It is nil for a
	// sequence and for a value left in the file.
	Value []byte
	// NoBytes reports that the element holds no bytes a rule may read, which
	// for a value in memory means one of a type its VR cannot hold. Value is
	// then ignored when the VR is settled.
	NoBytes bool
	// Encode, when set, renders the value as bytes once the VR is settled. A
	// data set built in memory holds Go numbers, whose bytes depend on the VR
	// they are written as. When nil, Value is the bytes.
	Encode func(vr string) ([]byte, error)

	// Sequence reports that the element holds items, however many, in Items.
	Sequence bool
	Items    []*Item

	// InFile reports that the value was left in the file it was read from:
	// Value is nil, and Offset and Length say where the bytes are.
	InFile bool
	// Offset is where the value begins in the bytes it was read from, 0 for an
	// attribute that was not read from a file, and Length how many bytes a
	// value left in the file has.
	Offset, Length int64
}

// bytes is what the VR rules may read of the attribute.
func (a *Attribute) bytes() ([]byte, bool) {
	if a.Sequence || a.InFile || a.NoBytes {
		return nil, false
	}
	return a.Value, true
}

// Item is a data set: the top level of a file or one item of a sequence.
//
// A tag that appears twice keeps the later element in the place of the first,
// as adding to a data set does.
type Item struct {
	attrs []Attribute
	index map[tag.Tag]int
}

// NewItem makes a data set of the attributes, in order.
func NewItem(attrs []Attribute) *Item {
	it := &Item{index: make(map[tag.Tag]int, len(attrs))}
	for _, a := range attrs {
		if at, ok := it.index[a.Tag]; ok {
			it.attrs[at] = a
			continue
		}
		it.index[a.Tag] = len(it.attrs)
		it.attrs = append(it.attrs, a)
	}
	return it
}

// Bytes returns the bytes of the element at t, and false when the data set
// has no such element or it holds none. It is what the VR rules read.
func (it *Item) Bytes(t tag.Tag) ([]byte, bool) {
	at, ok := it.index[t]
	if !ok {
		return nil, false
	}
	return it.attrs[at].bytes()
}

// Step is one sequence a value is nested in: the sequence's tag and the
// zero-based index of the item the value is in.
type Step struct {
	Sequence tag.Tag
	Item     int
}

// BulkValue is a value the writer is about to render, offered to
// Options.Bulk.
type BulkValue struct {
	// Tag and VR are the element's, VR the one it is written with.
	Tag tag.Tag
	VR  string
	// Path lists the sequences the element is nested in, outermost first. It
	// is empty at the top level.
	Path []Step
	// InFile reports that the value was left in the file and Value is nil.
	InFile bool
	// Offset is where the value begins and Length how long it is. Length is
	// the length stated in the file for a value left there, otherwise
	// len(Value). Offset is 0 for a value not read from a file, and counts
	// inflated bytes for a deflated file.
	Offset, Length int64
	// Value is the bytes, text in UTF-8; do not modify it.
	Value []byte
}

// BulkDecision is what Options.Bulk decides. The zero value renders the value
// as the model does by default.
type BulkDecision struct {
	// URI, when not empty, is written as the element's BulkDataURI in place of
	// its value.
	URI string
	// Omit leaves the element out of the output altogether.
	Omit bool
}

// Options controls Convert.
type Options struct {
	// Bulk, when set, is called for each value of a non-sequence element that
	// has bytes or is left in the file, and may replace it with a URI or drop
	// it. A value left in the file for which it gives neither is an error,
	// since there is nothing to write.
	Bulk func(BulkValue) BulkDecision
}

// Key renders a tag as the eight uppercase hexadecimal digits the standard
// uses as the member name.
func Key(t tag.Tag) string {
	return fmt.Sprintf("%04X%04X", t.Group(), t.Element())
}

// Convert renders the data set as the members of a DICOM JSON object, keyed
// by tag. Errors do not name the package, for the caller to do.
func Convert(item *Item, opts Options) (map[string]Element, error) {
	return convert(item, opts, nil, nil)
}

// convert converts the data set as an item of outers, nearest first, so an
// element can take its VR from the image the item describes. path is how the
// item is reached.
func convert(item *Item, opts Options, outers []vr.Scope, path []Step) (map[string]Element, error) {
	out := make(map[string]Element, len(item.attrs))
	for i := range item.attrs {
		a := &item.attrs[i]
		converted, keep, err := convertAttribute(item, a, opts, outers, path)
		if err != nil {
			return nil, err
		}
		if keep {
			out[Key(a.Tag)] = converted
		}
	}
	return out, nil
}

// convertAttribute converts one element.
func convertAttribute(item *Item, a *Attribute, opts Options, outers []vr.Scope, path []Step) (Element, bool, error) {
	resolved := resolveVR(item, a, outers)
	out := Element{VR: resolved}

	if a.Sequence {
		out.VR = "SQ"
		out.Value = []any{} // present, even with no items
		inner := append([]vr.Scope{item}, outers...)
		for i, child := range a.Items {
			if child == nil {
				continue
			}
			nested, err := convert(child, opts, inner, append(path[:len(path):len(path)], Step{a.Tag, i}))
			if err != nil {
				return out, false, err
			}
			out.Value = append(out.Value, nested)
		}
		return out, true, nil
	}

	raw := a.Value
	if a.Encode != nil {
		var err error
		if raw, err = a.Encode(resolved); err != nil {
			return out, false, fmt.Errorf("%s: %w", Key(a.Tag), err)
		}
	}
	if len(raw) == 0 && !a.InFile {
		return out, true, nil // no Value member at all, which is what empty means here
	}

	if opts.Bulk != nil {
		length := int64(len(raw))
		if a.InFile {
			length = a.Length
		}
		decision := opts.Bulk(BulkValue{
			Tag: a.Tag, VR: resolved, Path: path, InFile: a.InFile,
			Offset: a.Offset, Length: length, Value: raw,
		})
		switch {
		case decision.Omit:
			return out, false, nil
		case decision.URI != "":
			out.BulkDataURI = decision.URI
			return out, true, nil
		}
	}
	if a.InFile {
		return out, false, fmt.Errorf("%s: the value is %d bytes left in the file, and was neither "+
			"given a BulkDataURI nor omitted", Key(a.Tag), a.Length)
	}

	if vr.IsBinary(resolved) {
		out.InlineBinary = base64.StdEncoding.EncodeToString(raw)
		return out, true, nil
	}

	values, err := renderValues(resolved, raw)
	if err != nil {
		return out, false, fmt.Errorf("%s %s: %w", Key(a.Tag), resolved, err)
	}
	out.Value = values
	return out, true, nil
}

// resolveVR is the VR an element is written with, with one restriction on
// vr.Resolve, on an element nobody typed.
//
// An absent value representation and an explicit UN mean the same thing to a
// reader: nobody said what this is. They were handled differently, so a private
// creator stored without a VR — which is how an implicit VR file stores
// everything — came out as UN where the same element with an explicit UN came
// out as LO.
//
// PS3.5 6.2.2 lets a sender write UN when it does not know the value
// representation, and lets a receiver look the tag up instead. Carrying UN into
// JSON would base64 a date or a patient name, which is a valid document that no
// consumer can use — pydicom, dcm4che and dcmtk all resolve it, and a document
// that disagrees with all three is not interchange.
//
// Only for text: the bytes of a UN element already are the text, so reading them
// as the dictionary VR needs no reinterpretation. A UN that the dictionary calls
// SQ holds an encoded sequence, and claiming SQ without parsing it would produce
// a Value that is not there.
func resolveVR(item *Item, a *Attribute, outers []vr.Scope) string {
	if a.Sequence {
		return "SQ"
	}
	raw, ok := a.bytes()
	if a.VR == "" || a.VR == "UN" {
		if private := vr.PrivateVR(a.Tag, raw, ok, item); private != "" {
			return private
		}
		if known := a.Tag.GetVR(); known != "" && known != "UN" {
			if !vr.IsBinary(known) && known != "SQ" && !vr.OffersChoice(known) {
				return known
			}
		}
		return "UN"
	}
	return vr.Resolve(a.Tag, a.VR, false, raw, ok, item, outers)
}

// renderValues renders a non-sequence, non-binary value.
func renderValues(vrName string, raw []byte) ([]any, error) {
	switch vrName {
	case "AT":
		return attributeTagValues(raw)
	case "US", "SS", "UL", "SL", "SV", "UV", "FL", "FD":
		return binaryNumberValues(vrName, raw)
	}

	text := strings.TrimRight(string(raw), "\x00 ")

	// Padding alone is an element with no value, and the model says that by
	// leaving Value out. It was written as [""], one value that happens to be
	// empty, which neither pydicom nor dcm4che produces (#121).
	if text == "" {
		return nil, nil
	}

	// LT, ST, UT and UR hold one value, and a backslash in them is a
	// character, not a separator (PS3.5 6.2). Splitting them turned the XML in
	// examples_ybr_color.dcm's private UT into 17 values with every backslash
	// gone, and trimming the pieces changed the text besides (#121).
	switch vrName {
	case "LT", "ST", "UT", "UR":
		return []any{text}, nil
	}

	// The rest are text, multi-valued by backslash.
	parts := strings.Split(text, "\\")

	switch vrName {
	case "PN":
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			out = append(out, personName(p))
		}
		return out, nil

	case "DS", "IS":
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				// A present but empty item. The standard has null for this, and
				// it has to stay distinguishable from a missing one.
				out = append(out, nil)
				continue
			}
			if vrName == "IS" {
				if n, err := strconv.ParseInt(p, 10, 64); err == nil {
					out = append(out, n)
					continue
				}
			} else if f, err := strconv.ParseFloat(p, 64); err == nil {
				if math.IsInf(f, 0) || math.IsNaN(f) {
					// Not representable in JSON, and silently turning it into
					// null would lose the distinction from an empty value.
					out = append(out, p)
					continue
				}
				out = append(out, f)
				continue
			}
			// Not a number. The standard has no encoding for that, and files
			// carrying one exist — pydicom's badVR.dcm has an IS of "1A". The
			// string is kept rather than dropped: a consumer can see what was
			// there, where null would say the element was empty.
			out = append(out, p)
		}
		return out, nil
	}

	out := make([]any, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimRight(p, " "))
	}
	return out, nil
}

// attributeTagValues renders AT, which holds tags rather than text.
func attributeTagValues(raw []byte) ([]any, error) {
	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("an AT value of %d bytes is not a whole number of tags", len(raw))
	}
	out := make([]any, 0, len(raw)/4)
	for i := 0; i+4 <= len(raw); i += 4 {
		group := binary.LittleEndian.Uint16(raw[i:])
		element := binary.LittleEndian.Uint16(raw[i+2:])
		out = append(out, fmt.Sprintf("%04X%04X", group, element))
	}
	return out, nil
}

// binaryNumberValues renders the fixed-width numeric VRs.
func binaryNumberValues(vrName string, raw []byte) ([]any, error) {
	width := map[string]int{
		"US": 2, "SS": 2,
		"UL": 4, "SL": 4, "FL": 4,
		"SV": 8, "UV": 8, "FD": 8,
	}[vrName]
	if width == 0 {
		return nil, fmt.Errorf("no width known for %s", vrName)
	}
	if len(raw)%width != 0 {
		return nil, fmt.Errorf("a %s value of %d bytes is not a whole number of %d-byte values",
			vrName, len(raw), width)
	}

	out := make([]any, 0, len(raw)/width)
	for i := 0; i+width <= len(raw); i += width {
		chunk := raw[i : i+width]
		switch vrName {
		case "US":
			out = append(out, binary.LittleEndian.Uint16(chunk))
		case "SS":
			out = append(out, int16(binary.LittleEndian.Uint16(chunk)))
		case "UL":
			out = append(out, binary.LittleEndian.Uint32(chunk))
		case "SL":
			out = append(out, int32(binary.LittleEndian.Uint32(chunk)))
		case "UV":
			out = append(out, binary.LittleEndian.Uint64(chunk))
		case "SV":
			out = append(out, int64(binary.LittleEndian.Uint64(chunk)))
		case "FL":
			f := float64(math.Float32frombits(binary.LittleEndian.Uint32(chunk)))
			out = append(out, safeFloat(f))
		case "FD":
			f := math.Float64frombits(binary.LittleEndian.Uint64(chunk))
			out = append(out, safeFloat(f))
		}
	}
	return out, nil
}

// safeFloat keeps a value JSON can carry.
//
// Infinity and NaN are legal IEEE values and appear in real data sets, but JSON
// has no literal for either. They become their DICOM text spellings rather than
// null, which would be indistinguishable from an empty value.
func safeFloat(f float64) any {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	return f
}

// personName splits a stored person name into the three groups the standard
// gives separate members.
//
// A stored name separates them with "=" in the order alphabetic, ideographic,
// phonetic, and trailing empty groups are omitted rather than written as empty
// members — the standard treats an absent member and an empty one differently.
func personName(value string) map[string]string {
	groups := strings.Split(value, "=")
	out := make(map[string]string, 3)
	for i, name := range []string{"Alphabetic", "Ideographic", "Phonetic"} {
		if i >= len(groups) {
			break
		}
		if g := strings.TrimRight(groups[i], " "); g != "" {
			out[name] = g
		}
	}
	return out
}
