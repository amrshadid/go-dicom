package dataset

import (
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// PrivateBlock represents a private tag block for a specific creator.
// In DICOM, private tags use (group, element_offset, creator_string) addressing.
// Elements 0x0010-0x00FF in odd groups are private creator strings.
// Elements 0x0100-0xFFFF in odd groups are private data, with the upper byte
// indicating which creator to use.
type PrivateBlock struct {
	dataset *Dataset
	group   uint16
	creator string
	block   uint8 // The block number (0x10-0xFF in the creator element)
}

// PrivateBlock gets or creates a private block for the given group and creator.
// The creator string is registered in the dataset if not already present.
// Returns nil if the group is not odd (not a private group).
func (ds *Dataset) PrivateBlock(group uint16, creator string) (*PrivateBlock, error) {
	if (group & 0x0001) == 0 {
		return nil, fmt.Errorf("group 0x%04X is not a private group (must be odd)", group)
	}

	if creator == "" {
		return nil, fmt.Errorf("private creator string cannot be empty")
	}

	// Look for existing private creator
	block, found := ds.findPrivateCreator(group, creator)
	if found {
		return &PrivateBlock{
			dataset: ds,
			group:   group,
			creator: creator,
			block:   block,
		}, nil
	}

	// Find available block (0x10-0xFF range)
	block, err := ds.findAvailablePrivateBlock(group)
	if err != nil {
		return nil, err
	}

	// Register the private creator
	creatorTag := tag.New(group, uint16(block))
	creatorElem := dataelem.NewDataElement(creatorTag, dataelem.LO, []byte(creator))
	if err := ds.Add(creatorElem); err != nil {
		return nil, fmt.Errorf("failed to register private creator: %w", err)
	}

	return &PrivateBlock{
		dataset: ds,
		group:   group,
		creator: creator,
		block:   block,
	}, nil
}

// findPrivateCreator searches for an existing private creator string.
// Returns the block number if found.
func (ds *Dataset) findPrivateCreator(group uint16, creator string) (uint8, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	// Check elements 0x0010-0x00FF for matching creator string
	for elem := uint16(0x0010); elem <= 0x00FF; elem++ {
		t := tag.New(group, elem)
		if element, exists := ds.elements[uint32(t)]; exists {
			value := element.GetValue()
			if b, ok := value.([]byte); ok {
				// Trim null padding and whitespace
				existingCreator := strings.TrimRight(string(b), "\x00 ")
				if existingCreator == creator {
					return uint8(elem), true
				}
			}
		}
	}

	return 0, false
}

// findAvailablePrivateBlock finds an available block number in the 0x10-0xFF range.
func (ds *Dataset) findAvailablePrivateBlock(group uint16) (uint8, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	// Try blocks 0x10-0xFF
	for block := uint16(0x0010); block <= 0x00FF; block++ {
		t := tag.New(group, block)
		if _, exists := ds.elements[uint32(t)]; !exists {
			return uint8(block), nil
		}
	}

	return 0, fmt.Errorf("no available private blocks in group 0x%04X (all 256 slots occupied)", group)
}

// AddNew adds a new private data element to this block by offset.
// The offset should be in the range 0x00-0xFF (the lower byte of the element number).
func (pb *PrivateBlock) AddNew(offset uint8, vr dataelem.VR, value interface{}) error {
	if offset == 0 {
		return fmt.Errorf("offset 0x00 is reserved")
	}

	// Calculate full element number: (block << 8) | offset
	elementNumber := (uint16(pb.block) << 8) | uint16(offset)
	t := tag.New(pb.group, elementNumber)

	elem := dataelem.NewDataElement(t, vr, value)
	return pb.dataset.Add(elem)
}

// Get retrieves a private data element by offset.
func (pb *PrivateBlock) Get(offset uint8) (*dataelem.DataElement, bool) {
	elementNumber := (uint16(pb.block) << 8) | uint16(offset)
	t := tag.New(pb.group, elementNumber)
	return pb.dataset.Get(t)
}

// Contains checks if a private element exists at the given offset.
func (pb *PrivateBlock) Contains(offset uint8) bool {
	elementNumber := (uint16(pb.block) << 8) | uint16(offset)
	t := tag.New(pb.group, elementNumber)
	return pb.dataset.Contains(t)
}

// Remove removes a private data element by offset.
func (pb *PrivateBlock) Remove(offset uint8) bool {
	elementNumber := (uint16(pb.block) << 8) | uint16(offset)
	t := tag.New(pb.group, elementNumber)
	return pb.dataset.Remove(t)
}

// SetValue sets the value of a private element by offset.
// Creates a new element if it doesn't exist.
func (pb *PrivateBlock) SetValue(offset uint8, value []byte) error {
	if offset == 0 {
		return fmt.Errorf("offset 0x00 is reserved")
	}

	elementNumber := (uint16(pb.block) << 8) | uint16(offset)
	t := tag.New(pb.group, elementNumber)
	return pb.dataset.SetValue(t, value)
}

// GetValue retrieves the raw value of a private element by offset.
func (pb *PrivateBlock) GetValue(offset uint8) []byte {
	elementNumber := (uint16(pb.block) << 8) | uint16(offset)
	t := tag.New(pb.group, elementNumber)
	return pb.dataset.GetValue(t)
}

// GetTag returns the full DICOM tag for a private element at the given offset.
func (pb *PrivateBlock) GetTag(offset uint8) tag.Tag {
	elementNumber := (uint16(pb.block) << 8) | uint16(offset)
	return tag.New(pb.group, elementNumber)
}

// Group returns the group number of this private block.
func (pb *PrivateBlock) Group() uint16 {
	return pb.group
}

// Creator returns the private creator string for this block.
func (pb *PrivateBlock) Creator() string {
	return pb.creator
}

// Block returns the block number (0x10-0xFF).
func (pb *PrivateBlock) Block() uint8 {
	return pb.block
}

// String returns a string representation of the private block.
func (pb *PrivateBlock) String() string {
	return fmt.Sprintf("PrivateBlock{group=0x%04X, creator=%q, block=0x%02X}", pb.group, pb.creator, pb.block)
}

// GetPrivateItem retrieves a private data element using group, offset, and creator.
// This is a convenience method that handles the private block lookup automatically.
func (ds *Dataset) GetPrivateItem(group uint16, offset uint8, creator string) (*dataelem.DataElement, error) {
	block, found := ds.findPrivateCreator(group, creator)
	if !found {
		return nil, fmt.Errorf("private creator %q not found in group 0x%04X", creator, group)
	}

	elementNumber := (uint16(block) << 8) | uint16(offset)
	t := tag.New(group, elementNumber)

	elem, exists := ds.Get(t)
	if !exists {
		return nil, fmt.Errorf("private element at offset 0x%02X not found", offset)
	}

	return elem, nil
}

// GetPrivateValue retrieves the raw value of a private element.
func (ds *Dataset) GetPrivateValue(group uint16, offset uint8, creator string) ([]byte, error) {
	elem, err := ds.GetPrivateItem(group, offset, creator)
	if err != nil {
		return nil, err
	}

	value := elem.GetValue()
	if b, ok := value.([]byte); ok {
		return b, nil
	}

	return nil, fmt.Errorf("value is not a byte slice")
}

// HasPrivateCreator checks if a private creator exists in the given group.
func (ds *Dataset) HasPrivateCreator(group uint16, creator string) bool {
	_, found := ds.findPrivateCreator(group, creator)
	return found
}

// GetAllPrivateCreators returns all private creator strings in the dataset.
// Returns a map of group number to list of creator strings.
func (ds *Dataset) GetAllPrivateCreators() map[uint16][]string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	creators := make(map[uint16][]string)

	for tagVal, elem := range ds.elements {
		t := tag.FromInt(tagVal)
		if t.IsPrivateCreator() {
			group := t.Group()
			value := elem.GetValue()
			if b, ok := value.([]byte); ok {
				creator := strings.TrimRight(string(b), "\x00 ")
				if creator != "" {
					creators[group] = append(creators[group], creator)
				}
			}
		}
	}

	return creators
}

