package dataset

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/internal/dicomjson"
	"github.com/amrshadid/go-dicom/internal/vr"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The DICOM JSON Model, PS3.18 Annex F.
//
// This is the interchange format: what DICOMweb serves, what pydicom's to_json
// and from_json produce and consume, and what dcm4che reads. It is not a
// convenience rendering of a data set — its shape is specified, and a consumer
// that expects it will not accept anything else.
//
// ToJSON, which predates this, produces a different structure: keyed by
// "(0008,0005)" rather than "00080005", every value a single joined string
// rather than an array, with the keyword and name alongside, and the whole
// wrapped in an elements object. That is a readable rendering and stays as it
// is, but nothing outside this library can consume it, and the documentation
// used to call it Part 18 support. It is not.
//
// The writing is internal/dicomjson, which attributes.DICOMJSON uses as well,
// so a program that only reads a header and wants its JSON gets the same
// document without linking this package and the codecs it brings. The
// encoding per VR is described there; reading it back is below.

// DICOMJSONElement is one element in the DICOM JSON Model.
//
// Exactly one of Value, InlineBinary and BulkDataURI is present, or none when
// the element is empty.
//
//	type DICOMJSONElement struct {
//		VR           string `json:"vr"`
//		Value        []any  `json:"Value,omitempty"`
//		InlineBinary string `json:"InlineBinary,omitempty"`
//		BulkDataURI  string `json:"BulkDataURI,omitempty"`
//	}
//
// Value is nil for an element with no value and non-nil for one with a value,
// which for a sequence includes one with no items: a sequence that is present
// and empty is not the same as an absent value. MarshalJSON keeps that
// difference, which the struct tags alone would lose. The type is the one
// attributes.JSONElement names, so the two writers' results are the same type.
type DICOMJSONElement = dicomjson.Element

// DICOMJSONOptions controls how a data set is converted.
type DICOMJSONOptions struct {
	// BulkDataThreshold, when positive, replaces the InlineBinary of any binary
	// value at least this many bytes with a BulkDataURI from BulkDataURIFunc.
	//
	// Base64 costs a third more than the bytes it carries, so inlining pixel
	// data turns a 500 KB image into 680 KB of JSON that most consumers will
	// hold entirely in memory. DICOMweb serves bulk data separately for that
	// reason.
	BulkDataThreshold int

	// BulkDataURIFunc returns the URI for a value held out of line. Required
	// when BulkDataThreshold is positive; without it the value is inlined
	// anyway, since dropping it silently would be worse.
	BulkDataURIFunc func(t tag.Tag) string
}

// ToDICOMJSON converts the data set to the DICOM JSON Model of PS3.18 Annex F.
func (ds *Dataset) ToDICOMJSON() (map[string]DICOMJSONElement, error) {
	return ds.ToDICOMJSONWithOptions(DICOMJSONOptions{})
}

// ToDICOMJSONWithOptions converts the data set, holding large binary values out
// of line when asked.
func (ds *Dataset) ToDICOMJSONWithOptions(opts DICOMJSONOptions) (map[string]DICOMJSONElement, error) {
	var bulk func(dicomjson.BulkValue) dicomjson.BulkDecision
	if opts.BulkDataThreshold > 0 && opts.BulkDataURIFunc != nil {
		bulk = func(v dicomjson.BulkValue) dicomjson.BulkDecision {
			if vr.IsBinary(v.VR) && v.Length >= int64(opts.BulkDataThreshold) {
				return dicomjson.BulkDecision{URI: opts.BulkDataURIFunc(v.Tag)}
			}
			return dicomjson.BulkDecision{}
		}
	}
	item, err := ds.jsonItem()
	if err != nil {
		return nil, fmt.Errorf("dataset: %w", err)
	}
	out, err := dicomjson.Convert(item, dicomjson.Options{Bulk: bulk})
	if err != nil {
		return nil, fmt.Errorf("dataset: %w", err)
	}
	return out, nil
}

