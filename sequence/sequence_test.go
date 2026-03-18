package sequence_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/sequence"
)

// TestNewSequence tests creating a new sequence.
func TestNewSequence(t *testing.T) {
	s := sequence.New()

	if s == nil {
		t.Fatal("sequence.New() returned nil")
	}

	if s.Length() != 0 {
		t.Errorf("sequence.New() sequence length = %d, want 0", s.Length())
	}

	if !s.IsEmpty() {
		t.Error("sequence.New() sequence should be empty")
	}
}

// TestNewFromItems tests creating a sequence with initial items.
func TestNewFromItems(t *testing.T) {
	items := []sequence.Item{"item1", "item2", "item3"}
	s := sequence.NewFromItems(items...)

	if s.Length() != 3 {
		t.Errorf("NewFromItems length = %d, want 3", s.Length())
	}

	for i, expected := range items {
		item, err := s.Get(i)
		if err != nil {
			t.Fatalf("Get(%d) error = %v", i, err)
		}
		if item != expected {
			t.Errorf("Get(%d) = %v, want %v", i, item, expected)
		}
	}
}

// TestAppend tests appending items to a sequence.
func TestAppend(t *testing.T) {
	s := sequence.New()

	err := s.Append("item1")
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if s.Length() != 1 {
		t.Errorf("After Append, length = %d, want 1", s.Length())
	}

	item, err := s.Get(0)
	if err != nil {
		t.Fatalf("Get(0) error = %v", err)
	}

	if item != "item1" {
		t.Errorf("Get(0) = %v, want 'item1'", item)
	}
}

// TestAppendNil tests that appending nil returns an error.
func TestAppendNil(t *testing.T) {
	s := sequence.New()

	err := s.Append(nil)
	if err == nil {
		t.Error("Append(nil) should return error")
	}
}

// TestInsert tests inserting items at specific indices.
func TestInsert(t *testing.T) {
	s := sequence.New()
	s.Append("a")
	s.Append("c")

	err := s.Insert(1, "b")
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if s.Length() != 3 {
		t.Errorf("After Insert, length = %d, want 3", s.Length())
	}

	items := s.Items()
	expected := []sequence.Item{"a", "b", "c"}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d = %v, want %v", i, items[i], exp)
		}
	}
}

// TestInsertOutOfRange tests inserting at invalid indices.
func TestInsertOutOfRange(t *testing.T) {
	s := sequence.New()
	s.Append("a")

	err := s.Insert(5, "b")
	if err == nil {
		t.Error("Insert(5) should return error for out of range")
	}
}

// TestGet tests retrieving items by index.
func TestGet(t *testing.T) {
	s := sequence.New()
	s.Append("test")

	item, err := s.Get(0)
	if err != nil {
		t.Fatalf("Get(0) error = %v", err)
	}

	if item != "test" {
		t.Errorf("Get(0) = %v, want 'test'", item)
	}
}

// TestGetOutOfRange tests getting items at invalid indices.
func TestGetOutOfRange(t *testing.T) {
	s := sequence.New()
	s.Append("a")

	_, err := s.Get(5)
	if err == nil {
		t.Error("Get(5) should return error for out of range")
	}
}

// TestSet tests setting items at specific indices.
func TestSet(t *testing.T) {
	s := sequence.New()
	s.Append("old")

	err := s.Set(0, "new")
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	item, _ := s.Get(0)
	if item != "new" {
		t.Errorf("Get(0) after Set = %v, want 'new'", item)
	}
}

// TestRemove tests removing items by index.
func TestRemove(t *testing.T) {
	s := sequence.New()
	s.Append("a")
	s.Append("b")
	s.Append("c")

	err := s.Remove(1)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if s.Length() != 2 {
		t.Errorf("After Remove, length = %d, want 2", s.Length())
	}

	items := s.Items()
	expected := []sequence.Item{"a", "c"}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d = %v, want %v", i, items[i], exp)
		}
	}
}

// TestRemoveOutOfRange tests removing at invalid indices.
func TestRemoveOutOfRange(t *testing.T) {
	s := sequence.New()
	s.Append("a")

	err := s.Remove(5)
	if err == nil {
		t.Error("Remove(5) should return error for out of range")
	}
}

// TestLength tests the Length method.
func TestLength(t *testing.T) {
	s := sequence.New()

	if s.Length() != 0 {
		t.Errorf("Empty sequence length = %d, want 0", s.Length())
	}

	s.Append("item")
	if s.Length() != 1 {
		t.Errorf("After append, length = %d, want 1", s.Length())
	}
}

