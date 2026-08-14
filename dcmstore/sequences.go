package dcmstore

import (
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Sequence matching, from PS3.4 C.2.2.2.6.
//
// A query may carry a sequence whose single item holds attributes to match
// against the items of the stored sequence. The stored sequence matches if **any**
// of its items satisfies **all** of the query item's attributes — so a worklist
// query for one scheduled station matches a step scheduled there, whichever
// position it occupies.
//
// # Which sequences are indexed
//
// Not all of them, and this is the design constraint that shapes the rest. The
// index exists so that answering a query does not mean reading the archive; an
// index holding every nested attribute of every instance would put the whole data
// set back in memory, which is what writing the files was meant to avoid.
//
// So the sequences here are the ones the standard defines as matching keys:
// Scheduled Procedure Step Sequence, which every Modality Worklist query filters
// on (PS3.4 K.6.1.2.2), and Request Attributes Sequence, which carries the
// scheduled step a stored instance belongs to.
//
// A sequence outside this set is an unsupported optional key, handled as any other
// is: returned with a zero-length value rather than matched on.

// indexedSequences names the sequences whose items are indexed, and the attributes
// indexed within each.
//
// The VR is the fallback for an element that does not state one, as with the
// flat keys. Written out rather than derived from the dictionary so that adding a
// key is a deliberate act with a memory cost someone has considered.
var indexedSequences = map[tag.Tag]map[tag.Tag]dataelem.VR{
	// (0040,0100) Scheduled Procedure Step Sequence — the Modality Worklist
	// matching keys from PS3.4 K.6.1.2.2.
	tag.New(0x0040, 0x0100): {
		tag.New(0x0040, 0x0001): dataelem.AE, // ScheduledStationAETitle
		tag.New(0x0040, 0x0002): dataelem.DA, // ScheduledProcedureStepStartDate
		tag.New(0x0040, 0x0003): dataelem.TM, // ScheduledProcedureStepStartTime
		tag.New(0x0008, 0x0060): dataelem.CS, // Modality
		tag.New(0x0040, 0x0006): dataelem.PN, // ScheduledPerformingPhysicianName
		tag.New(0x0040, 0x0007): dataelem.LO, // ScheduledProcedureStepDescription
		tag.New(0x0040, 0x0009): dataelem.SH, // ScheduledProcedureStepID
		tag.New(0x0040, 0x0010): dataelem.SH, // ScheduledStationName
		tag.New(0x0040, 0x0011): dataelem.SH, // ScheduledProcedureStepLocation
	},

	// (0040,0275) Request Attributes Sequence — which scheduled step a stored
	// instance belongs to.
	tag.New(0x0040, 0x0275): {
		tag.New(0x0040, 0x1001): dataelem.SH, // RequestedProcedureID
		tag.New(0x0040, 0x0009): dataelem.SH, // ScheduledProcedureStepID
		tag.New(0x0040, 0x0007): dataelem.LO, // ScheduledProcedureStepDescription
		tag.New(0x0032, 0x1060): dataelem.LO, // RequestedProcedureDescription
	},
}

// indexSequences pulls the indexed sequences out of a data set, one map of
// attributes per item.
func indexSequences(ds *dataset.Dataset) map[string][]map[string]IndexedValue {
	var indexed map[string][]map[string]IndexedValue

	for seqTag, attrs := range indexedSequences {
		seq, err := ds.GetSequence(seqTag)
		if err != nil || seq == nil {
			continue
		}

		items := make([]map[string]IndexedValue, 0, seq.Length())
		for i := 0; i < seq.Length(); i++ {
			item, err := seq.Get(i)
			if err != nil {
				continue
			}
			itemDS, ok := item.(*dataset.Dataset)
			if !ok {
				// An item that is not a data set cannot be matched against. Skipping
				// it is right: the alternative is treating it as matching, which
				// would return instances that do not.
				continue
			}

			values := make(map[string]IndexedValue, len(attrs))
			for attrTag, fallback := range attrs {
				elem, ok := itemDS.Get(attrTag)
				if !ok {
					continue
				}
				vr := elem.GetVR()
				if vr == "" || !dataelem.IsValidVR(vr) {
					vr = fallback
				}
				values[attrTag.String()] = IndexedValue{VR: string(vr), Value: elementString(elem)}
			}

			// An item with none of the indexed attributes is still an item: a query
			// carrying only a universal sequence key matches an instance that has
			// the sequence at all.
			items = append(items, values)
		}

		if len(items) == 0 {
			continue
		}
		if indexed == nil {
			indexed = make(map[string][]map[string]IndexedValue, len(indexedSequences))
		}
		indexed[seqTag.String()] = items
	}

	return indexed
}

// matchSequence reports whether a stored instance satisfies a query sequence.
//
// The query element must have VR SQ. Its item — the standard permits at most one —
// holds the attributes to match. An empty or absent item is universal matching:
// it selects everything and asks for the sequence to be returned.
func matchSequence(queryElem *dataelem.DataElement, inst *Instance, seqTag tag.Tag) bool {
	queryItem, ok := sequenceQueryItem(queryElem)
	if !ok {
		// No item, or an item that is not a data set: universal matching, which
		// matches an instance whether or not it carries the sequence. Returning the
		// sequence is buildResponse's job.
		return true
	}

	criteria := sequenceCriteria(queryItem, seqTag)
	if len(criteria) == 0 {
		// An item holding only zero-length values is universal matching too.
		return true
	}

	storedItems := inst.Sequences[seqTag.String()]
	if len(storedItems) == 0 {
		// The query asks for values in a sequence the instance does not have, so
		// nothing in it can match.
		return false
	}

	// Any one item satisfying every criterion is a match. All criteria must be met
	// by the *same* item: a step scheduled at station A on Monday and another at
	// station B on Tuesday must not match a query for station A on Tuesday.
	for _, storedItem := range storedItems {
		if itemSatisfies(storedItem, criteria) {
			return true
		}
	}
	return false
}

// sequenceCriterion is one attribute to match inside a sequence item.
type sequenceCriterion struct {
	tag   tag.Tag
	vr    dataelem.VR
	value string
}

// sequenceCriteria reads the match criteria out of a query sequence item.
//
// An attribute the index does not hold for this sequence is skipped, for the same
// reason an unindexed flat key is: matching on it would return nothing, which
// reads as an empty archive rather than as an unsupported query.
func sequenceCriteria(queryItem *dataset.Dataset, seqTag tag.Tag) []sequenceCriterion {
	indexedAttrs := indexedSequences[seqTag]

	var criteria []sequenceCriterion
	for _, elem := range queryItem.GetAll() {
		t, ok := elem.Tag()
		if !ok {
			continue
		}

		value := trimPadding(elementString(elem))
		if value == "" {
			// Universal matching on this attribute: it selects nothing.
			continue
		}

		fallback, indexed := indexedAttrs[t]
		if !indexed {
			storeLogger.Debug("dcmstore: ignoring %s inside %s in a query; it is not indexed",
				t.String(), seqTag.String())
			continue
		}

		vr := elem.GetVR()
		if vr == "" || !dataelem.IsValidVR(vr) {
			vr = fallback
		}

		criteria = append(criteria, sequenceCriterion{tag: t, vr: vr, value: value})
	}

	return criteria
}

// itemSatisfies reports whether one stored item meets every criterion.
func itemSatisfies(storedItem map[string]IndexedValue, criteria []sequenceCriterion) bool {
	for _, c := range criteria {
		if !matchValue(c.vr, c.value, storedItem[c.tag.String()].Value) {
			return false
		}
	}
	return true
}

// sequenceQueryItem returns the single item of a query sequence.
//
// PS3.4 C.2.2.2.6 allows zero or one item. More than one is non-conformant; the
// first is used and the rest reported, since refusing the query outright would
// answer a peer's minor error with no results at all.
func sequenceQueryItem(elem *dataelem.DataElement) (*dataset.Dataset, bool) {
	if elem == nil {
		return nil, false
	}

	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok || seq == nil || seq.Length() == 0 {
		return nil, false
	}

	if seq.Length() > 1 {
		if t, ok := elem.Tag(); ok {
			storeLogger.Warn("dcmstore: the query sequence %s carries %d items and "+
				"PS3.4 C.2.2.2.6 allows one; matching on the first",
				t.String(), seq.Length())
		}
	}

	item, err := seq.Get(0)
	if err != nil {
		return nil, false
	}
	itemDS, ok := item.(*dataset.Dataset)
	if !ok {
		return nil, false
	}
	return itemDS, true
}

// isIndexedSequence reports whether a tag names a sequence this store matches on.
func isIndexedSequence(t tag.Tag) bool {
	_, ok := indexedSequences[t]
	return ok
}

// buildSequenceResponse returns the sequence to put in a C-FIND response for a
// matched instance.
//
// It carries one item, holding the indexed attributes of the first stored item
// that satisfies the query — the item that caused the match, which is the one the
// requestor asked about. A matched instance with no stored items yields an empty
// sequence, which is how a universal sequence key is answered for an instance that
// does not carry the sequence at all.
func buildSequenceResponse(queryElem *dataelem.DataElement, inst *Instance,
	seqTag tag.Tag) *dataelem.DataElement {

	seq := sequence.New()

	storedItems := inst.Sequences[seqTag.String()]
	if len(storedItems) > 0 {
		chosen := storedItems[0]

		// Prefer the item that matched, so a response describes the step the query
		// was about rather than whichever came first.
		if queryItem, ok := sequenceQueryItem(queryElem); ok {
			criteria := sequenceCriteria(queryItem, seqTag)
			for _, storedItem := range storedItems {
				if itemSatisfies(storedItem, criteria) {
					chosen = storedItem
					break
				}
			}
		}

		itemDS := dataset.NewDataset()
		for tagString, value := range chosen {
			t, ok := parseTagString(tagString)
			if !ok {
				continue
			}
			_ = itemDS.Add(dataelem.NewDataElement(t, dataelem.VR(value.VR), value.Value))
		}
		_ = seq.Append(itemDS)
	}

	return dataelem.NewDataElement(seqTag, dataelem.SQ, seq)
}

// parseTagString turns the "(gggg,eeee)" form a tag stringifies to back into a
// tag.
//
// The index is JSON, and a map key has to be a string, so the tags in it are
// stored in their printed form. Reading them back needs this.
func parseTagString(s string) (tag.Tag, bool) {
	// Expect exactly "(gggg,eeee)".
	if len(s) != 11 || s[0] != '(' || s[5] != ',' || s[10] != ')' {
		return 0, false
	}

	group, ok := parseHex4(s[1:5])
	if !ok {
		return 0, false
	}
	element, ok := parseHex4(s[6:10])
	if !ok {
		return 0, false
	}
	return tag.New(group, element), true
}

// parseHex4 parses exactly four hexadecimal digits.
func parseHex4(s string) (uint16, bool) {
	if len(s) != 4 {
		return 0, false
	}

	var out uint16
	for i := 0; i < 4; i++ {
		var digit uint16
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digit = uint16(c - '0')
		case c >= 'a' && c <= 'f':
			digit = uint16(c-'a') + 10
		case c >= 'A' && c <= 'F':
			digit = uint16(c-'A') + 10
		default:
			return 0, false
		}
		out = out<<4 | digit
	}
	return out, true
}
