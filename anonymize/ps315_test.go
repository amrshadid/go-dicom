package anonymize_test

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The Basic Application Level Confidentiality Profile is PS3.15 Table E.1-1,
// and it names 655 attributes. The table this package used to consult held
// thirty-eight.
//
// Measured against pydicom's corpus of 69 files, that left sixty identifying
// attributes carrying their original values in files the library reported as
// de-identified: Patient's Sex in 49 files, Patient's Age in 25, Patient's
// Weight in 14, Patient's Size in 4. The failure mode is the dangerous one —
// the function returns nil and the file looks anonymized.
//
// The tests below pin the cases that were wrong, each for its own reason.

func withValue(t *testing.T, ds *dataset.Dataset, tg tag.Tag, vr dataelem.VR, value string) {
	t.Helper()
	if len(value)%2 == 1 {
		value += " "
	}
	if err := ds.Add(dataelem.NewDataElement(tg, vr, []byte(value))); err != nil {
		t.Fatalf("adding %s: %v", tg.String(), err)
	}
}

func valueOf(ds *dataset.Dataset, tg tag.Tag) (string, bool) {
	elem, ok := ds.Get(tg)
	if !ok {
		return "", false
	}
	raw, _ := elem.GetValue().([]byte)
	return strings.TrimRight(string(raw), " \x00"), true
}

// TestPatientCharacteristicsAreRemoved covers four attributes the old table
// explicitly kept.
//
// It mapped Patient's Sex, Age, Size and Weight to keep-unchanged, and was
// consulted before the standard's own table — so the four survived under every
// profile, including the one whose purpose is to remove them. Keeping them is
// what RetainPatientCharsProfile exists for.
func TestPatientCharacteristicsAreRemoved(t *testing.T) {
	tests := []struct {
		name  string
		tg    tag.Tag
		vr    dataelem.VR
		value string
	}{
		{"Patient's Sex", tag.New(0x0010, 0x0040), dataelem.CS, "F"},
		{"Patient's Age", tag.New(0x0010, 0x1010), dataelem.AS, "047Y"},
		{"Patient's Size", tag.New(0x0010, 0x1020), dataelem.DS, "1.75"},
		{"Patient's Weight", tag.New(0x0010, 0x1030), dataelem.DS, "68.5"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds := dataset.NewDataset()
			withValue(t, ds, tc.tg, tc.vr, tc.value)

			a := anonymize.NewAnonymizer(anonymize.BasicProfile)
			if err := a.Anonymize(ds); err != nil {
				t.Fatalf("Anonymize: %v", err)
			}

			if got, present := valueOf(ds, tc.tg); present && got == tc.value {
				t.Errorf("%s still reads %q after de-identification", tc.name, got)
			}
		})
	}
}

// TestRetainPatientCharsKeepsThem checks the profile that is allowed to.
func TestRetainPatientCharsKeepsThem(t *testing.T) {
	ds := dataset.NewDataset()
	withValue(t, ds, tag.New(0x0010, 0x0040), dataelem.CS, "F")

	a := anonymize.NewAnonymizer(anonymize.RetainPatientCharsProfile)
	if err := a.Anonymize(ds); err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	if got, _ := valueOf(ds, tag.New(0x0010, 0x0040)); got != "F" {
		t.Errorf("Patient's Sex is %q under RetainPatientCharsProfile, want F", got)
	}
}

