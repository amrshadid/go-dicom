package dataset

import (
	"fmt"
	"sort"
	"sync"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Dataset represents an in-memory DICOM dataset.
// It holds a collection of data elements indexed by tag for fast access.
// All operations are thread-safe using sync.RWMutex.
type Dataset struct {
	mu       sync.RWMutex                     // Protects concurrent access
	elements map[uint32]*dataelem.DataElement // Tag -> DataElement mapping
	order    []uint32                         // Maintains insertion order
	parent   *Dataset                         // Parent dataset (for sequences)

	// transferSyntaxUID records how the data set was encoded, when the caller
	// knows. It is not an element: (0002,0010) lives in the file meta header,
	// which is not part of the data set, so a Dataset built from a file would
	// otherwise have no way to learn how its own pixel data is compressed.
	//
	// Empty means unknown, which is the case for a Dataset assembled by hand.
	// Pixel access falls back to inspecting the data when it is empty.
	transferSyntaxUID string
}

// NewDataset creates a new empty DICOM dataset.
func NewDataset() *Dataset {
	return &Dataset{
		elements: make(map[uint32]*dataelem.DataElement),
		order:    make([]uint32, 0),
	}
}

// SetTransferSyntaxUID records how this data set was encoded.
//
// Readers set this from the file meta header. It matters for pixel data:
// whether PixelData is raw or encapsulated, and which codec compressed it, are
// properties of the transfer syntax and cannot be derived from the data set's
// own elements.
func (ds *Dataset) SetTransferSyntaxUID(uid string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.transferSyntaxUID = uid
}

// TransferSyntaxUID returns the transfer syntax this data set was encoded with,
// or an empty string if it is unknown.
func (ds *Dataset) TransferSyntaxUID() string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.transferSyntaxUID
}

// Add adds a data element to the dataset.
// If the tag already exists, it replaces the existing element.
// Elements are validated against the DICOM dictionary for semantic checks.
// Validation is non-fatal; warnings are printed but the element is added.
func (ds *Dataset) Add(elem *dataelem.DataElement) error {
	if elem == nil {
		return fmt.Errorf("element is nil")
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Extract tag from element
	var tagVal uint32
	var t tag.Tag
	tagIface := elem.GetTag()
	if tagAsTag, ok := tagIface.(tag.Tag); ok {
		tagVal = uint32(tagAsTag)
		t = tagAsTag
	} else {
		return fmt.Errorf("invalid tag type")
	}

	// Perform semantic validation against DICOM dictionary.
	// Non-fatal; logs a warning through the configurable logger and continues.
	if err := validateElementSemantics(t, elem); err != nil {
		config.Logger.Warn("dataset: semantic validation",
			"tag", t.String(), "err", err)
	}

	// Add to order if new tag
	if _, exists := ds.elements[tagVal]; !exists {
		ds.order = append(ds.order, tagVal)
	}

	// Store or replace element
	ds.elements[tagVal] = elem
	return nil
}

// Get retrieves a data element by tag.
func (ds *Dataset) Get(t tag.Tag) (*dataelem.DataElement, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	elem, exists := ds.elements[uint32(t)]
	return elem, exists
}

// Contains checks if a tag exists in the dataset.
func (ds *Dataset) Contains(t tag.Tag) bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	_, exists := ds.elements[uint32(t)]
	return exists
}

// Remove removes a data element by tag.
// Returns false if the tag does not exist in the dataset.
func (ds *Dataset) Remove(t tag.Tag) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	tagVal := uint32(t)
	if _, exists := ds.elements[tagVal]; !exists {
		return false
	}

	// Remove from map
	delete(ds.elements, tagVal)

	// Remove from insertion order slice
	for i, v := range ds.order {
		if v == tagVal {
			ds.order = append(ds.order[:i], ds.order[i+1:]...)
			break
		}
	}

	return true
}

// GetAll returns all data elements in insertion order.
func (ds *Dataset) GetAll() []*dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]*dataelem.DataElement, 0, len(ds.order))
	for _, tagVal := range ds.order {
		result = append(result, ds.elements[tagVal])
	}

	return result
}

// Length returns the number of data elements in the dataset.
func (ds *Dataset) Length() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	return len(ds.elements)
}

// Clear removes all data elements from the dataset.
func (ds *Dataset) Clear() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.elements = make(map[uint32]*dataelem.DataElement)
	ds.order = make([]uint32, 0)
}

// Clone creates a deep copy of the dataset.
//
// Nothing is shared with the original: byte values are copied, and sequences
// are rebuilt item by item so that a nested data set in the copy is a distinct
// object. Modifying either data set, at any depth, leaves the other unchanged.
//
// It previously copied only []byte values and shared everything else by
// reference, while documenting itself as a deep copy. A sequence therefore
// pointed at the same nested data sets in both, so editing an item of the
// original silently edited the copy — the exact thing a caller clones to avoid,
// and invisible until the shared item is written to.
func (ds *Dataset) Clone() *Dataset {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	cloned := NewDataset()

	// The transfer syntax is part of what the data set is, not an element of
	// it: it says how the values are encoded. Dropping it left a clone whose
	// pixel data could no longer be decoded from its own metadata, so pixel
	// access fell back to guessing the codec from structure.
	cloned.transferSyntaxUID = ds.transferSyntaxUID

	for _, tagVal := range ds.order {
		origElem := ds.elements[tagVal]

		clonedElem := dataelem.NewDataElement(
			origElem.GetTag(), origElem.GetVR(), cloneValue(origElem.GetValue()))
		cloned.elements[tagVal] = clonedElem
		cloned.order = append(cloned.order, tagVal)
	}

	return cloned
}

