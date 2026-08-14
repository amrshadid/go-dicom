package filewriter

import (
	"fmt"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
)

// elementValueBytes renders an element's value as the bytes to write.
//
// A Dataset holds whatever a caller put in it. Elements read by filereader carry
// []byte, and elements built in code carry a string — dataelem.NewDataElement
// takes an interface{}, and a string is the obvious thing to pass. Both have to
// write, or building a data set the natural way produces a file with every value
// empty.
//
// The bool is what stops that being silent: a value of any other type is refused
// here and reported by the caller, rather than becoming a zero-length element.
//
// network.EncodeDataset has the same helper for the same reason. The two are
// small enough to keep separate, and a data set that sends over the network but
// writes to disk empty is the failure that came of them disagreeing.
func elementValueBytes(elem *dataelem.DataElement) ([]byte, bool) {
	switch v := elem.GetValue().(type) {
	case nil:
		// An element with no value at all is legitimate: a type 2 attribute is
		// sent present and empty.
		return nil, true
	case []byte:
		return v, true
	case string:
		return []byte(v), true
	default:
		return nil, false
	}
}

// ElementsFromDataset converts a Dataset into elements this package can write,
// descending into sequences.
//
// Reading a file and writing it back needs this conversion, and without it every
// caller writes their own — which is how sequences get lost. A sequence holds
// child data sets rather than a byte value, so a conversion that copies Value
// and ignores Items produces a file that looks complete and has every nested
// item missing. That failure is silent: the element is present, its length is
// zero, and nothing reports it.
//
// Elements whose tag cannot be read are dropped, with a warning naming the type
// found. There is no tag to write them under, so the alternative is refusing the
// whole data set for one unreadable element.
func ElementsFromDataset(ds *dataset.Dataset) []*DataElement {
	if ds == nil {
		return nil
	}

	out := make([]*DataElement, 0, ds.Length())
	for _, elem := range ds.GetAll() {
		t, ok := elem.Tag()
		if !ok {
			config.Logger.Warn("filewriter: dropping an element with an unreadable tag",
				"type", elem.GetTag())
			continue
		}

		if seq, isSeq := elem.GetValue().(*sequence.Sequence); isSeq {
			out = append(out, &DataElement{
				Tag:   t,
				VR:    "SQ",
				Items: sequenceItems(seq),
			})
			continue
		}

		value, ok := elementValueBytes(elem)
		if !ok {
			// A value of a type this cannot render is reported rather than written
			// as empty. Silently writing nothing is what a discarded type assertion
			// used to do here, and it produced files where every element was
			// present, correctly typed, and empty.
			config.Logger.Warn("filewriter: dropping an element whose value cannot be written",
				"tag", t.String(), "vr", elem.GetVR(), "type", fmt.Sprintf("%T", elem.GetValue()))
			continue
		}

		out = append(out, &DataElement{
			Tag:    t,
			VR:     string(elem.GetVR()),
			Value:  value,
			Length: uint32(len(value)),
		})
	}
	return out
}

// sequenceItems converts a sequence's items, recursing through
// ElementsFromDataset so nesting of any depth is carried across.
func sequenceItems(seq *sequence.Sequence) []*SequenceItem {
	if seq == nil {
		return nil
	}

	items := make([]*SequenceItem, 0, seq.Length())
	for _, raw := range seq.Items() {
		child, ok := raw.(*dataset.Dataset)
		if !ok {
			// Not something this package produced; there is no way to write it
			// as an item, and guessing would corrupt the sequence.
			continue
		}
		items = append(items, &SequenceItem{Elements: ElementsFromDataset(child)})
	}
	return items
}
