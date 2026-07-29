package dataset

import (
	"strconv"

	"github.com/amrshadid/go-dicom/dataelem"
)

// parseTagHex converts a group/element hex pair into a tag.
//
// The error from parsing was previously discarded, leaving the value at zero on
// failure — so a caller passing anything unparseable asked for (0000,0000) and
// got it if the data set held Command Group Length, which every DIMSE command
// data set does. HasTag("garbage", "garbage") could therefore return true.
//
// Both components must parse completely. Sscanf stops at the first character it
// cannot use, so "00ZZ" would otherwise yield 0x0000 without complaint.
func parseTagHex(groupHex, elemHex string) (uint32, bool) {
	group, ok := parseHex16(groupHex)
	if !ok {
		return 0, false
	}
	elem, ok := parseHex16(elemHex)
	if !ok {
		return 0, false
	}
	return uint32(group)<<16 | uint32(elem), true
}

// parseHex16 parses a 16-bit hex value, requiring the whole string to be
// consumed.
func parseHex16(s string) (uint16, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(v), true
}

// GetByTag retrieves an element by group/element hex strings.
//
// Returns nil if either component is not valid hexadecimal, rather than
// silently treating it as zero.
func (ds *Dataset) GetByTag(groupHex, elemHex string) *dataelem.DataElement {
	tagVal, ok := parseTagHex(groupHex, elemHex)
	if !ok {
		return nil
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()

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
	tagVal, ok := parseTagHex(groupHex, elemHex)
	if !ok {
		return false
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	if !ok {
		return false
	}

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

	startGroup, ok := parseHex16(startGroupHex)
	if !ok {
		return nil
	}
	endGroup, ok := parseHex16(endGroupHex)
	if !ok {
		return nil
	}

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