// TestIsEmpty tests the IsEmpty method.
func TestIsEmpty(t *testing.T) {
	s := sequence.New()

	if !s.IsEmpty() {
		t.Error("New sequence should be empty")
	}

	s.Append("item")
	if s.IsEmpty() {
		t.Error("Sequence with item should not be empty")
	}
}

// TestItems tests the Items method.
func TestItems(t *testing.T) {
	s := sequence.New()
	s.Append("a")
	s.Append("b")
	s.Append("c")

	items := s.Items()

	if len(items) != 3 {
		t.Errorf("Items() length = %d, want 3", len(items))
	}

	expected := []sequence.Item{"a", "b", "c"}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d = %v, want %v", i, items[i], exp)
		}
	}
}

// TestClear tests the Clear method.
func TestClear(t *testing.T) {
	s := sequence.New()
	s.Append("a")
	s.Append("b")
	s.Append("c")

	s.Clear()

	if s.Length() != 0 {
		t.Errorf("After Clear, length = %d, want 0", s.Length())
	}

	if !s.IsEmpty() {
		t.Error("After Clear, sequence should be empty")
	}
}

// TestContains tests the Contains method.
func TestContains(t *testing.T) {
	item1 := "item1"
	item2 := "item2"

	s := sequence.New()
	s.Append(item1)

	if !s.Contains(item1) {
		t.Error("Contains(item1) should return true")
	}

	if s.Contains(item2) {
		t.Error("Contains(item2) should return false")
	}
}

// TestIndexOf tests the IndexOf method.
func TestIndexOf(t *testing.T) {
	item1 := "item1"
	item2 := "item2"
	item3 := "item3"

	s := sequence.New()
	s.Append(item1)
	s.Append(item2)
	s.Append(item1)

	if idx := s.IndexOf(item1); idx != 0 {
		t.Errorf("IndexOf(item1) = %d, want 0", idx)
	}

	if idx := s.IndexOf(item2); idx != 1 {
		t.Errorf("IndexOf(item2) = %d, want 1", idx)
	}

	if idx := s.IndexOf(item3); idx != -1 {
		t.Errorf("IndexOf(item3) = %d, want -1", idx)
	}
}

// TestRemoveItem tests the RemoveItem method.
func TestRemoveItem(t *testing.T) {
	item1 := "item1"
	item2 := "item2"

	s := sequence.New()
	s.Append(item1)
	s.Append(item2)

	err := s.RemoveItem(item1)
	if err != nil {
		t.Fatalf("RemoveItem(item1) error = %v", err)
	}

	if s.Length() != 1 {
		t.Errorf("After RemoveItem, length = %d, want 1", s.Length())
	}

	if !s.Contains(item2) {
		t.Error("item2 should still be in sequence")
	}

	err = s.RemoveItem("nonexistent")
	if err == nil {
		t.Error("RemoveItem('nonexistent') should return error")
	}
}

// TestClone tests the Clone method.
func TestClone(t *testing.T) {
	s := sequence.New()
	s.Append("a")
	s.Append("b")

	cloned := s.Clone()

	if cloned.Length() != s.Length() {
		t.Errorf("Cloned length = %d, want %d", cloned.Length(), s.Length())
	}

	// Modify original
	s.Append("c")

	if cloned.Length() != 2 {
		t.Error("Clone should be independent from original")
	}
}

// TestReverse tests the Reverse method.
func TestReverse(t *testing.T) {
	s := sequence.New()
	s.Append("a")
	s.Append("b")
	s.Append("c")

	s.Reverse()

	items := s.Items()
	expected := []sequence.Item{"c", "b", "a"}
	for i, exp := range expected {
		if items[i] != exp {
			t.Errorf("Item %d after Reverse = %v, want %v", i, items[i], exp)
		}
	}
}

// TestForEach tests the ForEach method.
func TestForEach(t *testing.T) {
	s := sequence.New()
	s.Append(1)
	s.Append(2)
	s.Append(3)

	sum := 0
	err := s.ForEach(func(item sequence.Item) error {
		sum += item.(int)
		return nil
	})

	if err != nil {
		t.Fatalf("ForEach() error = %v", err)
	}

	if sum != 6 {
		t.Errorf("Sum from ForEach = %d, want 6", sum)
	}
}