// jsonItem gives the data set, and the items below it, to the writer in the
// one shape it reads. Nothing is copied but the descriptions: a value's bytes
// are the element's own.
func (ds *Dataset) jsonItem() (*dicomjson.Item, error) {
	elements := ds.GetAll()
	attrs := make([]dicomjson.Attribute, 0, len(elements))
	for _, elem := range elements {
		t, ok := elem.Tag()
		if !ok {
			return nil, fmt.Errorf("an element has an unreadable tag (%T); "+
				"refusing to write JSON with it omitted", elem)
		}
		a := dicomjson.Attribute{Tag: t, VR: string(elem.GetVR())}

		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			a.Sequence = true
			for i := 0; i < seq.Length(); i++ {
				got, _ := seq.Get(i)
				inner, ok := got.(*Dataset)
				if !ok {
					continue
				}
				child, err := inner.jsonItem()
				if err != nil {
					return nil, err
				}
				a.Items = append(a.Items, child)
			}
			attrs = append(attrs, a)
			continue
		}

		// The file reader holds a value as bytes; an element built in memory
		// holds a Go value. ValueBytes is what the file writer and the network
		// encoder render it with, so the three agree on what a value is, and
		// one that does not fit its VR is refused here as it is there (#295).
		// The bytes depend on the VR it is written as, which is settled later.
		value := elem.GetValue()
		a.Value, ok = elem.Bytes()
		a.NoBytes = !ok
		if _, isBytes := value.([]byte); !isBytes {
			a.Encode = func(vr string) ([]byte, error) {
				return dataelem.ValueBytes(dataelem.VR(vr), value)
			}
		}
		attrs = append(attrs, a)
	}
	return dicomjson.NewItem(attrs), nil
}

// ToDICOMJSONString renders the data set as DICOM JSON Model text.
func (ds *Dataset) ToDICOMJSONString() (string, error) {
	m, err := ds.ToDICOMJSON()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("dataset: marshaling DICOM JSON: %w", err)
	}
	return string(b), nil
}

// dicomJSONKey renders a tag as the eight uppercase hexadecimal digits the
// standard uses as the member name.
func dicomJSONKey(t tag.Tag) string {
	return dicomjson.Key(t)
}

// datasetScope is a data set as the VR rules read it.
type datasetScope struct{ ds *Dataset }

// Bytes is the bytes of the element at t, and false when there is none.
func (s datasetScope) Bytes(t tag.Tag) ([]byte, bool) {
	elem, ok := s.ds.Get(t)
	if !ok {
		return nil, false
	}
	return elem.Bytes()
}

// FromDICOMJSON replaces the data set's contents with a DICOM JSON Model object.
//
// Numbers decoded with json.Decoder.UseNumber keep their exact value, which an
// SV or UV above 2^53 needs; FromDICOMJSONString decodes that way.
func (ds *Dataset) FromDICOMJSON(m map[string]DICOMJSONElement) error {
	if err := ds.fromDICOMJSON(m); err != nil {
		return err
	}
	ds.declareUTF8(true)
	return nil
}

// fromDICOMJSON reads the members of one data set, top level or item, and
// relabels a declaration the item carries; only the top level adds one.
func (ds *Dataset) fromDICOMJSON(m map[string]DICOMJSONElement) error {
	for key, elem := range m {
		t, err := parseDICOMJSONKey(key)
		if err != nil {
			return err
		}
		if err := ds.setFromDICOMJSON(t, elem); err != nil {
			return err
		}
	}
	ds.declareUTF8(false)
	return nil
}

// FromDICOMJSONString parses DICOM JSON Model text into the data set.
func (ds *Dataset) FromDICOMJSONString(s string) error {
	m, err := decodeDICOMJSON([]byte(s))
	if err != nil {
		return fmt.Errorf("dataset: parsing DICOM JSON: %w", err)
	}
	return ds.FromDICOMJSON(m)
}

