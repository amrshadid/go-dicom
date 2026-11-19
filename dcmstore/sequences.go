package dcmstore

import (
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/matching"
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
	encodings := textEncodings(ds, nil)

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

			// An item that declares no character set is in its data set's
			// (PS3.3 C.12.1.1.2).
			itemEncodings := textEncodings(itemDS, encodings)
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
				values[attrTag.String()] = IndexedValue{VR: string(vr), Value: elementText(elem, vr, itemEncodings)}
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

// instanceAttributes is an indexed instance as package matching reads a
// candidate: its indexed keys, and the items of its indexed sequences. The
// matching of a sequence — any one stored item meeting every key of the query
// item, all of them by the same item — is matching's; what is this store's is
// that only the indexed attributes are there to meet them, which
// skipUnindexed has already narrowed the query to.
type instanceAttributes struct {
	inst *Instance
}

// Value returns an indexed attribute's value.
func (a instanceAttributes) Value(t tag.Tag) (string, bool) {
	v, ok := a.inst.Keys[t.String()]
	return v.Value, ok
}

// Items returns the indexed items of a sequence.
func (a instanceAttributes) Items(t tag.Tag) []matching.Attributes {
	stored := a.inst.Sequences[t.String()]
	if len(stored) == 0 {
		return nil
	}
	items := make([]matching.Attributes, len(stored))
	for i, item := range stored {
		items[i] = indexedItem(item)
	}
	return items
}

// indexedItem is one indexed item of a sequence, as matching reads it.
type indexedItem map[string]IndexedValue

// Value returns an indexed attribute of the item.
func (item indexedItem) Value(t tag.Tag) (string, bool) {
	v, ok := item[t.String()]
	return v.Value, ok
}

// Items returns nothing: no sequence inside an item is indexed.
func (item indexedItem) Items(tag.Tag) []matching.Attributes { return nil }

// isIndexedSequence reports whether a tag names a sequence this store matches on.
func isIndexedSequence(t tag.Tag) bool {
	_, ok := indexedSequences[t]
	return ok
}

// buildSequenceResponse returns the sequence to put in a C-FIND response for a
// matched instance.
//
// It carries one item, holding the indexed attributes of the first stored item
// that satisfies the query's item, itemID — the item that caused the match,
// which is the one the requestor asked about. A nil itemID, a query item of
// universal keys only, takes the first. A matched instance with no stored items
// yields an empty sequence, which is how a universal sequence key is answered
// for an instance that does not carry the sequence at all.
func buildSequenceResponse(itemID *matching.Identifier, inst *Instance, seqTag tag.Tag) *dataelem.DataElement {
	seq := sequence.New()

	storedItems := inst.Sequences[seqTag.String()]
	if len(storedItems) > 0 {
		chosen := storedItems[0]

		// Prefer the item that matched, so a response describes the step the query
		// was about rather than whichever came first.
		if itemID != nil {
			for _, storedItem := range storedItems {
				if itemID.Match(indexedItem(storedItem)) {
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
