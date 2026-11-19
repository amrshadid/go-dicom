package matching

import (
	"errors"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/internal/attrmatch"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Response builds the answer to a query from a candidate that matched it: the
// keys the query carries, each with the candidate's value, and nothing else
// (PS3.4 C.4.1.1.3.2, K.6.1.2.2).
//
// A key the candidate does not have is returned with no value, which is how
// an SCP says it holds none for it. A sequence key with an item is
// answered with the candidate's items that match that item, each holding the
// keys the item asks for; a sequence key with no item, with the candidate's
// sequence whole. A sequence the candidate does not have is returned with no
// items.
//
// Text is returned as UTF-8, and Specific Character Set is ISO_IR 192 when
// any of it is outside ASCII, absent when none is: a query's Specific
// Character Set says how the query is written and is not returned.
func Response(query, candidate *dataset.Dataset, opts Options) (*dataset.Dataset, error) {
	if query == nil {
		return nil, errors.New("matching: the query is nil")
	}
	if candidate == nil {
		return nil, errors.New("matching: the candidate is nil")
	}
	answer, err := respond(query, candidate, 0, nil, attrmatch.TextEncodings(candidate, nil), opts)
	if err != nil {
		return nil, err
	}
	attrmatch.DeclareCharacterSet(answer)
	return answer, nil
}

// respond answers one level: the query or a sequence key's item, against the
// candidate or one of its items. queryInherited and encodings are the
// encodings of the data sets they are in.
func respond(query, candidate *dataset.Dataset, parent tag.Tag, queryInherited, encodings []string,
	opts Options) (*dataset.Dataset, error) {

	queryEncodings := attrmatch.TextEncodings(query, queryInherited)
	answer := dataset.NewDataset()
	for _, elem := range query.GetAll() {
		t, ok := elem.Tag()
		if !ok || !isKey(t) {
			continue
		}
		if opts.Skip != nil && opts.Skip(parent, elem) {
			continue
		}

		var out *dataelem.DataElement
		var err error
		switch stored, held := candidate.Get(t); {
		case isSequence(elem):
			out, err = respondSequence(elem, t, candidate, queryEncodings, encodings, opts)
		case held:
			out, err = copyElement(stored, t, encodings)
		default:
			out = dataelem.NewDataElement(t, keyVR(elem, t), "")
		}
		if err != nil {
			return nil, err
		}
		if err := answer.Add(out); err != nil {
			return nil, err
		}
	}
	return answer, nil
}

// respondSequence answers a sequence key.
func respondSequence(queryElem *dataelem.DataElement, t tag.Tag, candidate *dataset.Dataset,
	queryEncodings, encodings []string, opts Options) (*dataelem.DataElement, error) {

	answered := sequence.New()
	stored, err := candidate.GetSequence(t)
	if err != nil || stored == nil {
		return dataelem.NewDataElement(t, dataelem.SQ, answered), nil
	}

	item, hasItem := queryItem(queryElem)
	var itemID *Identifier
	if hasItem {
		itemID = compile(item, t, queryEncodings, opts)
	}

	for i := 0; i < stored.Length(); i++ {
		raw, err := stored.Get(i)
		if err != nil {
			continue
		}
		storedItem, ok := raw.(*dataset.Dataset)
		if !ok {
			continue
		}
		itemEncodings := attrmatch.TextEncodings(storedItem, encodings)

		var out *dataset.Dataset
		if !hasItem {
			out, err = copyDataset(storedItem, itemEncodings)
		} else {
			if !itemID.Match(dataSet{ds: storedItem, encodings: itemEncodings}) {
				continue
			}
			out, err = respond(item, storedItem, t, queryEncodings, itemEncodings, opts)
		}
		if err != nil {
			return nil, err
		}
		if err := answered.Append(out); err != nil {
			return nil, err
		}
	}
	return dataelem.NewDataElement(t, dataelem.SQ, answered), nil
}

// copyElement returns a candidate's element for an answer: text decoded to
// UTF-8, since the answer declares its own character set, and any other
// value as it is held.
func copyElement(elem *dataelem.DataElement, t tag.Tag, encodings []string) (*dataelem.DataElement, error) {
	vr := keyVR(elem, t)
	switch value := elem.GetValue().(type) {
	case *sequence.Sequence:
		copied := sequence.New()
		for i := 0; i < value.Length(); i++ {
			raw, err := value.Get(i)
			if err != nil {
				continue
			}
			item, ok := raw.(*dataset.Dataset)
			if !ok {
				continue
			}
			out, err := copyDataset(item, attrmatch.TextEncodings(item, encodings))
			if err != nil {
				return nil, err
			}
			if err := copied.Append(out); err != nil {
				return nil, err
			}
		}
		return dataelem.NewDataElement(t, dataelem.SQ, copied), nil
	case []byte:
		if dataelem.IsCharacterSetVR(vr) {
			return dataelem.NewDataElement(t, vr, attrmatch.ElementText(elem, vr, encodings)), nil
		}
		return dataelem.NewDataElement(t, vr, append([]byte(nil), value...)), nil
	default:
		return dataelem.NewDataElement(t, vr, value), nil
	}
}

// copyDataset copies an item whole, for a sequence key with no item. Its own
// Specific Character Set is left out: its text is UTF-8 now, which the
// answer declares.
func copyDataset(ds *dataset.Dataset, encodings []string) (*dataset.Dataset, error) {
	out := dataset.NewDataset()
	for _, elem := range ds.GetAll() {
		t, ok := elem.Tag()
		if !ok || t == attrmatch.TagSpecificCharacterSet {
			continue
		}
		copied, err := copyElement(elem, t, encodings)
		if err != nil {
			return nil, err
		}
		if err := out.Add(copied); err != nil {
			return nil, err
		}
	}
	return out, nil
}
