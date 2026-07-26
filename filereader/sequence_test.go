package filereader

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/tag"
)

// dcmBuilder assembles raw DICOM byte streams for parser tests.
type dcmBuilder struct {
	buf bytes.Buffer
}

// explicitElement writes a short-form explicit VR element.
func (b *dcmBuilder) explicitElement(group, element uint16, vr string, value []byte) *dcmBuilder {
	_ = binary.Write(&b.buf, binary.LittleEndian, group)
	_ = binary.Write(&b.buf, binary.LittleEndian, element)
	b.buf.WriteString(vr)
	_ = binary.Write(&b.buf, binary.LittleEndian, uint16(len(value)))
	b.buf.Write(value)
	return b
}

// implicitElement writes an implicit VR element (tag + 4-byte length).
func (b *dcmBuilder) implicitElement(group, element uint16, value []byte) *dcmBuilder {
	_ = binary.Write(&b.buf, binary.LittleEndian, group)
	_ = binary.Write(&b.buf, binary.LittleEndian, element)
	_ = binary.Write(&b.buf, binary.LittleEndian, uint32(len(value)))
	b.buf.Write(value)
	return b
}

// sequenceHeader writes an explicit VR SQ header with the given length.
func (b *dcmBuilder) sequenceHeader(group, element uint16, length uint32) *dcmBuilder {
	_ = binary.Write(&b.buf, binary.LittleEndian, group)
	_ = binary.Write(&b.buf, binary.LittleEndian, element)
	b.buf.WriteString("SQ")
	b.buf.Write([]byte{0x00, 0x00}) // reserved
	_ = binary.Write(&b.buf, binary.LittleEndian, length)
	return b
}

// rawTag writes a bare tag + 4-byte length (item and delimitation items).
func (b *dcmBuilder) rawTag(t tag.Tag, length uint32) *dcmBuilder {
	_ = binary.Write(&b.buf, binary.LittleEndian, t.Group())
	_ = binary.Write(&b.buf, binary.LittleEndian, t.Element())
	_ = binary.Write(&b.buf, binary.LittleEndian, length)
	return b
}

func (b *dcmBuilder) bytes() []byte { return b.buf.Bytes() }

// newReaderFor returns a DCMFileReader over raw bytes.
func newReaderFor(data []byte) *DCMFileReader {
	return NewDCMFileReader(filebase.NewFileReader(bytes.NewReader(data)))
}

// TestReadDefinedLengthSequence covers a sequence whose length is stated up
// front, containing two items each with stated lengths.
func TestReadDefinedLengthSequence(t *testing.T) {
	// Build the two items first so the enclosing lengths can be computed.
	var item1, item2 dcmBuilder
	item1.explicitElement(0x0008, 0x0100, "SH", []byte("CODE01  "))
	item2.explicitElement(0x0008, 0x0100, "SH", []byte("CODE02  "))

	// Each item on the wire is: item tag (4) + length (4) + content.
	itemsLen := uint32(8+item1.buf.Len()) + uint32(8+item2.buf.Len())

	var b dcmBuilder
	b.sequenceHeader(0x0040, 0xA730, itemsLen) // ContentSequence
	b.rawTag(tag.ItemTag, uint32(item1.buf.Len()))
	b.buf.Write(item1.bytes())
	b.rawTag(tag.ItemTag, uint32(item2.buf.Len()))
	b.buf.Write(item2.bytes())

	dfr := newReaderFor(b.bytes())
	elem, err := dfr.ReadDataElement(true)
	if err != nil {
		t.Fatalf("ReadDataElement: %v", err)
	}

	if elem.VR != "SQ" {
		t.Fatalf("VR = %q, want SQ", elem.VR)
	}
	if len(elem.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(elem.Items))
	}

	want := []string{"CODE01  ", "CODE02  "}
	for i, item := range elem.Items {
		if len(item.Elements) != 1 {
			t.Fatalf("item %d: got %d elements, want 1", i, len(item.Elements))
		}
		if got := string(item.Elements[0].Value); got != want[i] {
			t.Errorf("item %d value = %q, want %q", i, got, want[i])
		}
	}
}

