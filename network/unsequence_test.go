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
