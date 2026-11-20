package anonymize_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestLookupSafePrivateGivesTheTablesVR proves a lookup by group, private
// creator and element finds the attribute Table E.3.10-1 lists and the value
// representation the table gives it, and that nothing near it matches: a
// creator in other letters, another group, another element.
func TestLookupSafePrivateGivesTheTablesVR(t *testing.T) {
	tests := []struct {
		group   uint16
		creator string
		element byte
		vr      dataelem.VR
		safe    bool
	}{
		{0x00E1, "ELSCINT1", 0x21, dataelem.DS, true},
		{0x01E1, "ELSCINT1", 0x26, dataelem.CS, true},
		{0x0019, "GEMS_ACQU_01", 0x9E, dataelem.LO, true},
		{0x0043, "GEMS_PARM_01", 0x39, dataelem.IS, true},
		{0x0019, "GEMS_ACQU_01", 0x9E, dataelem.LO, true},
		// The creator is compared as written, minus its padding.
		{0x00E1, "ELSCINT1 ", 0x21, dataelem.DS, true},
		{0x00E1, "elscint1", 0x21, "", false},
		{0x00E3, "ELSCINT1", 0x21, "", false},
		{0x00E1, "ELSCINT1", 0x22, "", false},
		{0x00E1, "", 0x21, "", false},
	}
	for _, tc := range tests {
		vr, ok := anonymize.LookupSafePrivate(tc.group, tc.creator, tc.element)
		if ok != tc.safe || vr != tc.vr {
			t.Errorf("(%04X, %q, %02X) = %q, %v; want %q, %v", tc.group, tc.creator, tc.element, vr, ok, tc.vr, tc.safe)
		}
	}
}

// TestSafePrivateEntriesAreTheTableLookupFinds proves the iterator lists the
// 479 rows of the table, each of which the lookup agrees is safe with the
// same VR (five leave it blank in the standard), and that the Retain Safe Private
// Option keeps exactly such an attribute and no other one.
func TestSafePrivateEntriesAreTheTableLookupFinds(t *testing.T) {
	n, blank := 0, 0
	var first anonymize.SafePrivateEntry
	for e := range anonymize.SafePrivateEntries() {
		if n == 0 {
			first = e
		}
		n++
		vr, ok := anonymize.LookupSafePrivate(e.Group, e.Creator, e.Element)
		if !ok || vr != e.VR {
			t.Errorf("%+v: lookup gave %q, %v", e, vr, ok)
		}
		if e.VR == "" {
			blank++
		}
		if e.Group%2 == 0 {
			t.Errorf("%+v: group %04X is not private", e, e.Group)
		}
	}
	if n != 479 {
		t.Errorf("table lists %d attributes, want 479", n)
	}
	if blank != 5 {
		t.Errorf("%d rows have no VR, the table leaves 5 blank", blank)
	}
	if anonymize.SafePrivateEdition == "" || anonymize.SafePrivateEdition != anonymize.ProfileTableEdition {
		t.Errorf("safe private edition %q, profile edition %q", anonymize.SafePrivateEdition, anonymize.ProfileTableEdition)
	}

	// The profile keeps the first row, put in a block of the file's choosing,
	// and removes a neighbor the table does not list.
	ds := dataset.NewDataset()
	const block = 0x0042
	creator := tag.New(first.Group, block)
	safe := tag.New(first.Group, block<<8|uint16(first.Element))
	other := tag.New(first.Group, block<<8|uint16(first.Element^0x80))
	if _, ok := anonymize.LookupSafePrivate(first.Group, first.Creator, first.Element^0x80); ok {
		t.Skip("the neighbor is listed too")
	}
	for _, e := range []struct {
		tg tag.Tag
		vr dataelem.VR
		v  []byte
	}{
		{creator, dataelem.LO, padEven(first.Creator)},
		{safe, first.VR, []byte("12")},
		{other, dataelem.LO, []byte("AB")},
	} {
		if err := ds.Add(dataelem.NewDataElement(e.tg, e.vr, e.v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := anonymize.NewAnonymizer(anonymize.RetainSafePrivateProfile).Anonymize(ds); err != nil {
		t.Fatal(err)
	}
	if _, ok := ds.Get(safe); !ok {
		t.Errorf("%s, listed as safe, was removed", safe)
	}
	if _, ok := ds.Get(other); ok {
		t.Errorf("%s, not listed, was kept", other)
	}
}

func padEven(s string) []byte {
	if len(s)%2 == 1 {
		s += " "
	}
	return []byte(s)
}
