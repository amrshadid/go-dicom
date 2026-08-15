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

// IsGroupLength reports whether this is a group length element, (gggg,0000).
//
// Every group may carry one, and the dictionary lists only the two with names of their
// own — Command Group Length and File Meta Information Group Length — so the rest look
// like unknown tags to a dictionary lookup. They are not: the form is defined by
// PS3.5 7.2, and although group lengths were retired for data sets, files written
// before that are common and contain one per group.
//
// Without this, validating such a file warned once per group:
//
//	level=WARN msg="dataset: semantic validation" tag=(0008,0000)
//	   err="unknown standard tag: (0008,0000)"
//
// which is six lines for an ordinary image, describing nothing wrong.
func (t Tag) IsGroupLength() bool {
	return t.Element() == 0x0000
}

// Exists checks if this tag is in the DICOM dictionary.
func (t Tag) Exists() bool {
	return GlobalDictionary().Get(t) != nil
}
