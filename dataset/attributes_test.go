package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// populated builds a data set with elements in three groups, in a known
// insertion order, so ordering and range behavior are both observable.
func populated(t *testing.T) *dataset.Dataset {
	t.Helper()

	ds := dataset.NewDataset()
	for _, e := range []struct {
		tg    tag.Tag
		vr    dataelem.VR
		value string
	}{
		{tag.New(0x0008, 0x0060), dataelem.CS, "MR"},
		{tag.New(0x0010, 0x0010), dataelem.PN, "Doe^John"},
		{tag.New(0x0010, 0x0020), dataelem.LO, "ID-0001 "},
		{tag.New(0x0028, 0x0010), dataelem.US, "\x40\x00"},
	} {
		if err := ds.Add(dataelem.NewDataElement(e.tg, e.vr, []byte(e.value))); err != nil {
			t.Fatalf("Add %s: %v", e.tg, err)
		}
	}
	return ds
}

func TestGetByTag(t *testing.T) {
	ds := populated(t)

	t.Run("finds an element", func(t *testing.T) {
		elem := ds.GetByTag("0010", "0010")
		if elem == nil {
			t.Fatal("GetByTag returned nil for a tag that is present")
		}
		if got := string(elem.GetValue().([]byte)); got != "Doe^John" {
			t.Errorf("value = %q, want %q", got, "Doe^John")
		}
	})

	t.Run("lowercase hex", func(t *testing.T) {
		// %X accepts either case, and callers write tags both ways.
		if ds.GetByTag("0028", "0010") == nil {
			t.Error("uppercase hex failed")
		}
		if ds.GetByTag("002a", "0010") != nil {
			t.Error("GetByTag matched a tag that is not present")
		}
	})

	t.Run("absent tag", func(t *testing.T) {
		if elem := ds.GetByTag("0008", "0018"); elem != nil {
			t.Errorf("GetByTag returned %v for an absent tag", elem)
		}
	})
}

// TestUnparseableHexIsRejected verifies a malformed tag string is refused
// rather than silently read as zero.
//
// The hex arguments were parsed with fmt.Sscanf and its error discarded, so
// anything unparseable left the value at zero: a caller passing garbage asked
// for (0000,0000) and got it if the data set held Command Group Length, which
// every DIMSE command data set does. HasTag("garbage", "garbage") returned true.
//
// Sscanf also stops at the first unusable character, so "00ZZ" yielded 0x0000
// without complaint. Parsing now requires the whole component to be consumed.
func TestUnparseableHexIsRejected(t *testing.T) {
	ds := dataset.NewDataset()
	if err := ds.Add(dataelem.NewDataElement(
		tag.New(0x0000, 0x0000), dataelem.UL, []byte{0, 0, 0, 0})); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// A real element, so a rejection cannot be mistaken for an empty data set.
	if err := ds.Add(dataelem.NewDataElement(
		tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John"))); err != nil {
		t.Fatalf("Add: %v", err)
	}

	for _, tc := range []struct{ group, element string }{
		{"garbage", "garbage"},
		{"", ""},
		{"0010", ""},
		{"", "0010"},
		{"00ZZ", "0010"},
		{"0010", "00ZZ"},
		{"00100", "0010"},  // too wide for 16 bits
		{"-1", "0010"},     // ParseUint rejects a sign
		{"0x0010", "0010"}, // a Go literal, not DICOM hex
	} {
		name := tc.group + "/" + tc.element
		t.Run(name, func(t *testing.T) {
			if elem := ds.GetByTag(tc.group, tc.element); elem != nil {
				t.Errorf("GetByTag(%q, %q) returned %s", tc.group, tc.element, elem.MustTag())
			}
			if ds.HasTag(tc.group, tc.element) {
				t.Errorf("HasTag(%q, %q) = true", tc.group, tc.element)
			}
			if ds.RemoveByTag(tc.group, tc.element) {
				t.Errorf("RemoveByTag(%q, %q) = true", tc.group, tc.element)
			}
		})
	}

	// Nothing may have been removed by any of the above.
	if ds.Length() != 2 {
		t.Errorf("data set has %d elements after rejected lookups, want 2", ds.Length())
	}

	// (0000,0000) is still reachable when asked for properly.
	if ds.GetByTag("0000", "0000") == nil {
		t.Error("GetByTag could not find (0000,0000) when named correctly")
	}
}

// TestGetRangeRejectsUnparseableHex covers the range variant, which parses two
// group values of its own.
func TestGetRangeRejectsUnparseableHex(t *testing.T) {
	ds := populated(t)

	for _, tc := range []struct{ start, end string }{
		{"garbage", "0028"},
		{"0008", "garbage"},
		{"", "0028"},
	} {
		if got := ds.GetRange(tc.start, tc.end); got != nil {
			t.Errorf("GetRange(%q, %q) returned %d elements", tc.start, tc.end, len(got))
		}
	}
}

func TestGetFirstAndGetLast(t *testing.T) {
	t.Run("insertion order", func(t *testing.T) {
		ds := populated(t)

		first := ds.GetFirst()
		if first == nil {
			t.Fatal("GetFirst returned nil for a populated data set")
		}
		if got := first.MustTag(); got != tag.New(0x0008, 0x0060) {
			t.Errorf("GetFirst = %s, want (0008,0060)", got)
		}

		last := ds.GetLast()
		if last == nil {
			t.Fatal("GetLast returned nil for a populated data set")
		}
		if got := last.MustTag(); got != tag.New(0x0028, 0x0010) {
			t.Errorf("GetLast = %s, want (0028,0010)", got)
		}
	})

	t.Run("empty data set", func(t *testing.T) {
		ds := dataset.NewDataset()
		if ds.GetFirst() != nil {
			t.Error("GetFirst returned an element from an empty data set")
		}
		if ds.GetLast() != nil {
			t.Error("GetLast returned an element from an empty data set")
		}
	})

	t.Run("single element is both", func(t *testing.T) {
		ds := dataset.NewDataset()
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("MR")))
		if ds.GetFirst() != ds.GetLast() {
			t.Error("with one element, GetFirst and GetLast returned different elements")
		}
	})
}