// TestFilteredItems tests the FilteredItems method.
func TestFilteredItems(t *testing.T) {
	s := sequence.New()
	s.Append(1)
	s.Append(2)
	s.Append(3)
	s.Append(4)

	even := s.FilteredItems(func(item sequence.Item) bool {
		return item.(int)%2 == 0
	})

	if len(even) != 2 {
		t.Errorf("FilteredItems returned %d items, want 2", len(even))
	}
}

// TestString tests the String method.
func TestString(t *testing.T) {
	s := sequence.New()
	s.Append("a")
	s.Append("b")

	str := s.String()
	if str == "" {
		t.Error("String() returned empty string")
	}

	if str != "Sequence[2 items]" {
		t.Errorf("String() = %s, want 'Sequence[2 items]'", str)
	}
}

// TestItemSequence tests the ItemSequence type.
func TestItemSequence(t *testing.T) {
	is := sequence.NewItemSequence()

	if is == nil {
		t.Fatal("sequence.NewItemSequence() returned nil")
	}

	err := is.Append("item1")
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if is.Length() != 1 {
		t.Errorf("ItemSequence length = %d, want 1", is.Length())
	}
}

// TestItemSequenceFromItems tests creating ItemSequence with initial items.
func TestItemSequenceFromItems(t *testing.T) {
	items := []sequence.Item{"item1", "item2"}
	is := sequence.NewItemSequenceFromItems(items...)

	if is.Length() != 2 {
		t.Errorf("ItemSequenceFromItems length = %d, want 2", is.Length())
	}
}

// TestNestedSequence tests the NestedSequence type.
func TestNestedSequence(t *testing.T) {
	ns := sequence.NewNestedSequence(3)

	if ns == nil {
		t.Fatal("sequence.NewNestedSequence() returned nil")
	}

	if ns.GetMaxNestingLevel() != 3 {
		t.Errorf("MaxNestingLevel = %d, want 3", ns.GetMaxNestingLevel())
	}

	if ns.GetCurrentLevel() != 1 {
		t.Errorf("CurrentLevel = %d, want 1", ns.GetCurrentLevel())
	}
}

// TestNestedSequenceAppend tests appending to nested sequence.
func TestNestedSequenceAppend(t *testing.T) {
	ns := sequence.NewNestedSequence(2)

	err := ns.AppendNested("item1")
	if err != nil {
		t.Fatalf("AppendNested() error = %v", err)
	}

	if ns.Length() != 1 {
		t.Errorf("After AppendNested, length = %d, want 1", ns.Length())
	}
}

// TestNestedSequenceSetLevel tests setting nesting level.
func TestNestedSequenceSetLevel(t *testing.T) {
	ns := sequence.NewNestedSequence(3)

	err := ns.SetCurrentLevel(2)
	if err != nil {
		t.Fatalf("SetCurrentLevel(2) error = %v", err)
	}

	if ns.GetCurrentLevel() != 2 {
		t.Errorf("CurrentLevel = %d, want 2", ns.GetCurrentLevel())
	}

	// Try invalid level
	err = ns.SetCurrentLevel(5)
	if err == nil {
		t.Error("SetCurrentLevel(5) should return error")
	}
}

// TestNestedSequenceZeroLevel tests with zero max level (should default to 1).
func TestNestedSequenceZeroLevel(t *testing.T) {
	ns := sequence.NewNestedSequence(0)

	if ns.GetMaxNestingLevel() != 1 {
		t.Errorf("MaxNestingLevel with 0 = %d, want 1", ns.GetMaxNestingLevel())
	}
}

// TestThreadSafety tests concurrent access to sequence.
func TestThreadSafety(t *testing.T) {
	s := sequence.New()

	done := make(chan bool, 2)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			s.Append(i)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = s.Length()
			if s.Length() > 0 {
				_, _ = s.Get(0)
			}
		}
		done <- true
	}()

	<-done
	<-done
}

// TestSequenceWithDifferentTypes tests sequence with mixed types.
func TestSequenceWithDifferentTypes(t *testing.T) {
	s := sequence.New()
	s.Append("string")
	s.Append(42)
	s.Append(3.14)

	if s.Length() != 3 {
		t.Errorf("Mixed type sequence length = %d, want 3", s.Length())
	}

	str, _ := s.Get(0)
	if str != "string" {
		t.Errorf("Get(0) = %v, want 'string'", str)
	}

	num, _ := s.Get(1)
	if num != 42 {
		t.Errorf("Get(1) = %v, want 42", num)
	}

	f, _ := s.Get(2)
	if f != 3.14 {
		t.Errorf("Get(2) = %v, want 3.14", f)
	}
}
