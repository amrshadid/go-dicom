package anonymize

import (
	"iter"
	"strings"
	"sync"

	"github.com/amrshadid/go-dicom/tag"
)

// The Basic Application Level Confidentiality Profile, PS3.15 Table E.1-1.
//
// ps315_table.go is generated from the standard by scripts/ps315-table.py. It
// holds every attribute the profile names, which is the point: the table it
// replaces held thirty-eight of them, and measured against pydicom's corpus
// that left sixty identifying attributes carrying their original values in
// files the library reported as de-identified — Patient's Sex in 49 of 69
// files, Patient's Age in 25, Patient's Weight in 14.
//
// Two of those matter more than their count suggests. Frame of Reference UID
// and Referenced SOP Instance UID were left intact, and either one re-links a
// de-identified file to the instance it came from. An anonymizer that misses
// them has not anonymized anything; it has only made the file harder to read.
//
// The profile gives some attributes a choice of action, written X/Z, X/D,
// X/Z/D or X/Z/U*: remove it, unless emptying or replacing it is needed to keep
// the object conformant to its IOD. The library resolves them toward keeping
// the attribute and removing its content, which is what dcmtk and
// dicom-anonymizer also do — an object stripped of a Type 1 attribute is no
// longer a valid object, and an invalid object is not a safe one. The exported
// view of the table does not resolve them; it gives the code as the standard
// writes it.

// ProfileColumn names one column of PS3.15 Table E.1-1: the Basic Profile or
// one of the options that modify it.
type ProfileColumn int

const (
	// ColumnBasic is the Basic Profile column (Basic Prof.).
	ColumnBasic ProfileColumn = iota
	// ColumnRetainSafePrivate is the Retain Safe Private Option column.
	ColumnRetainSafePrivate
	// ColumnRetainUIDs is the Retain UIDs Option column.
	ColumnRetainUIDs
	// ColumnRetainDeviceIdentity is the Retain Device Identity Option column.
	ColumnRetainDeviceIdentity
	// ColumnRetainInstitutionIdentity is the Retain Institution Identity
	// Option column.
	ColumnRetainInstitutionIdentity
	// ColumnRetainPatientCharacteristics is the Retain Patient Characteristics
	// Option column.
	ColumnRetainPatientCharacteristics
	// ColumnRetainLongitudinalFullDates is the Retain Longitudinal Temporal
	// Information with Full Dates Option column.
	ColumnRetainLongitudinalFullDates
	// ColumnRetainLongitudinalModifiedDates is the Retain Longitudinal
	// Temporal Information with Modified Dates Option column.
	ColumnRetainLongitudinalModifiedDates
	// ColumnCleanDescriptors is the Clean Descriptors Option column.
	ColumnCleanDescriptors
	// ColumnCleanStructuredContent is the Clean Structured Content Option
	// column.
	ColumnCleanStructuredContent
	// ColumnCleanGraphics is the Clean Graphics Option column.
	ColumnCleanGraphics

	numColumns int = iota
)

// columnNames are the names the standard gives the columns.
var columnNames = [numColumns]string{
	"Basic Profile",
	"Retain Safe Private Option",
	"Retain UIDs Option",
	"Retain Device Identity Option",
	"Retain Institution Identity Option",
	"Retain Patient Characteristics Option",
	"Retain Longitudinal Temporal Information with Full Dates Option",
	"Retain Longitudinal Temporal Information with Modified Dates Option",
	"Clean Descriptors Option",
	"Clean Structured Content Option",
	"Clean Graphics Option",
}

// String returns the name of the column as the standard words it.
func (c ProfileColumn) String() string {
	if c < 0 || int(c) >= numColumns {
		return "unknown column"
	}
	return columnNames[c]
}

// ps315Row is one row of Table E.1-1 as generated.
type ps315Row struct {
	tag     tag.Tag
	pattern string
	name    string
	codes   [numColumns]string
}

// ProfileEntry is one row of PS3.15 Table E.1-1: an attribute and what each
// column of the table says to do with it. An entry is a copy; changing it
// does not change the library's table.
type ProfileEntry struct {
	// Tag is the attribute's tag. For a row the standard gives as a range of
	// groups, Tag is the first tag of the range and Pattern says which tags
	// the row covers.
	Tag tag.Tag
	// Pattern is empty for an attribute named by one tag. For curve data and
	// overlay data and comments, which the standard names by a range of
	// groups, it is the standard's own text, for example "(60xx,3000)".
	Pattern string
	// Name is the attribute's name as the table gives it.
	Name  string
	codes [numColumns]string
}

