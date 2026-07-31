package tag

import (
	"sort"
	"sync"
)

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
	for _, r := range compiledRepeaters() {
		if uint32(tag)&r.mask == r.value {
			return r.pattern
		}
	}
	return ""
}

// compiledRepeater is a repeater pattern reduced to integer form.
//
// A pattern like "60xx0010" covers every overlay group. Matching it as text
// meant formatting the tag with fmt.Sprintf and comparing up to 88 strings on
// every dictionary miss — and a miss happens for each private or unrecognized
// tag, of which a real file has many. It was half the parse time of
// CT_small.dcm.
type compiledRepeater struct {
	value   uint32 // the fixed nibbles
	mask    uint32 // 0xF where a nibble is fixed, 0 where it is 'x'
	pattern string // the original, to look the entry back up
}

var (
	compiledRepeatersOnce  sync.Once
	compiledRepeatersCache []compiledRepeater
)

// compiledRepeaters returns the repeater patterns in integer form, compiling
// them on first use.
//
// Compiled once rather than at package initialization so a program that never
// looks up a tag does not pay for it, and so the cost is not charged to whichever
// test happens to run first.
func compiledRepeaters() []compiledRepeater {
	compiledRepeatersOnce.Do(func() {
		compiledRepeatersCache = make([]compiledRepeater, 0, len(RepeaterDictionary))
		for pattern := range RepeaterDictionary {
			if c, ok := compileRepeater(pattern); ok {
				compiledRepeatersCache = append(compiledRepeatersCache, c)
			}
		}
		// Deterministic order: map iteration is random, and two patterns can
		// match the same tag, so an arbitrary order would make lookups
		// nondeterministic between runs.
		sort.Slice(compiledRepeatersCache, func(i, j int) bool {
			return compiledRepeatersCache[i].pattern < compiledRepeatersCache[j].pattern
		})
	})
	return compiledRepeatersCache
}

// compileRepeater converts an 8-character pattern into value and mask.
//
// Reports false for anything that is not 8 characters of hex digits and 'x', so
// a malformed entry is skipped rather than matching everything.
func compileRepeater(pattern string) (compiledRepeater, bool) {
	if len(pattern) != 8 {
		return compiledRepeater{}, false
	}

	var value, mask uint32
	for i := 0; i < 8; i++ {
		value <<= 4
		mask <<= 4

		c := pattern[i]
		switch {
		case c == 'x' || c == 'X':
			// Wildcard nibble: contributes nothing to either.
		case c >= '0' && c <= '9':
			value |= uint32(c - '0')
			mask |= 0xF
		case c >= 'A' && c <= 'F':
			value |= uint32(c-'A') + 10
			mask |= 0xF
		case c >= 'a' && c <= 'f':
			value |= uint32(c-'a') + 10
			mask |= 0xF
		default:
			return compiledRepeater{}, false
		}
	}
	return compiledRepeater{value: value, mask: mask, pattern: pattern}, true
}

// GlobalDictionary returns the global DICOM dictionary
func GlobalDictionary() *DicomDictionary {
	return globalDict
}
