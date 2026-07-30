package filewriter_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// Encapsulated pixel data is a sequence of fragments, not a value with a byte
// count. PS3.5 A.4 requires undefined length and a closing sequence delimiter.
//
// This wrote an explicit length instead. The fragments were correct, so the file
// held the right bytes behind a length field that made it unreadable to a strict
// parser: dcmtk refused 34 of the 69 files in pydicom's corpus after a round
// trip through this writer, every one of them compressed.
//
//	Found explicit length Pixel Data in top level dataset with transfer syntax
//	RLE Lossless: Only undefined length permitted
//
// pydicom read them all, which is why comparing against pydicom alone never
// showed it.

// encapsulatedFragments builds a value holding one fragment, the way a reader
// hands encapsulated pixel data back.
func encapsulatedFragments(payload []byte) []byte {
	var buf bytes.Buffer
	// Basic offset table, empty.
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xE000))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	// One fragment.
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xE000))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)
	return buf.Bytes()
}

// writeWithSyntax writes one pixel data element under the given transfer syntax
// and returns the file bytes.
func writeWithSyntax(t *testing.T, syntax string, value []byte) []byte {
	t.Helper()

	out := &growBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4",
		MediaStorageSOPInstanceUID: "1.2.826.0.1.3680043.10.511.3.1",
		TransferSyntaxUID:          syntax,
	})
	if err := w.AddDataElement(&filewriter.DataElement{
		Tag:    tag.New(0x7FE0, 0x0010),
		VR:     "OB",
		Value:  value,
		Length: uint32(len(value)),
	}); err != nil {
		t.Fatalf("AddDataElement: %v", err)
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return out.Bytes()
}

// findPixelData returns the offset just past the (7FE0,0010) tag.
func findPixelData(t *testing.T, file []byte) int {
	t.Helper()
	want := []byte{0xE0, 0x7F, 0x10, 0x00}
	i := bytes.Index(file, want)
	if i < 0 {
		t.Fatal("the written file has no pixel data element")
	}
	return i + 4
}

// TestEncapsulatedPixelDataUsesUndefinedLength covers the compressed case.
func TestEncapsulatedPixelDataUsesUndefinedLength(t *testing.T) {
	for _, syntax := range []string{
		"1.2.840.10008.1.2.5",    // RLE Lossless
		"1.2.840.10008.1.2.4.50", // JPEG Baseline
		"1.2.840.10008.1.2.4.70", // JPEG Lossless
		"1.2.840.10008.1.2.4.80", // JPEG-LS
		"1.2.840.10008.1.2.4.90", // JPEG 2000
	} {
		t.Run(syntax, func(t *testing.T) {
			value := encapsulatedFragments([]byte{0xDE, 0xAD, 0xBE, 0xEF})
			file := writeWithSyntax(t, syntax, value)

			at := findPixelData(t, file)
			// Explicit VR long form: VR, two reserved bytes, then the length.
			if got := string(file[at : at+2]); got != "OB" {
				t.Fatalf("pixel data VR is %q, want OB", got)
			}
			length := binary.LittleEndian.Uint32(file[at+4:])
			if length != 0xFFFFFFFF {
				t.Errorf("pixel data length is %d, want undefined (0xFFFFFFFF); "+
					"a strict parser refuses an explicit length here", length)
			}

			// And the element has to be closed.
			tail := file[len(file)-8:]
			wantDelimiter := []byte{0xFE, 0xFF, 0xDD, 0xE0, 0x00, 0x00, 0x00, 0x00}
			if !bytes.Equal(tail, wantDelimiter) {
				t.Errorf("the file ends % X, want a sequence delimiter % X", tail, wantDelimiter)
			}
		})
	}
}

// TestNativePixelDataKeepsItsLength guards the other side: an uncompressed
// syntax must still write a byte count, since there are no fragments to delimit.
func TestNativePixelDataKeepsItsLength(t *testing.T) {
	for _, syntax := range []string{
		"1.2.840.10008.1.2",      // Implicit VR Little Endian
		"1.2.840.10008.1.2.1",    // Explicit VR Little Endian
		"1.2.840.10008.1.2.2",    // Explicit VR Big Endian
		"1.2.840.10008.1.2.1.99", // Deflated
	} {
		t.Run(syntax, func(t *testing.T) {
			value := []byte{0x01, 0x02, 0x03, 0x04}
			file := writeWithSyntax(t, syntax, value)

			if bytes.Contains(file, []byte{0xFE, 0xFF, 0xDD, 0xE0}) {
				t.Error("native pixel data was closed with a sequence delimiter")
			}
			if bytes.Contains(file, []byte{0xFF, 0xFF, 0xFF, 0xFF}) {
				t.Error("native pixel data was written with undefined length")
			}
		})
	}
}