// cloneValue copies an element value so the copy shares no mutable state with
// the original.
//
// Anything not listed here is immutable or a scalar, and returning it as-is is
// correct. A new mutable value type added to the data model needs a case here,
// or Clone quietly goes back to being shallow for it.
func cloneValue(value interface{}) interface{} {
	switch v := value.(type) {
	case []byte:
		return copyBytes(v)

	case *sequence.Sequence:
		return cloneSequence(v)

	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out

	default:
		return value
	}
}

// cloneSequence rebuilds a sequence, cloning each item.
//
// Items are data sets, so this recurses through Clone and copies nested
// sequences to any depth.
func cloneSequence(seq *sequence.Sequence) *sequence.Sequence {
	if seq == nil {
		return nil
	}

	out := sequence.New()
	for i := 0; i < seq.Length(); i++ {
		item, err := seq.Get(i)
		if err != nil {
			continue
		}
		if nested, ok := item.(*Dataset); ok {
			_ = out.Append(nested.Clone())
			continue
		}
		// An item that is not a data set is not something this package
		// created; copy the reference rather than guess at its structure.
		_ = out.Append(item)
	}
	return out
}

// ForEach iterates over all elements in insertion order.
func (ds *Dataset) ForEach(fn func(*dataelem.DataElement) error) error {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	for _, tagVal := range ds.order {
		if err := fn(ds.elements[tagVal]); err != nil {
			return err
		}
	}

	return nil
}

// Tags returns all tags in the dataset in insertion order.
func (ds *Dataset) Tags() []tag.Tag {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]tag.Tag, 0, len(ds.order))
	for _, tagVal := range ds.order {
		result = append(result, tag.FromInt(tagVal))
	}

	return result
}

// GetByTagRange returns all elements within a tag range.
func (ds *Dataset) GetByTagRange(start, end tag.Tag) []*dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	startVal := uint32(start)
	endVal := uint32(end)

	result := make([]*dataelem.DataElement, 0)
	for _, tagVal := range ds.order {
		if tagVal >= startVal && tagVal <= endVal {
			result = append(result, ds.elements[tagVal])
		}
	}

	return result
}

// GetByGroup returns all elements in a specific group.
func (ds *Dataset) GetByGroup(group uint16) []*dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]*dataelem.DataElement, 0)
	for _, tagVal := range ds.order {
		t := tag.FromInt(tagVal)
		if t.Group() == group {
			result = append(result, ds.elements[tagVal])
		}
	}

	return result
}

// GetByVR returns all elements with a specific VR.
func (ds *Dataset) GetByVR(vr dataelem.VR) []*dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]*dataelem.DataElement, 0)
	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		if elem.GetVR() == vr {
			result = append(result, elem)
		}
	}

	return result
}

// FilteredElements returns elements that match the filter function.
func (ds *Dataset) FilteredElements(filter func(*dataelem.DataElement) bool) []*dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]*dataelem.DataElement, 0)
	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		if filter(elem) {
			result = append(result, elem)
		}
	}

	return result
}

// Merge merges another dataset into this one.
// Elements from the other dataset override matching elements in this dataset.
// Returns error if other is nil.
func (ds *Dataset) Merge(other *Dataset) error {
	if other == nil {
		return fmt.Errorf("other dataset is nil")
	}

	// Snapshot other dataset's elements to avoid holding locks
	other.mu.RLock()
	otherElems := make([]*dataelem.DataElement, 0, len(other.order))
	for _, tagVal := range other.order {
		otherElems = append(otherElems, other.elements[tagVal])
	}
	other.mu.RUnlock()

	// Add each element from other to this dataset
	for _, elem := range otherElems {
		if err := ds.Add(elem); err != nil {
			return err
		}
	}

	return nil
}

// String returns a string representation of the dataset.
func (ds *Dataset) String() string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var result string
	result += fmt.Sprintf("Dataset with %d elements:\n", len(ds.elements))
	for i, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		t := tag.FromInt(tagVal)
		vr := elem.GetVR()
		value := elem.GetValue()
		var valueLen int
		if b, ok := value.([]byte); ok {
			valueLen = len(b)
		}
		result += fmt.Sprintf("  [%d] %s (%s): %d bytes\n", i, t.Hex(), vr, valueLen)
	}

	return result
}

// Helper function to copy byte slice
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	copied := make([]byte, len(b))
	copy(copied, b)
	return copied
}

