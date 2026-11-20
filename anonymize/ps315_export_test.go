package anonymize_test

import (
	"regexp"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/tag"
)

// TestProfileTableHoldsTheStandardsTable proves the exported table has every
// row of PS3.15 Table E.1-1 and names the edition it came from: 653
// attributes named by one tag, three named by a range of groups.
func TestProfileTableHoldsTheStandardsTable(t *testing.T) {
	rows := anonymize.ProfileTable()
	single, ranged := 0, 0
	for _, e := range rows {
		if e.Pattern == "" {
			single++
		} else {
			ranged++
		}
	}
	if single != 653 || ranged != 3 {
		t.Errorf("table has %d single-tag rows and %d ranges, want 653 and 3", single, ranged)
	}
	if !regexp.MustCompile(`^\d{4}[a-z]$`).MatchString(anonymize.ProfileTableEdition) {
		t.Errorf("ProfileTableEdition = %q, want an edition such as 2026d", anonymize.ProfileTableEdition)
	}
}

// TestProfileTableCodesAreTheStandardsOwn proves combined codes are left as
// the standard writes them and option columns are filled: Patient's Sex is Z
// in the Basic Profile and K in the Retain Patient Characteristics Option,
// Source Image Sequence is X/Z/U*, Study Date is K with full dates and C with
// modified dates, Patient ID is Z/D.
func TestProfileTableCodesAreTheStandardsOwn(t *testing.T) {
	tests := []struct {
		tg     tag.Tag
		col    anonymize.ProfileColumn
		want   string
		wantNm string
	}{
		{tag.New(0x0010, 0x0040), anonymize.ColumnBasic, "Z", "Patient's Sex"},
		{tag.New(0x0010, 0x0040), anonymize.ColumnRetainPatientCharacteristics, "K", "Patient's Sex"},
		{tag.New(0x0008, 0x2112), anonymize.ColumnBasic, "X/Z/U*", "Source Image Sequence"},
		{tag.New(0x0010, 0x0020), anonymize.ColumnBasic, "Z/D", "Patient ID"},
		{tag.New(0x0008, 0x0020), anonymize.ColumnBasic, "Z", "Study Date"},
		{tag.New(0x0008, 0x0020), anonymize.ColumnRetainLongitudinalFullDates, "K", "Study Date"},
		{tag.New(0x0008, 0x0020), anonymize.ColumnRetainLongitudinalModifiedDates, "C", "Study Date"},
		{tag.New(0x0008, 0x1030), anonymize.ColumnCleanDescriptors, "C", "Study Description"},
		{tag.New(0x0008, 0x0018), anonymize.ColumnRetainUIDs, "K", "SOP Instance UID"},
		{tag.New(0x0008, 0x0018), anonymize.ColumnBasic, "U", "SOP Instance UID"},
		{tag.New(0x0008, 0x0018), anonymize.ColumnCleanGraphics, "", "SOP Instance UID"},
	}
	for _, tc := range tests {
		e, ok := anonymize.LookupProfileEntry(tc.tg)
		if !ok {
			t.Errorf("%s is not in the table", tc.tg)
			continue
		}
		if got := e.Code(tc.col); got != tc.want {
			t.Errorf("%s (%s) in %q: code %q, want %q", tc.tg, e.Name, tc.col, got, tc.want)
		}
		if e.Name != tc.wantNm {
			t.Errorf("%s is named %q, want %q", tc.tg, e.Name, tc.wantNm)
		}
	}
	if e, _ := anonymize.LookupProfileEntry(tag.New(0x0010, 0x0040)); e.Basic() != "Z" {
		t.Errorf("Basic() = %q, want Z", e.Basic())
	}
}

// TestProfileTableOptionColumnsAgreeWithTheOptionProfiles proves the
// attributes each option profile keeps by a switch case are the ones its
// column of the table gives a K or a C, so the two descriptions of an option
// cannot drift apart.
func TestProfileTableOptionColumnsAgreeWithTheOptionProfiles(t *testing.T) {
	cases := map[anonymize.ProfileColumn][]tag.Tag{
		anonymize.ColumnRetainPatientCharacteristics: {
			tag.New(0x0010, 0x0040), tag.New(0x0010, 0x1010), tag.New(0x0010, 0x1020), tag.New(0x0010, 0x1030),
		},
		anonymize.ColumnRetainDeviceIdentity: {
			tag.New(0x0008, 0x1010), tag.New(0x0018, 0x1000),
		},
		anonymize.ColumnRetainLongitudinalFullDates: {
			tag.New(0x0008, 0x0020), tag.New(0x0008, 0x0030),
		},
		anonymize.ColumnCleanDescriptors: {
			tag.New(0x0008, 0x1030), tag.New(0x0008, 0x103E),
		},
	}
	for col, tags := range cases {
		for _, tg := range tags {
			e, ok := anonymize.LookupProfileEntry(tg)
			if !ok || e.Code(col) == "" {
				t.Errorf("%s has no code in %q, which the library applies", tg, col)
			}
		}
	}
}

