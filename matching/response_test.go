package matching

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// text returns an element's value as text, and whether ds has it.
func text(t *testing.T, ds *dataset.Dataset, key tag.Tag) (string, bool) {
	t.Helper()
	elem, ok := ds.Get(key)
	if !ok {
		return "", false
	}
	raw, ok := elem.Bytes()
	if !ok {
		t.Fatalf("%s has no value as bytes", key)
	}
	return string(raw), true
}

// items returns the items of a sequence in an answer.
func items(t *testing.T, ds *dataset.Dataset, key tag.Tag) []*dataset.Dataset {
	t.Helper()
	elem, ok := ds.Get(key)
	if !ok {
		t.Fatalf("the answer has no %s", key)
	}
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok {
		t.Fatalf("%s in the answer is not a sequence", key)
	}
	var out []*dataset.Dataset
	for i := 0; i < seq.Length(); i++ {
		item, err := seq.Get(i)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, item.(*dataset.Dataset))
	}
	return out
}

// TestResponseHoldsTheKeysAskedFor: the answer to a worklist query carries
// each key with the entry's value, an empty value for one the entry does not
// have, and in the Scheduled Procedure Step Sequence the step that matched
// cut to the keys the query item asks for (PS3.4 K.6.1.2.2). The skipped
// Query/Retrieve Level and the query's Specific Character Set are not
// returned.
func TestResponseHoldsTheKeysAskedFor(t *testing.T) {
	entry := withSequence(t, build(t,
		attr{tagCharset, dataelem.CS, "ISO_IR 100"},
		attr{tagPatientName, dataelem.PN, "DOE^JANE"},
		attr{tagModality, dataelem.CS, "MR"},
	), tagSPS, step(t, "STA", "20261116", "S1"), step(t, "STB", "20261117", "S2"))

	query := withSequence(t, build(t,
		attr{tagCharset, dataelem.CS, "ISO_IR 100"},
		attr{tagQRLevel, dataelem.CS, "WORKLIST"},
		attr{tagPatientName, dataelem.PN, ""},
		attr{tagPatientID, dataelem.LO, ""},
	), tagSPS, build(t,
		attr{tagStationAET, dataelem.AE, "STB"},
		attr{tagSPSDate, dataelem.DA, ""},
		attr{tagSPSID, dataelem.SH, ""},
	))
	skipLevel := Options{Skip: func(parent tag.Tag, elem *dataelem.DataElement) bool {
		key, _ := elem.Tag()
		return parent == 0 && key == tagQRLevel
	}}

	answer, err := Response(query, entry, skipLevel)
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := text(t, answer, tagPatientName); got != "DOE^JANE" {
		t.Errorf("Patient's Name = %q, want DOE^JANE", got)
	}
	if got, ok := text(t, answer, tagPatientID); !ok || got != "" {
		t.Errorf("Patient ID = %q (present %v), want present and empty", got, ok)
	}
	for _, absent := range []tag.Tag{tagModality, tagQRLevel, tagCharset} {
		if answer.Contains(absent) {
			t.Errorf("the answer holds %s, which it was not asked for or was skipped", absent)
		}
	}

	steps := items(t, answer, tagSPS)
	if len(steps) != 1 {
		t.Fatalf("the answer holds %d steps, want the 1 that matched", len(steps))
	}
	if got, _ := text(t, steps[0], tagSPSID); got != "S2" {
		t.Errorf("the step answered is %q, want S2, the one at STB", got)
	}
	if got, _ := text(t, steps[0], tagSPSDate); got != "20261117" {
		t.Errorf("Scheduled Procedure Step Start Date = %q, want 20261117", got)
	}
	if steps[0].Contains(tagPreMedLO) {
		t.Error("the step carries Pre-Medication, which the query item did not ask for")
	}
}

// TestResponseToSequenceKeysWithoutAnItem: a sequence key with no item is
// answered with the entry's sequence whole, every item and attribute; one the
// entry does not have, with a sequence of no items.
func TestResponseToSequenceKeysWithoutAnItem(t *testing.T) {
	entry := withSequence(t, build(t, attr{tagPatientName, dataelem.PN, "DOE^JANE"}), tagSPS,
		step(t, "STA", "20261116", "S1"), step(t, "STB", "20261117", "S2"))

	answer, err := Response(withSequence(t, build(t), tagSPS), entry, Options{})
	if err != nil {
		t.Fatal(err)
	}
	steps := items(t, answer, tagSPS)
	if len(steps) != 2 {
		t.Fatalf("the answer holds %d steps, want both", len(steps))
	}
	if got, _ := text(t, steps[1], tagPreMedLO); got != "NONE" {
		t.Errorf("the second step's Pre-Medication = %q, want NONE: the item is not whole", got)
	}

	bare := build(t, attr{tagPatientName, dataelem.PN, "DOE^JANE"})
	answer, err = Response(withSequence(t, build(t), tagSPS), bare, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if steps := items(t, answer, tagSPS); len(steps) != 0 {
		t.Errorf("an entry without the sequence was answered with %d items, want none", len(steps))
	}
}

// TestResponseTextIsUTF8AndSaysSo: an entry in ISO_IR 100 (pydicom's
// chrFren.dcm) is answered in UTF-8 with Specific Character Set ISO_IR 192,
// so a peer reads Jérôme rather than the Latin-1 bytes under no name; an
// answer all in ASCII declares no character set (PS3.4 C.4.1.1.3.2).
func TestResponseTextIsUTF8AndSaysSo(t *testing.T) {
	french := corpusFile(t, "chrFren.dcm")
	query := build(t, attr{tagPatientName, dataelem.PN, ""})

	answer, err := Response(query, french, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := text(t, answer, tagPatientName); got != "Buc^Jérôme" {
		t.Errorf("Patient's Name = %q, want Buc^Jérôme in UTF-8", got)
	}
	if got, _ := text(t, answer, tagCharset); got != "ISO_IR 192" {
		t.Errorf("Specific Character Set = %q, want ISO_IR 192", got)
	}

	ascii, err := Response(query, build(t, attr{tagCharset, dataelem.CS, "ISO_IR 100"},
		attr{tagPatientName, dataelem.PN, "DOE^JANE"}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if ascii.Contains(tagCharset) {
		t.Error("an answer all in ASCII declares a character set")
	}
}
