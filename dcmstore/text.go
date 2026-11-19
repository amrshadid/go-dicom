package dcmstore

import (
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/internal/attrmatch"
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
//
// The reading itself is internal/attrmatch's, the same that package matching
// reads a candidate data set with, so a value is the same text in the index
// as in a data set matched directly.

var tagSpecificCharacterSet = attrmatch.TagSpecificCharacterSet

// textEncodings returns the encodings in force for the text of ds: those it
// declares, or, when it declares none, inherited.
func textEncodings(ds *dataset.Dataset, inherited []string) []string {
	return attrmatch.TextEncodings(ds, inherited)
}

// elementText is the value of an element in the one form the index holds
// and matching compares: text as UTF-8, and a binary number in decimal.
func elementText(elem *dataelem.DataElement, vr dataelem.VR, encodings []string) string {
	return attrmatch.ElementText(elem, vr, encodings)
}

// declareCharacterSet adds Specific Character Set to an answer that needs
// one: ISO_IR 192 when any of its text is outside ASCII.
func declareCharacterSet(answer *dataset.Dataset) {
	attrmatch.DeclareCharacterSet(answer)
}

// indexedElement makes the element an answer returns for an indexed value.
//
// Text goes back as it is. A binary number goes back as numbers: written
// into a US as text, "512" would be sent as three bytes of a two byte
// value. A value that is not the number its VR holds is returned empty.
func indexedElement(t tag.Tag, vr dataelem.VR, value string) *dataelem.DataElement {
	if attrmatch.BinaryNumberWidth(vr) == 0 || value == "" {
		return dataelem.NewDataElement(t, vr, value)
	}
	parts := strings.Split(value, `\`)
	numbers := make([]int64, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return dataelem.NewDataElement(t, vr, "")
		}
		numbers = append(numbers, n)
	}
	if _, err := dataelem.ValueBytes(vr, numbers); err != nil {
		return dataelem.NewDataElement(t, vr, "")
	}
	return dataelem.NewDataElement(t, vr, numbers)
}