// decodeDICOMJSON decodes with UseNumber: through float64, an integer above
// 2^53 is no longer the integer that was written.
func decodeDICOMJSON(data []byte) (map[string]DICOMJSONElement, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var m map[string]DICOMJSONElement
	if err := decoder.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// declareUTF8 makes (0008,0005) say what DICOM JSON text is: UTF-8 (PS3.18
// F.2), whatever the JSON declared. Keeping its declaration labeled UTF-8
// bytes as, say, ISO_IR 100, and every reader of the file written from it saw
// "JÃ©rÃ´me". Text that is all ASCII is the same in every set and needs nothing.
//
// An item inherits its parent's declaration, so only the top level adds one,
// when non-ASCII text appears anywhere in it; an item only has its own relabeled.
func (ds *Dataset) declareUTF8(topLevel bool) {
	if elem, declared := ds.Get(specificCharacterSet); declared {
		// An empty declaration over ASCII text is already true: it names the
		// default repertoire, which ASCII is.
		raw, _ := elem.Bytes()
		if strings.TrimSpace(string(raw)) != "" || hasNonASCIIText(ds) {
			_ = ds.Add(dataelem.NewDataElement(specificCharacterSet, dataelem.CS, []byte(utf8CharacterSet)))
		}
		return
	}
	if topLevel && hasNonASCIIText(ds) {
		_ = ds.Add(dataelem.NewDataElement(specificCharacterSet, dataelem.CS, []byte(utf8CharacterSet)))
	}
}

// hasNonASCIIText reports whether any text the character set applies to, in the
// data set or its items, is outside ASCII.
func hasNonASCIIText(ds *Dataset) bool {
	for _, elem := range ds.GetAll() {
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			for _, item := range seq.Items() {
				if inner, ok := item.(*Dataset); ok && hasNonASCIIText(inner) {
					return true
				}
			}
			continue
		}
		raw, ok := elem.Bytes()
		if !ok || !dataelem.IsCharacterSetVR(elem.GetVR()) {
			continue
		}
		for _, b := range raw {
			if b > 0x7F {
				return true
			}
		}
	}
	return false
}

// specificCharacterSet is (0008,0005), and utf8CharacterSet its term for UTF-8.
var specificCharacterSet = tag.New(0x0008, 0x0005)

const utf8CharacterSet = "ISO_IR 192"

// parseDICOMJSONKey reads the eight-hex-digit member name.
func parseDICOMJSONKey(key string) (tag.Tag, error) {
	if len(key) != 8 {
		return tag.Tag(0), fmt.Errorf("dataset: %q is not an eight-digit DICOM JSON tag", key)
	}
	n, err := strconv.ParseUint(key, 16, 32)
	if err != nil {
		return tag.Tag(0), fmt.Errorf("dataset: %q is not a hexadecimal DICOM JSON tag", key)
	}
	return tag.New(uint16(n>>16), uint16(n)), nil
}

