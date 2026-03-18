package sequence

import (
	"fmt"
	"sync"
)

// Item represents a single item in a sequence.
type Item interface{}

// Sequence represents a DICOM sequence of items.
type Sequence struct {
	items []Item
	mu    sync.RWMutex
}

// New creates a new empty sequence.
func New() *Sequence {
	return &Sequence{
		items: make([]Item, 0),
	}
}

// NewFromItems creates a sequence initialized with items.
func NewFromItems(items ...Item) *Sequence {
	s := New()
	s.items = append(s.items, items...)
	return s
}

// Append adds an item to the sequence.
func (s *Sequence) Append(item Item) error {
	if item == nil {
		return fmt.Errorf("cannot append nil item to sequence")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = append(s.items, item)
	return nil
}

// Insert inserts an item at the specified index.
func (s *Sequence) Insert(index int, item Item) error {
	if item == nil {
		return fmt.Errorf("cannot insert nil item into sequence")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index > len(s.items) {
		return fmt.Errorf("index out of range: %d", index)
	}

	s.items = append(s.items[:index], append([]Item{item}, s.items[index:]...)...)
	return nil
}

// Get retrieves the item at the specified index.
func (s *Sequence) Get(index int) (Item, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if index < 0 || index >= len(s.items) {
		return nil, fmt.Errorf("index out of range: %d", index)
	}

	return s.items[index], nil
}

// Set sets the item at the specified index.
func (s *Sequence) Set(index int, item Item) error {
	if item == nil {
		return fmt.Errorf("cannot set nil item in sequence")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index >= len(s.items) {
		return fmt.Errorf("index out of range: %d", index)
	}

	s.items[index] = item
	return nil
}

// Remove removes the item at the specified index.
func (s *Sequence) Remove(index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index >= len(s.items) {
		return fmt.Errorf("index out of range: %d", index)
	}

	s.items = append(s.items[:index], s.items[index+1:]...)
	return nil
}

// Length returns the number of items in the sequence.
func (s *Sequence) Length() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.items)
}

// IsEmpty returns true if the sequence contains no items.
func (s *Sequence) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.items) == 0
}

// Items returns a copy of all items in the sequence.
func (s *Sequence) Items() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Item, len(s.items))
	copy(result, s.items)
	return result
}

// Clear removes all items from the sequence.
func (s *Sequence) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = make([]Item, 0)
}

// Contains checks if an item exists in the sequence (by reference).
func (s *Sequence) Contains(item Item) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, i := range s.items {
		if i == item {
			return true
		}
	}
	return false
}

// IndexOf returns the index of the first occurrence of an item (by reference).
func (s *Sequence) IndexOf(item Item) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for idx, i := range s.items {
		if i == item {
			return idx
		}
	}
	return -1
}

// RemoveItem removes the first occurrence of an item (by reference).
func (s *Sequence) RemoveItem(item Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for idx, i := range s.items {
		if i == item {
			s.items = append(s.items[:idx], s.items[idx+1:]...)
			return nil
		}
	}
	return fmt.Errorf("item not found in sequence")
}

// Clone creates a shallow copy of the sequence.
func (s *Sequence) Clone() *Sequence {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cloned := New()
	cloned.items = make([]Item, len(s.items))
	copy(cloned.items, s.items)
	return cloned
}

// Reverse reverses the order of items in the sequence.
func (s *Sequence) Reverse() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, j := 0, len(s.items)-1; i < j; i, j = i+1, j-1 {
		s.items[i], s.items[j] = s.items[j], s.items[i]
	}
}

// ForEach applies a function to each item in the sequence (read-only).
func (s *Sequence) ForEach(fn func(Item) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, item := range s.items {
		if err := fn(item); err != nil {
			return err
		}
	}
	return nil
}

// FilteredItems returns a slice of items that match a predicate function.
func (s *Sequence) FilteredItems(predicate func(Item) bool) []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Item
	for _, item := range s.items {
		if predicate(item) {
			result = append(result, item)
		}
	}
	return result
}

// String returns a string representation of the sequence.
func (s *Sequence) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fmt.Sprintf("Sequence[%d items]", len(s.items))
}

// ItemSequence is a specialized sequence for holding dataset items.
type ItemSequence struct {
	*Sequence
}

// NewItemSequence creates a new ItemSequence.
func NewItemSequence() *ItemSequence {
	return &ItemSequence{
		Sequence: New(),
	}
}

// NewItemSequenceFromItems creates an ItemSequence initialized with items.
func NewItemSequenceFromItems(items ...Item) *ItemSequence {
	return &ItemSequence{
		Sequence: NewFromItems(items...),
	}
}

// NestedSequence represents a sequence containing other sequences.
type NestedSequence struct {
	*Sequence
	maxNestingLevel int
	currentLevel    int
}

// NewNestedSequence creates a new NestedSequence with a maximum nesting level.
func NewNestedSequence(maxLevel int) *NestedSequence {
	if maxLevel < 1 {
		maxLevel = 1
	}
	return &NestedSequence{
		Sequence:        New(),
		maxNestingLevel: maxLevel,
		currentLevel:    1,
	}
}

// AppendNested appends an item to the nested sequence with level checking.
func (ns *NestedSequence) AppendNested(item Item) error {
	if ns.currentLevel > ns.maxNestingLevel {
		return fmt.Errorf("maximum nesting level exceeded: %d", ns.maxNestingLevel)
	}

	return ns.Append(item)
}

// GetMaxNestingLevel returns the maximum allowed nesting level.
func (ns *NestedSequence) GetMaxNestingLevel() int {
	return ns.maxNestingLevel
}

// GetCurrentLevel returns the current nesting level.
func (ns *NestedSequence) GetCurrentLevel() int {
	return ns.currentLevel
}

// SetCurrentLevel sets the current nesting level.
func (ns *NestedSequence) SetCurrentLevel(level int) error {
	if level < 1 || level > ns.maxNestingLevel {
		return fmt.Errorf("invalid nesting level: %d (range: 1-%d)", level, ns.maxNestingLevel)
	}

	ns.currentLevel = level
	return nil
}
