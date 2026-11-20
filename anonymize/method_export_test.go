package anonymize_test

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// TestMethodCodesAreCID7050 proves the exported constants carry the code
// values and meanings of PS3.16 CID 7050, in the DCM scheme. The expected
// values were taken from the standard and agree with the code table in
// pydicom's source.
func TestMethodCodesAreCID7050(t *testing.T) {
	want := map[anonymize.MethodCodeValue][2]string{
		anonymize.MethodBasicProfile:                    {"113100", "Basic Application Confidentiality Profile"},
		anonymize.MethodCleanPixelData:                  {"113101", "Clean Pixel Data Option"},
		anonymize.MethodCleanRecognizableVisualFeatures: {"113102", "Clean Recognizable Visual Features Option"},
		anonymize.MethodCleanGraphics:                   {"113103", "Clean Graphics Option"},
		anonymize.MethodCleanStructuredContent:          {"113104", "Clean Structured Content Option"},
		anonymize.MethodCleanDescriptors:                {"113105", "Clean Descriptors Option"},
		anonymize.MethodRetainLongitudinalFullDates:     {"113106", "Retain Longitudinal Temporal Information Full Dates Option"},
		anonymize.MethodRetainLongitudinalModifiedDates: {"113107", "Retain Longitudinal Temporal Information Modified Dates Option"},
		anonymize.MethodRetainPatientCharacteristics:    {"113108", "Retain Patient Characteristics Option"},
		anonymize.MethodRetainDeviceIdentity:            {"113109", "Retain Device Identity Option"},
		anonymize.MethodRetainUIDs:                      {"113110", "Retain UIDs Option"},
		anonymize.MethodRetainSafePrivate:               {"113111", "Retain Safe Private Option"},
		anonymize.MethodRetainInstitutionIdentity:       {"113112", "Retain Institution Identity Option"},
	}
	for v, w := range want {
		c := v.Code()
		if c.Value != w[0] || string(v) != w[0] || c.Meaning != w[1] || v.Meaning() != w[1] || c.Scheme != "DCM" {
			t.Errorf("%s = %+v, want value %s, meaning %q, scheme DCM", v, c, w[0], w[1])
		}
	}
	if anonymize.MethodCodeValue("999999").Meaning() != "" {
		t.Error("a value outside CID 7050 has a meaning")
	}
}

// sequenceCodes reads (0012,0064) as value|scheme|meaning per item.
func sequenceCodes(t *testing.T, ds *dataset.Dataset) []string {
	t.Helper()
	elem, ok := ds.Get(tag.New(0x0012, 0x0064))
	if !ok {
		t.Fatal("no De-identification Method Code Sequence")
	}
	var out []string
	for _, item := range elem.GetValue().(*sequence.Sequence).Items() {
		in := item.(*dataset.Dataset)
		out = append(out, strings.Join([]string{
			textValue(in, tag.New(0x0008, 0x0100)),
			textValue(in, tag.New(0x0008, 0x0102)),
			textValue(in, tag.New(0x0008, 0x0104)),
		}, "|"))
	}
	return out
}

