package dcmstore

import (
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
)

// Attribute matching from PS3.4 C.2.2.2.
//
// A C-FIND query is a data set of attributes to match against. Each attribute is
// matched by a rule chosen from its VR and the shape of the value, and getting
// that choice wrong is not a subtle failure: universal matching read as single
// value matching returns nothing where it should return everything, and a range
// read as a literal returns nothing at all.
//
// The rules implemented, in the order they are tried:
//
//   - Universal (C.2.2.2.3): a zero-length query value matches every stored
//     value, and the attribute is returned in the response.
//   - List of UID (C.2.2.2.2): for UI, a backslash-separated list matches if any
//     of its members matches.
//   - Range (C.2.2.2.5): for DA, TM and DT, "lower-upper", "-upper" or "lower-".
//   - Wildcard (C.2.2.2.4): for the string VRs, "*" for zero or more characters
//     and "?" for exactly one.
//   - Single value (C.2.2.2.1): everything else, compared for equality.
//
// Sequence matching (C.2.2.2.6) is handled in query.go, since it needs the
// data set rather than a single value.

// wildcardVRs are the VRs for which "*" and "?" are wildcards rather than
// literal characters.
//
// PS3.4 C.2.2.2.4 excludes the ones where those characters cannot occur or would
// be ambiguous — dates, times, UIDs and the numeric VRs. A "*" in a UI is a
// literal, not a wildcard, so a query for a UID containing one must not be
// treated as matching everything.
var wildcardVRs = map[dataelem.VR]bool{
	dataelem.AE: true,
	dataelem.CS: true,
	dataelem.LO: true,
	dataelem.LT: true,
	dataelem.PN: true,
	dataelem.SH: true,
	dataelem.ST: true,
	dataelem.UT: true,
	dataelem.UC: true,
}

// rangeVRs are the VRs for which a "-" separates a range rather than being part
// of the value.
var rangeVRs = map[dataelem.VR]bool{
	dataelem.DA: true,
	dataelem.TM: true,
	dataelem.DT: true,
}

// matchValue reports whether stored satisfies the query value for a VR.
//
// Both are the raw string forms, as they appear in the data set. A stored value
// may itself be multi-valued, backslash-separated, and matching any one
// component is a match — a query for one image type matches an instance whose
// ImageType is "ORIGINAL\PRIMARY\AXIAL".
func matchValue(vr dataelem.VR, query, stored string) bool {
	query = trimPadding(query)

	// Universal matching. Checked first and independently of VR, since an empty
	// value means "return this attribute" for every VR.
	if query == "" {
		return true
	}

	switch {
	case vr == dataelem.UI:
		return matchUIDList(query, stored)
	case rangeVRs[vr] && isRange(query):
		return matchRange(vr, query, stored)
	case wildcardVRs[vr] && hasWildcard(query):
		return matchAnyComponent(stored, func(component string) bool {
			return matchWildcard(query, component)
		})
	default:
		return matchAnyComponent(stored, func(component string) bool {
			return equalValues(vr, query, component)
		})
	}
}

