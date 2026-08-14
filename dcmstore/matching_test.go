package dcmstore

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
)

func TestUniversalMatching(t *testing.T) {
	// A zero-length query value matches every stored value, whatever the VR, and
	// whether the stored value is present or itself empty. Reading this as single
	// value matching returns nothing where it should return everything, which is
	// the difference between a working C-FIND and one that finds no studies at all.
	for _, vr := range []dataelem.VR{
		dataelem.PN, dataelem.UI, dataelem.DA, dataelem.TM, dataelem.CS, dataelem.IS,
	} {
		for _, stored := range []string{"SMITH^JOHN", "", "20240115", "1.2.3.4"} {
			if !matchValue(vr, "", stored) {
				t.Errorf("%s: empty query did not match stored %q", vr, stored)
			}
			// Padding is not content: a value of one space is still zero-length
			// after the padding PS3.5 6.2 requires is removed.
			if !matchValue(vr, " ", stored) {
				t.Errorf("%s: a single-space query did not match stored %q", vr, stored)
			}
		}
	}
}

func TestSingleValueMatching(t *testing.T) {
	cases := []struct {
		vr            dataelem.VR
		query, stored string
		want          bool
		why           string
	}{
		{dataelem.PN, "SMITH^JOHN", "SMITH^JOHN", true, "identical"},
		{dataelem.PN, "SMITH^JOHN", "SMITH^JANE", false, "different given name"},
		{dataelem.CS, "CT", "CT", true, "identical modality"},
		{dataelem.CS, "CT", "MR", false, "different modality"},
		{dataelem.CS, "ct", "CT", false, "CS is case sensitive"},

		// Padding is stripped from both sides. PS3.5 6.2 pads to an even length
		// with a space, so a stored "CT " and a queried "CT" are the same value.
		{dataelem.CS, "CT", "CT ", true, "trailing space padding on the stored value"},
		{dataelem.CS, "CT ", "CT", true, "trailing space padding on the query"},
		{dataelem.CS, "CT", "CT\x00", true, "NUL padding, which some equipment writes"},
		{dataelem.PN, "SMITH", " SMITH", true, "leading space"},

		// A multi-valued stored value matches if any component does.
		{dataelem.CS, "AXIAL", `ORIGINAL\PRIMARY\AXIAL`, true, "third component"},
		{dataelem.CS, "ORIGINAL", `ORIGINAL\PRIMARY\AXIAL`, true, "first component"},
		{dataelem.CS, "SAGITTAL", `ORIGINAL\PRIMARY\AXIAL`, false, "no component matches"},

		// Numbers are compared as written; IS and DS are not normalised.
		{dataelem.IS, "1", "1", true, "identical integer string"},
		{dataelem.IS, "1", "2", false, "different integer string"},
	}

	for _, tc := range cases {
		if got := matchValue(tc.vr, tc.query, tc.stored); got != tc.want {
			t.Errorf("matchValue(%s, %q, %q) = %v, want %v (%s)",
				tc.vr, tc.query, tc.stored, got, tc.want, tc.why)
		}
	}
}

func TestWildcardMatching(t *testing.T) {
	cases := []struct {
		query, stored string
		want          bool
	}{
		{"SMITH*", "SMITH^JOHN", true},
		{"SMITH*", "SMITHSON^JOHN", true},
		{"SMITH*", "SMITH", true}, // "*" matches zero characters
		{"SMITH*", "SMIT", false},
		{"*JOHN", "SMITH^JOHN", true},
		{"*SMITH*", "DR SMITH JR", true},
		{"*", "anything at all", true},
		{"**", "anything at all", true}, // adjacent stars are not an error
		{"*", "", true},

		{"SMITH?", "SMITHS", true},
		{"SMITH?", "SMITH", false}, // "?" needs exactly one character
		{"SMITH?", "SMITHSON", false},
		{"?MITH", "SMITH", true},
		{"S?I?H", "SMITH", true},

		// Both kinds together. "S*H?N" would need H,any,N as the final three
		// characters and they are "OHN", so it does not match — the ? is exactly
		// one character, never zero.
		{"S*H?N", "SMITH^JOHN", false},
		{"S*H^?OHN", "SMITH^JOHN", true},
		{"S*H*N", "SMITH^JOHN", true},
		{"A*B*C", "AxxBxxC", true},
		{"A*B*C", "AxxBxx", false},

		// Backtracking: the first "*" must give back characters so the literal
		// after it can match later in the value.
		{"*abc", "xxabcxxabc", true},
		{"*abc*", "xxabcxx", true},
		{"a*a*a", "aaa", true},
	}

	for _, tc := range cases {
		if got := matchValue(dataelem.PN, tc.query, tc.stored); got != tc.want {
			t.Errorf("wildcard %q against %q = %v, want %v", tc.query, tc.stored, got, tc.want)
		}
	}
}