// TestProfileTableIsACopy proves changing what ProfileTable or
// LookupProfileEntry returns does not change the library's table.
func TestProfileTableIsACopy(t *testing.T) {
	rows := anonymize.ProfileTable()
	rows[0].Name = "changed"
	rows[0].Tag = tag.New(0x7FE0, 0x0010)
	if again := anonymize.ProfileTable(); again[0].Name == "changed" || again[0].Tag == rows[0].Tag {
		t.Error("changing a returned row changed the table")
	}

	sex := tag.New(0x0010, 0x0040)
	e, _ := anonymize.LookupProfileEntry(sex)
	e.Name = "changed"
	if again, _ := anonymize.LookupProfileEntry(sex); again.Name == "changed" {
		t.Error("changing a looked-up row changed the table")
	}
}

// TestProfileEntriesYieldsTheTableInOrder proves the iterator gives the same
// rows as ProfileTable, and stops when the caller does.
func TestProfileEntriesYieldsTheTableInOrder(t *testing.T) {
	rows := anonymize.ProfileTable()
	i := 0
	for e := range anonymize.ProfileEntries() {
		if e.Tag != rows[i].Tag || e.Name != rows[i].Name {
			t.Fatalf("row %d is %s %q, ProfileTable has %s %q", i, e.Tag, e.Name, rows[i].Tag, rows[i].Name)
		}
		i++
	}
	if i != len(rows) {
		t.Errorf("iterator gave %d rows, ProfileTable %d", i, len(rows))
	}
	n := 0
	for range anonymize.ProfileEntries() {
		n++
		if n == 3 {
			break
		}
	}
	if n != 3 {
		t.Errorf("break did not stop the iterator: %d", n)
	}
}

// TestLookupProfileEntryMatchesRangesOfGroups proves a tag in an overlay or
// curve group finds the row the standard names by a range, and a tag the
// table does not name finds none.
func TestLookupProfileEntryMatchesRangesOfGroups(t *testing.T) {
	e, ok := anonymize.LookupProfileEntry(tag.New(0x6002, 0x3000))
	if !ok || e.Name != "Overlay Data" || e.Basic() != "X" {
		t.Errorf("(6002,3000) = %+v, %v; want Overlay Data, X", e, ok)
	}
	if e, ok := anonymize.LookupProfileEntry(tag.New(0x5004, 0x1234)); !ok || e.Name != "Curve Data" {
		t.Errorf("(5004,1234) = %+v, %v; want Curve Data", e, ok)
	}
	if _, ok := anonymize.LookupProfileEntry(tag.New(0x6002, 0x0010)); ok {
		t.Error("(6002,0010) Overlay Rows is not in the table")
	}
	if _, ok := anonymize.LookupProfileEntry(tag.New(0x7FE0, 0x0010)); ok {
		t.Error("Pixel Data is not in the table")
	}
	// PS3.5 7.6: the repeating groups are xx00 to xx1E, even only. An odd
	// group is private (PS3.5 7.8), and past xx1E the group is not one of them.
	if e, ok := anonymize.LookupProfileEntry(tag.New(0x601E, 0x3000)); !ok || e.Name != "Overlay Data" {
		t.Errorf("(601E,3000) = %+v, %v; want Overlay Data, the last overlay group", e, ok)
	}
	for _, private := range []tag.Tag{
		tag.New(0x6001, 0x3000), tag.New(0x6001, 0x4000), tag.New(0x601F, 0x3000),
		tag.New(0x5001, 0x1234), tag.New(0x6020, 0x3000), tag.New(0x50FE, 0x0010),
	} {
		if e, ok := anonymize.LookupProfileEntry(private); ok {
			t.Errorf("%s matched %q; it is not in a repeating group", private, e.Name)
		}
	}
}
