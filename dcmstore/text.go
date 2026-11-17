package dcmstore

import (
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The index holds text as UTF-8, whatever character set an instance arrived
// in, and a query's text is read in the character set the query declares
// before it is compared (PS3.5 6.1.2.3).
//
// An instance reaches the store two ways. From the network it is the bytes
// the sender wrote, in the set its Specific Character Set (0008,0005) names.
// From a file, on Rebuild, the reader has already decoded it. Indexing the
// value as found gave one instance two different names depending on which way
// it came, matched a query only when it happened to be written in the same
// set, and put bytes in a C-FIND answer that nothing in the answer explained
// (#297).

var tagSpecificCharacterSet = tag.New(0x0008, 0x0005)

// utf8CharacterSet is the defined term for UTF-8 (PS3.3 C.12.1.1.2).
const utf8CharacterSet = "ISO_IR 192"

// textEncodings returns the encodings in force for the text of ds: those it
// declares, or, when it declares none, inherited, which is what the data set
// it is an item of uses. Pass nil for a data set that is not an item.
func textEncodings(ds *dataset.Dataset, inherited []string) []string {
	var declared []*filereader.DataElementValue
	if elem, ok := ds.Get(tagSpecificCharacterSet); ok {
		declared = append(declared, &filereader.DataElementValue{
			Tag:   tagSpecificCharacterSet,
			Value: []byte(elementString(elem)),
		})
	}
	return filereader.TextEncodings(declared, inherited)
}

// elementText is elementString with the value decoded to UTF-8.
//
// Only bytes are decoded: they are what a sender wrote. A Go string in an
// element is text already, set by a program, and decoding it again would
// read its UTF-8 as something else. And only the VRs a character set applies
// to: a UID or a date is in the default repertoire whatever is declared.
func elementText(elem *dataelem.DataElement, vr dataelem.VR, encodings []string) string {
	if elem == nil {
		return ""
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok || !dataelem.IsCharacterSetVR(vr) {
		return elementString(elem)
	}
	return trimPadding(filereader.DecodeText(string(vr), raw, encodings))
}

// declareCharacterSet adds Specific Character Set to an answer that needs
// one: ISO_IR 192 when any of its text is outside ASCII, since the index it
// was built from is UTF-8 (PS3.4 C.4.1.1.3.2). An answer in the default
// repertoire is left without, as it needs none.
func declareCharacterSet(answer *dataset.Dataset) {
	if !holdsNonASCII(answer) {
		return
	}
	_ = answer.Add(dataelem.NewDataElement(tagSpecificCharacterSet, dataelem.CS, utf8CharacterSet))
}

// holdsNonASCII reports whether any value in ds, or in an item of one of its
// sequences, has a byte outside ASCII.
func holdsNonASCII(ds *dataset.Dataset) bool {
	for _, elem := range ds.GetAll() {
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			for i := 0; i < seq.Length(); i++ {
				item, err := seq.Get(i)
				if err != nil {
					continue
				}
				if itemDS, ok := item.(*dataset.Dataset); ok && holdsNonASCII(itemDS) {
					return true
				}
			}
			continue
		}
		value := elementString(elem)
		for i := 0; i < len(value); i++ {
			if value[i] >= 0x80 {
				return true
			}
		}
	}
	return false
}