// Wildcards are only wildcards for the VRs C.2.2.2.4 allows them in. A "*" in a
// UI is a literal character, so a UID query containing one must match nothing
// rather than everything — the difference between retrieving one study and
// retrieving the archive.
func TestWildcardsAreLiteralInNonWildcardVRs(t *testing.T) {
	if matchValue(dataelem.UI, "1.2.3.*", "1.2.3.4") {
		t.Error("a * in a UI was treated as a wildcard; it is a literal character")
	}
	if !matchValue(dataelem.UI, "1.2.3.*", "1.2.3.*") {
		t.Error("a UI containing * did not match itself literally")
	}
	if matchValue(dataelem.DA, "2024*", "20240115") {
		t.Error("a * in a DA was treated as a wildcard")
	}
}

func TestUIDListMatching(t *testing.T) {
	const list = `1.2.840.113619.2.55.3\1.2.840.113619.2.55.4\1.2.3`

	for _, stored := range []string{"1.2.840.113619.2.55.3", "1.2.840.113619.2.55.4", "1.2.3"} {
		if !matchValue(dataelem.UI, list, stored) {
			t.Errorf("UID list did not match its member %q", stored)
		}
	}
	if matchValue(dataelem.UI, list, "1.2.840.113619.2.55.5") {
		t.Error("UID list matched a UID not in it")
	}
	// A single UID is a list of one.
	if !matchValue(dataelem.UI, "1.2.3", "1.2.3") {
		t.Error("a single UID did not match itself")
	}
	// A prefix is not a match: UIDs are hierarchical but matching is not.
	if matchValue(dataelem.UI, "1.2.3", "1.2.3.4") {
		t.Error("a UID matched a longer UID it is a prefix of")
	}
}

func TestDateRangeMatching(t *testing.T) {
	cases := []struct {
		query, stored string
		want          bool
		why           string
	}{
		{"20240101-20240131", "20240115", true, "inside"},
		{"20240101-20240131", "20240101", true, "on the lower bound, which is inclusive"},
		{"20240101-20240131", "20240131", true, "on the upper bound, which is inclusive"},
		{"20240101-20240131", "20231231", false, "before"},
		{"20240101-20240131", "20240201", false, "after"},

		{"-20240131", "20200101", true, "open lower bound"},
		{"-20240131", "20240201", false, "open lower bound, past the upper"},
		{"20240101-", "20991231", true, "open upper bound"},
		{"20240101-", "20231231", false, "open upper bound, before the lower"},

		// A partial bound is padded to the end of the period it names, so a year
		// as an upper bound covers that whole year.
		{"2024-2024", "20240615", true, "a year as both bounds covers the year"},
		{"2024-2024", "20250101", false, "the next year is outside"},
		{"2024-2024", "20231231", false, "the previous year is outside"},
		{"202401-202403", "20240215", true, "months as bounds"},
		{"202401-202403", "20240401", false, "past the last month"},

		// An exact date is not a range and must still match exactly.
		{"20240115", "20240115", true, "exact date"},
		{"20240115", "20240116", false, "exact date, different day"},
	}

	for _, tc := range cases {
		if got := matchValue(dataelem.DA, tc.query, tc.stored); got != tc.want {
			t.Errorf("DA %q against %q = %v, want %v (%s)",
				tc.query, tc.stored, got, tc.want, tc.why)
		}
	}
}

func TestTimeRangeMatching(t *testing.T) {
	cases := []struct {
		query, stored string
		want          bool
		why           string
	}{
		{"090000-170000", "120000", true, "inside working hours"},
		{"090000-170000", "080000", false, "before"},
		{"090000-170000", "173000", false, "after"},
		{"090000-170000", "090000", true, "on the lower bound"},
		{"090000-170000", "170000", true, "on the upper bound"},

		// Partial times: an hour as a bound covers the hour.
		{"09-17", "1230", true, "hours as bounds"},
		// A partial bound covers the whole period it names, the same way "2024" does
		// for a date, so 17:30 is inside a bound of "17".
		{"09-17", "173000", true, "inside the last hour, which the bound covers"},
		{"09-16", "173000", false, "past a bound that does not include the hour"},
		{"09-09", "093000", true, "one hour as both bounds covers it"},

		// Precision differences compare equal where they name the same instant.
		{"0930", "093000", true, "minute precision against second precision"},
		{"093000", "0930", true, "the same, the other way round"},
		{"093000.000000", "0930", true, "with an explicit zero fraction"},

		// Colons appear in TM values from older equipment.
		{"093000", "09:30:00", true, "stored with ACR-NEMA colons"},
		{"09:30:00", "093000", true, "queried with colons"},
	}

	for _, tc := range cases {
		if got := matchValue(dataelem.TM, tc.query, tc.stored); got != tc.want {
			t.Errorf("TM %q against %q = %v, want %v (%s)",
				tc.query, tc.stored, got, tc.want, tc.why)
		}
	}
}