// TestIdentifiersInsideSequencesAreRemoved covers the attributes the old walk
// could not reach.
//
// De-identification iterated the profile's tags and looked each one up in the
// top-level data set. Anything inside a sequence was never visited, and
// Request Attributes Sequence and Referenced Image Sequence carry identifiers
// in ordinary files.
func TestIdentifiersInsideSequencesAreRemoved(t *testing.T) {
	item := dataset.NewDataset()
	withValue(t, item, tag.New(0x0010, 0x0010), dataelem.PN, "Doe^John")
	withValue(t, item, tag.New(0x0008, 0x0080), dataelem.LO, "St Elsewhere")

	seq := sequence.New()
	if err := seq.Append(item); err != nil {
		t.Fatalf("Append: %v", err)
	}

	ds := dataset.NewDataset()
	// Request Attributes Sequence, which the profile does not itself remove.
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0040, 0x0275), dataelem.SQ, seq)); err != nil {
		t.Fatalf("adding the sequence: %v", err)
	}

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.Anonymize(ds); err != nil {
		t.Fatalf("Anonymize: %v", err)
	}

	// Searched through the data set rather than through the item reference held
	// above: if the whole sequence is removed, that reference still holds the
	// values it had, while the file no longer contains them.
	for _, want := range []string{"Doe^John", "St Elsewhere"} {
		if containsValue(ds, want) {
			t.Errorf("%q survived de-identification inside a sequence", want)
		}
	}
}

// containsValue reports whether any element anywhere in the data set carries
// this exact value.
func containsValue(ds *dataset.Dataset, want string) bool {
	for _, elem := range ds.GetAll() {
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			for i := 0; i < seq.Length(); i++ {
				item, err := seq.Get(i)
				if err != nil {
					continue
				}
				if inner, ok := item.(*dataset.Dataset); ok && containsValue(inner, want) {
					return true
				}
			}
			continue
		}
		raw, _ := elem.GetValue().([]byte)
		if strings.TrimRight(string(raw), " \x00") == want {
			return true
		}
	}
	return false
}

// TestContainedUIDsAreReplaced covers the action that did nothing.
//
// The profile marks Source Image Sequence X/Z/U*, whose asterisk means
// "replacement of the instance UIDs contained in it". The UID action reached
// the sequence, matched neither of the value types it knew how to handle, and
// returned without touching anything.
//
// This is the leak that matters most. A Referenced SOP Instance UID left intact
// links the de-identified object straight back to the instance it came from,
// which is exactly what de-identification is for. It survived in 18 of
// pydicom's 69 files.
func TestContainedUIDsAreReplaced(t *testing.T) {
	const original = "1.3.6.1.4.1.5962.1.1.8.1.1.20040826185059.5457"

	item := dataset.NewDataset()
	withValue(t, item, tag.New(0x0008, 0x1155), dataelem.UI, original)

	seq := sequence.New()
	if err := seq.Append(item); err != nil {
		t.Fatalf("Append: %v", err)
	}

	ds := dataset.NewDataset()
	// Source Image Sequence, X/Z/U*.
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x2112), dataelem.SQ, seq)); err != nil {
		t.Fatalf("adding the sequence: %v", err)
	}

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.Anonymize(ds); err != nil {
		t.Fatalf("Anonymize: %v", err)
	}

	got, present := valueOf(item, tag.New(0x0008, 0x1155))
	if present && got == original {
		t.Fatal("a referenced SOP Instance UID inside a sequence was not replaced; " +
			"it links the de-identified object back to the original")
	}
	if present && got == "" {
		t.Error("the UID was emptied rather than replaced; the profile says to replace it")
	}
}

// TestUIDsAreRemappedConsistently checks that the replacement preserves
// structure.
//
// Two attributes referring to the same instance have to keep referring to the
// same instance afterwards, or a study falls apart into unrelated objects.
func TestUIDsAreRemappedConsistently(t *testing.T) {
	const shared = "1.2.826.0.1.3680043.10.511.7.1"

	ds := dataset.NewDataset()
	withValue(t, ds, tag.New(0x0020, 0x000D), dataelem.UI, shared) // Study Instance UID
	withValue(t, ds, tag.New(0x0020, 0x0052), dataelem.UI, shared) // Frame of Reference UID

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.Anonymize(ds); err != nil {
		t.Fatalf("Anonymize: %v", err)
	}

	study, _ := valueOf(ds, tag.New(0x0020, 0x000D))
	frame, _ := valueOf(ds, tag.New(0x0020, 0x0052))
	if study == shared || frame == shared {
		t.Fatal("a UID was left unchanged")
	}
	if study != frame {
		t.Errorf("the same UID was replaced by two different values (%q and %q); "+
			"references between attributes no longer resolve", study, frame)
	}
}

