package dataset

import (
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
)

// GetByTag retrieves an element by group/element hex strings.
func (ds *Dataset) GetByTag(groupHex, elemHex string) *dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var group, elem uint16
	_, _ = fmt.Sscanf(groupHex, "%X", &group)
	_, _ = fmt.Sscanf(elemHex, "%X", &elem)

	tagVal := uint32(group)<<16 | uint32(elem)
	if el, exists := ds.elements[tagVal]; exists {
		return el
	}

	return nil
}

// GetFirst returns the first element in the dataset.
func (ds *Dataset) GetFirst() *dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if len(ds.order) == 0 {
		return nil
	}

	return ds.elements[ds.order[0]]
}

// GetLast returns the last element in the dataset.
func (ds *Dataset) GetLast() *dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if len(ds.order) == 0 {
		return nil
	}

	return ds.elements[ds.order[len(ds.order)-1]]
}

// HasTag checks if a tag exists by group/element hex values.
func (ds *Dataset) HasTag(groupHex, elemHex string) bool {
	elem := ds.GetByTag(groupHex, elemHex)
	return elem != nil
}

// RemoveByTag removes an element by group/element hex values.
func (ds *Dataset) RemoveByTag(groupHex, elemHex string) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	var group, elem uint16
	_, _ = fmt.Sscanf(groupHex, "%X", &group)
	_, _ = fmt.Sscanf(elemHex, "%X", &elem)

	tagVal := uint32(group)<<16 | uint32(elem)

	if _, exists := ds.elements[tagVal]; !exists {
		return false
	}

	delete(ds.elements, tagVal)

	for i, v := range ds.order {
		if v == tagVal {
			ds.order = append(ds.order[:i], ds.order[i+1:]...)
			break
		}
	}

	return true
}

// GetRange returns elements within a tag range.
func (ds *Dataset) GetRange(startGroupHex string, endGroupHex string) []*dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var startGroup, endGroup uint16
	_, _ = fmt.Sscanf(startGroupHex, "%X", &startGroup)
	_, _ = fmt.Sscanf(endGroupHex, "%X", &endGroup)

	var result []*dataelem.DataElement

	for _, tagVal := range ds.order {
		group := uint16(tagVal >> 16)
		if group >= startGroup && group <= endGroup {
			result = append(result, ds.elements[tagVal])
		}
	}

	return result
}

// IsEmpty returns true if the dataset has no elements.
func (ds *Dataset) IsEmpty() bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.elements) == 0
}

// FilterFunc is a function for filtering elements.
type FilterFunc func(*dataelem.DataElement) bool

// Filter returns elements that match the filter function.
func (ds *Dataset) Filter(fn FilterFunc) []*dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var result []*dataelem.DataElement

	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		if fn(elem) {
			result = append(result, elem)
		}
	}

	return result
}

// MapFunc applies a function to each element.
type MapFunc func(*dataelem.DataElement) interface{}

// Map applies a function to each element and returns results.
func (ds *Dataset) Map(fn MapFunc) []interface{} {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var result []interface{}

	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		result = append(result, fn(elem))
	}

	return result
}

// FindElement finds an element matching a condition.
func (ds *Dataset) FindElement(fn FilterFunc) *dataelem.DataElement {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		if fn(elem) {
			return elem
		}
	}

	return nil
}