func TestDateTimeRangeMatching(t *testing.T) {
	cases := []struct {
		query, stored string
		want          bool
		why           string
	}{
		{"20240101000000-20240131235959", "20240115120000", true, "inside"},
		{"20240101000000-20240131235959", "20240201000000", false, "after"},
		{"2024-2024", "20240615120000", true, "a year as both bounds"},
		{"20240115-20240115", "20240115235959", true, "a day as both bounds covers it"},
		{"20240115-20240115", "20240116000000", false, "the next day is outside"},

		// An offset is dropped rather than applied, so a value carrying one is
		// compared on its local fields.
		{"20240115-20240115", "20240115120000+0200", true, "with a positive offset"},
		{"20240115-20240115", "20240115120000-0500", true, "with a negative offset"},
	}

	for _, tc := range cases {
		if got := matchValue(dataelem.DT, tc.query, tc.stored); got != tc.want {
			t.Errorf("DT %q against %q = %v, want %v (%s)",
				tc.query, tc.stored, got, tc.want, tc.why)
		}
	}
}

// A stored value that is not a date cannot be placed in a range. Matching it
// would put unparseable data into every ranged result; the safe answer is that it
// does not match.
func TestUnparseableTemporalValuesDoNotMatchARange(t *testing.T) {
	for _, stored := range []string{"not a date", "2024-01-15", "", "20241332x"} {
		if matchValue(dataelem.DA, "20240101-20241231", stored) {
			t.Errorf("stored DA %q matched a range", stored)
		}
	}

	// But an exact query against the same stored value still compares literally,
	// rather than matching everything.
	if !matchValue(dataelem.DA, "not a date", "not a date") {
		t.Error("an unparseable DA did not match itself literally")
	}
	if matchValue(dataelem.DA, "not a date", "also not a date") {
		t.Error("two different unparseable DA values matched")
	}
}

func TestNormalizeTemporalPadsBounds(t *testing.T) {
	cases := []struct {
		vr      dataelem.VR
		value   string
		toUpper bool
		want    string
		wantOK  bool
	}{
		{dataelem.DA, "2024", false, "20240101", true},
		// The upper bound pads with each field's maximum: December, and the 31st.
		// A month-end that does not exist — 20240231 for February — is deliberate
		// and safe, since it is still above every real date in that month.
		{dataelem.DA, "2024", true, "20241231", true},
		{dataelem.DA, "202402", true, "20240231", true},
		{dataelem.DA, "202402", false, "20240201", true},
		{dataelem.DA, "20240215", false, "20240215", true},

		{dataelem.TM, "09", false, "090000000000", true},
		{dataelem.TM, "0930", false, "093000000000", true},

		{dataelem.DA, "20240215999", false, "", false}, // longer than the VR allows
		{dataelem.DA, "abcd", false, "", false},        // not digits
		{dataelem.DA, "", false, "", false},            // empty
	}

	for _, tc := range cases {
		got, ok := normalizeTemporal(tc.vr, tc.value, tc.toUpper)
		if ok != tc.wantOK {
			t.Errorf("normalizeTemporal(%s, %q, %v) ok = %v, want %v",
				tc.vr, tc.value, tc.toUpper, ok, tc.wantOK)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("normalizeTemporal(%s, %q, %v) = %q, want %q",
				tc.vr, tc.value, tc.toUpper, got, tc.want)
		}
	}
}

// Matching runs against every stored instance for every query, so it must not be
// pathological on adversarial input. A pattern of many stars against a long
// non-matching value is the classic blow-up for a backtracking matcher.
func TestWildcardMatchingDoesNotBlowUp(t *testing.T) {
	pattern := ""
	for i := 0; i < 40; i++ {
		pattern += "a*"
	}
	value := ""
	for i := 0; i < 4000; i++ {
		value += "a"
	}
	// A literal the value cannot supply, so the matcher has to exhaust its
	// backtracking before answering. Ending the pattern in "*" instead would let
	// the trailing star absorb everything and return true immediately, testing
	// nothing.
	pattern += "c"

	// Without linear matching this does not return in any useful time.
	if matchWildcard(pattern, value) {
		t.Errorf("a pattern ending in the literal %q matched a value of only %q",
			"c", "a")
	}
}
