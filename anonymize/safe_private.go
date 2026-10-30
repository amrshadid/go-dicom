package anonymize

import (
	"strings"

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
				if raw, ok := elem.GetValue().([]byte); ok {
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
		if creators[b] != "" && safePrivateAttributes[key] {
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