// setFromDICOMJSON writes one element back into the data set.
func (ds *Dataset) setFromDICOMJSON(t tag.Tag, elem DICOMJSONElement) error {
	vr := dataelem.VR(elem.VR)

	if elem.InlineBinary != "" {
		raw, err := base64.StdEncoding.DecodeString(elem.InlineBinary)
		if err != nil {
			return fmt.Errorf("dataset: %s InlineBinary: %w", dicomJSONKey(t), err)
		}
		return ds.Add(dataelem.NewDataElement(t, vr, raw))
	}
	if elem.BulkDataURI != "" {
		// The value is elsewhere. Recording an empty element keeps the tag and
		// its VR, which is more than dropping it, and is as much as can be
		// known without fetching.
		return ds.Add(dataelem.NewDataElement(t, vr, []byte{}))
	}
	// A sequence present with no items is a sequence, not an absent value.
	if vr == dataelem.SQ && elem.Value != nil && len(elem.Value) == 0 {
		return ds.Add(dataelem.NewDataElement(t, dataelem.SQ, sequence.New()))
	}
	if len(elem.Value) == 0 || allNull(elem.Value) {
		return ds.Add(dataelem.NewDataElement(t, vr, []byte{}))
	}

	if vr == dataelem.SQ {
		seq := sequence.New()
		for _, item := range elem.Value {
			raw, ok := item.(map[string]any)
			if !ok {
				continue
			}
			inner := NewDataset()
			nested, err := reencodeJSONItem(raw)
			if err != nil {
				return err
			}
			if err := inner.fromDICOMJSON(nested); err != nil {
				return err
			}
			if err := seq.Append(inner); err != nil {
				return err
			}
		}
		return ds.Add(dataelem.NewDataElement(t, dataelem.SQ, seq))
	}

	// A null among binary numbers cannot be kept: the value has no empty
	// component, and 0, or closing the gap, would change what was sent. The
	// element is dropped with a warning and the rest kept, as a truncated
	// element is (CONFORMANCE §8.4). pydicom's own test1.json has one.
	if binaryNumberVRs[vr] && !allNull(elem.Value) && anyNull(elem.Value) {
		config.Logger.Warn("dataset: dropping a DICOM JSON element with a null among binary numbers",
			"tag", dicomJSONKey(t), "vr", string(vr))
		return nil
	}

	raw, err := dicomJSONToBytes(vr, elem.Value)
	if err != nil {
		return fmt.Errorf("dataset: %s %s: %w", dicomJSONKey(t), vr, err)
	}
	return ds.Add(dataelem.NewDataElement(t, vr, raw))
}

// binaryNumberVRs hold numbers in binary, with no way to write an empty one.
var binaryNumberVRs = map[dataelem.VR]bool{
	dataelem.US: true, dataelem.SS: true, dataelem.UL: true, dataelem.SL: true,
	dataelem.UV: true, dataelem.SV: true, dataelem.FL: true, dataelem.FD: true,
}

// anyNull reports whether any value is null.
func anyNull(values []any) bool {
	for _, v := range values {
		if v == nil {
			return true
		}
	}
	return false
}

// reencodeJSONItem converts a decoded sequence item back into typed elements.
//
// Round-tripping through JSON is the shortest correct path: the item arrives as
// map[string]any because DICOMJSONElement was not the target type, and hand
// converting it would duplicate the decoding rules for every member.
func reencodeJSONItem(raw map[string]any) (map[string]DICOMJSONElement, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("dataset: re-encoding a sequence item: %w", err)
	}
	if out, err := decodeDICOMJSON(b); err == nil {
		return out, nil
	}
	var out map[string]DICOMJSONElement
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("dataset: decoding a sequence item: %w", err)
	}
	return out, nil
}

