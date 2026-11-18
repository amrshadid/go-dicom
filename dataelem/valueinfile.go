package dataelem

import "fmt"

// ValueInFile is the value of an element that was left in the file it was read
// from: Length bytes starting at Offset, counted from the first byte of the
// file, in that file's byte order.
//
// filereader.DICOMFile.GetDatasetWithValuesInFile puts one where a value was
// skipped, and filewriter copies the bytes from the file when it is given the
// file as its value source. Image data is what this is for: a program that
// changes a data set's attributes and writes it back need not hold its pixels
// in memory to do so (#331).
//
// For encapsulated Pixel Data the bytes are its items, from the first item's
// tag to the Sequence Delimitation Item, which is not included: the same bytes
// the value holds when the file is read whole.
//
// The value is a reference, not the bytes. Anything else that wants the bytes
// (ValueBytes, and so the network encoder) refuses it rather than treating it
// as empty.
type ValueInFile struct {
	Offset int64
	Length int64
}

// String says where the value is, for logs and errors.
func (v ValueInFile) String() string {
	return fmt.Sprintf("%d bytes left in the file at offset %d", v.Length, v.Offset)
}
