package dataset

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
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

// jsonBinaryVRs are the value representations carried as InlineBinary.
var jsonBinaryVRs = map[dataelem.VR]bool{
	dataelem.OB: true, dataelem.OD: true, dataelem.OF: true,
	dataelem.OL: true, dataelem.OV: true, dataelem.OW: true,
	dataelem.UN: true,
}

// DICOMJSONElement is one element in the DICOM JSON Model.
//
// Exactly one of Value, InlineBinary and BulkDataURI is present, or none when
// the element is empty.
type DICOMJSONElement struct {
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
func (e DICOMJSONElement) MarshalJSON() ([]byte, error) {
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
	out := make(map[string]DICOMJSONElement)

	for _, elem := range ds.GetAll() {
		t, ok := elem.Tag()
		if !ok {
			return nil, fmt.Errorf("dataset: an element has an unreadable tag (%T); "+
				"refusing to write JSON with it omitted", elem)
		}
		converted, err := ds.elementToDICOMJSON(t, elem, opts)
		if err != nil {
			return nil, err
		}
		out[dicomJSONKey(t)] = converted
	}
	return out, nil
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
	return fmt.Sprintf("%04X%04X", t.Group(), t.Element())
}

// elementToDICOMJSON converts one element.
func (ds *Dataset) elementToDICOMJSON(t tag.Tag, elem *dataelem.DataElement,
	opts DICOMJSONOptions) (DICOMJSONElement, error) {

	vr := ds.resolveJSONVR(t, elem)
	out := DICOMJSONElement{VR: string(vr)}

	if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
		out.VR = string(dataelem.SQ)
		out.Value = []any{} // present, even with no items
		for i := 0; i < seq.Length(); i++ {
			item, _ := seq.Get(i)
			inner, ok := item.(*Dataset)
			if !ok {
				continue
			}
			nested, err := inner.ToDICOMJSONWithOptions(opts)
			if err != nil {
				return out, err
			}
			out.Value = append(out.Value, nested)
		}
		return out, nil
	}

	raw, _ := elem.GetValue().([]byte)
	if len(raw) == 0 {
		return out, nil // no Value member at all, which is what empty means here
	}

	if jsonBinaryVRs[vr] {
		if opts.BulkDataThreshold > 0 && len(raw) >= opts.BulkDataThreshold &&
			opts.BulkDataURIFunc != nil {
			out.BulkDataURI = opts.BulkDataURIFunc(t)
			return out, nil
		}
		out.InlineBinary = base64.StdEncoding.EncodeToString(raw)
		return out, nil
	}

	values, err := dicomJSONValues(vr, raw)
	if err != nil {
		return out, fmt.Errorf("dataset: %s %s: %w", dicomJSONKey(t), vr, err)
	}
	out.Value = values
	return out, nil
}

// resolveJSONVR settles on one value representation to write.
//
// The JSON model has no way to say "either of these". A dictionary entry may,
// because some attributes take their VR from another attribute in the same data
// set, and a reader that has not resolved it carries the ambiguity in the VR
// itself — "OB or OW", "US or SS". Writing that verbatim produces a document no
// consumer will accept, and picking arbitrarily produces one that is accepted
// and wrong: US read as SS turns 40000 into -25536.
//
// So the deciding attribute is consulted. Pixel Representation (0028,0103) says
// whether pixel values are signed, and Bits Allocated (0028,0100) whether pixel
// data is bytes or words. An element with no value representation at all becomes
// UN, which is what the standard says an unknown one is.
func (ds *Dataset) resolveJSONVR(t tag.Tag, elem *dataelem.DataElement) dataelem.VR {
	vr := elem.GetVR()
	if _, ok := elem.GetValue().(*sequence.Sequence); ok {
		return dataelem.SQ
	}

	switch vr {
	case "":
		return dataelem.UN

	case dataelem.UN:
		// PS3.5 6.2.2 lets a sender write UN when it does not know the value
		// representation, and lets a receiver look the tag up instead. Carrying
		// UN into JSON would base64 a date or a patient name, which is a valid
		// document that no consumer can use — pydicom, dcm4che and dcmtk all
		// resolve it, and a document that disagrees with all three is not
		// interchange.
		//
		// Only for text: the bytes of a UN element already are the text, so
		// reading them as the dictionary VR needs no reinterpretation. A UN that
		// the dictionary calls SQ holds an encoded sequence, and claiming SQ
		// without parsing it would produce a Value that is not there.
		// A private creator says which vendor owns a block of private tags, and
		// PS3.5 7.8.1 fixes it as LO — an odd group, element 0x10 to 0xFF. No
		// dictionary is needed or possible: the value is the vendor's own name.
		if t.Group()%2 == 1 && t.Element() >= 0x0010 && t.Element() <= 0x00FF {
			return dataelem.LO
		}
		if known := dataelem.VR(t.GetVR()); known != "" && known != dataelem.UN {
			if !jsonBinaryVRs[known] && known != dataelem.SQ &&
				!strings.Contains(string(known), " or ") {
				return known
			}
		}
		return dataelem.UN

	case "US or SS":
		if ds.unsignedPixelValues() {
			return dataelem.US
		}
		return dataelem.SS

	case "OB or OW", "OW or OB":
		// PS3.5 A.1: pixel data is OW unless each sample fits in a byte.
		if bits, ok := ds.uint16Value(tag.New(0x0028, 0x0100)); ok && bits <= 8 {
			return dataelem.OB
		}
		return dataelem.OW

	case "OB or OD":
		return dataelem.OB
	}
	return vr
}

