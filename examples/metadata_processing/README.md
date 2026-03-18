# Metadata Processing Examples

This directory contains examples demonstrating how to work with DICOM metadata, sequences, and hierarchical data.

## Examples Overview

### 1. sequences.go
Working with DICOM sequences and nested datasets.

**Demonstrates:**
- Creating datasets with nested sequences
- Adding sequences with `AddSequence()`
- Accessing sequence items
- Iterating through sequence items
- Adding and removing sequence items
- Modifying nested datasets
- Getting sequence counts and information

**Run:**
```bash
go run sequences.go
```

**Example Output:**
```
=== Working with DICOM Sequences ===

Creating block datasets...
  Block1 created
  Block2 created

Creating treatment plan with beam sequence...
  Adding BeamSequence to plan...
  BeamSequence added successfully

Accessing sequence items...
  Number of beam items: 1
  First beam information:
    Beam Name: Beam1
    Dose Rate: 100

  Adding BlockSequence to first beam...
  BlockSequence added
  Number of blocks: 2

Iterating through all beam blocks...
  Beam 0:
    Number of blocks: 2
    Block 0:
      Name: Block1
    Block 1:
      Name: Block2

Adding a third block to the first beam...
  Block3 added successfully
  Total blocks: 3

Deleting second block from first beam...
  Block deleted
  Remaining blocks: 2

=== Final Dataset Summary ===
Total elements in plan dataset: 12
Number of beams: 1
  Beam 0: 8 elements
    Blocks: 2

Done!
```

---

## Common Patterns

### Creating a Sequence
```go
import (
    "github.com/amrshadid/go-dicom/sequence"
    "github.com/amrshadid/go-dicom/dataset"
)

// Create datasets to put in sequence
ds1 := dataset.NewDataset()
ds1.Add(dataelem.NewDataElement(...))

ds2 := dataset.NewDataset()
ds2.Add(dataelem.NewDataElement(...))

// Create sequence
seq, err := sequence.NewSequence([]*dataset.Dataset{ds1, ds2})
if err != nil {
    log.Fatal(err)
}

// Add to parent dataset
parentDS.AddSequence(tag.New(0x300A, 0x00B0), seq)
```

### Accessing Sequence Items
```go
import "github.com/amrshadid/go-dicom/tag"

// Get the sequence element
if seqElem, ok := ds.Get(tag.New(0x300A, 0x00B0)); ok {
    // Get the sequence value
    if seqData, ok := seqElem.GetValue().(*sequence.Sequence); ok {
        // Access individual items
        for i := 0; i < seqData.Length(); i++ {
            item, err := seqData.Item(i)
            if err != nil {
                continue
            }
            // Process item
            fmt.Printf("Item %d: %d elements\n", i, item.Length())
        }
    }
}
```

### Adding Items to a Sequence
```go
// Append to existing sequence
if seqElem, ok := ds.Get(tag.New(0x300A, 0x00B0)); ok {
    if seqData, ok := seqElem.GetValue().(*sequence.Sequence); ok {
        newItem := dataset.NewDataset()
        newItem.Add(dataelem.NewDataElement(...))
        seqData.Append(newItem)
    }
}
```

### Removing Sequence Items
```go
// Delete item by index
if seqElem, ok := ds.Get(tag.New(0x300A, 0x00B0)); ok {
    if seqData, ok := seqElem.GetValue().(*sequence.Sequence); ok {
        seqData.Delete(1)  // Delete second item (0-indexed)
    }
}
```

### Nested Sequences
```go
// Sequences can contain nested sequences
// Access grandparent -> parent -> child
if beamSeq, ok := planDS.Get(tag.New(0x300A, 0x00B0)); ok {
    if beamSeqData, ok := beamSeq.GetValue().(*sequence.Sequence); ok {
        beam0, _ := beamSeqData.Item(0)

        if blockSeq, ok := beam0.Get(tag.New(0x300A, 0x00F4)); ok {
            if blockSeqData, ok := blockSeq.GetValue().(*sequence.Sequence); ok {
                // Access nested items
                block0, _ := blockSeqData.Item(0)
            }
        }
    }
}
```

---

## Common DICOM Sequences

### Beam Sequence (0x300A, 0x00B0)
Contains beam information in treatment plans.
```
Plan Dataset
└── BeamSequence
    ├── Beam 0
    │   ├── BeamNumber
    │   ├── BeamName
    │   └── BlockSequence
    │       ├── Block 0
    │       └── Block 1
    └── Beam 1
```

### Referenced Study Sequence (0x0008, 0x1110)
Contains references to related studies.
```
Dataset
└── ReferencedStudySequence
    ├── Study 0
    │   ├── ReferencedSOPSequence
    │   └── ReferencedImageSequence
    └── Study 1
```

### Request Attributes Sequence (0x0040, 0x0275)
Contains request information.
```
Dataset
└── RequestAttributesSequence
    ├── Request 0
    │   ├── ScheduledProcedure
    │   └── ReferencedStudySequence
    └── Request 1
```

---

## Tips

1. **Always check Length()** before accessing sequence items
2. **Use defensive programming**: Check if `ok` is true after type assertion
3. **Handle errors** when creating sequences and accessing items
4. **Clone nested datasets** before modifying them to avoid unintended side effects
5. **Keep track of sequence indices** when modifying sequences during iteration
6. **Use meaningful variable names** for nested sequences (beam0, block0, etc.)

---

## Dataset Properties in Sequences

When accessing datasets within sequences:

```go
// Get element count
elementCount := item.Length()

// Get all elements
elements := item.GetAll()

// Check for specific element
if item.Contains(tag.New(0x300A, 0x00B0)) {
    elem, _ := item.Get(tag.New(0x300A, 0x00B0))
}

// Get statistics
stats := item.GetStatistics()
```

---

For more examples, see the parent [EXAMPLES.md](../EXAMPLES.md) document.
