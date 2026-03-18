// Package util provides utility functions for DICOM data manipulation and analysis.
//
// This package offers a collection of utilities for working with DICOM files and datasets,
// including hexadecimal conversion, binary dump formatting, dataset information extraction,
// and data element separator fixing utilities.
//
// # Core Features
//
//   - **Hex Utilities**: Convert between hex strings and bytes with whitespace handling
//   - **Dump Utilities**: Generate formatted hex dumps similar to Unix hexdump -C output
//   - **Dataset Utilities**: Extract clinical information and pretty-print DICOM datasets
//   - **Fixer Utilities**: Fix DICOM files with invalid multi-value separators
//
// # Hex Utilities
//
// ## Hex2Bytes
//
// Converts a hex string with optional whitespace to bytes.
//
//	hexString := "08 00 32 10 08 00 00 00"
//	bytes, err := util.Hex2Bytes(hexString)
//
// Supports various whitespace formats:
//   - Spaces: "48 65 6c 6c 6f"
//   - Newlines: "48 65\n6c 6c\n6f"
//   - Tabs: "48\t65\t6c\t6c\t6f"
//   - Mixed: "48 65\n6c\t6c 6f"
//
// ## Bytes2Hex
//
// Converts bytes to a hex string with spaces between pairs.
//
//	data := []byte("Hello")
//	hexString := util.Bytes2Hex(data)
//	// Returns: "48 65 6c 6c 6f"
//
// # Dump Utilities
//
// ## PrintCharacter
//
// Returns a printable character or '.' for non-printable bytes.
//
//	util.PrintCharacter('A')  // Returns "A"
//	util.PrintCharacter(0x01) // Returns "."
//
// Non-printable characters include:
//   - Control characters (0x00-0x1F)
//   - DEL character (0x7F)
//   - High bytes (0x80-0xFF)
//   - Backslash (special case)
//
// ## HexDump
//
// Generates a formatted hex dump of bytes in hexdump -C format.
//
//	data := []byte("Hello World")
//	dump := util.HexDump(data, 0, 0)
//	fmt.Println(dump)
//	// Output:
//	// 00000000  48 65 6c 6c 6f 20 57 6f 72 6c 64           |Hello World|
//
// Parameters:
//   - startAddress: Starting address offset for display
//   - stopAddress: Optional stop address (0 = no limit)
//
// ## HexDumpReader
//
// Generates a formatted hex dump from an io.Reader.
//
//	reader := bytes.NewReader(data)
//	dump := util.HexDumpReader(reader, 0, 0, true)
//
// Parameters:
//   - startAddress: Starting address offset
//   - stopAddress: Optional stop address (0 = no limit)
//   - showAddress: Whether to display memory addresses
//
// # Dataset Utilities
//
// ## DatasetInfo
//
// Structure containing extracted clinical information from a DICOM dataset.
//
//	type DatasetInfo struct {
//		PatientName    string
//		PatientID      string
//		StudyDate      string
//		Modality       string
//		SeriesNumber   int
//		InstanceNumber int
//		Rows           int
//		Columns        int
//		BitsAllocated  int
//		BitsStored     int
//		SOPClassUID    string
//		SOPInstanceUID string
//	}
//
// ## GetDatasetInfo
//
// Extracts common clinical information from a DICOM dataset.
//
//	info, err := util.GetDatasetInfo(dataset)
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("Patient: %s\n", info.PatientName)
//
// Extracts information from standard DICOM tags:
//   - (0010,0010): Patient Name
//   - (0010,0020): Patient ID
//   - (0008,0020): Study Date
//   - (0008,0060): Modality
//   - (0020,0011): Series Number
//   - (0020,0013): Instance Number
//   - (0008,0016): SOP Class UID
//   - (0008,0018): SOP Instance UID
//   - (0028,0008): Pixel Data (dimensions and bits)
//
// ## PrintDatasetInfo
//
// Prints dataset information in human-readable format.
//
//	util.PrintDatasetInfo(info)
//	// Output:
//	// === DICOM Dataset Information ===
//	// Patient Name:     Doe^John
//	// Patient ID:       12345
//	// Study Date:       20231015
//	// ...
//
// ## PrettyPrint
//
// Pretty-prints a dataset with nice indentation.
//
//	util.PrettyPrint(dataset, 0)
//	// Output:
//	// (0010,0010) [PN]: Doe^John
//	// (0010,0020) [LO]: 12345
//	// ...
//
// ## DumpDataset
//
// Returns a string representation of all dataset elements.
//
//	dump := util.DumpDataset(dataset)
//	fmt.Println(dump)
//
// # Fixer Utilities
//
// ## FixSeparatorConfig
//
// Configuration for fixing invalid DICOM element separators.
//
//	type FixSeparatorConfig struct {
//		InvalidSeparator  byte     // Character to replace (e.g., space)
//		ForVRs            []string // VR types to process (e.g., ["DS", "IS"])
//		ProcessUnknownVRs bool     // Process unknown VRs when true
//	}
//
// ## DefaultFixSeparatorConfig
//
// Returns default configuration for fixing space-separated values.
//
//	config := util.DefaultFixSeparatorConfig()
//	// Fixes space to backslash in DS and IS VRs
//
// ## FixSeparator
//
// Fixes DICOM datasets with invalid separators in multi-value elements.
//
//	config := util.DefaultFixSeparatorConfig()
//	fixedDS, err := util.FixSeparator(dataset, config)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Example:
//   - Before: "100 200 300" (DS value with spaces)
//   - After: "100\\200\\300" (corrected multi-value format)
//
// The function:
//   - Creates a new dataset without modifying the original
//   - Only processes specified VR types or unknown VRs (if configured)
//   - Handles string, byte, and multi-value types
//   - Preserves elements that don't need fixing
//
// # Use Cases
//
// ## Analyzing DICOM Files
//
//	// Extract patient and study information
//	info, _ := util.GetDatasetInfo(ds)
//	fmt.Printf("Patient: %s, ID: %s\n", info.PatientName, info.PatientID)
//	util.PrintDatasetInfo(info)
//
// ## Debugging DICOM Data
//
//	// Display hex dump of problematic data
//	data := extractElementBytes(ds, tag)
//	dump := util.HexDump(data, 0, 0)
//	fmt.Println(dump)
//
// ## Fixing Corrupted Files
//
//	// Fix files with space-separated values
//	config := util.DefaultFixSeparatorConfig()
//	fixedDS, _ := util.FixSeparator(ds, config)
//	// Use fixedDS instead of ds
//
// ## Converting Hex Data
//
//	// Convert hex string to bytes for processing
//	raw, _ := util.Hex2Bytes("48 65 6c 6c 6f")
//	// raw = []byte("Hello")
//
// # Examples
//
// ## Complete Information Extraction
//
//	package main
//
//	import (
//		"fmt"
//		"github.com/amrshadid/go-dicom/util"
//	)
//
//	func main() {
//		// Assume ds is a *dataset.Dataset
//		info, err := util.GetDatasetInfo(ds)
//		if err != nil {
//			panic(err)
//		}
//
//		fmt.Printf("Patient: %s\n", info.PatientName)
//		fmt.Printf("Study: %s\n", info.StudyDate)
//		fmt.Printf("Modality: %s\n", info.Modality)
//		fmt.Printf("Image Dimensions: %d x %d\n", info.Rows, info.Columns)
//	}
//
// ## Hex Conversion Workflow
//
//	// Convert hex string to bytes
//	hexString := "0010 0010"
//	bytes, err := util.Hex2Bytes(hexString)
//
//	// Process bytes...
//
//	// Convert back to hex
//	hexOutput := util.Bytes2Hex(bytes)
//	fmt.Println(hexOutput) // "00 10 00 10"
//
// ## Dataset Separator Fixing
//
//	// Fix multi-value separators
//	config := &util.FixSeparatorConfig{
//		InvalidSeparator:  ' ',
//		ForVRs:            []string{"DS", "IS", "LO"},
//		ProcessUnknownVRs: false,
//	}
//
//	fixedDS, err := util.FixSeparator(originalDS, config)
//	if err != nil {
//		panic(err)
//	}
//
//	// Use fixedDS...
//
// # Performance Considerations
//
//   - Hex2Bytes: O(n) where n is the length of hex string
//   - HexDump: O(m) where m is the number of bytes dumped (16 bytes per line)
//   - FixSeparator: O(n) where n is the number of elements in dataset
//   - GetDatasetInfo: O(k) where k is the number of standard tags extracted
//
// # Error Handling
//
// Most functions return errors for:
//   - Invalid hex strings (Hex2Bytes)
//   - I/O errors when reading (HexDumpReader)
//   - Dataset access failures (GetDatasetInfo)
//
// Some functions gracefully handle edge cases:
//   - Empty datasets return empty or zero-valued DatasetInfo
//   - nil inputs to FixSeparator return unchanged input
//   - Missing DICOM tags result in empty string values
//
// # Thread Safety
//
// All functions in this package are stateless and safe for concurrent use.
// FixSeparator creates a new dataset, so concurrent calls will not interfere.
//
// # DICOM Compliance
//
// The package follows DICOM standards for:
//   - Multi-value element separator (backslash)
//   - Standard DICOM tag definitions
//   - Value Representation (VR) handling
//   - Dataset structure and access
//
// See: https://www.dicomstandard.org/
//
// # See Also
//
//   - values package: Value conversion and encoding utilities
//   - valuerep package: Value Representation metadata and validation
//   - dataset package: DICOM dataset structure and handling
//   - tag package: DICOM tag definitions and utilities
package util
