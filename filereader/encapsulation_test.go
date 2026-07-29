package filereader_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// encapsulatedPixelData builds a Part 10 file whose PixelData is encapsulated:
// a Basic Offset Table naming each frame, then one fragment per frame.
func encapsulatedPixelData(t *testing.T, frames [][]byte) []byte {
	t.Helper()

	// Offsets are measured from the first byte after the offset table item,
	// and each fragment costs an 8-byte item header.
	var table bytes.Buffer
	offset := uint32(0)
	for _, f := range frames {
		_ = binary.Write(&table, binary.LittleEndian, offset)
		offset += uint32(8 + len(f))
	}

	var pd bytes.Buffer
	item := func(payload []byte) {
		_ = binary.Write(&pd, binary.LittleEndian, uint16(0xFFFE))
		_ = binary.Write(&pd, binary.LittleEndian, uint16(0xE000))
		_ = binary.Write(&pd, binary.LittleEndian, uint32(len(payload)))
		pd.Write(payload)
	}
	item(table.Bytes())
	for _, f := range frames {
		item(f)
	}

	var ds bytes.Buffer
	// (7FE0,0010) OB, undefined length, then the items, then the delimiter.
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x7FE0))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x0010))
	ds.WriteString("OB")
	ds.Write([]byte{0x00, 0x00})
	_ = binary.Write(&ds, binary.LittleEndian, uint32(0xFFFFFFFF))
	ds.Write(pd.Bytes())
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xE0DD))
	_ = binary.Write(&ds, binary.LittleEndian, uint32(0))

	// RLE Lossless, so the data set is explicit VR little endian.
	return buildFile("1.2.840.10008.1.2.5", ds.Bytes(), false)
}

// TestEncapsulationSurvivesParsing verifies the reader keeps the item structure
// of encapsulated pixel data rather than concatenating the payloads.
//
// The reader used to skip the Basic Offset Table and join the fragments, on the
// reasoning that frame boundaries would be recovered later. They could not be:
// the encaps package recovers them by parsing the item structure, and there was
// none left to parse. ExtractEncapsulatedFrames failed with "failed to parse
// basic offset table" on every compressed file, and multi-frame images could
// not be split at all.
func TestEncapsulationSurvivesParsing(t *testing.T) {
	frameA := bytes.Repeat([]byte{0xA1}, 16)
	frameB := bytes.Repeat([]byte{0xB2}, 24)

	df := readFile(t, encapsulatedPixelData(t, [][]byte{frameA, frameB}))

	elem, ok := df.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("PixelData missing")
	}
	value := elem.GetValue().([]byte)

	// The value must begin with an item header, not with pixel content. This is
	// the single byte-level check that distinguishes the two behaviors.
	if len(value) < 4 || !bytes.Equal(value[:4], []byte{0xFE, 0xFF, 0x00, 0xE0}) {
		t.Fatalf("value starts with % X, want FE FF 00 E0 — the item header was stripped", value[:min(4, len(value))])
	}

	// Both frames must come back separately, at their original sizes.
	encData, err := df.GetDataset().ExtractEncapsulatedFrames()
	if err != nil {
		t.Fatalf("ExtractEncapsulatedFrames: %v", err)
	}
	if len(encData.Fragments) != 2 {
		t.Fatalf("got %d fragments, want 2", len(encData.Fragments))
	}
	if !bytes.Equal(encData.Fragments[0], frameA) {
		t.Errorf("fragment 0 = % X, want % X", encData.Fragments[0], frameA)
	}
	if !bytes.Equal(encData.Fragments[1], frameB) {
		t.Errorf("fragment 1 = % X, want % X", encData.Fragments[1], frameB)
	}

	// The offset table must survive too: without it a reader cannot tell which
	// fragment starts which frame when a frame spans several fragments.
	if len(encData.BasicOffsetTable) != 2 {
		t.Fatalf("basic offset table has %d entries, want 2", len(encData.BasicOffsetTable))
	}
	if encData.BasicOffsetTable[0] != 0 {
		t.Errorf("offset[0] = %d, want 0", encData.BasicOffsetTable[0])
	}
	if want := uint32(8 + len(frameA)); encData.BasicOffsetTable[1] != want {
		t.Errorf("offset[1] = %d, want %d", encData.BasicOffsetTable[1], want)
	}
}

// TestEncapsulationWithEmptyOffsetTable covers the common case of a single
// frame written with no offsets, which is legal and must not be mistaken for a
// missing table.
func TestEncapsulationWithEmptyOffsetTable(t *testing.T) {
	frame := bytes.Repeat([]byte{0xC3}, 12)

	var pd bytes.Buffer
	item := func(payload []byte) {
		_ = binary.Write(&pd, binary.LittleEndian, uint16(0xFFFE))
		_ = binary.Write(&pd, binary.LittleEndian, uint16(0xE000))
		_ = binary.Write(&pd, binary.LittleEndian, uint32(len(payload)))
		pd.Write(payload)
	}
	item(nil) // empty Basic Offset Table
	item(frame)

	var ds bytes.Buffer
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x7FE0))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x0010))
	ds.WriteString("OB")
	ds.Write([]byte{0x00, 0x00})
	_ = binary.Write(&ds, binary.LittleEndian, uint32(0xFFFFFFFF))
	ds.Write(pd.Bytes())
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xE0DD))
	_ = binary.Write(&ds, binary.LittleEndian, uint32(0))

	parsed := readFile(t, buildFile("1.2.840.10008.1.2.5", ds.Bytes(), false))

	encData, err := parsed.GetDataset().ExtractEncapsulatedFrames()
	if err != nil {
		t.Fatalf("ExtractEncapsulatedFrames: %v", err)
	}
	if len(encData.Fragments) != 1 {
		t.Fatalf("got %d fragments, want 1", len(encData.Fragments))
	}
	if !bytes.Equal(encData.Fragments[0], frame) {
		t.Errorf("fragment = % X, want % X", encData.Fragments[0], frame)
	}
}

// TestUncompressedPixelDataIsUnchanged guards the other side: a defined-length
// element carries no encapsulation and must not gain item headers.
func TestUncompressedPixelDataIsUnchanged(t *testing.T) {
	pixels := bytes.Repeat([]byte{0xD4}, 32)

	var ds bytes.Buffer
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x7FE0))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x0010))
	ds.WriteString("OB")
	ds.Write([]byte{0x00, 0x00})
	_ = binary.Write(&ds, binary.LittleEndian, uint32(len(pixels)))
	ds.Write(pixels)

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))

	elem, ok := df.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("PixelData missing")
	}
	if got := elem.GetValue().([]byte); !bytes.Equal(got, pixels) {
		t.Errorf("uncompressed pixel data changed: got %d bytes, want %d", len(got), len(pixels))
	}
}
