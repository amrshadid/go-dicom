// Package sequence provides a thread-safe ordered sequence container for DICOM datasets.
//
// A sequence in DICOM is a structured list of datasets that can contain nested sequences.
// This package implements efficient, thread-safe operations for managing sequences of items.
//
// # Core Concepts
//
// ## Item
//
// An Item is a generic interface{} representing any object that can be stored in a sequence.
// Items can be simple values, complex structures, or even other sequences for nesting.
//
// ## Sequence
//
// A Sequence is an ordered, thread-safe collection of items. It provides:
//   - Append/Insert/Remove operations
//   - Index-based access (Get/Set)
//   - Searching and filtering
//   - Iteration and transformation
//   - Cloning and reversal
//
// ## ItemSequence
//
// ItemSequence is a specialized Sequence for holding DICOM dataset items.
// It extends Sequence with DICOM-specific semantics (Sequence Value Representation).
//
// ## NestedSequence
//
// NestedSequence is a Sequence that can contain other sequences with controlled nesting depth.
// This ensures DICOM compliance and prevents infinite nesting.
//
// # Basic Usage
//
// ## Creating a Sequence
//
//	// Create empty sequence
//	seq := sequence.New()
//
//	// Create sequence with initial items
//	seq := sequence.NewFromItems("item1", "item2", "item3")
//
// ## Adding Items
//
//	// Append to end
//	err := seq.Append("new_item")
//
//	// Insert at specific position
//	err := seq.Insert(1, "inserted_item")
//
// ## Retrieving Items
//
//	// Get by index
//	item, err := seq.Get(0)
//
//	// Get all items
//	items := seq.Items()
//
// ## Modifying Items
//
//	// Set value at index
//	err := seq.Set(0, "new_value")
//
//	// Remove by index
//	err := seq.Remove(0)
//
//	// Remove by value (first occurrence)
//	err := seq.RemoveItem(item)
//
// ## Searching and Filtering
//
//	// Check if item exists
//	exists := seq.Contains(item)
//
//	// Find index of item
//	index := seq.IndexOf(item)
//
//	// Filter items
//	filtered := seq.FilteredItems(func(item sequence.Item) bool {
//	    return item.(int) > 10  // Keep items > 10
//	})
//
// ## Iteration
//
//	// Apply function to each item
//	err := seq.ForEach(func(item sequence.Item) error {
//	    fmt.Println(item)
//	    return nil
//	})
//
// ## Utilities
//
//	// Get sequence length
//	length := seq.Length()
//
//	// Check if empty
//	if seq.IsEmpty() { ... }
//
//	// Clear all items
//	seq.Clear()
//
//	// Create shallow copy
//	cloned := seq.Clone()
//
//	// Reverse order
//	seq.Reverse()
//
//	// String representation
//	str := seq.String()  // "Sequence[5 items]"
//
// # ItemSequence for DICOM
//
// ItemSequence is specialized for DICOM datasets:
//
//	is := sequence.NewItemSequence()
//	is.Append(dicomDataset1)
//	is.Append(dicomDataset2)
//
//	// Same operations as Sequence
//	dataset, err := is.Get(0)
//	count := is.Length()
//
// # NestedSequence for Controlled Nesting
//
// NestedSequence prevents excessive nesting depth:
//
//	// Max nesting level of 3
//	ns := sequence.NewNestedSequence(3)
//
//	// Append with level checking
//	err := ns.AppendNested(item)
//
//	// Query nesting levels
//	maxLevel := ns.GetMaxNestingLevel()    // 3
//	currentLevel := ns.GetCurrentLevel()   // 1
//
//	// Set current nesting level
//	err := ns.SetCurrentLevel(2)
//
// # Thread Safety
//
// All Sequence operations are thread-safe through internal sync.RWMutex:
//   - Multiple goroutines can read concurrently
//   - Write operations are mutually exclusive
//   - Safe for concurrent use without external synchronization
//
// Example of concurrent access:
//
//	seq := sequence.New()
//
//	// Multiple readers
//	go func() {
//	    for i := 0; i < 100; i++ {
//	        _ = seq.Length()
//	        _ = seq.Items()
//	    }
//	}()
//
//	// Single writer
//	go func() {
//	    for i := 0; i < 100; i++ {
//	        seq.Append(i)
//	    }
//	}()
//
// # Performance Characteristics
//
//   - **Append**: O(1) amortized
//   - **Insert**: O(n) where n is items after insertion point
//   - **Get/Set**: O(1)
//   - **Remove**: O(n) where n is items after removal point
//   - **Length**: O(1)
//   - **Contains/IndexOf**: O(n)
//   - **Clone**: O(n)
//   - **ForEach/FilteredItems**: O(n)
//   - **Memory**: O(n) for n items
//
// # Error Handling
//
// Operations return errors for:
//   - Nil items (Append, Insert, Set)
//   - Out of range indices (Get, Set, Insert, Remove)
//   - Item not found (RemoveItem)
//   - Invalid nesting levels (SetCurrentLevel)
//
// Example:
//
//	err := seq.Append(nil)  // Error: cannot append nil item
//	item, err := seq.Get(100)  // Error: index out of range
//	err := seq.RemoveItem(notInSeq)  // Error: item not found
//
// # Use Cases
//
// ## DICOM Sequence VR
//
//	// Represent DICOM Sequence Value Representation
//	seq := sequence.NewItemSequence()
//	seq.Append(dataset1)
//	seq.Append(dataset2)
//
// ## Nested DICOM Structures
//
//	// Control nesting depth for complex DICOM structures
//	nested := sequence.NewNestedSequence(5)
//	for _, ds := range datasets {
//	    nested.AppendNested(ds)
//	}
//
// ## Filtering Operations
//
//	// Filter sequences based on predicates
//	largeItems := seq.FilteredItems(func(item sequence.Item) bool {
//	    ds := item.(*Dataset)
//	    return len(ds.Elements()) > 100
//	})
//
// ## Batch Processing
//
//	// Process items with error handling
//	err := seq.ForEach(func(item sequence.Item) error {
//	    return processDataset(item.(*Dataset))
//	})
//
// # See Also
//
//   - dataset package: DICOM dataset structure and handling
//   - tag package: DICOM tag definitions
//   - values package: Value encoding and representation
//   - [DICOM Standard](https://www.dicomstandard.org/)
package sequence