// TestReadUndefinedLengthSequence covers a sequence delimited by a Sequence
// Delimitation Item, with items delimited by Item Delimitation Items. This is
// the form that previously caused the reader to stop mid-file.
func TestReadUndefinedLengthSequence(t *testing.T) {
	var b dcmBuilder
	b.sequenceHeader(0x0040, 0xA730, UndefinedLength)

	// Item 1, undefined length, closed by an item delimiter.
	b.rawTag(tag.ItemTag, UndefinedLength)
	b.explicitElement(0x0008, 0x0100, "SH", []byte("FIRST   "))
	b.rawTag(tag.ItemDelimiterTag, 0)

	// Item 2, undefined length, closed by an item delimiter.
	b.rawTag(tag.ItemTag, UndefinedLength)
	b.explicitElement(0x0008, 0x0100, "SH", []byte("SECOND  "))
	b.rawTag(tag.ItemDelimiterTag, 0)

	b.rawTag(tag.SequenceDelimiterTag, 0)

	dfr := newReaderFor(b.bytes())
	elem, err := dfr.ReadDataElement(true)
	if err != nil {
		t.Fatalf("ReadDataElement: %v", err)
	}

	if !elem.UndefinedLength {
		t.Error("UndefinedLength = false, want true")
	}
	if len(elem.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(elem.Items))
	}
	if got := string(elem.Items[0].Elements[0].Value); got != "FIRST   " {
		t.Errorf("item 0 = %q, want %q", got, "FIRST   ")
	}
	if got := string(elem.Items[1].Elements[0].Value); got != "SECOND  " {
		t.Errorf("item 1 = %q, want %q", got, "SECOND  ")
	}
}

// TestReadNestedSequences covers a sequence inside a sequence — the structure
// used by Structured Reports and multi-frame functional groups.
func TestReadNestedSequences(t *testing.T) {
	var b dcmBuilder
	b.sequenceHeader(0x0040, 0xA730, UndefinedLength) // outer ContentSequence
	b.rawTag(tag.ItemTag, UndefinedLength)

	b.explicitElement(0x0040, 0xA040, "CS", []byte("CONTAINER "))

	// Inner sequence within the outer item.
	b.sequenceHeader(0x0040, 0xA043, UndefinedLength) // ConceptNameCodeSequence
	b.rawTag(tag.ItemTag, UndefinedLength)
	b.explicitElement(0x0008, 0x0100, "SH", []byte("121071  "))
	b.explicitElement(0x0008, 0x0104, "LO", []byte("Finding "))
	b.rawTag(tag.ItemDelimiterTag, 0)
	b.rawTag(tag.SequenceDelimiterTag, 0)

	b.rawTag(tag.ItemDelimiterTag, 0)
	b.rawTag(tag.SequenceDelimiterTag, 0)

	dfr := newReaderFor(b.bytes())
	outer, err := dfr.ReadDataElement(true)
	if err != nil {
		t.Fatalf("ReadDataElement: %v", err)
	}

	if len(outer.Items) != 1 {
		t.Fatalf("outer: got %d items, want 1", len(outer.Items))
	}
	outerItem := outer.Items[0]
	if len(outerItem.Elements) != 2 {
		t.Fatalf("outer item: got %d elements, want 2", len(outerItem.Elements))
	}

	inner := outerItem.Elements[1]
	if inner.Tag != tag.New(0x0040, 0xA043) {
		t.Fatalf("inner tag = %s, want (0040,A043)", inner.Tag)
	}
	if len(inner.Items) != 1 {
		t.Fatalf("inner: got %d items, want 1", len(inner.Items))
	}
	if len(inner.Items[0].Elements) != 2 {
		t.Fatalf("inner item: got %d elements, want 2", len(inner.Items[0].Elements))
	}
	if got := string(inner.Items[0].Elements[1].Value); got != "Finding " {
		t.Errorf("inner value = %q, want %q", got, "Finding ")
	}
}

// TestReadSequenceImplicitVR verifies sequences are recognized under implicit
// VR, where the VR is recovered from the data dictionary rather than the wire.
func TestReadSequenceImplicitVR(t *testing.T) {
	var b dcmBuilder
	// (0040,A730) ContentSequence with undefined length, implicit VR.
	b.rawTag(tag.New(0x0040, 0xA730), UndefinedLength)
	b.rawTag(tag.ItemTag, UndefinedLength)
	b.implicitElement(0x0008, 0x0100, []byte("CODE99  "))
	b.rawTag(tag.ItemDelimiterTag, 0)
	b.rawTag(tag.SequenceDelimiterTag, 0)

	dfr := newReaderFor(b.bytes())
	elem, err := dfr.ReadDataElement(false)
	if err != nil {
		t.Fatalf("ReadDataElement: %v", err)
	}

	if elem.VR != "SQ" {
		t.Fatalf("VR = %q, want SQ (from the dictionary)", elem.VR)
	}
	if len(elem.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(elem.Items))
	}
	if got := string(elem.Items[0].Elements[0].Value); got != "CODE99  " {
		t.Errorf("value = %q, want %q", got, "CODE99  ")
	}
}

