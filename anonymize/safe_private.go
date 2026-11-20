package anonymize

import (
	"iter"
	"sort"
	"strings"
	"sync"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// safePrivateKey names a private attribute independently of the block a file
// put it in: its group, its private creator, and its element within the
// creator's block (PS3.5 7.8.1).
type safePrivateKey struct {
	group   uint16
	creator string
	element byte
}

// SafePrivateEdition is the edition of the DICOM Standard that Table E.3.10-1,
// the safe private attributes, was generated from.
const SafePrivateEdition = "2026d"

// LookupSafePrivate reports whether PS3.15 Table E.3.10-1 lists a private
// attribute as safe to retain, and the value representation the table gives
// it. The attribute is named as the table names it: by the group of the data
// element, the private creator that owns the block, and the element within
// that block, the low byte of the tag once the block is chosen
// (PS3.5 7.8.1) — the block itself is the file's choice and is not part of the
// key. The creator is compared as written, without its padding; a creator the
// table does not list, even by a letter's case, is not safe.
//
// ok is false for an attribute the table does not list, which the Retain Safe
// Private Option removes. vr is empty for the five rows of the SIEMENS
// Ultrasound SC2000 creator whose value representation the table leaves blank.
func LookupSafePrivate(group uint16, creator string, element byte) (vr dataelem.VR, ok bool) {
	v, ok := safePrivateAttributes[safePrivateKey{group: group, creator: strings.TrimRight(creator, " \x00"), element: element}]
	return dataelem.VR(v), ok
}

// SafePrivateEntry is one row of PS3.15 Table E.3.10-1.
type SafePrivateEntry struct {
	// Group is the odd group of the data element.
	Group uint16
	// Creator is the private creator that owns the block.
	Creator string
	// Element is the element within the creator's block, 0x00 to 0xFF.
	Element byte
	// VR is the value representation the table gives the attribute, empty
	// where the table leaves it blank.
	VR dataelem.VR
}

// safePrivateSorted is the table in creator, group, element order.
var safePrivateSorted = sync.OnceValue(func() []SafePrivateEntry {
	out := make([]SafePrivateEntry, 0, len(safePrivateAttributes))
	for k, vr := range safePrivateAttributes {
		out = append(out, SafePrivateEntry{Group: k.group, Creator: k.creator, Element: k.element, VR: dataelem.VR(vr)})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Creator != b.Creator {
			return a.Creator < b.Creator
		}
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return a.Element < b.Element
	})
	return out
})

// SafePrivateEntries returns the rows of PS3.15 Table E.3.10-1 one at a time,
// ordered by creator, group and element. The entries are copies.
func SafePrivateEntries() iter.Seq[SafePrivateEntry] {
	return func(yield func(SafePrivateEntry) bool) {
		for _, e := range safePrivateSorted() {
			if !yield(e) {
				return
			}
		}
	}
}

// contentSequence is Content Sequence (0040,A730), the tree of an SR document.
var contentSequence = tag.New(0x0040, 0xA730)

// contentFreeValues are the content item attributes whose values are free,
// not coded, and so can name a patient: Text Value, Person Name, and the
// date, time, datetime and UID values (PS3.3 C.17.3). Cleaning structured
// content empties them and keeps the coded tree around them.
var contentFreeValues = map[tag.Tag]bool{
	tag.New(0x0040, 0xA160): true, // Text Value
	tag.New(0x0040, 0xA123): true, // Person Name
	tag.New(0x0040, 0xA120): true, // DateTime
	tag.New(0x0040, 0xA121): true, // Date
	tag.New(0x0040, 0xA122): true, // Time
	tag.New(0x0040, 0xA124): true, // UID
}

// removeUnsafePrivate removes every private element that Table E.3.10-1 does
// not list, and every private creator left with nothing to own. An attribute
// is looked up by its creator and its element within the block, because the
// block itself is the file's choice (PS3.5 7.8.1).
func removeUnsafePrivate(ds *dataset.Dataset) {
	type block struct {
		group uint16
		block uint16
	}
	creators := map[block]string{}
	for _, t := range ds.Tags() {
		if t.IsPrivate() && t.Element() >= 0x0010 && t.Element() <= 0x00FF {
			if elem, ok := ds.Get(t); ok {
				// Bytes, not a []byte assertion: a creator held as a string
				// read as none, and the safe attributes it owns were dropped.
				if raw, ok := elem.Bytes(); ok {
					creators[block{t.Group(), t.Element()}] = strings.TrimRight(string(raw), " \x00")
				}
			}
		}
	}

	used := map[block]bool{}
	for _, t := range ds.Tags() {
		if !t.IsPrivate() || t.Element() < 0x1000 {
			continue
		}
		b := block{t.Group(), t.Element() >> 8}
		key := safePrivateKey{group: t.Group(), creator: creators[b], element: byte(t.Element())}
		if _, safe := safePrivateAttributes[key]; creators[b] != "" && safe {
			used[b] = true
			continue
		}
		ds.Remove(t)
	}
	for _, t := range ds.Tags() {
		if !t.IsPrivate() || t.Element() >= 0x1000 {
			continue
		}
		if t.Element() >= 0x0010 && used[block{t.Group(), t.Element()}] {
			continue
		}
		ds.Remove(t)
	}
}
