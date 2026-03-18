# Dataset

Thread-safe in-memory DICOM dataset with O(1) tag-based lookup, keyword access, sequence support, and pixel/image operations.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/dataset"

ds := dataset.NewDataset()

// Add elements
ds.Add(elem)
ds.AddByKeyword("PatientID", "LO", []byte("P123456"))

// Access elements
elem, exists := ds.Get(tag.New(0x0010, 0x0010))
elem, exists := ds.GetByKeyword("PatientID")
text, err := ds.GetString(tag.New(0x0010, 0x0010))

// Search and iterate
allElements := ds.GetAll()
textElems := ds.GetByVR("PN")
matches := ds.Find(func(elem *DataElement) bool { return true })

// Sequences
ds.AddSequence(tag, []*Dataset{itemDS})
items, _ := ds.GetSequenceItems(tag)
```

## API Reference

```go
func NewDataset() *Dataset
func NewDatasetWithCapacity(capacity int) *Dataset

// Element operations
func (ds *Dataset) Add(elem *DataElement) error
func (ds *Dataset) Get(tag Tag) (*DataElement, bool)
func (ds *Dataset) GetByKeyword(keyword string) (*DataElement, bool)
func (ds *Dataset) GetString(tag Tag) (string, error)
func (ds *Dataset) Remove(tag Tag) bool
func (ds *Dataset) UpdateElement(tag Tag, newValue []byte) error
func (ds *Dataset) GetAll() []*DataElement

// Search
func (ds *Dataset) Find(predicate func(*DataElement) bool) []*DataElement
func (ds *Dataset) GetByVR(vr string) []*DataElement
func (ds *Dataset) Contains(tag Tag) bool
func (ds *Dataset) Count() int

// Sequences
func (ds *Dataset) AddSequence(tag Tag, items []*Dataset) error
func (ds *Dataset) GetSequenceItems(tag Tag) ([]*Dataset, error)
func (ds *Dataset) GetNested(tags ...Tag) (*Dataset, error)
```

## References

- [DICOM PS3.5 Section 7](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Data Element Structure
- [DICOM PS3.6](https://dicom.nema.org/medical/dicom/current/output/html/part06.html) - Data Dictionary