// matchAnyComponent applies match to each backslash-separated component of a
// stored value, reporting whether any satisfies it.
func matchAnyComponent(stored string, match func(string) bool) bool {
	for _, component := range strings.Split(stored, `\`) {
		if match(trimPadding(component)) {
			return true
		}
	}
	return false
}

// matchUIDList implements list of UID matching: the query is one or more UIDs
// separated by backslashes, and a stored value matching any of them matches.
//
// UIDs are compared exactly. They are not wildcard-capable and not
// case-insensitive: a UID is a sequence of digits and dots, so anything else is
// a different UID rather than a variant spelling of the same one.
func matchUIDList(query, stored string) bool {
	stored = trimPadding(stored)
	for _, candidate := range strings.Split(query, `\`) {
		if trimPadding(candidate) == stored {
			return true
		}
	}
	return false
}

// equalValues compares two single values for equality.
//
// Dates and times are normalised first, so that a query of "0930" matches a
// stored "093000" — the same instant written to different precision, which
// PS3.5 permits for both.
func equalValues(vr dataelem.VR, query, stored string) bool {
	if rangeVRs[vr] {
		q, qOK := normalizeTemporal(vr, query, false)
		s, sOK := normalizeTemporal(vr, stored, false)
		if qOK && sOK {
			return q == s
		}
		// An unparseable value falls back to a literal comparison rather than
		// matching everything, which is what returning true here would do.
	}
	return query == stored
}

// hasWildcard reports whether a query value contains wildcard characters.
func hasWildcard(query string) bool {
	return strings.ContainsAny(query, "*?")
}

// matchWildcard implements wildcard matching: "*" for zero or more characters,
// "?" for exactly one.
//
// Written directly rather than by translating to a regular expression: the query
// comes from a peer, and building a pattern out of it means every regexp
// metacharacter in a patient name — "." and "(" occur in real ones — either has
// to be escaped correctly or becomes an injection. Matching is linear here, with
// no backtracking blow-up to worry about either.
func matchWildcard(pattern, value string) bool {
	// p and v walk the pattern and value. star remembers where the last "*" was
	// seen so the match can resume after it, which is what makes this linear.
	var p, v int
	star, resume := -1, 0

	for v < len(value) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == value[v]):
			p++
			v++
		case p < len(pattern) && pattern[p] == '*':
			star = p
			p++
			resume = v
		case star >= 0:
			// Backtrack: let the last "*" absorb one more character.
			p = star + 1
			resume++
			v = resume
		default:
			return false
		}
	}

	// Trailing "*" in the pattern may match the empty remainder.
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

// isRange reports whether a DA, TM or DT query value is a range.
//
// A range is "lower-upper", and either end may be omitted. The hyphen is the
// only separator, and a value containing one is a range even where both ends are
// present, because none of the three VRs allows a hyphen inside a value.
func isRange(query string) bool {
	return strings.Contains(query, "-")
}

// matchRange implements range matching for DA, TM and DT.
//
// An open lower bound matches everything up to and including the upper, and an
// open upper bound everything from the lower on. The bounds are inclusive, as
// C.2.2.2.5 specifies.
func matchRange(vr dataelem.VR, query, stored string) bool {
	lower, upper, _ := strings.Cut(query, "-")
	lower, upper = trimPadding(lower), trimPadding(upper)

	value, ok := normalizeTemporal(vr, trimPadding(stored), false)
	if !ok {
		// A stored value that cannot be read as a date or time cannot be placed
		// in a range. Refusing it is right: treating it as matching would put
		// unparseable data into every ranged result.
		return false
	}

	if lower != "" {
		// The lower bound is padded with the smallest value each missing field
		// can take, so that a bound of "2024" means the start of 2024.
		bound, boundOK := normalizeTemporal(vr, lower, false)
		if !boundOK || value < bound {
			return false
		}
	}

	if upper != "" {
		// The upper bound is padded with the largest instead, so that "2024"
		// means the end of 2024 and a range of "2024-2024" covers the year.
		bound, boundOK := normalizeTemporal(vr, upper, true)
		if !boundOK || value > bound {
			return false
		}
	}

	return true
}

// normalizeTemporal expands a DA, TM or DT value to a fixed-width string that
// compares correctly with others of the same VR under ordinary string
// comparison.
//
// Comparing as strings rather than parsing to time.Time is deliberate. These
// values are already big-endian by construction — year, then month, then day —
// so once every field is present, lexical order is chronological order. It also
// sidesteps what a partial value would mean as an instant: "2024" is not a point
// in time, and the caller says with toUpper which end of it they want.
//
// toUpper pads missing fields with their maximum rather than their minimum, for
// the upper end of a range.
//
// The bool reports whether the value was well-formed enough to normalise.
func normalizeTemporal(vr dataelem.VR, value string, toUpper bool) (string, bool) {
	value = trimPadding(value)
	if value == "" {
		return "", false
	}

	switch vr {
	case dataelem.DA:
		return padTemporal(value, daFields, toUpper)
	case dataelem.TM:
		return padTemporal(stripTimeSeparators(value), tmFields, toUpper)
	case dataelem.DT:
		// A DT may carry a &ZZXX offset. It is dropped rather than applied: doing
		// otherwise would make the comparison depend on a timezone the query did
		// not state, and mixing offset-aware and offset-naive values in one range
		// silently. Real archives store DT without an offset.
		if i := strings.IndexAny(value, "+-"); i > 0 {
			value = value[:i]
		}
		return padTemporal(stripTimeSeparators(value), dtFields, toUpper)
	default:
		return value, false
	}
}

// A temporal VR is a sequence of fixed-width fields, each with a minimum and a
// maximum it can be padded to when absent.
type temporalField struct {
	width int
	min   string
	max   string
}

var (
	daFields = []temporalField{
		{4, "0000", "9999"}, // year
		{2, "01", "12"},     // month
		{2, "01", "31"},     // day
	}
	tmFields = []temporalField{
		{2, "00", "23"},         // hour
		{2, "00", "59"},         // minute
		{2, "00", "60"},         // second, 60 for a leap second
		{6, "000000", "999999"}, // fraction
	}
	dtFields = append(append([]temporalField{}, daFields...), tmFields...)
)

// padTemporal fills in the fields a value leaves out.
//
// A value longer than its fields allow, or one holding anything but digits, is
// refused — the caller then compares literally rather than pretending to
// understand it.
func padTemporal(value string, fields []temporalField, toUpper bool) (string, bool) {
	// The fraction is introduced by a "." which is not part of the field width.
	fraction := ""
	if i := strings.Index(value, "."); i >= 0 {
		fraction = value[i+1:]
		value = value[:i]
	}
	if !allDigits(value) || !allDigits(fraction) {
		return "", false
	}

	var out strings.Builder
	consumed := 0
	for i, f := range fields {
		isFraction := i == len(fields)-1 && f.width == 6

		var part string
		switch {
		case isFraction:
			part = fraction
		case consumed < len(value):
			end := min(consumed+f.width, len(value))
			part = value[consumed:end]
			consumed = end
		}

		switch {
		case part == "":
			if toUpper {
				part = f.max
			} else {
				part = f.min
			}
		case len(part) < f.width:
			// A partial field — "9" for a month, or a three-digit fraction. The
			// fraction is left-aligned and padded on the right; every other field
			// is a whole number and a short one is malformed.
			if isFraction {
				pad := f.max[0:1]
				if !toUpper {
					pad = f.min[0:1]
				}
				part += strings.Repeat(pad, f.width-len(part))
			} else {
				return "", false
			}
		}
		out.WriteString(part)
	}

	// Anything left over is more than the VR can hold.
	if consumed < len(value) {
		return "", false
	}
	return out.String(), true
}

// stripTimeSeparators removes the colons a TM or DT may carry.
//
// They are permitted by the ACR-NEMA versions of these VRs and still appear in
// files from older equipment, so a stored "09:30:00" has to compare equal to a
// queried "093000".
func stripTimeSeparators(value string) string {
	if !strings.Contains(value, ":") {
		return value
	}
	return strings.ReplaceAll(value, ":", "")
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// trimPadding removes the padding a DICOM value may carry.
//
// PS3.5 6.2 pads a value to an even length with a trailing space, and some
// equipment pads with NUL instead. Leading spaces are significant in some VRs
// and insignificant in others; they are trimmed here because a query for
// "SMITH" should match a stored " SMITH" — which is what every other
// implementation does.
func trimPadding(s string) string {
	return strings.Trim(s, " \x00")
}