// TestRecordDeidentificationMethodWritesTheFiveAttributes proves a call
// writes De-identification Method, its code sequence, Patient Identity
// Removed, Burned In Annotation and Longitudinal Temporal Information
// Modified for a list of codes, replaces what an earlier pass wrote, and
// leaves the other attributes alone when the record does not name them.
func TestRecordDeidentificationMethodWritesTheFiveAttributes(t *testing.T) {
	ds := dataset.NewDataset()
	withValue(t, ds, tag.New(0x0012, 0x0063), "LO", "an earlier method")

	codes := []anonymize.MethodCode{
		anonymize.MethodBasicProfile.Code(),
		anonymize.MethodCleanPixelData.Code(),
		anonymize.MethodRetainLongitudinalModifiedDates.Code(),
	}
	err := anonymize.RecordDeidentificationMethod(ds, codes, anonymize.MethodRecord{
		IdentityRemoved:                 true,
		BurnedInAnnotation:              "NO",
		LongitudinalTemporalInformation: "MODIFIED",
	})
	if err != nil {
		t.Fatal(err)
	}

	for tg, want := range map[tag.Tag]string{
		tag.New(0x0012, 0x0062): "YES",
		tag.New(0x0012, 0x0063): `Basic Application Confidentiality Profile\Clean Pixel Data Option\Retain Longitudinal Temporal Information Modified Dates Option`,
		tag.New(0x0028, 0x0301): "NO",
		tag.New(0x0028, 0x0303): "MODIFIED",
	} {
		if got := textValue(ds, tg); got != want {
			t.Errorf("%s = %q, want %q", tg, got, want)
		}
	}
	got := sequenceCodes(t, ds)
	wantCodes := []string{
		"113100|DCM|Basic Application Confidentiality Profile",
		"113101|DCM|Clean Pixel Data Option",
		"113107|DCM|Retain Longitudinal Temporal Information Modified Dates Option",
	}
	if strings.Join(got, "\n") != strings.Join(wantCodes, "\n") {
		t.Errorf("code sequence %q, want %q", got, wantCodes)
	}

	// A bare record writes the method and nothing else.
	bare := dataset.NewDataset()
	if err := anonymize.RecordDeidentificationMethod(bare, codes[:1], anonymize.MethodRecord{}); err != nil {
		t.Fatal(err)
	}
	for _, tg := range []tag.Tag{tag.New(0x0012, 0x0062), tag.New(0x0028, 0x0301), tag.New(0x0028, 0x0303)} {
		if _, ok := bare.Get(tg); ok {
			t.Errorf("%s written though the record did not name it", tg)
		}
	}
}

// TestRecordDeidentificationMethodRefusesWhatItCannotWrite proves a call with
// no code, a code without a value, or a value the attribute does not allow
// returns an error and writes nothing.
func TestRecordDeidentificationMethodRefusesWhatItCannotWrite(t *testing.T) {
	basic := []anonymize.MethodCode{anonymize.MethodBasicProfile.Code()}
	tests := []struct {
		name   string
		codes  []anonymize.MethodCode
		record anonymize.MethodRecord
	}{
		{"no code", nil, anonymize.MethodRecord{}},
		{"empty value", []anonymize.MethodCode{{Meaning: "x"}}, anonymize.MethodRecord{}},
		{"burned in", basic, anonymize.MethodRecord{BurnedInAnnotation: "MAYBE"}},
		{"longitudinal", basic, anonymize.MethodRecord{LongitudinalTemporalInformation: "SHIFTED"}},
	}
	for _, tc := range tests {
		ds := dataset.NewDataset()
		if err := anonymize.RecordDeidentificationMethod(ds, tc.codes, tc.record); err == nil {
			t.Errorf("%s: no error", tc.name)
		}
		if len(ds.Tags()) != 0 {
			t.Errorf("%s: wrote %v", tc.name, ds.Tags())
		}
	}
	if err := anonymize.RecordDeidentificationMethod(nil, basic, anonymize.MethodRecord{}); err == nil {
		t.Error("a nil data set was accepted")
	}
}

// TestAnonymizeRecordsThroughTheExportedFunction proves what Anonymize
// writes is what the exported function writes for the same codes, so there is
// one implementation: the Basic Profile with the Retain UIDs Option gives the
// two CID 7050 items with Patient Identity Removed.
func TestAnonymizeRecordsThroughTheExportedFunction(t *testing.T) {
	viaProfile := dataset.NewDataset()
	if err := anonymize.NewAnonymizer(anonymize.RetainUIDsProfile).Anonymize(viaProfile); err != nil {
		t.Fatal(err)
	}
	direct := dataset.NewDataset()
	err := anonymize.RecordDeidentificationMethod(direct,
		[]anonymize.MethodCode{anonymize.MethodBasicProfile.Code(), anonymize.MethodRetainUIDs.Code()},
		anonymize.MethodRecord{IdentityRemoved: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range []tag.Tag{tag.New(0x0012, 0x0062), tag.New(0x0012, 0x0063)} {
		if a, b := textValue(viaProfile, tg), textValue(direct, tg); a != b || a == "" {
			t.Errorf("%s: Anonymize wrote %q, the function %q", tg, a, b)
		}
	}
	if a, b := strings.Join(sequenceCodes(t, viaProfile), ";"), strings.Join(sequenceCodes(t, direct), ";"); a != b {
		t.Errorf("code sequences differ: %q and %q", a, b)
	}
}
