package multival

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ConstrainedList enforces a single type for all items using a constructor function.
// The constructor is called on every value added to ensure type consistency.
type ConstrainedList struct {
	items       []interface{}
	constructor func(interface{}) interface{}
	mu          sync.RWMutex
}

// New creates a new ConstrainedList with a constructor function.
// The constructor is responsible for type conversion and validation.
func New(constructor func(interface{}) interface{}) *ConstrainedList {
	return &ConstrainedList{
		items:       make([]interface{}, 0),
		constructor: constructor,
	}
}

// NewFromValues creates a ConstrainedList and initializes it with values.
func NewFromValues(constructor func(interface{}) interface{}, values ...interface{}) (*ConstrainedList, error) {
	cl := New(constructor)
	for _, v := range values {
		if err := cl.Append(v); err != nil {
			return nil, err
		}
	}
	return cl, nil
}

// Append adds a value to the list, applying the constructor for type enforcement.
func (cl *ConstrainedList) Append(value interface{}) error {
	if cl.constructor == nil {
		return fmt.Errorf("constructor is nil")
	}

	converted := cl.constructor(value)
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.items = append(cl.items, converted)
	return nil
}

// Insert inserts a value at the specified index.
func (cl *ConstrainedList) Insert(index int, value interface{}) error {
	if cl.constructor == nil {
		return fmt.Errorf("constructor is nil")
	}

	converted := cl.constructor(value)

	cl.mu.Lock()
	defer cl.mu.Unlock()

	if index < 0 || index > len(cl.items) {
		return fmt.Errorf("index out of range: %d", index)
	}

	cl.items = append(cl.items[:index], append([]interface{}{converted}, cl.items[index:]...)...)
	return nil
}

// Get returns the value at the specified index.
func (cl *ConstrainedList) Get(index int) (interface{}, error) {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	if index < 0 || index >= len(cl.items) {
		return nil, fmt.Errorf("index out of range: %d", index)
	}

	return cl.items[index], nil
}

// Set sets the value at the specified index.
func (cl *ConstrainedList) Set(index int, value interface{}) error {
	if cl.constructor == nil {
		return fmt.Errorf("constructor is nil")
	}

	converted := cl.constructor(value)

	cl.mu.Lock()
	defer cl.mu.Unlock()

	if index < 0 || index >= len(cl.items) {
		return fmt.Errorf("index out of range: %d", index)
	}

	cl.items[index] = converted
	return nil
}

// Remove removes the value at the specified index.
func (cl *ConstrainedList) Remove(index int) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if index < 0 || index >= len(cl.items) {
		return fmt.Errorf("index out of range: %d", index)
	}

	cl.items = append(cl.items[:index], cl.items[index+1:]...)
	return nil
}

// Length returns the number of items in the list.
func (cl *ConstrainedList) Length() int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return len(cl.items)
}

// Items returns a copy of all items in the list.
func (cl *ConstrainedList) Items() []interface{} {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	result := make([]interface{}, len(cl.items))
	copy(result, cl.items)
	return result
}

// Clear removes all items from the list.
func (cl *ConstrainedList) Clear() {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.items = make([]interface{}, 0)
}

// Extend adds multiple values to the list, applying the constructor for type enforcement.
// All values are validated and converted through the constructor function.
func (cl *ConstrainedList) Extend(values []interface{}) error {
	if cl.constructor == nil {
		return fmt.Errorf("constructor is nil")
	}

	// Validate all values first
	converted := make([]interface{}, len(values))
	for i, v := range values {
		converted[i] = cl.constructor(v)
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.items = append(cl.items, converted...)
	return nil
}

// Sort sorts the list items using the provided comparison function.
// The comparison function should return true if the first item comes before the second.
func (cl *ConstrainedList) Sort(less func(i, j interface{}) bool) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	sort.Slice(cl.items, func(i, j int) bool {
		return less(cl.items[i], cl.items[j])
	})

	return nil
}

// Equal compares this list with another for equality.
// Two lists are equal if they have the same length and the same items in the same order.
func (cl *ConstrainedList) Equal(other *ConstrainedList) bool {
	if other == nil {
		return false
	}

	cl.mu.RLock()
	defer cl.mu.RUnlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	if len(cl.items) != len(other.items) {
		return false
	}

	for i := range cl.items {
		if cl.items[i] != other.items[i] {
			return false
		}
	}

	return true
}

// String returns a string representation of the list for debugging purposes.
// The format is [item1, item2, item3, ...] with each item formatted using fmt.Sprintf("%v", item).
func (cl *ConstrainedList) String() string {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	if len(cl.items) == 0 {
		return "[]"
	}

	parts := make([]string, len(cl.items))
	for i, item := range cl.items {
		parts[i] = fmt.Sprintf("%v", item)
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

// Convenience types for common use cases

// StringList is a ConstrainedList for strings.
type StringList struct {
	*ConstrainedList
}

// NewStringList creates a new StringList.
func NewStringList() *StringList {
	return &StringList{
		ConstrainedList: New(func(v interface{}) interface{} {
			switch val := v.(type) {
			case string:
				return val
			default:
				return fmt.Sprintf("%v", val)
			}
		}),
	}
}

// IntList is a ConstrainedList for integers.
type IntList struct {
	*ConstrainedList
}

// NewIntList creates a new IntList.
func NewIntList() *IntList {
	return &IntList{
		ConstrainedList: New(func(v interface{}) interface{} {
			switch val := v.(type) {
			case int:
				return val
			case int64:
				return int(val)
			case int32:
				return int(val)
			default:
				return 0
			}
		}),
	}
}

// FloatList is a ConstrainedList for floats.
type FloatList struct {
	*ConstrainedList
}

// NewFloatList creates a new FloatList.
func NewFloatList() *FloatList {
	return &FloatList{
		ConstrainedList: New(func(v interface{}) interface{} {
			switch val := v.(type) {
			case float64:
				return val
			case float32:
				return float64(val)
			case int:
				return float64(val)
			default:
				return 0.0
			}
		}),
	}
}
