package cli

import (
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// displayScope is what showing an element needs to know of the data set it
// is in, and of the ones that data set is an item of.
//
// A value is not always described by its own element. An implicit VR file
// states no VR, and for some attributes the dictionary has no single one:
// Pixel Data is "OB or OW" and Smallest Image Pixel Value "US or SS", and
// which it is depends on other attributes of the same data set, or of an
// enclosing one. Shown under the dictionary's words, numbers came out as
// bytes and Pixel Data as text (#272).
type displayScope struct {
	// deciding holds the attributes of this data set that settle a VR.
	deciding *dataset.Dataset
	// enclosing are those of the data sets this one is an item of, nearest
	// first.
	enclosing []*dataset.Dataset
	// encodings are the character encodings its text is in: what its
	// Specific Character Set declares, or the enclosing data set's. Text
	// shown without them is right for Latin-1 alone (#270).
	encodings []string
}

// decidingTags are the attributes dataset.ResolveVR consults. Each is read
// as an unsigned short, which is all it asks of them: LUT Descriptor is
// "US or SS" itself, and only its first value, a count, is used.
var decidingTags = map[tag.Tag]bool{
	tag.New(0x0028, 0x0100): true, // Bits Allocated
	tag.New(0x0028, 0x0103): true, // Pixel Representation
	tag.New(0x0028, 0x3002): true, // LUT Descriptor
}

// newDisplayScope is the scope of a data set's elements, inside outer.
func newDisplayScope(elements []*filereader.DataElementValue, outer *displayScope) *displayScope {
	scope := &displayScope{deciding: dataset.NewDataset()}
	for _, e := range elements {
		if decidingTags[e.Tag] {
			_ = scope.deciding.Add(dataelem.NewDataElement(e.Tag, dataelem.US, e.Value))
		}
	}
	var inherited []string
	if outer != nil {
		scope.enclosing = append([]*dataset.Dataset{outer.deciding}, outer.enclosing...)
		inherited = outer.encodings
	}
	scope.encodings = filereader.TextEncodings(elements, inherited)
	return scope
}

// vr is the one VR an element is shown under: its own, or what the rules of
// dataset.ResolveVR make of a dictionary VR that offers a choice.
//
// An element the dictionary does not know has no VR in an implicit VR file.
// A private creator is LO by definition (PS3.5 7.8.1); anything else is UN.
func (s *displayScope) vr(e *filereader.DataElementValue) string {
	if e.Items != nil {
		return e.VR
	}
	if e.VR == "" {
		if e.Tag.IsPrivateCreator() {
			return "LO"
		}
		return "UN"
	}
	if !strings.Contains(e.VR, " or ") {
		return e.VR
	}
	elem := dataelem.NewDataElement(e.Tag, dataelem.VR(e.VR), e.Value)
	return string(s.deciding.ResolveVR(e.Tag, elem, s.enclosing...))
}
