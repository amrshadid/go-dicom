package network

import (
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// implicitElement encodes one Implicit VR Little Endian element.
func implicitElement(group, element uint16, value string) []byte {
	b := binary.LittleEndian.AppendUint16(nil, group)
	b = binary.LittleEndian.AppendUint16(b, element)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(value)))
	return append(b, value...)
}

// itemOf wraps elements in an item of defined length, or of undefined length
// closed by an Item Delimitation Item.
func itemOf(defined bool, elements ...[]byte) []byte {
	var body []byte
	for _, e := range elements {
		body = append(body, e...)
	}
	b := binary.LittleEndian.AppendUint16(nil, 0xFFFE)
	b = binary.LittleEndian.AppendUint16(b, 0xE000)
	if defined {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(body)))
		return append(b, body...)
	}
	b = binary.LittleEndian.AppendUint32(b, 0xFFFFFFFF)
	b = append(b, body...)
	return append(b, 0xFE, 0xFF, 0x0D, 0xE0, 0, 0, 0, 0)
}

// TestDecodeUNSequenceWalksAPrivateSequence proves the value of a private
// sequence held as UN is read as its items, Implicit VR Little Endian (PS3.5
// 6.2.2): items of defined and of undefined length, a nested sequence of
// undefined length inside one, and a closing Sequence Delimitation Item.
func TestDecodeUNSequenceWalksAPrivateSequence(t *testing.T) {
	nested := implicitElement(0x0009, 0x1003, "")[:4]
	nested = binary.LittleEndian.AppendUint32(nested, 0xFFFFFFFF)
	nested = append(nested, itemOf(true, implicitElement(0x0010, 0x0020, "INNER1"))...)
	nested = append(nested, 0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0)

	value := append(itemOf(true, implicitElement(0x0009, 0x0010, "ACME 1.0"), implicitElement(0x0009, 0x1001, "FIRST ")),
		itemOf(false, implicitElement(0x0009, 0x0010, "ACME 1.0"), implicitElement(0x0009, 0x1001, "SECOND"), nested)...)

	for _, closed := range []bool{false, true} {
		v := value
		if closed {
			v = append(append([]byte(nil), value...), 0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0)
		}
		seq, err := DecodeUNSequence(v)
		if err != nil {
			t.Fatalf("closed=%v: %v", closed, err)
		}
		if seq.Length() != 2 {
			t.Fatalf("closed=%v: %d items, want 2", closed, seq.Length())
		}
		for i, want := range []string{"FIRST ", "SECOND"} {
			item, _ := seq.Get(i)
			ds := item.(*dataset.Dataset)
			el, ok := ds.Get(tag.New(0x0009, 0x1001))
			if !ok {
				t.Fatalf("item %d has no (0009,1001)", i)
			}
			if got := string(el.GetValue().([]byte)); got != want {
				t.Errorf("item %d (0009,1001) = %q, want %q", i, got, want)
			}
		}
		second, _ := seq.Get(1)
		inner, err := second.(*dataset.Dataset).GetSequence(tag.New(0x0009, 0x1003))
		if err != nil || inner.Length() != 1 {
			t.Fatalf("the nested sequence: %v, %v", inner, err)
		}
	}
}

// TestDecodeUNSequenceRefusesWhatIsNotItems proves bytes that are not a
// sequence's items are refused, not read as whatever they resemble: a value
// that does not start with an item, one with bytes after its last item, and
// one shorter than an item header. An empty value is a sequence of no items.
func TestDecodeUNSequenceRefusesWhatIsNotItems(t *testing.T) {
	good := itemOf(true, implicitElement(0x0009, 0x1001, "OK"))
	for name, v := range map[string][]byte{
		"not an item":           implicitElement(0x0009, 0x1001, "PLAIN BYTES"),
		"bytes after the items": append(append([]byte(nil), good...), 1, 2, 3, 4, 5, 6, 7, 8, 9, 10),
		"shorter than a header": {0xFE, 0xFF, 0x00},
		"an item past the end":  append(itemOf(true, implicitElement(0x0009, 0x1001, "OK"))[:8], 'x'),
	} {
		if seq, err := DecodeUNSequence(v); err == nil {
			t.Errorf("%s: read as %d items, want an error", name, seq.Length())
		}
	}
	if seq, err := DecodeUNSequence(nil); err != nil || seq.Length() != 0 {
		t.Errorf("an empty value: %v, %v; want a sequence of no items", seq, err)
	}
}

