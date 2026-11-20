package filereader

import (
	"context"
	"unicode/utf8"

	"github.com/amrshadid/go-dicom/internal/vr"
	"github.com/amrshadid/go-dicom/tag"
)

// utf8CharacterSet is the defined term for UTF-8, which every decoded data set
// declares.
const utf8CharacterSet = "ISO_IR 192"

// DecodedElement is one element of a data set after its text has been decoded
// into UTF-8, in the form every reader of the elements wants: the data set
// builder in filereader and the codec-free DICOM JSON writer in attributes.
type DecodedElement struct {
	// Source is the element as it was read. It is nil for the Specific
	// Character Set declaration DecodeItem adds, which no element was read for.
	Source *DataElementValue
	Tag    tag.Tag
	VR     string
	// Value is the bytes after decoding. A sequence's is as read, and so is a
	// value left in the file, which has none.
	Value []byte
}

// DecodedItem is the elements of one data set, or one item of a sequence, with
// the character set that applies to it and to the items below it.
type DecodedItem struct {
	// Encodings is what an item below this one inherits.
	Encodings []string
	Elements  []DecodedElement
}

// isSequence reports whether the element holds items, however many.
func (e *DataElementValue) isSequence() bool {
	return e.Items != nil || IsSequenceVR(e.VR)
}

// DecodeItem decodes the text of one data set into UTF-8 as its Specific
// Character Set says, and settles what the data set then declares.
//
// inherited is the character set in force from the enclosing data set, which an
// item may override: PS3.5 allows Specific Character Set inside a sequence item,
// and it applies to that item and anything below it. Passing nil at the top
// level means "read it from these elements".
//
// warn records a decision the caller should know about; it may be nil.
func DecodeItem(ctx context.Context, elements []*DataElementValue, inherited []string, warn func(string)) DecodedItem {
	encodings, fellBack := EncodingsFor(ctx, elements, inherited)

	// Decode first, decide afterwards. Whether this data set may declare UTF-8
	// depends on how the values came out, and the declaration has to be written
	// in tag order, before the values it describes.
	decoded := make([]DecodedElement, 0, len(elements)+1)
	guessed, allUTF8 := false, true
	for _, elem := range elements {
		if elem.isSequence() || elem.Tag == SpecificCharacterSetTag {
			decoded = append(decoded, DecodedElement{elem, elem.Tag, elem.VR, elem.Value})
			continue
		}
		value := DecodeTextValue(ctx, elem.VR, elem.Value, encodings)
		if vr.IsText(elem.VR) {
			if fellBack && !isASCII(elem.Value) {
				// Latin-1 is a guess: the file did not say, or named something
				// this build does not know, and another single-byte set is
				// possible. The caller is told rather than left to notice.
				guessed = true
			}
			if !utf8.Valid(value) {
				allUTF8 = false
			}
		}
		decoded = append(decoded, DecodedElement{elem, elem.Tag, elem.VR, value})
	}

	if guessed && warn != nil {
		warn("text with no usable Specific Character Set (0008,0005) was decoded as ISO-8859-1, " +
			"which is a guess; the default repertoire is ASCII (PS3.5 6.1.2.3)")
	}
	// Reached when a decoder returns an error and DecodeTextValue keeps the
	// bytes as they were found. The decoders here substitute U+FFFD for input
	// they cannot map, so this is insurance rather than a path a corpus file
	// takes today — and it is the difference between a data set that is wrong
	// and one that lies about being right.
	if !allUTF8 && warn != nil {
		warn("keeping the declared Specific Character Set (0008,0005): some text did not decode, " +
			"so the data set cannot claim ISO_IR 192 over it")
	}

	// A declaration is added when text was converted and the file had none.
	// Without it, writing the data set back produces UTF-8 bytes that a reader
	// decodes as the default repertoire — the same corruption, one file later.
	addDeclaration := guessed && allUTF8 && !hasSpecificCharacterSet(elements)
	declaration := DecodedElement{nil, SpecificCharacterSetTag, "CS", []byte(utf8CharacterSet)}

	out := make([]DecodedElement, 0, len(decoded)+1)
	for _, d := range decoded {
		if addDeclaration && d.Tag > SpecificCharacterSetTag {
			out = append(out, declaration)
			addDeclaration = false
		}
		if d.Source != nil && !d.Source.isSequence() && d.Tag == SpecificCharacterSetTag && allUTF8 {
			// The values below are UTF-8 now, so the attribute has to say so or
			// the data set contradicts itself — and anything writing it back out
			// would label UTF-8 bytes as something else. Only when they really
			// are UTF-8: this used to be unconditional, so a file whose text did
			// not decode was labeled ISO_IR 192 over the bytes it came with.
			d.Value = []byte(utf8CharacterSet)
		}
		out = append(out, d)
	}
	if addDeclaration {
		out = append(out, declaration)
	}
	return DecodedItem{Encodings: encodings, Elements: out}
}

// IsSequenceElement reports whether the element holds items, however many.
func IsSequenceElement(e *DataElementValue) bool { return e.isSequence() }

// ValueExtent is where the value of an element left in the file is, and how
// long: a counted value runs for the length it states. Encapsulated Pixel Data
// runs from its first item to the delimiter that closes it, or to where the
// reader stopped in a file that ends without one: the bytes a whole read puts
// in the value. For a deflated file the offset counts inflated bytes.
func ValueExtent(e *DataElementValue) (offset, length int64) {
	if !e.UndefinedLength {
		return e.Position.ValueOffset, int64(e.Length)
	}
	end := e.Position.DelimiterOffset
	if end == 0 {
		end = e.Position.End
	}
	return e.Position.ValueOffset, end - e.Position.ValueOffset
}

// isASCII reports whether every byte is in the default repertoire, which needs
// no decoding whatever the declaration says.
func isASCII(value []byte) bool {
	for _, b := range value {
		if b > 0x7F {
			return false
		}
	}
	return true
}

// hasSpecificCharacterSet reports whether (0008,0005) is present at all.
func hasSpecificCharacterSet(elements []*DataElementValue) bool {
	for _, elem := range elements {
		if elem.Tag == SpecificCharacterSetTag {
			return true
		}
	}
	return false
}