// unsignedPixelValues reports what Pixel Representation says. Absent, it is 0 —
// unsigned — which is the standard'"'"'s default and the common case.
func (ds *Dataset) unsignedPixelValues() bool {
	v, ok := ds.uint16Value(tag.New(0x0028, 0x0103))
	return !ok || v == 0
}

// uint16Value reads a two-byte unsigned value.
func (ds *Dataset) uint16Value(t tag.Tag) (uint16, bool) {
	elem, ok := ds.Get(t)
	if !ok {
		return 0, false
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok || len(raw) < 2 {
		return 0, false
	}
	return binary.LittleEndian.Uint16(raw), true
}

// dicomJSONValues renders a non-sequence, non-binary value.
func dicomJSONValues(vr dataelem.VR, raw []byte) ([]any, error) {
	switch vr {
	case dataelem.AT:
		return attributeTagValues(raw)
	case dataelem.US, dataelem.SS, dataelem.UL, dataelem.SL,
		dataelem.SV, dataelem.UV, dataelem.FL, dataelem.FD:
		return binaryNumberValues(vr, raw)
	}

	// The rest are text, multi-valued by backslash.
	text := strings.TrimRight(string(raw), "\x00 ")
	parts := strings.Split(text, "\\")

	switch vr {
	case dataelem.PN:
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			out = append(out, personNameJSON(p))
		}
		return out, nil

	case dataelem.DS, dataelem.IS:
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				// A present but empty item. The standard has null for this, and
				// it has to stay distinguishable from a missing one.
				out = append(out, nil)
				continue
			}
			if vr == dataelem.IS {
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
func binaryNumberValues(vr dataelem.VR, raw []byte) ([]any, error) {
	width := map[dataelem.VR]int{
		dataelem.US: 2, dataelem.SS: 2,
		dataelem.UL: 4, dataelem.SL: 4, dataelem.FL: 4,
		dataelem.SV: 8, dataelem.UV: 8, dataelem.FD: 8,
	}[vr]
	if width == 0 {
		return nil, fmt.Errorf("no width known for %s", vr)
	}
	if len(raw)%width != 0 {
		return nil, fmt.Errorf("a %s value of %d bytes is not a whole number of %d-byte values",
			vr, len(raw), width)
	}

	out := make([]any, 0, len(raw)/width)
	for i := 0; i+width <= len(raw); i += width {
		chunk := raw[i : i+width]
		switch vr {
		case dataelem.US:
			out = append(out, binary.LittleEndian.Uint16(chunk))
		case dataelem.SS:
			out = append(out, int16(binary.LittleEndian.Uint16(chunk)))
		case dataelem.UL:
			out = append(out, binary.LittleEndian.Uint32(chunk))
		case dataelem.SL:
			out = append(out, int32(binary.LittleEndian.Uint32(chunk)))
		case dataelem.UV:
			out = append(out, binary.LittleEndian.Uint64(chunk))
		case dataelem.SV:
			out = append(out, int64(binary.LittleEndian.Uint64(chunk)))
		case dataelem.FL:
			f := float64(math.Float32frombits(binary.LittleEndian.Uint32(chunk)))
			out = append(out, jsonSafeFloat(f))
		case dataelem.FD:
			f := math.Float64frombits(binary.LittleEndian.Uint64(chunk))
			out = append(out, jsonSafeFloat(f))
		}
	}
	return out, nil
}

// jsonSafeFloat keeps a value JSON can carry.
//
// Infinity and NaN are legal IEEE values and appear in real data sets, but JSON
// has no literal for either. They become their DICOM text spellings rather than
// null, which would be indistinguishable from an empty value.
func jsonSafeFloat(f float64) any {
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

// personNameJSON splits a stored person name into the three groups the standard
// gives separate members.
//
// A stored name separates them with "=" in the order alphabetic, ideographic,
// phonetic, and trailing empty groups are omitted rather than written as empty
// members — the standard treats an absent member and an empty one differently.
func personNameJSON(value string) map[string]string {
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

// FromDICOMJSON replaces the data set's contents with a DICOM JSON Model object.
func (ds *Dataset) FromDICOMJSON(m map[string]DICOMJSONElement) error {
	for key, elem := range m {
		t, err := parseDICOMJSONKey(key)
		if err != nil {
			return err
		}
		if err := ds.setFromDICOMJSON(t, elem); err != nil {
			return err
		}
	}
	return nil
}

// FromDICOMJSONString parses DICOM JSON Model text into the data set.
func (ds *Dataset) FromDICOMJSONString(s string) error {
	var m map[string]DICOMJSONElement
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return fmt.Errorf("dataset: parsing DICOM JSON: %w", err)
	}
	return ds.FromDICOMJSON(m)
}

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
	if len(elem.Value) == 0 {
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
			if err := inner.FromDICOMJSON(nested); err != nil {
				return err
			}
			if err := seq.Append(inner); err != nil {
				return err
			}
		}
		return ds.Add(dataelem.NewDataElement(t, dataelem.SQ, seq))
	}

	raw, err := dicomJSONToBytes(vr, elem.Value)
	if err != nil {
		return fmt.Errorf("dataset: %s %s: %w", dicomJSONKey(t), vr, err)
	}
	return ds.Add(dataelem.NewDataElement(t, vr, raw))
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
	case nil:
		return 0, true
	}
	return 0, false
}

// formatJSONNumber renders a number back into DS or IS text without the
// exponent Go's default would produce for large values, which DS does not allow.
func formatJSONNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
