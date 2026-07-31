package dataset_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The DICOM JSON Model is PS3.18 Annex F: what DICOMweb serves and what
// pydicom, dcm4che and dcmtk exchange. Its shape is specified, and a consumer
// expecting it will not accept anything else.
//
// The library claimed to support it and did not. ToJSON produces a readable
// rendering — keyed by "(0008,0005)", every value one joined string, keyword and
// name alongside, wrapped in an elements object — which nothing outside this
// library can read. The jsonrep package, whose doc comment said it implemented
// Part 18, is a struct of twenty-five named fields and cannot represent an
// arbitrary data set at all.
//
// The expectations below are pydicom's own to_json output for the same input.

// jsonOf converts a data set and unmarshals it, so the assertions are against
// the bytes a consumer would receive rather than the Go values behind them.
func jsonOf(t *testing.T, ds *dataset.Dataset) map[string]any {
	t.Helper()

	s, err := ds.ToDICOMJSONString()
	if err != nil {
		t.Fatalf("ToDICOMJSONString: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("the output is not valid JSON: %v", err)
	}
	return out
}

func elementOf(t *testing.T, doc map[string]any, key string) map[string]any {
	t.Helper()
	elem, ok := doc[key].(map[string]any)
	if !ok {
		t.Fatalf("the document has no element %s; it has %v", key, keysOf(doc))
	}
	return elem
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestTagsAreEightHexDigits covers the member name.
//
// "(0008,0005)" is what the older rendering used, and no consumer of the JSON
// model will find an element under it.
func TestTagsAreEightHexDigits(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("MR")))

	doc := jsonOf(t, ds)
	if _, ok := doc["00080060"]; !ok {
		t.Errorf("the element is not under 00080060; the document has %v", keysOf(doc))
	}
}

// TestValuesAreArrays covers the other half of the shape.
func TestValuesAreArrays(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0008), dataelem.CS,
		[]byte("ORIGINAL\\PRIMARY\\AXIAL")))

	elem := elementOf(t, jsonOf(t, ds), "00080008")
	want := []any{"ORIGINAL", "PRIMARY", "AXIAL"}
	if got := elem["Value"]; !reflect.DeepEqual(got, want) {
		t.Errorf("Value is %#v, want %#v; a multi-valued element is an array, not "+
			"one string with backslashes in it", got, want)
	}
}

// TestNumericVRsAreJSONNumbers covers the VRs the standard does not quote.
//
// DS and IS are stored as text and are numbers in JSON. A consumer that reads
// Rows as the string "128" cannot do arithmetic with it.
func TestNumericVRsAreJSONNumbers(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0030), dataelem.DS, []byte("0.5\\0.5")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x0013), dataelem.IS, []byte("42")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.US, []byte{0x80, 0x00}))

	doc := jsonOf(t, ds)
	for _, tc := range []struct {
		key  string
		want []any
	}{
		{"00280030", []any{0.5, 0.5}},
		{"00200013", []any{float64(42)}},
		{"00280010", []any{float64(128)}},
	} {
		if got := elementOf(t, doc, tc.key)["Value"]; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s Value is %#v, want %#v", tc.key, got, tc.want)
		}
	}
}

// TestPersonNameIsAnObject covers PN, which the standard splits into groups.
func TestPersonNameIsAnObject(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN,
		[]byte("Yamada^Tarou=山田^太郎=やまだ^たろう")))

	value := elementOf(t, jsonOf(t, ds), "00100010")["Value"].([]any)
	name, ok := value[0].(map[string]any)
	if !ok {
		t.Fatalf("a person name came out as %T, want an object with named groups", value[0])
	}
	for key, want := range map[string]string{
		"Alphabetic":  "Yamada^Tarou",
		"Ideographic": "山田^太郎",
		"Phonetic":    "やまだ^たろう",
	} {
		if got := name[key]; got != want {
			t.Errorf("%s is %v, want %q", key, got, want)
		}
	}
}

// TestPersonNameOmitsAbsentGroups keeps an absent group distinguishable from an
// empty one.
func TestPersonNameOmitsAbsentGroups(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^Jane")))

	value := elementOf(t, jsonOf(t, ds), "00100010")["Value"].([]any)
	name := value[0].(map[string]any)
	if len(name) != 1 || name["Alphabetic"] != "Doe^Jane" {
		t.Errorf("an ASCII-only name came out as %#v, want only an Alphabetic group", name)
	}
}

// TestBinaryValuesAreInlineBinary covers the VRs carried as base64.
func TestBinaryValuesAreInlineBinary(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB,
		[]byte{0x00, 0x01, 0x02, 0xFF}))

	elem := elementOf(t, jsonOf(t, ds), "7FE00010")
	if got := elem["InlineBinary"]; got != "AAEC/w==" {
		t.Errorf("InlineBinary is %v, want AAEC/w==", got)
	}
	if _, ok := elem["Value"]; ok {
		t.Error("a binary value carries both InlineBinary and Value; the standard has one or the other")
	}
}

