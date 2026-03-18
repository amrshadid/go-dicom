package tag

// GetInfo returns the DICOM dictionary information for this tag.
// Returns nil if the tag is not in the standard dictionary.
func (t Tag) GetInfo() *TagInfo {
	return GlobalDictionary().Get(t)
}

// GetName returns the human-readable name for this tag.
func (t Tag) GetName() string {
	return GlobalDictionary().GetName(t)
}

// GetVR returns the Value Representation for this tag.
func (t Tag) GetVR() string {
	return GlobalDictionary().GetVR(t)
}

// GetVM returns the Value Multiplicity for this tag.
func (t Tag) GetVM() string {
	return GlobalDictionary().GetVM(t)
}

// GetKeyword returns the keyword/identifier for this tag.
func (t Tag) GetKeyword() string {
	return GlobalDictionary().GetKeyword(t)
}

// IsRetired returns whether this tag is retired.
func (t Tag) IsRetired() bool {
	return GlobalDictionary().IsRetired(t)
}

// Exists checks if this tag is in the DICOM dictionary.
func (t Tag) Exists() bool {
	return GlobalDictionary().Get(t) != nil
}
