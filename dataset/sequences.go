package dataset

import (
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// AddSequence adds a sequence element to the dataset.
// The sequence value should contain Dataset items for proper DICOM compliance.
func (ds *Dataset) AddSequence(t tag.Tag, seq *sequence.Sequence) error {
	if seq == nil {
		return fmt.Errorf("sequence is nil")
	}

	// Create a data element with VR "SQ" (Sequence)
	elem := dataelem.NewDataElement(t, dataelem.SQ, seq)

	// Set parent relationship for all Dataset items in the sequence
	items := seq.Items()
	for _, item := range items {
		if childDS, ok := item.(*Dataset); ok {
			childDS.mu.Lock()
			childDS.parent = ds
			childDS.mu.Unlock()
		}
	}

	return ds.Add(elem)
}

// GetSequence retrieves a sequence element from the dataset.
// Returns nil if the tag doesn't exist or isn't a sequence.
func (ds *Dataset) GetSequence(t tag.Tag) (*sequence.Sequence, error) {
	elem, exists := ds.Get(t)
	if !exists {
		return nil, fmt.Errorf("tag %s not found", t.Hex())
	}

	// Check if VR is SQ
	if elem.GetVR() != dataelem.SQ {
		return nil, fmt.Errorf("tag %s is not a sequence (VR=%s)", t.Hex(), elem.GetVR())
	}

	// Extract sequence from value
	value := elem.GetValue()
	seq, ok := value.(*sequence.Sequence)
	if !ok {
		return nil, fmt.Errorf("tag %s has invalid sequence value", t.Hex())
	}

	return seq, nil
}

// HasSequence checks if a tag contains a sequence element.
func (ds *Dataset) HasSequence(t tag.Tag) bool {
	elem, exists := ds.Get(t)
	if !exists {
		return false
	}
	return elem.GetVR() == dataelem.SQ
}

// CreateSequence creates a new empty sequence and adds it to the dataset.
// Returns the created sequence for further manipulation.
func (ds *Dataset) CreateSequence(t tag.Tag) (*sequence.Sequence, error) {
	seq := sequence.New()
	if err := ds.AddSequence(t, seq); err != nil {
		return nil, err
	}
	return seq, nil
}

// AppendToSequence appends an item to an existing sequence in the dataset.
// If the sequence doesn't exist, it creates a new one.
func (ds *Dataset) AppendToSequence(t tag.Tag, item interface{}) error {
	seq, err := ds.GetSequence(t)
	if err != nil {
		// Sequence doesn't exist, create it
		seq, err = ds.CreateSequence(t)
		if err != nil {
			return err
		}
	}

	// Set parent if item is a Dataset
	if childDS, ok := item.(*Dataset); ok {
		childDS.mu.Lock()
		childDS.parent = ds
		childDS.mu.Unlock()
	}

	return seq.Append(item)
}

// GetSequenceItem retrieves a specific item from a sequence by index.
func (ds *Dataset) GetSequenceItem(t tag.Tag, index int) (interface{}, error) {
	seq, err := ds.GetSequence(t)
	if err != nil {
		return nil, err
	}

	return seq.Get(index)
}

// GetSequenceDataset retrieves a specific Dataset from a sequence by index.
// Returns an error if the item at the index is not a Dataset.
func (ds *Dataset) GetSequenceDataset(t tag.Tag, index int) (*Dataset, error) {
	item, err := ds.GetSequenceItem(t, index)
	if err != nil {
		return nil, err
	}

	childDS, ok := item.(*Dataset)
	if !ok {
		return nil, fmt.Errorf("item at index %d is not a Dataset", index)
	}

	return childDS, nil
}

// SequenceLength returns the number of items in a sequence.
func (ds *Dataset) SequenceLength(t tag.Tag) (int, error) {
	seq, err := ds.GetSequence(t)
	if err != nil {
		return 0, err
	}
	return seq.Length(), nil
}

// RemoveSequence removes a sequence element from the dataset.
func (ds *Dataset) RemoveSequence(t tag.Tag) error {
	// Get sequence first to clear parent references
	seq, err := ds.GetSequence(t)
	if err != nil {
		return err
	}

	// Clear parent references for all Dataset items
	items := seq.Items()
	for _, item := range items {
		if childDS, ok := item.(*Dataset); ok {
			childDS.mu.Lock()
			childDS.parent = nil
			childDS.mu.Unlock()
		}
	}

	// Remove the element
	if !ds.Remove(t) {
		return fmt.Errorf("failed to remove sequence tag %s", t.Hex())
	}

	return nil
}

// IterAll recursively iterates over all elements in the dataset and nested sequences.
// The callback function receives the current dataset and element.
// Returns early if the callback returns an error.
func (ds *Dataset) IterAll(callback func(*Dataset, *dataelem.DataElement) error) error {
	return ds.iterAllRecursive(callback, 0, 100) // max depth 100
}

// iterAllRecursive is the internal recursive iterator with depth checking.
func (ds *Dataset) iterAllRecursive(callback func(*Dataset, *dataelem.DataElement) error, depth, maxDepth int) error {
	if depth > maxDepth {
		return fmt.Errorf("maximum nesting depth exceeded: %d", maxDepth)
	}

	// Iterate over all elements in this dataset
	elems := ds.GetAll()
	for _, elem := range elems {
		// Call callback for this element
		if err := callback(ds, elem); err != nil {
			return err
		}

		// If element is a sequence, recurse into it
		if elem.GetVR() == dataelem.SQ {
			value := elem.GetValue()
			if seq, ok := value.(*sequence.Sequence); ok {
				items := seq.Items()
				for _, item := range items {
					if childDS, ok := item.(*Dataset); ok {
						if err := childDS.iterAllRecursive(callback, depth+1, maxDepth); err != nil {
							return err
						}
					}
				}
			}
		}
	}

	return nil
}

// FindInSequences searches for datasets within sequences that match a predicate.
// Returns all matching datasets found in any nested sequences.
func (ds *Dataset) FindInSequences(predicate func(*Dataset) bool) []*Dataset {
	var results []*Dataset

	_ = ds.IterAll(func(currentDS *Dataset, elem *dataelem.DataElement) error {
		// Check if current dataset matches
		if predicate(currentDS) && currentDS != ds {
			results = append(results, currentDS)
		}
		return nil
	})

	return results
}

// GetAllSequences returns all sequence elements in the dataset (non-recursive).
func (ds *Dataset) GetAllSequences() map[tag.Tag]*sequence.Sequence {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	sequences := make(map[tag.Tag]*sequence.Sequence)
	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		if elem.GetVR() == dataelem.SQ {
			value := elem.GetValue()
			if seq, ok := value.(*sequence.Sequence); ok {
				t := tag.FromInt(tagVal)
				sequences[t] = seq
			}
		}
	}

	return sequences
}