func TestHasTag(t *testing.T) {
	ds := populated(t)

	if !ds.HasTag("0010", "0010") {
		t.Error("HasTag reported a present tag as absent")
	}
	if ds.HasTag("0008", "0018") {
		t.Error("HasTag reported an absent tag as present")
	}
}

func TestRemoveByTag(t *testing.T) {
	t.Run("removes and reports", func(t *testing.T) {
		ds := populated(t)
		before := ds.Length()

		if !ds.RemoveByTag("0010", "0020") {
			t.Fatal("RemoveByTag reported failure for a present tag")
		}
		if ds.Length() != before-1 {
			t.Errorf("length = %d after removing one of %d", ds.Length(), before)
		}
		if ds.HasTag("0010", "0020") {
			t.Error("the element is still present after RemoveByTag")
		}
	})

	t.Run("absent tag reports failure", func(t *testing.T) {
		ds := populated(t)
		before := ds.Length()

		if ds.RemoveByTag("0008", "0018") {
			t.Error("RemoveByTag reported success for an absent tag")
		}
		if ds.Length() != before {
			t.Errorf("length changed from %d to %d while removing nothing", before, ds.Length())
		}
	})

	t.Run("order survives removal from the middle", func(t *testing.T) {
		ds := populated(t)

		if !ds.RemoveByTag("0010", "0010") {
			t.Fatal("RemoveByTag failed")
		}

		// Removal splices the order slice; the remaining elements must keep
		// their relative order rather than the last one filling the gap.
		want := []tag.Tag{
			tag.New(0x0008, 0x0060),
			tag.New(0x0010, 0x0020),
			tag.New(0x0028, 0x0010),
		}
		all := ds.GetAll()
		if len(all) != len(want) {
			t.Fatalf("got %d elements, want %d", len(all), len(want))
		}
		for i, w := range want {
			if got := all[i].MustTag(); got != w {
				t.Errorf("position %d = %s, want %s", i, got, w)
			}
		}
	})
}

