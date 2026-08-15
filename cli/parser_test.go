package cli

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// writeTestFile writes bytes to a temporary .dcm file and returns its path.
func writeTestFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.dcm")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// buildPart10 assembles a DICOM Part 10 file large enough to span more than one
// 64 KiB read, with a sequence in the middle.
func buildPart10(t *testing.T, pixelBytes int) []byte {
	t.Helper()

	var buf bytes.Buffer
	buf.Write(make([]byte, 128))
	buf.WriteString("DICM")

	// Minimal meta header declaring explicit VR little endian.
	var meta bytes.Buffer
	writeElem(&meta, 0x0002, 0x0002, "UI", []byte("1.2.840.10008.5.1.4.1.1.2\x00"))
	writeElem(&meta, 0x0002, 0x0003, "UI", []byte("1.2.3.4.5\x00"))
	writeElem(&meta, 0x0002, 0x0010, "UI", []byte("1.2.840.10008.1.2.1\x00"))

	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0002))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0000))
	buf.WriteString("UL")
	_ = binary.Write(&buf, binary.LittleEndian, uint16(4))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(meta.Len()))
	buf.Write(meta.Bytes())

	writeElem(&buf, 0x0008, 0x0060, "CS", []byte("CT"))
	writeElem(&buf, 0x0010, 0x0010, "PN", []byte("Doe^John"))

	// A sequence with one item, so nesting is exercised.
	var item bytes.Buffer
	writeElem(&item, 0x0010, 0x0020, "LO", []byte("NESTED12"))

	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0010))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x1002))
	buf.WriteString("SQ")
	buf.Write([]byte{0x00, 0x00})
	_ = binary.Write(&buf, binary.LittleEndian, uint32(8+item.Len()))
	_ = binary.Write(&buf, binary.LittleEndian, tag.ItemTag.Group())
	_ = binary.Write(&buf, binary.LittleEndian, tag.ItemTag.Element())
	_ = binary.Write(&buf, binary.LittleEndian, uint32(item.Len()))
	buf.Write(item.Bytes())

	// Pixel data large enough to push what follows past a 64 KiB boundary.
	var pixels bytes.Buffer
	_ = binary.Write(&pixels, binary.LittleEndian, uint16(0x7FE0))
	_ = binary.Write(&pixels, binary.LittleEndian, uint16(0x0010))
	pixels.WriteString("OW")
	pixels.Write([]byte{0x00, 0x00})
	_ = binary.Write(&pixels, binary.LittleEndian, uint32(pixelBytes))
	pixels.Write(bytes.Repeat([]byte{0xAB}, pixelBytes))
	buf.Write(pixels.Bytes())

	// An element after the pixel data: the old chunked parser lost everything
	// past the first 64 KiB boundary, so this is the one that used to vanish.
	writeElem(&buf, 0x0020, 0x0013, "IS", []byte("42"))

	return buf.Bytes()
}

func writeElem(buf *bytes.Buffer, group, element uint16, vr string, value []byte) {
	_ = binary.Write(buf, binary.LittleEndian, group)
	_ = binary.Write(buf, binary.LittleEndian, element)
	buf.WriteString(vr)
	_ = binary.Write(buf, binary.LittleEndian, uint16(len(value)))
	buf.Write(value)
}

// TestReadDICOMFileSpansChunkBoundaries verifies elements after a large value
// are still found.
//
// The CLI previously carried its own parser that read the file in 64 KiB chunks
// and parsed each independently. An element straddling a boundary
// desynchronized the stream, so a file larger than one chunk lost most of its
// elements and produced invented ones with impossible VRs.
func TestReadDICOMFileSpansChunkBoundaries(t *testing.T) {
	path := writeTestFile(t, buildPart10(t, 200*1024)) // well past 64 KiB

	elements, err := readDICOMFile(path)
	if err != nil {
		t.Fatalf("readDICOMFile: %v", err)
	}

	found := map[string]bool{}
	for _, e := range elements {
		found[e.Tag] = true
		// Every VR must be two uppercase letters; the old parser emitted
		// arbitrary bytes once it lost sync.
		if len(e.VR) == 2 {
			for _, c := range e.VR {
				if c < 'A' || c > 'Z' {
					t.Errorf("element %s has a non-alphabetic VR %q — parser lost sync", e.Tag, e.VR)
				}
			}
		}
	}

	for _, want := range []string{"0008,0060", "0010,0010", "7FE0,0010", "0020,0013"} {
		if !found[want] {
			t.Errorf("element %s missing; it sits past the 64 KiB boundary", want)
		}
	}
}

// TestReadDICOMFileDescendsIntoSequences verifies nested elements are returned
// with their nesting level. The old parser never descended into sequences.
func TestReadDICOMFileDescendsIntoSequences(t *testing.T) {
	path := writeTestFile(t, buildPart10(t, 1024))

	elements, err := readDICOMFile(path)
	if err != nil {
		t.Fatalf("readDICOMFile: %v", err)
	}

	var nested *DicomElement
	for i := range elements {
		if elements[i].Tag == "0010,0020" && elements[i].Depth > 0 {
			nested = &elements[i]
			break
		}
	}
	if nested == nil {
		t.Fatal("the element inside the sequence item was not returned")
	}
	if got := string(nested.Value); got != "NESTED12" {
		t.Errorf("nested value = %q, want %q", got, "NESTED12")
	}
	if nested.Depth != 1 {
		t.Errorf("nested Depth = %d, want 1", nested.Depth)
	}
}

// TestReadDICOMFileAcceptsRawDataset verifies a stream with no preamble or meta
// header is parsed as a raw data set, which is what modalities and the network
// produce.
func TestReadDICOMFileAcceptsRawDataset(t *testing.T) {
	// Implicit VR little endian, no preamble: tag then 4-byte length.
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0010))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0010))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(8))
	buf.WriteString("Doe^John")

	elements, err := readDICOMFile(writeTestFile(t, buf.Bytes()))
	if err != nil {
		t.Fatalf("readDICOMFile on a raw data set: %v", err)
	}
	if len(elements) != 1 {
		t.Fatalf("got %d elements, want 1", len(elements))
	}
	if got := string(elements[0].Value); got != "Doe^John" {
		t.Errorf("value = %q, want %q", got, "Doe^John")
	}
}

// TestReadDICOMFileEmptyFile verifies an empty file yields no elements rather
// than an error, since the CLI reports emptiness itself.
// An empty file is refused rather than read as a data set with nothing in it.
//
// This used to require the opposite: no error, zero elements. That is what let every
// file command accept any file at all — `show /etc/hosts` printed a header, a column
// heading, no rows, and exited zero, and `show /dev/null` did the same without even a
// warning. A mistyped filename is the common case and it looked like success.
func TestReadDICOMFileRefusesAnEmptyFile(t *testing.T) {
	_, err := readDICOMFile(writeTestFile(t, nil))
	if err == nil {
		t.Fatal("readDICOMFile accepted an empty file; a file with no elements in it " +
			"is not a DICOM file and saying so is more use than an empty table")
	}
	if !strings.Contains(err.Error(), "does not look like a DICOM file") {
		t.Errorf("the error does not explain what is wrong: %v", err)
	}
}