// CloneWithSequences creates a deep copy of the dataset including all nested sequences.
func (ds *Dataset) CloneWithSequences() *Dataset {
	cloned := ds.Clone()

	// Deep clone sequences
	seqs := ds.GetAllSequences()
	for t, seq := range seqs {
		clonedSeq := sequence.New()
		items := seq.Items()
		for _, item := range items {
			if childDS, ok := item.(*Dataset); ok {
				// Recursively clone nested datasets
				clonedChild := childDS.CloneWithSequences()
				clonedChild.parent = cloned
				_ = clonedSeq.Append(clonedChild)
			} else {
				_ = clonedSeq.Append(item)
			}
		}

		// Replace sequence in cloned dataset
		elem := dataelem.NewDataElement(t, dataelem.SQ, clonedSeq)
		cloned.mu.Lock()
		cloned.elements[uint32(t)] = elem
		cloned.mu.Unlock()
	}

	return cloned
}

// CountNestedDatasets returns the total number of nested datasets in all sequences.
// This is useful for understanding the complexity of the dataset hierarchy.
// Does not count the root dataset itself, only nested children.
func (ds *Dataset) CountNestedDatasets() int {
	// Track unique datasets to avoid counting duplicates
	seen := make(map[*Dataset]bool)

	// Recursively count datasets in sequences
	ds.countDatasetsRecursive(seen)

	// Don't count the root itself
	delete(seen, ds)

	return len(seen)
}

// countDatasetsRecursive is a helper to recursively count all datasets in the hierarchy
func (ds *Dataset) countDatasetsRecursive(seen map[*Dataset]bool) {
	// Mark this dataset as seen
	seen[ds] = true

	// Get all sequences and count their child datasets
	sequences := ds.GetAllSequences()
	for _, seq := range sequences {
		items := seq.Items()
		for _, item := range items {
			if childDS, ok := item.(*Dataset); ok {
				// Recursively count children
				childDS.countDatasetsRecursive(seen)
			}
		}
	}
}