// TestBulkDataURIReplacesLargeValues covers the option that keeps a document
// from carrying an image.
func TestBulkDataURIReplacesLargeValues(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, make([]byte, 4096)))

	m, err := ds.ToDICOMJSONWithOptions(dataset.DICOMJSONOptions{
		BulkDataThreshold: 1024,
		BulkDataURIFunc:   func(t tag.Tag) string { return "https://example.org/bulk/" + t.String() },
	})
	if err != nil {
		t.Fatalf("ToDICOMJSONWithOptions: %v", err)
	}
	elem := m["7FE00010"]
	if elem.BulkDataURI == "" {
		t.Error("a 4096-byte value was inlined despite a 1024-byte threshold")
	}
	if elem.InlineBinary != "" {
		t.Error("the value is both inline and out of line")
	}
}

// TestAttributeTagsAreHexStrings covers AT, which holds tags rather than text.
func TestAttributeTagsAreHexStrings(t *testing.T) {
	ds := dataset.NewDataset()
	// (3004,000C) stored little endian: group then element, low byte first.
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0009), dataelem.AT,
		[]byte{0x04, 0x30, 0x0C, 0x00}))

	value := elementOf(t, jsonOf(t, ds), "00280009")["Value"].([]any)
	if got := value[0]; got != "3004000C" {
		t.Errorf("an attribute tag came out as %v, want 3004000C", got)
	}
}

// TestEmptyElementHasNoValueMember covers the distinction the standard draws.
func TestEmptyElementHasNoValueMember(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x002A), dataelem.DT, []byte{}))

	elem := elementOf(t, jsonOf(t, ds), "0008002A")
	if _, ok := elem["Value"]; ok {
		t.Errorf("an empty element has a Value member: %#v", elem)
	}
	if elem["vr"] != "DT" {
		t.Errorf("vr is %v, want DT", elem["vr"])
	}
}

// TestEmptySequenceKeepsItsValueMember covers the case that looks the same and
// is not.
//
// A sequence with no items exists and is empty. An absent value is not there at
// all. Collapsing them loses the difference, and a consumer reading them apart
// is entitled to see it.
func TestEmptySequenceKeepsItsValueMember(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1111), dataelem.SQ, sequence.New()))

	elem := elementOf(t, jsonOf(t, ds), "00081111")
	value, ok := elem["Value"]
	if !ok {
		t.Fatalf("an empty sequence lost its Value member: %#v", elem)
	}
	if items, _ := value.([]any); len(items) != 0 {
		t.Errorf("Value is %#v, want an empty array", value)
	}
}

// TestSequencesNest covers SQ.
func TestSequencesNest(t *testing.T) {
	inner := dataset.NewDataset()
	_ = inner.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0100), dataelem.SH, []byte("T-D0010")))

	seq := sequence.New()
	if err := seq.Append(inner); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1110), dataelem.SQ, seq))

	elem := elementOf(t, jsonOf(t, ds), "00081110")
	items := elem["Value"].([]any)
	if len(items) != 1 {
		t.Fatalf("the sequence has %d items, want 1", len(items))
	}
	nested := items[0].(map[string]any)["00080100"].(map[string]any)
	if got := nested["Value"].([]any)[0]; got != "T-D0010" {
		t.Errorf("the nested value is %v, want T-D0010", got)
	}
}

// TestUnknownVRIsResolvedFromTheDictionary covers a file that says UN.
//
// PS3.5 6.2.2 lets a sender write UN when it does not know the VR and lets a
// receiver look the tag up instead. Carrying UN into JSON base64s a date or a
// patient name — a valid document no consumer can use. pydicom, dcm4che and
// dcmtk all resolve it, and pydicom's rtdose_rle.dcm has 60 such elements.
func TestUnknownVRIsResolvedFromTheDictionary(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0020), dataelem.UN, []byte("20050530")))

	elem := elementOf(t, jsonOf(t, ds), "00080020")
	if elem["vr"] != "DA" {
		t.Errorf("vr is %v, want DA from the dictionary", elem["vr"])
	}
	if got := elem["Value"].([]any)[0]; got != "20050530" {
		t.Errorf("Value is %v, want the date as text", got)
	}
	if _, ok := elem["InlineBinary"]; ok {
		t.Error("a date came out as base64")
	}
}

