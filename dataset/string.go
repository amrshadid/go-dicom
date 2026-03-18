package dataset

import (
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// FormatString returns a formatted string representation with custom options.
func (ds *Dataset) FormatString(opts StringFormatOptions) string {
	if opts.Compact {
		return ds.formatCompact()
	}
	return ds.formatDetailed(opts, 0)
}

// formatDetailed creates a detailed multi-line representation of the dataset.
// depth is used for indentation in recursive calls.
func (ds *Dataset) formatDetailed(opts StringFormatOptions, depth int) string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var sb strings.Builder
	indent := strings.Repeat(" ", depth*opts.IndentSize)

	// Write header at root level
	if depth == 0 {
		sb.WriteString(fmt.Sprintf("Dataset with %d elements:\n", len(ds.elements)))
	}

	// Iterate through elements in insertion order
	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		t := tag.FromInt(tagVal)

		// Apply filter options
		if !opts.ShowPrivateTags && t.IsPrivate() {
			continue
		}
		if !opts.ShowRetiredTags && t.IsRetired() {
			continue
		}

		// Format and write element
		sb.WriteString(indent)
		sb.WriteString(ds.formatElement(t, elem, opts, depth))
		sb.WriteString("\n")

		// Recurse into sequences if requested
		if opts.ShowHierarchy && elem.GetVR() == dataelem.SQ {
			value := elem.GetValue()
			if seq, ok := value.(*sequence.Sequence); ok {
				items := seq.Items()
				for i, item := range items {
					if childDS, ok := item.(*Dataset); ok {
						sb.WriteString(fmt.Sprintf("%s  [Item #%d]\n", indent, i))
						sb.WriteString(childDS.formatDetailed(opts, depth+2))
					}
				}
			}
		}
	}

	return sb.String()
}

// formatElement formats a single element with tag, VR, keyword, and value info.
func (ds *Dataset) formatElement(t tag.Tag, elem *dataelem.DataElement, opts StringFormatOptions, depth int) string {
	vr := elem.GetVR()
	keyword := t.GetKeyword()
	name := t.GetName()

	// Build base format: (GGGG,EEEE) VR [Keyword] Name
	var parts []string
	parts = append(parts, t.String())
	parts = append(parts, string(vr))

	if keyword != "" {
		parts = append(parts, fmt.Sprintf("[%s]", keyword))
	}

	if name != "" {
		parts = append(parts, name)
	}

	result := strings.Join(parts, " ")

	// Append value information if requested
	if opts.ShowValues && vr != dataelem.SQ {
		valueStr := ds.formatValue(elem, opts.MaxValueLength)
		if valueStr != "" {
			result += fmt.Sprintf(": %s", valueStr)
		}
	} else if vr == dataelem.SQ {
		// For sequences, show item count
		value := elem.GetValue()
		if seq, ok := value.(*sequence.Sequence); ok {
			result += fmt.Sprintf(" [Sequence with %d items]", seq.Length())
		}
	}

	return result
}

// formatValue formats an element's value for display purposes.
func (ds *Dataset) formatValue(elem *dataelem.DataElement, maxLen int) string {
	value := elem.GetValue()
	vr := elem.GetVR()

	if value == nil {
		return "<empty>"
	}

	// Handle byte slice values
	if b, ok := value.([]byte); ok {
		if len(b) == 0 {
			return "<empty>"
		}

		// Display text VRs as quoted strings
		if isTextVR(vr) {
			str := strings.TrimRight(string(b), "\x00 ")
			if len(str) > maxLen {
				return fmt.Sprintf("%q... (%d bytes)", str[:maxLen], len(b))
			}
			return fmt.Sprintf("%q", str)
		}

		// Show numeric VRs as bytes (limited for large data)
		if len(b) <= 8 {
			return fmt.Sprintf("%v (%d bytes)", b, len(b))
		}
		return fmt.Sprintf("[%d bytes]", len(b))
	}

	// Handle sequences
	if seq, ok := value.(*sequence.Sequence); ok {
		return fmt.Sprintf("[Sequence with %d items]", seq.Length())
	}

	// Fallback for other types
	return fmt.Sprintf("%v", value)
}

// formatCompact creates a single-line compact representation.
func (ds *Dataset) formatCompact() string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var tags []string
	for _, tagVal := range ds.order {
		t := tag.FromInt(tagVal)
		tags = append(tags, t.String())
	}

	return fmt.Sprintf("Dataset{%d elements: %s}", len(ds.elements), strings.Join(tags, ", "))
}

// PrettyString returns a nicely formatted string representation using default options.
func (ds *Dataset) PrettyString() string {
	return ds.FormatString(DefaultStringFormatOptions())
}

// CompactString returns a compact single-line representation.
func (ds *Dataset) CompactString() string {
	opts := DefaultStringFormatOptions()
	opts.Compact = true
	return ds.FormatString(opts)
}

