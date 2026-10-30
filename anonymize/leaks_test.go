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

// holds reports whether any value in the data set, at any depth, contains s.
func holds(ds *dataset.Dataset, s string) bool {
	for _, elem := range ds.GetAll() {
		switch v := elem.GetValue().(type) {
		case *sequence.Sequence:
			for i := 0; i < v.Length(); i++ {
				if item, err := v.Get(i); err == nil {
					if inner, ok := item.(*dataset.Dataset); ok && holds(inner, s) {
						return true
					}
				}
			}
		case []byte:
			if strings.Contains(string(v), s) {
				return true
			}
		case string:
			if strings.Contains(v, s) {
				return true
			}
		}
	}
	return false
}

func withSequence(t *testing.T, ds *dataset.Dataset, at tag.Tag, items ...*dataset.Dataset) {
	t.Helper()
	seq := sequence.New()
	for _, item := range items {
		if err := seq.Append(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := ds.Add(dataelem.NewDataElement(at, dataelem.SQ, seq)); err != nil {
		t.Fatal(err)
	}
}

// srWithFreeText is a report whose one TEXT content item names the patient.
func srWithFreeText(t *testing.T) *dataset.Dataset {
	t.Helper()
	concept := dataset.NewDataset()
	withValue(t, concept, tag.New(0x0008, 0x0100), dataelem.SH, "121071")
	withValue(t, concept, tag.New(0x0008, 0x0102), dataelem.SH, "DCM")
	withValue(t, concept, tag.New(0x0008, 0x0104), dataelem.LO, "Finding")

	item := dataset.NewDataset()
	withValue(t, item, tag.New(0x0040, 0xA010), dataelem.CS, "CONTAINS")
	withValue(t, item, tag.New(0x0040, 0xA040), dataelem.CS, "TEXT")
	withSequence(t, item, tag.New(0x0040, 0xA043), concept)
	withValue(t, item, tag.New(0x0040, 0xA160), dataelem.UT, "Patient Jane Doe MRN PID12345 seen 20190314")
	withValue(t, item, tag.New(0x0040, 0xA123), dataelem.PN, "Doe^Jane")

	ds := dataset.NewDataset()
	withValue(t, ds, tag.New(0x0040, 0xA040), dataelem.CS, "CONTAINER")
	withSequence(t, ds, tag.New(0x0040, 0xA730), item)
	return ds
}

// TestSRFreeTextDoesNotSurvive: Content Sequence (0040,A730) is D in the Basic
// Profile and C under Clean Structured Content (PS3.15 Table E.1-1). The walk
// went into it as into any sequence, and Text Value is not in the table, so a
// TEXT item naming the patient survived every profile; Clean Structured
// Content had no code at all (#212).
func TestSRFreeTextDoesNotSurvive(t *testing.T) {
	for _, profile := range []anonymize.Profile{anonymize.BasicProfile, anonymize.CleanStructuredContentProfile} {
		ds := srWithFreeText(t)
		if err := anonymize.NewAnonymizer(profile).Anonymize(ds); err != nil {
			t.Fatal(err)
		}
		for _, phi := range []string{"Jane Doe", "PID12345", "Doe^Jane"} {
			if holds(ds, phi) {
				t.Errorf("profile %v: %q survived in the SR content", profile, phi)
			}
		}
		// Cleaning keeps the report's structure: the coded concept is not
		// identifying, and a report with its tree stripped is not "cleaned".
		if profile == anonymize.CleanStructuredContentProfile && !holds(ds, "121071") {
			t.Error("Clean Structured Content removed the content tree instead of cleaning it")
		}
	}
}

// TestRetainSafePrivateKeepsOnlySafeAttributes: the Retain Safe Private Option
// keeps the private attributes PS3.15 Table E.3.10-1 lists as safe and no
// others. The profile switched private-tag removal off altogether, so a vendor
// field holding the patient's name and ID survived (#212).
func TestRetainSafePrivateKeepsOnlySafeAttributes(t *testing.T) {
	ds := dataset.NewDataset()
	// ELSCINT1 (00E1,xx21) is in Table E.3.10-1; (00E1,xx99) is not.
	withValue(t, ds, tag.New(0x00E1, 0x0010), dataelem.LO, "ELSCINT1")
	withValue(t, ds, tag.New(0x00E1, 0x1021), dataelem.DS, "1.5")
	withValue(t, ds, tag.New(0x00E1, 0x1099), dataelem.LO, "DOE^JANE PID12345")
	// The same safe attribute in a block the file chose itself.
	withValue(t, ds, tag.New(0x00E1, 0x0042), dataelem.LO, "ELSCINT1")
	withValue(t, ds, tag.New(0x00E1, 0x4221), dataelem.DS, "2.5")
	// A creator with nothing safe.
	withValue(t, ds, tag.New(0x0029, 0x0010), dataelem.LO, "SOME VENDOR")
	withValue(t, ds, tag.New(0x0029, 0x1010), dataelem.LO, "DOE^JANE PID12345")

	if err := anonymize.NewAnonymizer(anonymize.RetainSafePrivateProfile).Anonymize(ds); err != nil {
		t.Fatal(err)
	}
	for _, kept := range []tag.Tag{tag.New(0x00E1, 0x0010), tag.New(0x00E1, 0x1021), tag.New(0x00E1, 0x0042), tag.New(0x00E1, 0x4221)} {
		if _, ok := ds.Get(kept); !ok {
			t.Errorf("%s, safe or the creator of a safe attribute, was removed", kept)
		}
	}
	for _, removed := range []tag.Tag{tag.New(0x00E1, 0x1099), tag.New(0x0029, 0x0010), tag.New(0x0029, 0x1010)} {
		if _, ok := ds.Get(removed); ok {
			t.Errorf("%s, not in Table E.3.10-1, was kept", removed)
		}
	}
	if holds(ds, "PID12345") {
		t.Error("the patient's ID survived in a private attribute")
	}
}

// TestDoseCalculationModelNameIsRemoved: (3004,007F) is X in Table E.1-1 and
// was the one row missing from the package's table (#212).
func TestDoseCalculationModelNameIsRemoved(t *testing.T) {
	ds := dataset.NewDataset()
	withValue(t, ds, tag.New(0x3004, 0x007F), dataelem.LO, "DOSEMODEL-ST-MARYS")
	if err := anonymize.NewAnonymizer(anonymize.BasicProfile).Anonymize(ds); err != nil {
		t.Fatal(err)
	}
	if _, ok := ds.Get(tag.New(0x3004, 0x007F)); ok {
		t.Error("Dose Calculation Model Name survived")
	}
}