// TestUnknownVRHoldingASequenceStaysUnknown covers where that resolution stops.
//
// A UN element the dictionary calls SQ holds an encoded sequence, not a value.
// Claiming SQ without parsing it would produce a Value that is not there.
func TestUnknownVRHoldingASequenceStaysUnknown(t *testing.T) {
	ds := dataset.NewDataset()
	// (300C,0002) Referenced RT Plan Sequence, dictionary VR SQ.
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x300C, 0x0002), dataelem.UN,
		[]byte{0xFE, 0xFF, 0x00, 0xE0}))

	elem := elementOf(t, jsonOf(t, ds), "300C0002")
	if elem["vr"] != "UN" {
		t.Errorf("vr is %v, want UN; the bytes are an encoded sequence and were not parsed",
			elem["vr"])
	}
}

// TestPrivateCreatorIsLongString covers the one VR no dictionary is needed for.
func TestPrivateCreatorIsLongString(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0009, 0x0010), dataelem.UN, []byte("ACME 1.0")))

	elem := elementOf(t, jsonOf(t, ds), "00090010")
	if elem["vr"] != "LO" {
		t.Errorf("vr is %v, want LO; a private creator is the vendor's own name", elem["vr"])
	}
}

// TestAmbiguousVRIsResolved covers the dictionary entries that name two.
//
// The JSON model has no way to say "either of these", and picking arbitrarily
// produces a document that is accepted and wrong: 40000 read as signed is
// -25536.
func TestAmbiguousVRIsResolved(t *testing.T) {
	tests := []struct {
		name              string
		pixelRepresention []byte
		want              string
	}{
		{"unsigned pixel representation", []byte{0x00, 0x00}, "US"},
		{"signed pixel representation", []byte{0x01, 0x00}, "SS"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds := dataset.NewDataset()
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0103), dataelem.US,
				tc.pixelRepresention))
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0106), "US or SS",
				[]byte{0x40, 0x9C}))

			if got := elementOf(t, jsonOf(t, ds), "00280106")["vr"]; got != tc.want {
				t.Errorf("vr is %v, want %s", got, tc.want)
			}
		})
	}
}

// TestRoundTrip checks a document parses back to the values it came from.
func TestRoundTrip(t *testing.T) {
	inner := dataset.NewDataset()
	_ = inner.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0100), dataelem.SH, []byte("T-D0010")))
	seq := sequence.New()
	_ = seq.Append(inner)

	original := dataset.NewDataset()
	_ = original.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("MR")))
	_ = original.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^Jane")))
	_ = original.Add(dataelem.NewDataElement(tag.New(0x0020, 0x0013), dataelem.IS, []byte("42")))
	_ = original.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.US, []byte{0x80, 0x00}))
	_ = original.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0009), dataelem.AT,
		[]byte{0x04, 0x30, 0x0C, 0x00}))
	_ = original.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB,
		[]byte{0x00, 0x01, 0x02, 0xFF}))
	_ = original.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1110), dataelem.SQ, seq))

	text, err := original.ToDICOMJSONString()
	if err != nil {
		t.Fatalf("ToDICOMJSONString: %v", err)
	}

	back := dataset.NewDataset()
	if err := back.FromDICOMJSONString(text); err != nil {
		t.Fatalf("FromDICOMJSONString: %v", err)
	}

	for _, key := range []string{"00080060", "00100010", "00200013", "00280010",
		"00280009", "7FE00010", "00081110"} {
		if _, ok := jsonOf(t, back)[key]; !ok {
			t.Errorf("%s did not survive the round trip", key)
		}
	}

	// And the values, not only the tags.
	again, err := back.ToDICOMJSONString()
	if err != nil {
		t.Fatalf("re-encoding: %v", err)
	}
	var first, second map[string]any
	_ = json.Unmarshal([]byte(text), &first)
	_ = json.Unmarshal([]byte(again), &second)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("the document changed on a round trip:\n first: %s\nsecond: %s", text, again)
	}
}

// TestFloatsJSONCannotCarry covers values IEEE allows and JSON does not.
//
// Infinity and NaN appear in real data sets. Writing null for them would be
// indistinguishable from an empty value, so they keep their DICOM spellings.
func TestFloatsJSONCannotCarry(t *testing.T) {
	ds := dataset.NewDataset()
	buf := make([]byte, 16)
	putFloat64(buf[0:], math.Inf(1))
	putFloat64(buf[8:], math.NaN())
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0018, 0x9218), dataelem.FD, buf))

	value := elementOf(t, jsonOf(t, ds), "00189218")["Value"].([]any)
	if value[0] != "inf" || value[1] != "NaN" {
		t.Errorf("Value is %#v, want [\"inf\", \"NaN\"]", value)
	}
}

func putFloat64(b []byte, f float64) {
	bits := math.Float64bits(f)
	for i := 0; i < 8; i++ {
		b[i] = byte(bits >> (8 * i))
	}
}