// validateElementSemantics performs semantic validation of a data element using the DICOM dictionary.
// This is informational and non-fatal; errors are logged but do not prevent adding the element.
func validateElementSemantics(t tag.Tag, elem *dataelem.DataElement) error {
	dict := tag.GlobalDictionary()
	info := dict.Get(t)

	// Check if tag exists in dictionary (private tags are allowed without entry)
	if info == nil {
		if t.IsPrivate() {
			return nil
		}
		return fmt.Errorf("unknown standard tag: %s", t.String())
	}

	// Check for retired tags
	if info.Retired {
		return fmt.Errorf("tag %s (%s) is retired", t.String(), info.Name)
	}

	// Validate VR if present in element and dictionary
	elemVR := elem.GetVR()
	if elemVR != "" && info.VR != "" {
		if string(elemVR) != info.VR && !isValidVRVariant(string(elemVR), info.VR) {
			return fmt.Errorf("VR mismatch for %s: expected %s, got %s",
				t.String(), info.VR, elemVR)
		}
	}

	return nil
}

// isValidVRVariant checks if a VR is a valid variant of the expected VR.
// Handles DICOM cases where multiple VRs are acceptable (e.g., "OB or OW").
func isValidVRVariant(actual, expected string) bool {
	switch expected {
	case "OB or OW":
		return actual == "OB" || actual == "OW"
	case "OB or OD":
		return actual == "OB" || actual == "OD"
	case "US or SS":
		return actual == "US" || actual == "SS"
	case "OW or OB":
		return actual == "OW" || actual == "OB"
	default:
		return false
	}
}

// SortedDataset returns a new dataset with elements sorted by tag value.
// Elements are returned in ascending tag order in the new dataset.
func (ds *Dataset) SortedDataset() *Dataset {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	// Copy and sort the order
	sortedOrder := make([]uint32, len(ds.order))
	copy(sortedOrder, ds.order)
	sort.Slice(sortedOrder, func(i, j int) bool {
		return sortedOrder[i] < sortedOrder[j]
	})

	// Create new dataset with sorted element order
	sorted := NewDataset()
	for _, tagVal := range sortedOrder {
		sorted.elements[tagVal] = ds.elements[tagVal]
		sorted.order = append(sorted.order, tagVal)
	}

	return sorted
}

// GetValue retrieves the raw value bytes for a tag.
func (ds *Dataset) GetValue(t tag.Tag) []byte {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	elem, exists := ds.elements[uint32(t)]
	if !exists {
		return nil
	}

	value := elem.GetValue()
	if b, ok := value.([]byte); ok {
		return b
	}
	return nil
}

// SetValue sets the raw value bytes for a tag.
// Creates a new element if it doesn't exist.
func (ds *Dataset) SetValue(t tag.Tag, value []byte) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	tagVal := uint32(t)
	elem, exists := ds.elements[tagVal]

	if exists {
		elem.SetValue(copyBytes(value))
	} else {
		// Create new element with OB VR (generic)
		elem := dataelem.NewDataElement(t, dataelem.OB, copyBytes(value))
		if _, exists := ds.elements[tagVal]; !exists {
			ds.order = append(ds.order, tagVal)
		}
		ds.elements[tagVal] = elem
	}

	return nil
}

// UpdateElement updates only the value of an existing element.
func (ds *Dataset) UpdateElement(t tag.Tag, value []byte) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	tagVal := uint32(t)
	elem, exists := ds.elements[tagVal]
	if !exists {
		return fmt.Errorf("element with tag %s not found", t.Hex())
	}

	elem.SetValue(copyBytes(value))
	return nil
}

// GetElements returns elements matching multiple tags.
func (ds *Dataset) GetElements(tags ...tag.Tag) []*dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	result := make([]*dataelem.DataElement, 0, len(tags))
	tagMap := make(map[uint32]bool)
	for _, t := range tags {
		tagMap[uint32(t)] = true
	}

	for _, tagVal := range ds.order {
		if tagMap[tagVal] {
			result = append(result, ds.elements[tagVal])
		}
	}

	return result
}

// RemoveElements removes multiple elements by tags.
func (ds *Dataset) RemoveElements(tags ...tag.Tag) int {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	count := 0
	for _, t := range tags {
		tagVal := uint32(t)
		if _, exists := ds.elements[tagVal]; exists {
			delete(ds.elements, tagVal)
			count++

			// Remove from order
			for i, v := range ds.order {
				if v == tagVal {
					ds.order = append(ds.order[:i], ds.order[i+1:]...)
					break
				}
			}
		}
	}

	return count
}

// GetStatistics returns detailed statistics about the dataset.
func (ds *Dataset) GetStatistics() Statistics {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	stats := Statistics{
		TotalElements: len(ds.elements),
		ByVR:          make(map[string]int),
		ByGroup:       make(map[uint16]int),
	}

	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		vr := elem.GetVR()
		value := elem.GetValue()

		if b, ok := value.([]byte); ok {
			stats.TotalBytes += len(b)
		}
		stats.ByVR[string(vr)]++

		t := tag.FromInt(tagVal)
		stats.ByGroup[t.Group()]++
	}

	return stats
}