// TestReadEmptySequence covers a zero-length sequence, which is legal and
// common for optional attributes.
func TestReadEmptySequence(t *testing.T) {
	var b dcmBuilder
	b.sequenceHeader(0x0040, 0xA730, 0)

	dfr := newReaderFor(b.bytes())
	elem, err := dfr.ReadDataElement(true)
	if err != nil {
		t.Fatalf("ReadDataElement: %v", err)
	}
	if len(elem.Items) != 0 {
		t.Errorf("got %d items, want 0", len(elem.Items))
	}
}

// TestUndefinedLengthDoesNotAllocate is the regression test for the 4 GiB
// allocation: an element declaring length 0xFFFFFFFF must be recognized as
// delimited rather than triggering an allocation of that size.
func TestUndefinedLengthDoesNotAllocate(t *testing.T) {
	var b dcmBuilder
	// Encapsulated pixel data: OB with undefined length, one BOT item and one
	// fragment, closed by a sequence delimiter.
	_ = binary.Write(&b.buf, binary.LittleEndian, uint16(0x7FE0))
	_ = binary.Write(&b.buf, binary.LittleEndian, uint16(0x0010))
	b.buf.WriteString("OB")
	b.buf.Write([]byte{0x00, 0x00})
	_ = binary.Write(&b.buf, binary.LittleEndian, UndefinedLength)

	b.rawTag(tag.ItemTag, 0) // empty Basic Offset Table
	b.rawTag(tag.ItemTag, 4)
	b.buf.Write([]byte{0xAA, 0xBB, 0xCC, 0xDD})
	b.rawTag(tag.SequenceDelimiterTag, 0)

	dfr := newReaderFor(b.bytes())
	elem, err := dfr.ReadDataElement(true)
	if err != nil {
		t.Fatalf("ReadDataElement: %v", err)
	}

	if !elem.UndefinedLength {
		t.Error("UndefinedLength = false, want true")
	}
	// The Basic Offset Table is skipped; only the fragment payload is kept.
	if !bytes.Equal(elem.Value, []byte{0xAA, 0xBB, 0xCC, 0xDD}) {
		t.Errorf("value = % x, want AA BB CC DD", elem.Value)
	}
}

// TestOversizedLengthRejected verifies that an element claiming far more bytes
// than the stream holds is rejected instead of allocating for the claim.
func TestOversizedLengthRejected(t *testing.T) {
	var b dcmBuilder
	// OB with a stated length of 2 GiB in a stream only a few bytes long.
	_ = binary.Write(&b.buf, binary.LittleEndian, uint16(0x7FE0))
	_ = binary.Write(&b.buf, binary.LittleEndian, uint16(0x0010))
	b.buf.WriteString("OB")
	b.buf.Write([]byte{0x00, 0x00})
	_ = binary.Write(&b.buf, binary.LittleEndian, uint32(2<<30))
	b.buf.Write([]byte{0x01, 0x02})

	dfr := newReaderFor(b.bytes())
	if _, err := dfr.ReadDataElement(true); err == nil {
		t.Fatal("expected an error for a length exceeding the stream, got nil")
	}
}

// TestSequenceDepthLimit verifies that pathologically nested sequences are
// rejected rather than driving unbounded recursion.
func TestSequenceDepthLimit(t *testing.T) {
	var b dcmBuilder
	for i := 0; i <= MaxSequenceDepth+1; i++ {
		b.sequenceHeader(0x0040, 0xA730, UndefinedLength)
		b.rawTag(tag.ItemTag, UndefinedLength)
	}

	dfr := newReaderFor(b.bytes())
	if _, err := dfr.ReadDataElement(true); err == nil {
		t.Fatal("expected an error once the nesting limit is exceeded, got nil")
	}
}

