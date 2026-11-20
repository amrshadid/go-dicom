package dataelem

import (
	"fmt"
	"io"
)

// StreamedValue is the value of an element that is made as it is written:
// Length bytes that WriteValue produces into the writer's output, a part at
// a time, and that are never in memory whole.
//
// It is what redacting the pixels of an image that is not in memory comes
// to. A data set read with filereader.ReadOptions.SkipBulkData holds its
// Pixel Data as a ValueInFile, and filling a region of a 4 GB image, or
// decoding each of its frames to do so, must not read it all first. Dataset.
// RedactPixelRegions and Dataset.DecompressRedactingRegions put a
// StreamedValue in its place, and filewriter, which has the file the bytes
// are in, calls WriteValue when it reaches the element.
//
// The bytes WriteValue writes are the value as a data set holds it, whatever
// the file it came from: little endian. The writer reverses them again for
// a big endian transfer syntax. The element is written as long as Length
// says, padded to an even length like any other.
//
// The value is a recipe, not the bytes. Anything else that wants the bytes
// (ValueBytes, and so the network encoder) refuses it rather than treating
// it as empty.
type StreamedValue struct {
	// Length is how many bytes WriteValue writes, before any padding.
	Length int64

	// WriteValue writes the value to w. src is the file that values left in
	// it are read from (see ValueInFile), and littleEndian the byte order
	// that file is in; a writer with no value source passes nil. It may
	// write in parts of any size.
	WriteValue func(src io.ReaderAt, littleEndian bool, w io.Writer) error

	// Origin is for the package that made the value to recognize it again
	// and to add to what it will write. Nothing else reads it.
	Origin any
}

// String says what the value is, for logs and errors.
func (v *StreamedValue) String() string {
	return fmt.Sprintf("%d bytes made as the element is written", v.Length)
}
