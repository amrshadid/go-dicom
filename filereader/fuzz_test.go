package filereader_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// FuzzReadDICOMFile exercises the file parser on arbitrary bytes.
//
// The parser consumes files from untrusted sources, and two allocation defects
// were previously found in it by hand-reading code. It may reject anything;
// what it must never do is panic, hang, or allocate without bound.
//
//	go test ./filereader/ -run=Fuzz -fuzz=FuzzReadDICOMFile -fuzztime=60s
func FuzzReadDICOMFile(f *testing.F) {
	// A minimal well-formed file, so the fuzzer starts from valid structure
	// rather than having to discover the preamble and meta header.
	f.Add(buildMinimalFile())

	// A file containing a defined-length sequence.
	f.Add(buildFileWithSequence(false))
	// The same with undefined lengths, which are delimiter-terminated.
	f.Add(buildFileWithSequence(true))

	// Degenerate inputs.
	f.Add([]byte{})
	f.Add(make([]byte, 132))                            // preamble with no DICM
	f.Add(append(make([]byte, 128), []byte("DICM")...)) // DICM and nothing else

	f.Fuzz(func(t *testing.T, data []byte) {
		reader := filebase.NewFileReader(bytes.NewReader(data))

		df, err := filereader.ReadDICOMFile(reader)
		if err != nil {
			return
		}
		if df == nil {
			t.Fatal("ReadDICOMFile returned nil file with nil error")
		}

		// Converting to a Dataset walks the whole parsed tree, including any
		// nested sequences, so it must tolerate whatever the parser produced.
		ds := df.GetDataset()
		if ds == nil {
			t.Fatal("GetDataset returned nil")
		}
		_ = ds.Length()
		_ = ds.Tags()
	})
}

// buildMinimalFile produces a valid DICOM Part 10 file: preamble, DICM, a
// group-0002 meta header, and one data element.
func buildMinimalFile() []byte {
	var buf bytes.Buffer
	buf.Write(make([]byte, 128))
	buf.WriteString("DICM")

	meta := metaHeader("1.2.840.10008.1.2.1")
	buf.Write(meta)

	// (0010,0010) PN "Doe^John"
	writeExplicit(&buf, 0x0010, 0x0010, "PN", []byte("Doe^John"))
	return buf.Bytes()
}

// buildFileWithSequence produces a file whose data set contains a sequence,
// either with explicit lengths or delimiter-terminated.
func buildFileWithSequence(undefined bool) []byte {
	var buf bytes.Buffer
	buf.Write(make([]byte, 128))
	buf.WriteString("DICM")
	buf.Write(metaHeader("1.2.840.10008.1.2.1"))

	var item bytes.Buffer
	writeExplicit(&item, 0x0008, 0x0100, "SH", []byte("CODE01  "))

	// (0040,A730) ContentSequence
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0040))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xA730))
	buf.WriteString("SQ")
	buf.Write([]byte{0x00, 0x00})

	if undefined {
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0xFFFFFFFF))
		writeRawTag(&buf, tag.ItemTag, 0xFFFFFFFF)
		buf.Write(item.Bytes())
		writeRawTag(&buf, tag.ItemDelimiterTag, 0)
		writeRawTag(&buf, tag.SequenceDelimiterTag, 0)
	} else {
		_ = binary.Write(&buf, binary.LittleEndian, uint32(8+item.Len()))
		writeRawTag(&buf, tag.ItemTag, uint32(item.Len()))
		buf.Write(item.Bytes())
	}

	return buf.Bytes()
}

// metaHeader builds a group-0002 file meta header declaring a transfer syntax.
func metaHeader(transferSyntax string) []byte {
	var elems bytes.Buffer
	writeExplicit(&elems, 0x0002, 0x0002, "UI", padUID("1.2.840.10008.5.1.4.1.1.2"))
	writeExplicit(&elems, 0x0002, 0x0003, "UI", padUID("1.2.3.4.5"))
	writeExplicit(&elems, 0x0002, 0x0010, "UI", padUID(transferSyntax))

	var out bytes.Buffer
	// (0002,0000) UL group length
	_ = binary.Write(&out, binary.LittleEndian, uint16(0x0002))
	_ = binary.Write(&out, binary.LittleEndian, uint16(0x0000))
	out.WriteString("UL")
	_ = binary.Write(&out, binary.LittleEndian, uint16(4))
	_ = binary.Write(&out, binary.LittleEndian, uint32(elems.Len()))
	out.Write(elems.Bytes())
	return out.Bytes()
}

// writeExplicit writes one short-form explicit VR element.
func writeExplicit(buf *bytes.Buffer, group, element uint16, vr string, value []byte) {
	_ = binary.Write(buf, binary.LittleEndian, group)
	_ = binary.Write(buf, binary.LittleEndian, element)
	buf.WriteString(vr)
	_ = binary.Write(buf, binary.LittleEndian, uint16(len(value)))
	buf.Write(value)
}

// writeRawTag writes a bare tag and 4-byte length, the form used by item and
// delimitation items.
func writeRawTag(buf *bytes.Buffer, t tag.Tag, length uint32) {
	_ = binary.Write(buf, binary.LittleEndian, t.Group())
	_ = binary.Write(buf, binary.LittleEndian, t.Element())
	_ = binary.Write(buf, binary.LittleEndian, length)
}

// padUID pads a UID to even length with NUL, as DICOM requires.
func padUID(s string) []byte {
	b := []byte(s)
	if len(b)%2 != 0 {
		b = append(b, 0x00)
	}
	return b
}
