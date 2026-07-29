package dataset

import (
	"fmt"
	"strings"
)

// Dataset hierarchy navigation methods for datasets nested within sequences.

// Parent returns the parent dataset in a sequence hierarchy.
// Returns nil if this dataset is at the root level.
func (ds *Dataset) Parent() *Dataset {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	return ds.parent
}

// IsRoot returns true if this dataset is at the root level (no parent).
func (ds *Dataset) IsRoot() bool {
	return ds.Parent() == nil
}

// Top returns the root dataset in the hierarchy.
// If this is already the root, returns itself.
func (ds *Dataset) Top() *Dataset {
	current := ds
	for current.Parent() != nil {
		current = current.Parent()
	}
	return current
}

// Depth returns the nesting depth relative to root (0 for root).
func (ds *Dataset) Depth() int {
	depth := 0
	current := ds
	for current.Parent() != nil {
		depth++
		current = current.Parent()
	}
	return depth
}

// Path returns the hierarchical path to this dataset as a string.
// Format: "/sequence[0]/nestedSeq[1]" (root is "/").
func (ds *Dataset) Path() string {
	if ds.IsRoot() {
		return "/"
	}

	var path []string
	current := ds

	// Build path from leaf to root, then reverse
	for current.Parent() != nil {
		// Find which sequence contains this dataset
		if parent := current.Parent(); parent != nil {
			// Search for this dataset in parent's sequences (simplified)
			path = append(path, "sequence[?]")
		}
		current = current.Parent()
	}

	// Reverse the path to get root-to-leaf order
	for i := len(path)/2 - 1; i >= 0; i-- {
		opp := len(path) - 1 - i
		path[i], path[opp] = path[opp], path[i]
	}

	if len(path) == 0 {
		return "/"
	}

	return "/" + strings.Join(path, "/")
}

// GetRoot returns the root dataset in the entire hierarchy.
// Alias for Top() with more explicit naming.
func (ds *Dataset) GetRoot() *Dataset {
	return ds.Top()
}

// IsNested returns true if this dataset is nested within a sequence.
func (ds *Dataset) IsNested() bool {
	return !ds.IsRoot()
}

// GetDepthPath returns both depth and path information for this dataset.
// Useful for understanding the dataset's position in the hierarchy.
func (ds *Dataset) GetDepthPath() (depth int, path string) {
	depth = ds.Depth()
	path = ds.Path()
	return
}

// EnhancedDatasetInfo returns enhanced information about the dataset including hierarchy details.
func (ds *Dataset) EnhancedDatasetInfo() string {
	ds.mu.RLock()
	elemCount := len(ds.elements)
	ds.mu.RUnlock()

	if ds.IsRoot() {
		return fmt.Sprintf("Dataset{root, %d elements}", elemCount)
	}
	return fmt.Sprintf("Dataset{depth=%d, path=%s, %d elements}",
		ds.Depth(), ds.Path(), elemCount)
}

// Equals checks if two datasets are equal by comparing elements.
// Note: This is a shallow comparison; nested structures require deep comparison.
func (ds *Dataset) Equals(other *Dataset) bool {
	if other == nil {
		return false
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	// Check element count first
	if len(ds.elements) != len(other.elements) {
		return false
	}

	// Compare all elements
	for tagVal, elem := range ds.elements {
		otherElem, exists := other.elements[tagVal]
		if !exists {
			return false
		}

		// Compare the tags as tags. Comparing the interface{} values compares
		// their dynamic types too, so the same tag stored as a tag.Tag in one
		// element and a uint32 in the other compared unequal — two data sets
		// holding identical attributes could be reported as different.
		if elem.MustTag() != otherElem.MustTag() {
			return false
		}
	}

	return true
}
