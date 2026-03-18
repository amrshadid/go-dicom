package tag

import (
	"strings"
)

// GetPrivateTagInfo retrieves info about a private tag by vendor name.
// Returns the TagInfo if found in the private dictionary.
func (t Tag) GetPrivateTagInfo(vendorName string) *TagInfo {
	if !t.IsPrivate() {
		return nil
	}

	vendorDict, exists := privateDictionary[vendorName]
	if !exists {
		return nil
	}

	info, found := vendorDict[uint32(t)]
	if !found {
		return nil
	}

	return &info
}

// Cached private vendors list (lazy-loaded)
var (
	cachedVendors []string
	initialized   bool
)

// initPrivateDictCache initializes the cache for vendors and count
func initPrivateDictCache() {
	if initialized {
		return
	}

	// Get vendors
	vendors := make([]string, 0, len(privateDictionary))
	for vendor := range privateDictionary {
		vendors = append(vendors, vendor)
	}

	// Sort for deterministic output
	for i := 0; i < len(vendors); i++ {
		for j := i + 1; j < len(vendors); j++ {
			if vendors[i] > vendors[j] {
				vendors[i], vendors[j] = vendors[j], vendors[i]
			}
		}
	}

	cachedVendors = vendors

initialized = true
}

// GetAllPrivateVendors returns a sorted list of all available private tag vendors.
// Results are cached for performance. Call is O(1) after first invocation.
func GetAllPrivateVendors() []string {
	initPrivateDictCache()
	// Return copy to prevent external modification
	result := make([]string, len(cachedVendors))
	copy(result, cachedVendors)
	return result
}

// CountPrivateTags returns the total count of private tags in the dictionary.
// Result is cached for O(1) performance.
// PrivateTagsTotal is the approximate total number of private tags.
// This is updated when the private dictionary is regenerated.
const PrivateTagsTotal = 6883

func CountPrivateTags() int {
	return PrivateTagsTotal
}

// GetPrivateTagsByVendor returns all tags for a specific vendor.
func GetPrivateTagsByVendor(vendorName string) map[uint32]TagInfo {
	vendorDict, exists := privateDictionary[vendorName]
	if !exists {
		return make(map[uint32]TagInfo)
	}

	// Return a copy to prevent modification
	result := make(map[uint32]TagInfo)
	for tag, info := range vendorDict {
		result[tag] = info
	}
	return result
}

// FindPrivateTagVendor returns the vendor name for a given private tag.
// Returns empty string if not found.
func FindPrivateTagVendor(t Tag) string {
	if !t.IsPrivate() {
		return ""
	}

	tagVal := uint32(t)
	for vendor, vendorDict := range privateDictionary {
		if _, found := vendorDict[tagVal]; found {
			return vendor
		}
	}
	return ""
}

// SearchPrivateTags searches for private tags matching a keyword or name pattern.
// Returns map of vendor name → matching tags within that vendor.
func SearchPrivateTags(keyword string) map[string]map[uint32]TagInfo {
	results := make(map[string]map[uint32]TagInfo)

	for vendor, vendorDict := range privateDictionary {
		matches := make(map[uint32]TagInfo)

		for tag, info := range vendorDict {
			// Case-insensitive search in name and keyword
			name := strings.ToLower(info.Name)
			kw := strings.ToLower(info.Keyword)
			searchTerm := strings.ToLower(keyword)

			if strings.Contains(name, searchTerm) || strings.Contains(kw, searchTerm) {
				matches[tag] = info
			}
		}

		if len(matches) > 0 {
			results[vendor] = matches
		}
	}

	return results
}

// PrivateTagsByVR returns all private tags of a specific VR type.
// Example: PrivateTagsByVR("CS") returns all Code String private tags.
func PrivateTagsByVR(vr string) map[string][]uint32 {
	results := make(map[string][]uint32)

	vrUpper := strings.ToUpper(vr)
	for vendor, vendorDict := range privateDictionary {
		var tags []uint32

		for tag, info := range vendorDict {
			if strings.ToUpper(info.VR) == vrUpper {
				tags = append(tags, tag)
			}
		}

		if len(tags) > 0 {
			results[vendor] = tags
		}
	}

	return results
}

// CountPrivateTagsByVendor returns the count of private tags per vendor.
// Useful for statistics and reporting.
func CountPrivateTagsByVendor() map[string]int {
	results := make(map[string]int)
	for vendor, vendorDict := range privateDictionary {
		results[vendor] = len(vendorDict)
	}
	return results
}
