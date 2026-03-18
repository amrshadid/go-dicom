# Multival

Type-safe multi-value lists with constructor-based type enforcement. Provides `StringList`, `IntList`, and `FloatList` for DICOM multi-value elements.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/multival"

// StringList - auto-converts to strings
sl := multival.NewStringList()
sl.Append("hello")
sl.Append(123) // becomes "123"

// IntList / FloatList
il := multival.NewIntList()
il.Append(42)

fl := multival.NewFloatList()
fl.Append(3.14)

// Custom constructor
positives := multival.New(func(v interface{}) interface{} {
    if i, ok := v.(int); ok && i > 0 { return i }
    return 0
})

// Access
value, _ := sl.Get(0)
items := sl.Items()
sl.Insert(1, "world")
sl.Remove(0)
sl.Clear()
```

## API Reference

```go
type ConstrainedList struct { ... }
func New(constructor func(interface{}) interface{}) *ConstrainedList
func NewFromValues(constructor func(interface{}) interface{}, values ...interface{}) (*ConstrainedList, error)

func (cl *ConstrainedList) Append(value interface{}) error
func (cl *ConstrainedList) Insert(index int, value interface{}) error
func (cl *ConstrainedList) Get(index int) (interface{}, error)
func (cl *ConstrainedList) Set(index int, value interface{}) error
func (cl *ConstrainedList) Remove(index int) error
func (cl *ConstrainedList) Length() int
func (cl *ConstrainedList) Items() []interface{}
func (cl *ConstrainedList) Clear()

type StringList struct { *ConstrainedList }
func NewStringList() *StringList

type IntList struct { *ConstrainedList }
func NewIntList() *IntList

type FloatList struct { *ConstrainedList }
func NewFloatList() *FloatList
```

## References

- [DICOM PS3.5 Section 6.2](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Multi-value element handling
