package tag

import "fmt"

// DicomDictionary provides access to DICOM tag metadata
type DicomDictionary struct {
	standard map[uint32]*TagInfo
	repeater map[string]*RepeaterTag
	keyword  map[string]uint32
}

var globalDict *DicomDictionary

// init initializes the global dictionary
func init() {
	globalDict = &DicomDictionary{
		standard: StandardDicomDictionary,
		repeater: RepeaterDictionary,
		keyword:  buildKeywordIndex(),
	}
}

// buildKeywordIndex creates reverse lookup from keyword to tag
func buildKeywordIndex() map[string]uint32 {
	m := make(map[string]uint32)
	for tagVal, info := range StandardDicomDictionary {
		if info.Keyword != "" {
			m[info.Keyword] = tagVal
		}
	}
	return m
}

// Get returns metadata for a tag
func (d *DicomDictionary) Get(tag Tag) *TagInfo {
	tagVal := uint32(tag)

	// Check standard tags
	if info, ok := d.standard[tagVal]; ok {
		return info
	}

	// Check repeater tags
	if mask := d.matchRepeater(tag); mask != "" {
		if r, ok := d.repeater[mask]; ok {
			return &TagInfo{
				Tag:     tagVal,
				VR:      r.VR,
				VM:      r.VM,
				Name:    r.Name,
				Retired: r.Retired,
				Keyword: r.Keyword,
			}
		}
	}

	return nil
}

// GetByKeyword returns a tag by its keyword
func (d *DicomDictionary) GetByKeyword(keyword string) Tag {
	if tagVal, ok := d.keyword[keyword]; ok {
		return Tag(tagVal)
	}
	return Tag(0)
}

// GetVR returns the VR for a tag
func (d *DicomDictionary) GetVR(tag Tag) string {
	info := d.Get(tag)
	if info != nil {
		return info.VR
	}
	return ""
}

// GetVM returns the VM for a tag
func (d *DicomDictionary) GetVM(tag Tag) string {
	info := d.Get(tag)
	if info != nil {
		return info.VM
	}
	return ""
}

// GetName returns the human-readable name for a tag
func (d *DicomDictionary) GetName(tag Tag) string {
	info := d.Get(tag)
	if info != nil {
		return info.Name
	}
	return ""
}

// GetKeyword returns the keyword for a tag
func (d *DicomDictionary) GetKeyword(tag Tag) string {
	info := d.Get(tag)
	if info != nil {
		return info.Keyword
	}
	return ""
}

// IsRetired checks if a tag is retired
func (d *DicomDictionary) IsRetired(tag Tag) bool {
	info := d.Get(tag)
	if info != nil {
		return info.Retired
	}
	return false
}

// matchRepeater checks if tag matches a repeater pattern
func (d *DicomDictionary) matchRepeater(tag Tag) string {
	group := tag.Group()
	element := tag.Element()
	tagStr := fmt.Sprintf("%04X%04X", group, element)

	for mask := range d.repeater {
		if matchesMask(tagStr, mask) {
			return mask
		}
	}

	return ""
}

// matchesMask checks if a tag string matches a mask pattern
func matchesMask(tagStr, mask string) bool {
	if len(tagStr) != len(mask) {
		return false
	}

	for i := 0; i < len(mask); i++ {
		if mask[i] == 'x' {
			continue
		}
		if tagStr[i] != mask[i] {
			return false
		}
	}

	return true
}

// GlobalDictionary returns the global DICOM dictionary
func GlobalDictionary() *DicomDictionary {
	return globalDict
}