// dicomJSONToBytes renders JSON values back into stored bytes.
func dicomJSONToBytes(vr dataelem.VR, values []any) ([]byte, error) {
	switch vr {
	case dataelem.AT:
		var buf []byte
		for _, v := range values {
			s, _ := v.(string)
			n, err := strconv.ParseUint(s, 16, 32)
			if err != nil {
				return nil, fmt.Errorf("%q is not an eight-digit tag", s)
			}
			buf = binary.LittleEndian.AppendUint16(buf, uint16(n>>16))
			buf = binary.LittleEndian.AppendUint16(buf, uint16(n))
		}
		return buf, nil

	case dataelem.US, dataelem.SS, dataelem.UL, dataelem.SL,
		dataelem.SV, dataelem.UV, dataelem.FL, dataelem.FD:
		var buf []byte
		for _, v := range values {
			// A binary value has no empty component, so a null among numbers
			// cannot be kept, and 0 would be a value nobody sent.
			if v == nil {
				return nil, fmt.Errorf("a null among %s values, which have no empty component", vr)
			}
			// SV and UV go up to 2^64, past what a float64 holds exactly.
			if n, ok := v.(json.Number); ok && (vr == dataelem.UV || vr == dataelem.SV) {
				if vr == dataelem.UV {
					u, err := strconv.ParseUint(n.String(), 10, 64)
					if err != nil {
						return nil, fmt.Errorf("%s is not a UV value", n)
					}
					buf = binary.LittleEndian.AppendUint64(buf, u)
				} else {
					i, err := strconv.ParseInt(n.String(), 10, 64)
					if err != nil {
						return nil, fmt.Errorf("%s is not an SV value", n)
					}
					buf = binary.LittleEndian.AppendUint64(buf, uint64(i))
				}
				continue
			}
			f, ok := jsonNumber(v)
			if !ok {
				return nil, fmt.Errorf("%v is not a number", v)
			}
			switch vr {
			case dataelem.US:
				buf = binary.LittleEndian.AppendUint16(buf, uint16(f))
			case dataelem.SS:
				buf = binary.LittleEndian.AppendUint16(buf, uint16(int16(f)))
			case dataelem.UL:
				buf = binary.LittleEndian.AppendUint32(buf, uint32(f))
			case dataelem.SL:
				buf = binary.LittleEndian.AppendUint32(buf, uint32(int32(f)))
			case dataelem.UV:
				buf = binary.LittleEndian.AppendUint64(buf, uint64(f))
			case dataelem.SV:
				buf = binary.LittleEndian.AppendUint64(buf, uint64(int64(f)))
			case dataelem.FL:
				buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(float32(f)))
			case dataelem.FD:
				buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(f))
			}
		}
		return buf, nil
	}

	parts := make([]string, 0, len(values))
	for _, v := range values {
		switch value := v.(type) {
		case nil:
			parts = append(parts, "")
		case string:
			parts = append(parts, value)
		case float64:
			parts = append(parts, formatJSONNumber(value))
		case json.Number:
			// As written, where IS and DS allow it: the text is the value.
			if f, err := value.Float64(); err == nil && strings.ContainsAny(value.String(), "eE") {
				parts = append(parts, formatJSONNumber(f))
			} else {
				parts = append(parts, value.String())
			}
		case map[string]any:
			// A person name, back into its "=" separated groups. Trailing empty
			// groups are dropped, which is how a stored name spells them.
			groups := make([]string, 3)
			for i, name := range []string{"Alphabetic", "Ideographic", "Phonetic"} {
				if s, ok := value[name].(string); ok {
					groups[i] = s
				}
			}
			for len(groups) > 1 && groups[len(groups)-1] == "" {
				groups = groups[:len(groups)-1]
			}
			parts = append(parts, strings.Join(groups, "="))
		default:
			parts = append(parts, fmt.Sprintf("%v", value))
		}
	}

	out := strings.Join(parts, "\\")
	// PS3.5 7.1.1: every value has an even length.
	if len(out)%2 == 1 {
		if vr == dataelem.UI {
			out += "\x00"
		} else {
			out += " "
		}
	}
	return []byte(out), nil
}

// jsonNumber reads a JSON value as a number, accepting the string spellings the
// float VRs use for values JSON cannot carry.
func jsonNumber(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		f, err := value.Float64()
		return f, err == nil
	case string:
		switch value {
		case "inf", "Infinity":
			return math.Inf(1), true
		case "-inf", "-Infinity":
			return math.Inf(-1), true
		case "NaN":
			return math.NaN(), true
		}
		f, err := strconv.ParseFloat(value, 64)
		return f, err == nil
	}
	return 0, false
}

// allNull reports whether every value is null, which is an empty element.
func allNull(values []any) bool {
	for _, v := range values {
		if v != nil {
			return false
		}
	}
	return true
}

// formatJSONNumber renders a number back into DS or IS text without the
// exponent Go's default would produce for large values, which DS does not allow.
func formatJSONNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