// TestGetDatasetMaterializesSequences verifies the parsed tree converts into a
// Dataset whose sequence elements hold child Datasets.
func TestGetDatasetMaterializesSequences(t *testing.T) {
	seqTag := tag.New(0x0040, 0xA730)

	df := &DICOMFile{
		DataElements: []*DataElementValue{
			{Tag: tag.New(0x0010, 0x0010), VR: "PN", Value: []byte("Smith^John")},
			{
				Tag: seqTag,
				VR:  "SQ",
				Items: []*SequenceItemValue{
					{Elements: []*DataElementValue{
						{Tag: tag.New(0x0008, 0x0100), VR: "SH", Value: []byte("CODE01  ")},
					}},
					{Elements: []*DataElementValue{
						{Tag: tag.New(0x0008, 0x0100), VR: "SH", Value: []byte("CODE02  ")},
					}},
				},
			},
		},
	}

	ds := df.GetDataset()

	if ds.Length() != 2 {
		t.Fatalf("dataset has %d elements, want 2", ds.Length())
	}
	if !ds.HasSequence(seqTag) {
		t.Fatal("sequence element missing from the dataset")
	}

	seq, err := ds.GetSequence(seqTag)
	if err != nil {
		t.Fatalf("GetSequence: %v", err)
	}
	if seq.Length() != 2 {
		t.Fatalf("sequence has %d items, want 2", seq.Length())
	}

	// Each item must be a child Dataset carrying the nested element.
	want := []string{"CODE01  ", "CODE02  "}
	for i := 0; i < seq.Length(); i++ {
		item, err := seq.Get(i)
		if err != nil {
			t.Fatalf("item %d: %v", i, err)
		}

		childDS, ok := item.(*dataset.Dataset)
		if !ok {
			t.Fatalf("item %d is %T, want *dataset.Dataset", i, item)
		}

		elem, ok := childDS.Get(tag.New(0x0008, 0x0100))
		if !ok {
			t.Fatalf("item %d: nested element missing", i)
		}
		if got := string(elem.GetValue().([]byte)); got != want[i] {
			t.Errorf("item %d value = %q, want %q", i, got, want[i])
		}

		// AddSequence wires the parent pointer so nested datasets can walk up.
		if childDS.Parent() == nil {
			t.Errorf("item %d has no parent dataset", i)
		}
	}
}

// TestPositionTrackingIsAccurate verifies the reader's byte counter matches the
// actual stream offset.
//
// The counter is used to bound declared element lengths against the bytes
// remaining, so drift makes that guard reject valid files or admit oversized
// ones. Two paths in the meta header previously added the tag length twice —
// ReadTag had already accounted for it — which put the counter 4 bytes ahead
// per meta element. Nothing noticed until the counter was used for a decision.
func TestPositionTrackingIsAccurate(t *testing.T) {
	var b dcmBuilder
	b.explicitElement(0x0008, 0x0060, "CS", []byte("CT"))
	b.explicitElement(0x0010, 0x0010, "PN", []byte("Doe^John"))
	b.explicitElement(0x0010, 0x0020, "LO", []byte("PID-1234"))
	raw := b.bytes()

	dfr := newReaderFor(raw)

	var consumed int64
	for i := 0; i < 3; i++ {
		elem, err := dfr.ReadDataElement(true)
		if err != nil {
			t.Fatalf("element %d: %v", i, err)
		}
		// 8-byte short-form explicit VR header plus the value.
		consumed += 8 + int64(elem.Length)

		if got := dfr.GetPosition(); got != consumed {
			t.Errorf("after element %d: position = %d, want %d", i, got, consumed)
		}
	}

	if consumed != int64(len(raw)) {
		t.Errorf("consumed %d bytes, but the stream holds %d", consumed, len(raw))
	}
}

// TestOversizedLengthRejectedWithoutLargeAllocation verifies that an element
// declaring more bytes than the stream holds is rejected on the strength of the
// declared length alone, at any size.
//
// An earlier version only verified lengths above 16 MiB, so a 200-byte file
// claiming a 15 MiB element still allocated 15 MiB before discovering the
// stream was short — negligible once, ruinous in a loop.
func TestOversizedLengthRejectedWithoutLargeAllocation(t *testing.T) {
	sizes := []uint32{1 << 10, 1 << 20, 15 << 20, 1 << 30}

	for _, declared := range sizes {
		var b dcmBuilder
		// An OB element claiming `declared` bytes, with only 2 present.
		_ = binary.Write(&b.buf, binary.LittleEndian, uint16(0x7FE0))
		_ = binary.Write(&b.buf, binary.LittleEndian, uint16(0x0010))
		b.buf.WriteString("OB")
		b.buf.Write([]byte{0x00, 0x00})
		_ = binary.Write(&b.buf, binary.LittleEndian, declared)
		b.buf.Write([]byte{0x01, 0x02})

		dfr := newReaderFor(b.bytes())
		if _, err := dfr.ReadDataElement(true); err == nil {
			t.Errorf("declared length %d in a %d byte stream was accepted",
				declared, b.buf.Len())
		}
	}
}
