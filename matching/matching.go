package matching

import (
	"errors"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/internal/attrmatch"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Attributes is a candidate as matching reads it: an entity a query may
// match, whether a data set ([Identifier.MatchDataset] adapts one) or an index
// built from one.
type Attributes interface {
	// Value returns the value of the attribute t as text: UTF-8 for the VRs a
	// character set applies to, a binary number in decimal, more than one
	// value separated by backslashes. ok is false when the candidate has no
	// such attribute.
	Value(t tag.Tag) (value string, ok bool)

	// Items returns the items of the sequence t, and none when the candidate
	// does not have it.
	Items(t tag.Tag) []Attributes
}

// Options adjust how a query is read. The zero value matches on every key.
type Options struct {
	// Skip, when set, names the elements of a query that are not keys for
	// this caller: they are not matched on, and Response does not return
	// them. parent is the sequence the element is in an item of, and the
	// zero Tag at the top of the query.
	//
	// It is asked about every element but Specific Character Set and group
	// lengths, which are never keys, including elements with no value.
	Skip func(parent tag.Tag, key *dataelem.DataElement) bool
}

// Identifier is a query identifier read once to be matched against many
// candidates. It is safe for concurrent use.
type Identifier struct {
	keys []key
}

// key is one key of a query that selects something: universal keys select
// nothing and are not kept.
type key struct {
	tag tag.Tag
	vr  dataelem.VR

	// values are the query's values, read as UTF-8; a candidate matching any
	// one of them matches (C.2.2.2.8).
	values []string

	// empty is empty value matching (C.2.2.2.7).
	empty bool

	// item is the identifier of a sequence key's item (C.2.2.2.6); nil for
	// any other key.
	item *Identifier
}

// emptyValueVRs are the VRs for which a value of "" asks for the candidates
// where the attribute is empty (PS3.4 C.2.2.2.7). For any other VR the two
// characters are a value like any other.
var emptyValueVRs = map[dataelem.VR]bool{
	dataelem.AE: true, dataelem.CS: true, dataelem.DA: true, dataelem.DT: true,
	dataelem.LO: true, dataelem.LT: true, dataelem.PN: true, dataelem.SH: true,
	dataelem.ST: true, dataelem.TM: true, dataelem.UC: true, dataelem.UR: true,
	dataelem.UT: true,
}

// Compile reads a query identifier. Its text is read in the Specific
// Character Set it declares, and an item of a sequence in its own or, when it
// declares none, the query's.
func Compile(query *dataset.Dataset, opts Options) (*Identifier, error) {
	if query == nil {
		return nil, errors.New("matching: the query is nil")
	}
	return compile(query, 0, nil, opts), nil
}

func compile(query *dataset.Dataset, parent tag.Tag, inherited []string, opts Options) *Identifier {
	encodings := attrmatch.TextEncodings(query, inherited)
	id := &Identifier{}
	for _, elem := range query.GetAll() {
		t, ok := elem.Tag()
		if !ok || !isKey(t) {
			continue
		}
		if opts.Skip != nil && opts.Skip(parent, elem) {
			continue
		}

		if isSequence(elem) {
			item, ok := queryItem(elem)
			if !ok {
				// No item: universal matching of the sequence.
				continue
			}
			sub := compile(item, t, encodings, opts)
			if len(sub.keys) == 0 {
				// An item of universal keys selects nothing either.
				continue
			}
			id.keys = append(id.keys, key{tag: t, vr: dataelem.SQ, item: sub})
			continue
		}

		if attrmatch.ElementString(elem) == "" {
			// Universal matching (C.2.2.2.3): it selects nothing, and is
			// returned in the answer, which is Response's.
			continue
		}

		vr := keyVR(elem, t)
		text := attrmatch.ElementText(elem, vr, encodings)
		if emptyValueVRs[vr] && text == `""` {
			id.keys = append(id.keys, key{tag: t, vr: vr, empty: true})
			continue
		}
		if values := keyValues(vr, text); len(values) > 0 {
			id.keys = append(id.keys, key{tag: t, vr: vr, values: values})
		}
	}
	return id
}

// keyValues splits a key into the values a candidate is matched against.
//
// A UI list stays whole: list of UID matching (C.2.2.2.2) is the value
// rule's, and has been since before keys of more than one value were matched.
// So does a key of LT, ST, UT or UR, in which a backslash is a character of
// the text (PS3.5 6.2). An empty value between two backslashes selects
// nothing, rather than turning the key into universal matching.
func keyValues(vr dataelem.VR, text string) []string {
	if vr == dataelem.UI || attrmatch.SingleValueVR(vr) || !strings.Contains(text, `\`) {
		return []string{text}
	}
	var values []string
	for _, value := range strings.Split(text, `\`) {
		if value = attrmatch.TrimPadding(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

// Match reports whether a candidate satisfies every key of the identifier.
func (id *Identifier) Match(candidate Attributes) bool {
	for _, k := range id.keys {
		if !k.match(candidate) {
			return false
		}
	}
	return true
}

func (k key) match(candidate Attributes) bool {
	if k.item != nil {
		// Any one item meeting every key of the query item; all of them by the
		// same item.
		for _, item := range candidate.Items(k.tag) {
			if k.item.Match(item) {
				return true
			}
		}
		return false
	}

	stored, _ := candidate.Value(k.tag)
	if k.empty {
		return attrmatch.TrimPadding(stored) == ""
	}
	for _, value := range k.values {
		if attrmatch.MatchValue(k.vr, value, stored) {
			return true
		}
	}
	return false
}

// MatchDataset reports whether a data set satisfies every key of the
// identifier. Its text is read in the Specific Character Set it declares.
func (id *Identifier) MatchDataset(candidate *dataset.Dataset) bool {
	if candidate == nil {
		return false
	}
	return id.Match(datasetAttributes(candidate, nil))
}

// Item returns the identifier of the item of the sequence key t, for a caller
// that answers with the candidate item that matched. It is nil when t is not a
// sequence key of the identifier, or is one with universal matching, which
// any item satisfies.
func (id *Identifier) Item(t tag.Tag) *Identifier {
	for _, k := range id.keys {
		if k.tag == t && k.item != nil {
			return k.item
		}
	}
	return nil
}

// MatchValue reports whether a stored value satisfies one query value of a VR,
// by the rules of PS3.4 C.2.2.2: universal, list of UID, range, wildcard and
// single value matching. Both are text, as Attributes gives it; a stored value
// of more than one value matches when any one does.
func MatchValue(vr dataelem.VR, query, stored string) bool {
	return attrmatch.MatchValue(vr, query, stored)
}

// isKey reports whether an element of a query can be a key. Specific
// Character Set says how the query is written, and a group length how long a
// group was when it was encoded; neither is something to match.
func isKey(t tag.Tag) bool {
	return t != attrmatch.TagSpecificCharacterSet && !t.IsGroupLength()
}

// isSequence reports whether an element is a sequence key.
func isSequence(elem *dataelem.DataElement) bool {
	if elem.GetVR() == dataelem.SQ {
		return true
	}
	_, ok := elem.GetValue().(*sequence.Sequence)
	return ok
}

// queryItem returns the item of a sequence key.
//
// PS3.4 C.2.2.2.6 allows zero items or one. More than one is non-conformant;
// the first is used, since refusing the query outright would answer a peer's
// minor error with no results at all.
func queryItem(elem *dataelem.DataElement) (*dataset.Dataset, bool) {
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok || seq == nil || seq.Length() == 0 {
		return nil, false
	}
	item, err := seq.Get(0)
	if err != nil {
		return nil, false
	}
	ds, ok := item.(*dataset.Dataset)
	return ds, ok
}

// keyVR is the VR a key is matched by: the element's own, and the
// dictionary's for an element that does not state a usable one, as one read
// from Implicit VR does not.
func keyVR(elem *dataelem.DataElement, t tag.Tag) dataelem.VR {
	if vr := elem.GetVR(); vr != "" && dataelem.IsValidVR(vr) {
		return vr
	}
	if vr := dataelem.VR(t.GetVR()); dataelem.IsValidVR(vr) {
		return vr
	}
	return dataelem.UN
}

// dataSet is a data set seen as Attributes, with the encodings its text is
// read in.
type dataSet struct {
	ds        *dataset.Dataset
	encodings []string
}

// datasetAttributes adapts a data set, an item of one when inherited is the
// encodings of the data set it is in.
func datasetAttributes(ds *dataset.Dataset, inherited []string) dataSet {
	return dataSet{ds: ds, encodings: attrmatch.TextEncodings(ds, inherited)}
}

// Value returns the attribute's value as text.
func (d dataSet) Value(t tag.Tag) (string, bool) {
	elem, ok := d.ds.Get(t)
	if !ok {
		return "", false
	}
	return attrmatch.ElementText(elem, keyVR(elem, t), d.encodings), true
}

// Items returns the items of a sequence that are data sets. An item that is
// not one cannot be matched against, and treating it as matching would
// return candidates that do not.
func (d dataSet) Items(t tag.Tag) []Attributes {
	seq, err := d.ds.GetSequence(t)
	if err != nil || seq == nil {
		return nil
	}
	items := make([]Attributes, 0, seq.Length())
	for i := 0; i < seq.Length(); i++ {
		item, err := seq.Get(i)
		if err != nil {
			continue
		}
		if ds, ok := item.(*dataset.Dataset); ok {
			items = append(items, datasetAttributes(ds, d.encodings))
		}
	}
	return items
}