// TestDecodeUNSequenceRefusesAValueCutShort proves a value cut short or
// overrunning what holds it is refused, where a received data set would be
// read as far as it goes: an item cut inside an element, an element whose
// length runs past its item, an item or nested sequence of undefined length
// with no delimiter before the end, an item header cut short, a Sequence
// Delimitation Item inside an item, and an item whose length runs past the
// value. Each is a value a caller must not take for a sequence.
func TestDecodeUNSequenceRefusesAValueCutShort(t *testing.T) {
	whole := itemOf(true, implicitElement(0x0009, 0x0010, "ACME 1.0"), implicitElement(0x0009, 0x1001, "VALUE1"))

	// An element whose length says 12 inside an item whose length leaves 6.
	overrun := itemOf(true, implicitElement(0x0009, 0x1001, "VALUE1"))
	binary.LittleEndian.PutUint32(overrun[8+4:], 12)
	overrun = append(overrun, implicitElement(0x0009, 0x1002, "XX")...)

	undefinedItem := itemOf(false, implicitElement(0x0009, 0x1001, "VALUE1"))
	undefinedItem = undefinedItem[:len(undefinedItem)-8] // its delimiter gone

	nested := binary.LittleEndian.AppendUint16(nil, 0x0009)
	nested = binary.LittleEndian.AppendUint16(nested, 0x1003)
	nested = binary.LittleEndian.AppendUint32(nested, 0xFFFFFFFF)
	nested = append(nested, itemOf(true, implicitElement(0x0010, 0x0020, "IN"))...) // no delimiter
	nestedUnclosed := itemOf(false, nested)
	nestedUnclosed = nestedUnclosed[:len(nestedUnclosed)-8]

	seqDelimInItem := itemOf(false, implicitElement(0x0009, 0x1001, "VALUE1"))
	copy(seqDelimInItem[len(seqDelimInItem)-8:], []byte{0xFE, 0xFF, 0xDD, 0xE0})

	pastValue := append([]byte(nil), whole...)
	binary.LittleEndian.PutUint32(pastValue[4:], uint32(len(whole)))

	for name, v := range map[string][]byte{
		"cut inside an element":               whole[:len(whole)-3],
		"cut inside an element header":        whole[:8+5],
		"an element past its item":            overrun,
		"an item with no delimiter":           undefinedItem,
		"a nested sequence with no delimiter": nestedUnclosed,
		"an item header cut short":            append(append([]byte(nil), whole...), 0xFE, 0xFF, 0x00, 0xE0, 0x10),
		"a sequence delimiter in an item":     seqDelimInItem,
		"an item past the value":              pastValue,
	} {
		if seq, err := DecodeUNSequence(v); err == nil {
			t.Errorf("%s: read as %d items, want an error", name, seq.Length())
		}
	}
	if _, err := DecodeUNSequence(whole); err != nil {
		t.Errorf("the whole value: %v", err)
	}
}

// TestReceivedDataSetIsStillReadLeniently proves the strict reading is
// DecodeUNSequence's alone: a received data set whose sequence is cut short
// still decodes as far as it goes, as it did before.
func TestReceivedDataSetIsStillReadLeniently(t *testing.T) {
	item := itemOf(true, implicitElement(0x0010, 0x0020, "ID1"))
	sq := binary.LittleEndian.AppendUint16(nil, 0x0008)
	sq = binary.LittleEndian.AppendUint16(sq, 0x1115)
	sq = binary.LittleEndian.AppendUint32(sq, 0xFFFFFFFF)
	sq = append(sq, item...) // no Sequence Delimitation Item
	data := append(implicitElement(0x0008, 0x0060, "CT"), sq...)
	ds, err := DecodeDataset(data, ImplicitVRLittleEndianUID)
	if err != nil {
		t.Fatalf("a received data set with an unclosed sequence: %v", err)
	}
	if _, err := ds.GetSequence(tag.New(0x0008, 0x1115)); err != nil {
		t.Errorf("the unclosed sequence was not read: %v", err)
	}
}