func TestGetRange(t *testing.T) {
	ds := populated(t)

	t.Run("inclusive of both bounds", func(t *testing.T) {
		got := ds.GetRange("0008", "0010")
		if len(got) != 3 {
			t.Fatalf("got %d elements for groups 0008-0010, want 3", len(got))
		}
		// One from group 0008 and two from group 0010.
		if got[0].MustTag() != tag.New(0x0008, 0x0060) {
			t.Errorf("first = %s, want (0008,0060)", got[0].MustTag())
		}
	})

	t.Run("single group", func(t *testing.T) {
		got := ds.GetRange("0010", "0010")
		if len(got) != 2 {
			t.Errorf("got %d elements in group 0010, want 2", len(got))
		}
	})

	t.Run("range matching nothing", func(t *testing.T) {
		if got := ds.GetRange("0040", "0050"); len(got) != 0 {
			t.Errorf("got %d elements for an empty range", len(got))
		}
	})

	t.Run("inverted range matches nothing", func(t *testing.T) {
		// start > end cannot be satisfied by any group.
		if got := ds.GetRange("0028", "0008"); len(got) != 0 {
			t.Errorf("got %d elements for an inverted range", len(got))
		}
	})
}

func TestIsEmpty(t *testing.T) {
	if !dataset.NewDataset().IsEmpty() {
		t.Error("a new data set is not reported as empty")
	}

	ds := populated(t)
	if ds.IsEmpty() {
		t.Error("a populated data set is reported as empty")
	}

	ds.Clear()
	if !ds.IsEmpty() {
		t.Error("a cleared data set is not reported as empty")
	}
}

func TestFilter(t *testing.T) {
	ds := populated(t)

	t.Run("matching subset", func(t *testing.T) {
		got := ds.Filter(func(e *dataelem.DataElement) bool {
			return e.MustTag().Group() == 0x0010
		})
		if len(got) != 2 {
			t.Errorf("got %d elements in group 0010, want 2", len(got))
		}
	})

	t.Run("matching nothing", func(t *testing.T) {
		got := ds.Filter(func(*dataelem.DataElement) bool { return false })
		if len(got) != 0 {
			t.Errorf("got %d elements from a filter matching nothing", len(got))
		}
	})

	t.Run("matching everything preserves order", func(t *testing.T) {
		got := ds.Filter(func(*dataelem.DataElement) bool { return true })
		all := ds.GetAll()
		if len(got) != len(all) {
			t.Fatalf("got %d, want %d", len(got), len(all))
		}
		for i := range got {
			if got[i].MustTag() != all[i].MustTag() {
				t.Errorf("position %d differs from GetAll", i)
			}
		}
	})
}

func TestMap(t *testing.T) {
	ds := populated(t)

	got := ds.Map(func(e *dataelem.DataElement) interface{} {
		return e.MustTag()
	})
	if len(got) != ds.Length() {
		t.Fatalf("Map returned %d results for %d elements", len(got), ds.Length())
	}
	if first, ok := got[0].(tag.Tag); !ok || first != tag.New(0x0008, 0x0060) {
		t.Errorf("first result = %v, want (0008,0060)", got[0])
	}

	t.Run("empty data set", func(t *testing.T) {
		got := dataset.NewDataset().Map(func(*dataelem.DataElement) interface{} { return 1 })
		if len(got) != 0 {
			t.Errorf("Map on an empty data set returned %d results", len(got))
		}
	})
}

func TestFindElement(t *testing.T) {
	ds := populated(t)

	t.Run("returns the first match, not any match", func(t *testing.T) {
		// Two elements are in group 0010; the earlier must win.
		got := ds.FindElement(func(e *dataelem.DataElement) bool {
			return e.MustTag().Group() == 0x0010
		})
		if got == nil {
			t.Fatal("FindElement returned nil for a condition that matches")
		}
		if want := tag.New(0x0010, 0x0010); got.MustTag() != want {
			t.Errorf("FindElement = %s, want %s — the first match in order",
				got.MustTag(), want)
		}
	})

	t.Run("no match", func(t *testing.T) {
		if got := ds.FindElement(func(*dataelem.DataElement) bool { return false }); got != nil {
			t.Errorf("FindElement returned %v when nothing matched", got)
		}
	})
}
