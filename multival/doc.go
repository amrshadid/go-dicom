// Package multival provides type-safe multi-value lists with constructor-based type enforcement.
//
// This package implements containers that enforce type consistency across all items
// using constructor functions. It offers both a generic ConstrainedList and
// convenience types (StringList, IntList, FloatList) for common use cases.
//
// # Core Concepts
//
// ## ConstrainedList
//
// A generic list that enforces type consistency by applying a constructor function
// to every value added to the list. The constructor is responsible for type
// conversion, validation, and normalization.
//
// ## Constructor Function
//
// A function that takes an interface{} and returns a normalized/converted interface{}.
// It's called for every value added, allowing custom type enforcement and conversion logic.
//
// ## Convenience Types
//
// Pre-configured list types for common scenarios:
//   - StringList: Enforces string type, converts values to strings
//   - IntList: Enforces integer type, converts compatible types to int
//   - FloatList: Enforces float64 type, converts compatible types to float64
//
// # Basic Usage
//
// ## Creating a ConstrainedList
//
//	// Create with custom constructor
//	cl := multival.New(func(v interface{}) interface{} {
//	    return v  // Pass-through constructor
//	})
//
// ## Using Convenience Types
//
//	// StringList - automatically converts to strings
//	sl := multival.NewStringList()
//	sl.Append("hello")
//	sl.Append(123)  // Automatically converted to "123"
//
//	// IntList - automatically converts to integers
//	il := multival.NewIntList()
//	il.Append(42)
//	il.Append(3.14)  // Converted to 3
//
//	// FloatList - automatically converts to floats
//	fl := multival.NewFloatList()
//	fl.Append(3.14)
//	fl.Append(2)  // Converted to 2.0
//
// ## Adding and Retrieving Values
//
//	cl := multival.NewStringList()
//	cl.Append("first")
//	cl.Append("second")
//
//	value, err := cl.Get(0)  // "first"
//	items := cl.Items()      // ["first", "second"]
//
// ## Modifying Values
//
//	cl.Set(0, "updated")
//	cl.Insert(1, "inserted")
//	cl.Remove(0)
//	cl.Clear()
//
// # Advanced Features
//
// ## Custom Constructors
//
// Create lists with custom type enforcement and validation:
//
//	// Constructor that enforces positive integers
//	positiveIntConstructor := func(v interface{}) interface{} {
//	    switch val := v.(type) {
//	    case int:
//	        if val > 0 {
//	            return val
//	        }
//	        return 1  // Default to 1 if not positive
//	    default:
//	        return 1
//	    }
//	}
//	cl := multival.New(positiveIntConstructor)
//
// ## Constructor with Validation
//
// Validation can be incorporated into the constructor logic:
//
//	// Constructor that ensures uppercase strings
//	cl := multival.New(func(v interface{}) interface{} {
//	    switch val := v.(type) {
//	    case string:
//	        return strings.ToUpper(val)
//	    default:
//	        return strings.ToUpper(fmt.Sprintf("%v", val))
//	    }
//	})
//
// ## Initialization with Values
//
// Create and populate a list in one call:
//
//	sl := multival.NewStringList()
//	cl, err := multival.NewFromValues(sl.constructor, "a", "b", "c")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # ConstrainedList Operations
//
// ## Length and Capacity
//
//	count := cl.Length()  // O(1)
//	items := cl.Items()   // O(n) - returns copy
//
// ## Access and Modification
//
//	// Get value
//	value, err := cl.Get(index)
//
//	// Set value
//	err := cl.Set(index, newValue)
//
//	// Insert at position
//	err := cl.Insert(index, value)
//
//	// Append to end
//	err := cl.Append(value)
//
//	// Append multiple values
//	err := cl.Extend([]interface{}{val1, val2, val3})
//
//	// Remove by index
//	err := cl.Remove(index)
//
//	// Clear all items
//	cl.Clear()
//
// ## Utility Operations
//
//	// Sort the list (in-place)
//	err := cl.Sort(func(i, j interface{}) bool {
//	    return i.(int) < j.(int)
//	})
//
//	// Compare two lists for equality
//	isEqual := cl1.Equal(cl2)
//
//	// Get string representation for debugging
//	str := cl.String()  // "[item1, item2, item3]"
//
// # StringList Details
//
// Automatically converts values to strings:
//   - string: Passed through unchanged
//   - Other types: Formatted using fmt.Sprintf("%v", val)
//
// Example conversions:
//   - 123 -> "123"
//   - 3.14 -> "3.14"
//   - true -> "true"
//
// # IntList Details
//
// Automatically converts compatible types to integers:
//   - int: Passed through unchanged
//   - int64: Converted to int
//   - int32: Converted to int
//   - Other types: Default to 0
//
// Example conversions:
//   - int(42) -> 42
//   - int64(100) -> 100
//   - int32(50) -> 50
//   - "123" -> 0 (no string conversion)
//   - 3.14 -> 0 (no float conversion)
//
// # FloatList Details
//
// Automatically converts compatible types to float64:
//   - float64: Passed through unchanged
//   - float32: Converted to float64
//   - int: Converted to float64
//   - Other types: Default to 0.0
//
// Example conversions:
//   - float64(3.14) -> 3.14
//   - float32(2.5) -> 2.5
//   - int(42) -> 42.0
//   - "3.14" -> 0.0 (no string conversion)
//
// # Thread Safety
//
// All ConstrainedList operations are thread-safe through internal sync.RWMutex:
//   - Multiple goroutines can read concurrently (Get, Items, Length)
//   - Write operations are mutually exclusive (Append, Insert, Set, Remove, Clear)
//   - Safe for concurrent use without external synchronization
//
// Example of concurrent operations:
//
//	cl := multival.NewStringList()
//
//	// Multiple readers
//	go func() {
//	    for i := 0; i < 100; i++ {
//	        _ = cl.Length()
//	        if cl.Length() > 0 {
//	            _, _ = cl.Get(0)
//	        }
//	    }
//	}()
//
//	// Single writer
//	go func() {
//	    for i := 0; i < 100; i++ {
//	        cl.Append("value")
//	    }
//	}()
//
// # Performance Characteristics
//
//   - **Append**: O(1) amortized - append to underlying slice
//   - **Insert**: O(n) - requires slice copying at insertion point
//   - **Get**: O(1) - direct index access
//   - **Set**: O(1) - direct index assignment
//   - **Remove**: O(n) - requires slice copying after removal point
//   - **Length**: O(1) - cached length
//   - **Items**: O(n) - copy all items for safety
//   - **Clear**: O(1) - reset slice
//   - **Extend**: O(m) - appends m items with type conversion
//   - **Sort**: O(n log n) - uses sort.Slice for efficient sorting
//   - **Equal**: O(n) - compares all items
//   - **String**: O(n) - formats all items as strings
//   - **Memory**: O(n) for n items in underlying slice
//
// # Error Handling
//
// Operations return errors for:
//   - Nil constructor (Append, Insert, Set, NewFromValues)
//   - Index out of range (Get, Set, Remove, Insert)
//   - Other constructor-specific errors (propagated from constructor)
//
// Example:
//
//	cl := multival.New(nil)  // Nil constructor
//	err := cl.Append("test")  // Returns "constructor is nil"
//	if err != nil {
//	    log.Printf("Error: %v", err)
//	}
//
// # Use Cases
//
// ## DICOM Multi-Value Elements
//
//	// Store multiple string values from DICOM element
//	patientNames := multival.NewStringList()
//	patientNames.Append("Doe^John")
//	patientNames.Append("Doe^Jane")
//
// ## Type-Safe Numeric Collections
//
//	// Collect measurements with automatic type conversion
//	measurements := multival.NewFloatList()
//	measurements.Append(100.5)  // float64
//	measurements.Append(int(50))  // automatically converted to 50.0
//
// ## Custom Validation Lists
//
//	// Ensure all values meet criteria
//	ageList := multival.New(func(v interface{}) interface{} {
//	    switch val := v.(type) {
//	    case int:
//	        if val >= 0 && val <= 150 {
//	            return val
//	        }
//	    }
//	    return 0
//	})
//
// # See Also
//
//   - sequence package: Ordered, generic sequence containers
//   - values package: Value encoding and representation
//   - dataset package: DICOM dataset structure
package multival
