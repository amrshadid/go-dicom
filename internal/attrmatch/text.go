package attrmatch

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Matching compares text as UTF-8, whatever character set a value arrived
// in, and a query's text is read in the character set the query declares
// before it is compared (PS3.5 6.1.2.3).
//
// A data set reaches a matcher two ways. From the network it is the bytes
// the sender wrote, in the set its Specific Character Set (0008,0005) names.
// From a file the reader has already decoded it. Comparing the value as found
// gave one instance two different names depending on which way it came,
// matched a query only when it happened to be written in the same set, and
// put bytes in a C-FIND answer that nothing in the answer explained (#297).

// TagSpecificCharacterSet is (0008,0005). In a query it says how the query is
// written and is not a key; in an answer it is DeclareCharacterSet's to give.
var TagSpecificCharacterSet = tag.New(0x0008, 0x0005)

// utf8CharacterSet is the defined term for UTF-8 (PS3.3 C.12.1.1.2).
const utf8CharacterSet = "ISO_IR 192"

// ElementString renders an element's value as a string, with its padding
// removed.
func ElementString(elem *dataelem.DataElement) string {
	if elem == nil {
		return ""
	}
	// Through Bytes, the conversion the writer uses: a PersonName printed with
	// %v gave its alphabetic group alone, and was indexed as a different name
	// from the one written to the file (#420). What Bytes cannot render, a
	// value whose VR does not say how, is still printed rather than lost.
	if raw, ok := elem.Bytes(); ok {
		return TrimPadding(string(raw))
	}
	return TrimPadding(fmt.Sprintf("%v", elem.GetValue()))
}

// TextEncodings returns the encodings in force for the text of ds: those it
// declares, or, when it declares none, inherited, which is what the data set
// it is an item of uses. Pass nil for a data set that is not an item.
func TextEncodings(ds *dataset.Dataset, inherited []string) []string {
	var declared []*filereader.DataElementValue
	if elem, ok := ds.Get(TagSpecificCharacterSet); ok {
		declared = append(declared, &filereader.DataElementValue{
			Tag:   TagSpecificCharacterSet,
			Value: []byte(ElementString(elem)),
		})
	}
	return filereader.TextEncodings(declared, inherited)
}

// ElementText is the value of an element in the one form an index holds
// and matching compares: text as UTF-8, and a binary number in decimal.
//
// Only bytes are decoded: they are what a sender wrote. A Go string in an
// element is text already, set by a program, and decoding it again would
// read its UTF-8 as something else. And only the VRs a character set applies
// to: a UID or a date is in the default repertoire whatever is declared.
func ElementText(elem *dataelem.DataElement, vr dataelem.VR, encodings []string) string {
	if elem == nil {
		return ""
	}
	if width := BinaryNumberWidth(vr); width > 0 {
		return numberText(elem, vr, width)
	}
	// The []byte assertion is meant: it picks what to decode, not whether there
	// is a value. Anything else is read through ElementString, which uses
	// DataElement.Bytes (#420).
	raw, ok := elem.GetValue().([]byte)
	if !ok || !dataelem.IsCharacterSetVR(vr) {
		return ElementString(elem)
	}
	return TrimPadding(filereader.DecodeText(string(vr), raw, encodings))
}

// DeclareCharacterSet adds Specific Character Set to an answer that needs
// one: ISO_IR 192 when any of its text is outside ASCII, since the text it
// was built from is UTF-8 (PS3.4 C.4.1.1.3.2). An answer in the default
// repertoire is left without, as it needs none.
func DeclareCharacterSet(answer *dataset.Dataset) {
	if !holdsNonASCII(answer) {
		return
	}
	_ = answer.Add(dataelem.NewDataElement(TagSpecificCharacterSet, dataelem.CS, utf8CharacterSet))
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
		value := ElementString(elem)
		for i := 0; i < len(value); i++ {
			if value[i] >= 0x80 {
				return true
			}
		}
	}
	return false
}

// BinaryNumberWidth is the size in bytes of one value of a VR that holds
// whole numbers in binary, and 0 for any other VR.
func BinaryNumberWidth(vr dataelem.VR) int {
	switch vr {
	case dataelem.US, dataelem.SS:
		return 2
	case dataelem.UL, dataelem.SL:
		return 4
	}
	return 0
}

// numberText renders a binary number as decimal text, values separated by
// a backslash as text values are.
//
// The index is text, and a number reaches it three ways: as the little
// endian bytes a peer or a file holds, as a Go number a program set, and as
// decimal text from a URL. Rendered alike, Rows of 512 is "512" whichever
// way it came, and matches itself.
func numberText(elem *dataelem.DataElement, vr dataelem.VR, width int) string {
	if text, ok := elem.GetValue().(string); ok {
		return TrimPadding(text)
	}
	raw, err := dataelem.ValueBytes(vr, elem.GetValue())
	if err != nil || len(raw)%width != 0 {
		return ""
	}
	parts := make([]string, 0, len(raw)/width)
	for i := 0; i < len(raw); i += width {
		switch vr {
		case dataelem.US:
			parts = append(parts, strconv.FormatUint(uint64(binary.LittleEndian.Uint16(raw[i:])), 10))
		case dataelem.SS:
			parts = append(parts, strconv.FormatInt(int64(int16(binary.LittleEndian.Uint16(raw[i:]))), 10))
		case dataelem.UL:
			parts = append(parts, strconv.FormatUint(uint64(binary.LittleEndian.Uint32(raw[i:])), 10))
		default: // SL
			parts = append(parts, strconv.FormatInt(int64(int32(binary.LittleEndian.Uint32(raw[i:]))), 10))
		}
	}
	return strings.Join(parts, `\`)
}