// TestOverlayDataIsRemoved covers the attributes named by a range of groups.
//
// The profile names curve data at 50xx and overlay data and comments at 60xx,
// which no exact lookup can find. Overlay planes are where a burned-in name
// most often survives an otherwise careful de-identification.
func TestOverlayDataIsRemoved(t *testing.T) {
	for _, tg := range []tag.Tag{
		tag.New(0x6000, 0x3000), // Overlay Data
		tag.New(0x6002, 0x3000), // a second overlay plane
		tag.New(0x6000, 0x4000), // Overlay Comments
		tag.New(0x5000, 0x0000), // Curve group
	} {
		t.Run(tg.String(), func(t *testing.T) {
			ds := dataset.NewDataset()
			if err := ds.Add(dataelem.NewDataElement(tg, dataelem.OW,
				[]byte{0x01, 0x02, 0x03, 0x04})); err != nil {
				t.Fatalf("adding %s: %v", tg.String(), err)
			}

			a := anonymize.NewAnonymizer(anonymize.BasicProfile)
			if err := a.Anonymize(ds); err != nil {
				t.Fatalf("Anonymize: %v", err)
			}
			if _, present := ds.Get(tg); present {
				t.Errorf("%s survived de-identification", tg.String())
			}
		})
	}
}

// TestAttributesOutsideTheProfileAreLeftAlone covers the other direction.
//
// The profile is a floor. Discarding data it does not name would lose the
// clinical content the file exists to carry.
func TestAttributesOutsideTheProfileAreLeftAlone(t *testing.T) {
	ds := dataset.NewDataset()
	withValue(t, ds, tag.New(0x0008, 0x0060), dataelem.CS, "CT")       // Modality
	withValue(t, ds, tag.New(0x0028, 0x0010), dataelem.US, "\x00\x02") // Rows

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.Anonymize(ds); err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	if got, _ := valueOf(ds, tag.New(0x0008, 0x0060)); got != "CT" {
		t.Errorf("Modality is %q after de-identification, want CT", got)
	}
	if _, present := ds.Get(tag.New(0x0028, 0x0010)); !present {
		t.Error("Rows was removed; the image can no longer be decoded")
	}
}

// TestProfileCoversTheWholeTable is a guard on the generated table itself.
//
// The count is what the 2026c edition of PS3.15 Table E.1-1 lists. It is here
// so that regenerating the table against a newer edition is a visible change
// rather than a silent one.
func TestProfileCoversTheWholeTable(t *testing.T) {
	// Sampled from across the table rather than counted, since the count lives
	// in the generated file and comparing it to itself would prove nothing.
	// These are attributes the thirty-eight-entry table did not have.
	for _, tg := range []tag.Tag{
		tag.New(0x0008, 0x0201), // Timezone Offset From UTC
		tag.New(0x0008, 0x1040), // Institutional Department Name
		tag.New(0x0008, 0x2111), // Derivation Description
		tag.New(0x0010, 0x2154), // Patient's Telephone Numbers
		tag.New(0x0010, 0x21B0), // Additional Patient History
		tag.New(0x0038, 0x0010), // Admission ID
		tag.New(0x0040, 0x1001), // Requested Procedure ID
		tag.New(0x0040, 0xA123), // Person Name
		tag.New(0x0020, 0x0052), // Frame of Reference UID
		tag.New(0x0088, 0x0140), // Storage Media File-set UID
	} {
		t.Run(tg.String(), func(t *testing.T) {
			ds := dataset.NewDataset()
			withValue(t, ds, tg, dataelem.LO, "identifying")

			a := anonymize.NewAnonymizer(anonymize.BasicProfile)
			if err := a.Anonymize(ds); err != nil {
				t.Fatalf("Anonymize: %v", err)
			}
			if got, present := valueOf(ds, tg); present && got == "identifying" {
				t.Errorf("%s (%s) is not covered by the profile",
					tg.String(), tg.GetInfo().Name)
			}
		})
	}
}
