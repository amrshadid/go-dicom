package dataset

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/amrshadid/go-dicom/tag"
)

// Dir searches for elements matching a pattern
// Supports wildcard patterns (*, ?) and returns matching tag strings
// Example: ds.Dir("0010,*") returns tags matching the pattern
func (ds *Dataset) Dir(pattern string) []string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var matches []string

	for _, tagVal := range ds.order {
		t := tag.Tag(tagVal)
		tagStr := t.String() // Format: "XXXX,XXXX"

		// Check if tag matches pattern
		if matchesPattern(tagStr, pattern) {
			matches = append(matches, tagStr)
		}
	}

	return matches
}

// matchesPattern checks if a string matches a pattern with wildcards or regex
func matchesPattern(s, pattern string) bool {
	// Try exact match first
	if strings.EqualFold(s, pattern) {
		return true
	}

	// Convert wildcard pattern to filepath.Match format
	// This handles *, ?, and [char ranges]
	matched, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(s))
	if err == nil && matched {
		return true
	}

	// Try as regex pattern if it looks like regex
	if strings.ContainsAny(pattern, "[](){}+^$|\\") {
		if regex, err := regexp.Compile("(?i)" + pattern); err == nil {
			return regex.MatchString(s)
		}
	}

	return false
}

// GetTags returns all tags in the dataset as strings
func (ds *Dataset) GetTags() []string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var tags []string
	for _, tagVal := range ds.order {
		t := tag.Tag(tagVal)
		tags = append(tags, t.String())
	}

	return tags
}