// Code returns the entry's code in a column exactly as the standard writes it,
// or an empty string where the column is blank. Codes that are a choice are
// returned unresolved: "X/Z/D", "X/D", "Z/D", "X/Z", "X/Z/U*" (the asterisk
// is the standard's, and means the UIDs contained in a sequence are
// replaced). The single codes are X remove, Z replace with a zero length
// value, D replace with a non-zero length dummy value, U replace with a
// consistent UID, K keep and C clean.
func (e ProfileEntry) Code(c ProfileColumn) string {
	if c < 0 || int(c) >= numColumns {
		return ""
	}
	return e.codes[c]
}

// Basic is shorthand for Code(ColumnBasic), the action of the Basic Profile.
func (e ProfileEntry) Basic() string { return e.codes[ColumnBasic] }

func entryOf(r ps315Row) ProfileEntry {
	return ProfileEntry{Tag: r.tag, Pattern: r.pattern, Name: r.name, codes: r.codes}
}

// ProfileTable returns PS3.15 Table E.1-1, Application Level Confidentiality
// Profile Attributes, as data: every attribute the Basic Profile names, with
// its code in the Basic Profile column and in each option column. The
// attributes named by one tag come first in tag order, then the three the
// standard names by a range of groups (curve data and overlay data and
// comments). The row for private attributes, "(gggg,eeee) where gggg is odd",
// is not here: [LookupSafePrivate] and [RetainSafePrivateProfile] carry it.
//
// The table is the edition named by [ProfileTableEdition]. The slice is a new
// one on every call, so callers may keep and change it; the library's own
// table is not affected.
func ProfileTable() []ProfileEntry {
	out := make([]ProfileEntry, len(ps315Rows))
	for i, r := range ps315Rows {
		out[i] = entryOf(r)
	}
	return out
}

// ProfileEntries returns the rows of [ProfileTable] one at a time, without
// copying the table into a slice.
func ProfileEntries() iter.Seq[ProfileEntry] {
	return func(yield func(ProfileEntry) bool) {
		for _, r := range ps315Rows {
			if !yield(entryOf(r)) {
				return
			}
		}
	}
}

// profileIndex maps a tag to its row, for the rows named by one tag.
var profileIndex = sync.OnceValue(func() map[tag.Tag]int {
	m := make(map[tag.Tag]int, len(ps315Rows))
	for i, r := range ps315Rows {
		if r.pattern == "" {
			m[r.tag] = i
		}
	}
	return m
})

// LookupProfileEntry returns the row of PS3.15 Table E.1-1 for a tag, and
// whether the table names it. A tag in curve data or overlay data and
// comments is matched by the range of groups the standard gives.
func LookupProfileEntry(t tag.Tag) (ProfileEntry, bool) {
	if i, ok := profileIndex()[t]; ok {
		return entryOf(ps315Rows[i]), true
	}
	for _, r := range ps315Rows {
		if r.pattern != "" && patternMatches(r, t) {
			return entryOf(r), true
		}
	}
	return ProfileEntry{}, false
}

// patternMatches reports whether t falls in the range of groups a pattern row
// names: "(50xx,xxxx)" is any element of an even group 5000 to 50FF, and
// "(60xx,3000)" one element of the groups 6000 to 60FF.
func patternMatches(r ps315Row, t tag.Tag) bool {
	if t.Group()&0xFF00 != r.tag.Group() {
		return false
	}
	return strings.HasSuffix(r.pattern, "xxxx)") || t.Element() == r.tag.Element()
}

// repeatingGroupRule matches a family of tags that differ only in part of the
// group or element number.
type repeatingGroupRule struct {
	group, element         uint16
	groupMask, elementMask uint16
	action                 Action
}

// matches reports whether the rule covers a tag.
func (r repeatingGroupRule) matches(t tag.Tag) bool {
	return t.Group()&r.groupMask == r.group&r.groupMask &&
		t.Element()&r.elementMask == r.element&r.elementMask
}

// ps315RepeatingGroups covers the attributes the profile names by a range of
// groups rather than by one tag: curve data at 50xx and overlay data and
// comments at 60xx. Overlay planes are where a burned-in name most often
// survives an otherwise careful de-identification.
var ps315RepeatingGroups = []repeatingGroupRule{
	{group: 0x5000, element: 0x0000, groupMask: 0xFF00, elementMask: 0x0000, action: ActionRemove}, // X
	{group: 0x6000, element: 0x3000, groupMask: 0xFF00, elementMask: 0xFFFF, action: ActionRemove}, // X
	{group: 0x6000, element: 0x4000, groupMask: 0xFF00, elementMask: 0xFFFF, action: ActionRemove}, // X
}
