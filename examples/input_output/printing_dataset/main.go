// Example: Format and Print DICOM Dataset Information
//
// This example illustrates how to:
// - Create a Dataset using NewDataset()
// - Add data elements with proper tags, VRs, and values
// - Use the various String() methods (String, PrettyString, CompactString, etc.)
// - Iterate elements and display dataset statistics
//
// This example does not require a DICOM file on disk; it builds a dataset in memory.

package main

import (
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

func main() {
	fmt.Println("=== Format and Print DICOM Dataset ===")
	fmt.Println()

	// Create a new empty dataset
	ds := dataset.NewDataset()

	// Add patient information elements.
	// NewDataElement takes (tag, VR, value). The tag must be a tag.Tag,
	// VR is a dataelem.VR constant, and value is typically []byte for strings.
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("12345678")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0030), dataelem.DA, []byte("19800101")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0040), dataelem.CS, []byte("M")))

	// Add study/series information
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0020), dataelem.DA, []byte("20231224")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("MR")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte("1.2.840.10008.5.1.4.1.1.4")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.3.4.5.6.7.8.9")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D), dataelem.UI, []byte("1.2.3.4.5.6.7.8")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000E), dataelem.UI, []byte("1.2.3.4.5.6.7.9")))

	fmt.Printf("Dataset has %d elements\n\n", ds.Length())

	// --- Method 1: String() ---
	// The default String() method shows a summary with tag hex, VR, and byte count.
	fmt.Println("=== String() output ===")
	fmt.Println(ds.String())

	// --- Method 2: PrettyString() ---
	// PrettyString() gives a more detailed, human-readable output.
	fmt.Println("=== PrettyString() output ===")
	fmt.Println(ds.PrettyString())

	// --- Method 3: CompactString() ---
	// CompactString() gives a one-line-per-element compact output.
	fmt.Println("=== CompactString() output ===")
	fmt.Println(ds.CompactString())

	// --- Method 4: FormatString() with custom options ---
	// You can control what's shown via StringFormatOptions.
	opts := dataset.DefaultStringFormatOptions()
	opts.ShowValues = true
	opts.MaxValueLength = 32
	opts.Compact = false
	fmt.Println("=== FormatString() with custom options ===")
	fmt.Println(ds.FormatString(opts))

	// --- Method 5: GetStatistics() ---
	// Returns counts by VR type and group number.
	stats := ds.GetStatistics()
	fmt.Println("=== Dataset Statistics ===")
	fmt.Printf("Total elements: %d\n", stats.TotalElements)
	fmt.Printf("Total bytes:    %d\n", stats.TotalBytes)
	fmt.Println()
	fmt.Println("Elements by VR:")
	for vr, count := range stats.ByVR {
		fmt.Printf("  %s: %d\n", vr, count)
	}
	fmt.Println()
	fmt.Println("Elements by Group:")
	for group, count := range stats.ByGroup {
		fmt.Printf("  %04X: %d\n", group, count)
	}

	// --- Method 6: Iterate with ForEach ---
	// Walk over every element and print it using DataElement.String().
	fmt.Println()
	fmt.Println("=== ForEach iteration ===")
	ds.ForEach(func(elem *dataelem.DataElement) error {
		fmt.Printf("  %s\n", elem.String())
		return nil
	})

	fmt.Println()
	fmt.Println("Done!")
}