// SummaryString returns a summary with key metadata.
func (ds *Dataset) SummaryString() string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("Dataset Summary:\n")
	sb.WriteString(fmt.Sprintf("  Elements: %d\n", len(ds.elements)))

	// Count sequences
	seqCount := 0
	for _, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		if elem.GetVR() == dataelem.SQ {
			seqCount++
		}
	}
	if seqCount > 0 {
		sb.WriteString(fmt.Sprintf("  Sequences: %d\n", seqCount))
	}

	// Count private tags
	privateCount := 0
	for _, tagVal := range ds.order {
		t := tag.FromInt(tagVal)
		if t.IsPrivate() {
			privateCount++
		}
	}
	if privateCount > 0 {
		sb.WriteString(fmt.Sprintf("  Private Tags: %d\n", privateCount))
	}

	// Hierarchy info
	if !ds.IsRoot() {
		sb.WriteString(fmt.Sprintf("  Depth: %d\n", ds.Depth()))
		sb.WriteString(fmt.Sprintf("  Path: %s\n", ds.Path()))
	}

	// Key patient/study info if present
	if ds.ContainsByKeyword("PatientName") {
		sb.WriteString(fmt.Sprintf("  Patient: %s\n", ds.GetStringByKeyword("PatientName")))
	}
	if ds.ContainsByKeyword("StudyDescription") {
		sb.WriteString(fmt.Sprintf("  Study: %s\n", ds.GetStringByKeyword("StudyDescription")))
	}
	if ds.ContainsByKeyword("Modality") {
		sb.WriteString(fmt.Sprintf("  Modality: %s\n", ds.GetStringByKeyword("Modality")))
	}

	return sb.String()
}

// ElementList returns a formatted list of elements with details.
func (ds *Dataset) ElementList() string {
	opts := DefaultStringFormatOptions()
	opts.ShowHierarchy = false // Don't recurse into sequences
	return ds.FormatString(opts)
}

// TreeString returns a tree-like representation showing hierarchy.
func (ds *Dataset) TreeString() string {
	return ds.formatTree(0)
}

// formatTree creates a tree-like representation with ASCII art branches.
// Used for hierarchical display of nested datasets in sequences.
func (ds *Dataset) formatTree(depth int) string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var sb strings.Builder
	indent := strings.Repeat("  ", depth)
	branch := "├─"     // Branch prefix
	lastBranch := "└─" // Last branch prefix

	for i, tagVal := range ds.order {
		elem := ds.elements[tagVal]
		t := tag.FromInt(tagVal)

		// Determine if this is last element (for correct tree drawing)
		isLast := i == len(ds.order)-1
		prefix := branch
		if isLast {
			prefix = lastBranch
		}

		sb.WriteString(fmt.Sprintf("%s%s %s %s", indent, prefix, t.String(), t.GetKeyword()))

		// Show sequence items
		if elem.GetVR() == dataelem.SQ {
			value := elem.GetValue()
			if seq, ok := value.(*sequence.Sequence); ok {
				sb.WriteString(fmt.Sprintf(" [%d items]\n", seq.Length()))

				// Show first few items (max 3)
				items := seq.Items()
				for j, item := range items {
					if j >= 3 {
						sb.WriteString(fmt.Sprintf("%s    ... (%d more items)\n", indent, len(items)-3))
						break
					}
					if childDS, ok := item.(*Dataset); ok {
						sb.WriteString(fmt.Sprintf("%s    Item #%d:\n", indent, j))
						sb.WriteString(childDS.formatTree(depth + 2))
					}
				}
			}
		} else {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// isTextVR checks if a VR represents text data.
func isTextVR(vr dataelem.VR) bool {
	switch vr {
	case dataelem.AE, dataelem.AS, dataelem.CS, dataelem.DA, dataelem.DS,
		dataelem.DT, dataelem.IS, dataelem.LO, dataelem.LT, dataelem.PN,
		dataelem.SH, dataelem.ST, dataelem.TM, dataelem.UC, dataelem.UI,
		dataelem.UR, dataelem.UT:
		return true
	default:
		return false
	}
}

// DebugString returns a debug-friendly string with all information.
func (ds *Dataset) DebugString() string {
	var sb strings.Builder

	sb.WriteString("=== Dataset Debug Info ===\n")
	sb.WriteString(ds.SummaryString())
	sb.WriteString("\n=== Element List ===\n")
	sb.WriteString(ds.PrettyString())

	if !ds.IsRoot() {
		sb.WriteString("\n=== Hierarchy Info ===\n")
		sb.WriteString(fmt.Sprintf("Parent: %p\n", ds.Parent()))
		sb.WriteString(fmt.Sprintf("Depth: %d\n", ds.Depth()))
		sb.WriteString(fmt.Sprintf("Path: %s\n", ds.Path()))
	}

	// Statistics
	stats := ds.GetStatistics()
	sb.WriteString("\n=== Statistics ===\n")
	sb.WriteString(fmt.Sprintf("Total Elements: %d\n", stats.TotalElements))
	sb.WriteString(fmt.Sprintf("Total Bytes: %d\n", stats.TotalBytes))
	sb.WriteString(fmt.Sprintf("VR Distribution: %v\n", stats.ByVR))
	sb.WriteString(fmt.Sprintf("Group Distribution: %v\n", stats.ByGroup))

	return sb.String()
}