// GetPrivateElements returns all private data elements for a specific creator.
func (ds *Dataset) GetPrivateElements(group uint16, creator string) []*dataelem.DataElement {
	block, found := ds.findPrivateCreator(group, creator)
	if !found {
		return nil
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var elements []*dataelem.DataElement

	// Find all elements in the block's range (block00 - blockFF)
	startElem := (uint16(block) << 8)
	endElem := startElem | 0x00FF

	for elem := startElem; elem <= endElem; elem++ {
		if elem&0x00FF == 0 {
			continue // Skip the offset 0x00
		}
		t := tag.New(group, elem)
		if element, exists := ds.elements[uint32(t)]; exists {
			elements = append(elements, element)
		}
	}

	return elements
}

// RemovePrivateBlock removes all private data elements for a specific creator,
// including the private creator tag itself.
func (ds *Dataset) RemovePrivateBlock(group uint16, creator string) error {
	block, found := ds.findPrivateCreator(group, creator)
	if !found {
		return fmt.Errorf("private creator %q not found in group 0x%04X", creator, group)
	}

	// Remove all data elements in this block
	startElem := (uint16(block) << 8)
	endElem := startElem | 0x00FF

	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Remove data elements
	for elem := startElem; elem <= endElem; elem++ {
		t := tag.New(group, elem)
		tagVal := uint32(t)
		if _, exists := ds.elements[tagVal]; exists {
			delete(ds.elements, tagVal)
			// Remove from order
			for i, v := range ds.order {
				if v == tagVal {
					ds.order = append(ds.order[:i], ds.order[i+1:]...)
					break
				}
			}
		}
	}

	// Remove the private creator tag
	creatorTag := tag.New(group, uint16(block))
	creatorTagVal := uint32(creatorTag)
	delete(ds.elements, creatorTagVal)
	for i, v := range ds.order {
		if v == creatorTagVal {
			ds.order = append(ds.order[:i], ds.order[i+1:]...)
			break
		}
	}

	return nil
}

// GetAllPrivateTags returns all private tags in the dataset (both creators and data).
func (ds *Dataset) GetAllPrivateTags() []tag.Tag {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var privateTags []tag.Tag

	for _, tagVal := range ds.order {
		t := tag.FromInt(tagVal)
		if t.IsPrivate() {
			privateTags = append(privateTags, t)
		}
	}

	return privateTags
}
