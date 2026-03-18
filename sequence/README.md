# Sequence

Thread-safe ordered sequence container for DICOM datasets. Provides generic `Sequence`, DICOM-specific `ItemSequence`, and depth-controlled `NestedSequence` types.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/sequence"

// Generic sequence
seq := sequence.NewFromItems("a", "b", "c")
seq.Append("d")
item, _ := seq.Get(0)

// DICOM item sequence
itemSeq := sequence.NewItemSequence()
itemSeq.Append(dataset1)
itemSeq.Append(dataset2)

// Nested sequence with depth control
nested := sequence.NewNestedSequence(3) // max depth 3
nested.AppendNested(item)

// Operations
filtered := seq.FilteredItems(func(item sequence.Item) bool { return true })
seq.ForEach(func(item sequence.Item) error { return nil })
clone := seq.Clone()
seq.Reverse()
```

## API Reference

```go
type Item interface{}

func New() *Sequence
func NewFromItems(items ...Item) *Sequence
func (s *Sequence) Append(item Item) error
func (s *Sequence) Insert(index int, item Item) error
func (s *Sequence) Get(index int) (Item, error)
func (s *Sequence) Set(index int, item Item) error
func (s *Sequence) Remove(index int) error
func (s *Sequence) RemoveItem(item Item) error
func (s *Sequence) Length() int
func (s *Sequence) IsEmpty() bool
func (s *Sequence) Items() []Item
func (s *Sequence) Contains(item Item) bool
func (s *Sequence) IndexOf(item Item) int
func (s *Sequence) Clone() *Sequence
func (s *Sequence) Reverse()
func (s *Sequence) Clear()
func (s *Sequence) ForEach(fn func(Item) error) error
func (s *Sequence) FilteredItems(predicate func(Item) bool) []Item

func NewItemSequence() *ItemSequence
func NewItemSequenceFromItems(items ...Item) *ItemSequence

func NewNestedSequence(maxLevel int) *NestedSequence
func (ns *NestedSequence) AppendNested(item Item) error
func (ns *NestedSequence) GetMaxNestingLevel() int
func (ns *NestedSequence) GetCurrentLevel() int
func (ns *NestedSequence) SetCurrentLevel(level int) error
```

## References

- [DICOM PS3.5 Section 5.2](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Sequences of Items (VR = SQ)
