// Example: Modify and Update DICOM Element Values
//
// This example illustrates how to:
// - Read a DICOM file
// - Modify element values using native keyword API
// - Remove elements from a dataset for anonymization
// - Add new elements to a dataset
// - Update patient information
// - Anonymize DICOM data (remove PII)
// - Track modifications with before/after values
// - Save the modified DICOM dataset to a new file
//

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// UpdatedElement holds update information
type UpdatedElement struct {
	Keyword  string
	OldValue string
	NewValue string
	Status   string
}

func main() {
	fmt.Println("=== Modify and Update DICOM Element Values ===")

	// Get DICOM file path from command line arguments
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <dicom-file>\n", os.Args[0])
		os.Exit(1)
	}
	filePath := os.Args[1]

	// Open the DICOM file
	fmt.Printf("Reading DICOM file: %s\n\n", filePath)
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Error opening file: %v", err)
	}
	defer file.Close()

	// Create a filebase reader
	reader := filebase.NewFileReader(file)

	// Read the complete DICOM file
	dicomFile, err := filereader.ReadDICOMFile(reader)
	if err != nil {
		log.Fatalf("Error reading DICOM file: %v", err)
	}

	fmt.Println("=== Initial State ===")
	fmt.Printf("Total elements in file: %d\n\n", len(dicomFile.DataElements))

	// Convert DataElementValue slice to native Dataset for keyword access
	ds := dataset.NewDataset()
	for _, elem := range dicomFile.DataElements {
		if elem != nil {
			t := tag.New(elem.Tag.Group(), elem.Tag.Element())
			err := ds.SetValue(t, elem.Value)
			if err != nil {
				log.Printf("Warning: could not set element %04X,%04X: %v", elem.Tag.Group(), elem.Tag.Element(), err)
			}
		}
	}

	updates := []UpdatedElement{}

	// ============================================
	// EXAMPLE 1: Update Patient Name
	// ============================================
	fmt.Println("=== Example 1: Update Patient Name ===")

	if ds.HasKeyword("PatientName") {
		oldName := ds.GetStringByKeyword("PatientName")
		newName := "Doe^John"

		fmt.Printf("Found Patient Name\n")
		fmt.Printf("Old Value: %s\n", oldName)
		fmt.Printf("New Value: %s\n", newName)

		// Update the element value using native API
		err := ds.SetStringByKeyword("PatientName", newName)
		if err != nil {
			fmt.Printf("Error updating Patient Name: %v\n", err)
		} else {
			fmt.Println("✓ Patient Name updated")

			updates = append(updates, UpdatedElement{
				Keyword:  "PatientName",
				OldValue: oldName,
				NewValue: newName,
				Status:   "UPDATED",
			})
		}
	} else {
		fmt.Println("Patient Name not found in dataset")
	}

	// ============================================
	// EXAMPLE 2: Update Patient ID
	// ============================================
	fmt.Println("=== Example 2: Update Patient ID ===")

	if ds.HasKeyword("PatientID") {
		oldID := ds.GetStringByKeyword("PatientID")
		newID := "987654321"

		fmt.Printf("Found Patient ID\n")
		fmt.Printf("Old Value: %s\n", oldID)
		fmt.Printf("New Value: %s\n", newID)

		err := ds.SetStringByKeyword("PatientID", newID)
		if err != nil {
			fmt.Printf("Error updating Patient ID: %v\n", err)
		} else {
			fmt.Println("✓ Patient ID updated")

			updates = append(updates, UpdatedElement{
				Keyword:  "PatientID",
				OldValue: oldID,
				NewValue: newID,
				Status:   "UPDATED",
			})
		}
	} else {
		fmt.Println("Patient ID not found in dataset")
	}

	// ============================================
	// EXAMPLE 3: Update Patient Age
	// ============================================
	fmt.Println("=== Example 3: Update Patient Age ===")

	if ds.HasKeyword("PatientAge") {
		oldAge := ds.GetStringByKeyword("PatientAge")
		newAge := "050Y" // 50 years

		fmt.Printf("Found Patient Age\n")
		fmt.Printf("Old Value: %s\n", oldAge)
		fmt.Printf("New Value: %s\n", newAge)

		err := ds.SetStringByKeyword("PatientAge", newAge)
		if err != nil {
			fmt.Printf("Error updating Patient Age: %v\n", err)
		} else {
			fmt.Println("✓ Patient Age updated")

			updates = append(updates, UpdatedElement{
				Keyword:  "PatientAge",
				OldValue: oldAge,
				NewValue: newAge,
				Status:   "UPDATED",
			})
		}
	} else {
		fmt.Println("Patient Age not found in dataset")
	}

	// ============================================
	// EXAMPLE 4: Update Study Description
	// ============================================
	fmt.Println("=== Example 4: Update Study Description ===")

	if ds.HasKeyword("StudyDescription") {
		oldDesc := ds.GetStringByKeyword("StudyDescription")
		newDesc := "Brain^MRI Study"

		fmt.Printf("Found Study Description\n")
		fmt.Printf("Old Value: %s\n", oldDesc)
		fmt.Printf("New Value: %s\n", newDesc)

		err := ds.SetStringByKeyword("StudyDescription", newDesc)
		if err != nil {
			fmt.Printf("Error updating Study Description: %v\n", err)
		} else {
			fmt.Println("✓ Study Description updated")

			updates = append(updates, UpdatedElement{
				Keyword:  "StudyDescription",
				OldValue: oldDesc,
				NewValue: newDesc,
				Status:   "UPDATED",
			})
		}
	} else {
		fmt.Println("Study Description not found in dataset")
	}

	// ============================================
	// EXAMPLE 5: Remove Physician Name (Anonymization)
	// ============================================
	fmt.Println("=== Example 5: Remove Element (Anonymization) ===")

	if ds.HasKeyword("PerformingPhysicianName") {
		oldPhys := ds.GetStringByKeyword("PerformingPhysicianName")
		fmt.Printf("Found Performing Physician Name\n")
		fmt.Printf("Old Value: %s\n", oldPhys)
		fmt.Println("Removing element for anonymization...")

		// Remove element using native API
		removed := ds.RemoveByKeyword("PerformingPhysicianName")
		if removed {
			fmt.Println("✓ Performing Physician Name removed")

			updates = append(updates, UpdatedElement{
				Keyword:  "PerformingPhysicianName",
				OldValue: oldPhys,
				NewValue: "(REMOVED)",
				Status:   "DELETED",
			})
		} else {
			fmt.Println("Failed to remove Performing Physician Name")
		}
	} else {
		fmt.Println("Performing Physician Name not found in dataset")
	}

	// ============================================
	// EXAMPLE 6: Remove Patient Birth Date (Anonymization)
	// ============================================
	fmt.Println("=== Example 6: Remove Another Element ===")

	if ds.HasKeyword("PatientBirthDate") {
		oldBirth := ds.GetStringByKeyword("PatientBirthDate")
		fmt.Printf("Found Patient Birth Date\n")
		fmt.Printf("Old Value: %s\n", oldBirth)
		fmt.Println("Removing element for anonymization...")

		removed := ds.RemoveByKeyword("PatientBirthDate")
		if removed {
			fmt.Println("✓ Patient Birth Date removed")

			updates = append(updates, UpdatedElement{
				Keyword:  "PatientBirthDate",
				OldValue: oldBirth,
				NewValue: "(REMOVED)",
				Status:   "DELETED",
			})
		} else {
			fmt.Println("Failed to remove Patient Birth Date")
		}
	}

	// ============================================
	// EXAMPLE 7: Clear Patient Comments
	// ============================================
	fmt.Println("=== Example 7: Clear Patient Comments ===")

	if ds.HasKeyword("PatientComments") {
		oldComments := ds.GetStringByKeyword("PatientComments")
		fmt.Printf("Found Patient Comments\n")
		fmt.Printf("Old Value: %s\n", oldComments)
		fmt.Println("Clearing comments for anonymization...")

		// Set to empty value using native API
		err := ds.SetStringByKeyword("PatientComments", "")
		if err != nil {
			fmt.Printf("Error clearing Patient Comments: %v\n", err)
		} else {
			fmt.Println("✓ Patient Comments cleared")

			updates = append(updates, UpdatedElement{
				Keyword:  "PatientComments",
				OldValue: oldComments,
				NewValue: "(EMPTY)",
				Status:   "CLEARED",
			})
		}
	} else {
		fmt.Println("Patient Comments not found (optional element)")
	}

	// ============================================
	// Verify Changes Were Applied
	// ============================================
	fmt.Println("=== Verify Changes Were Applied ===")

	if len(updates) > 0 {
		fmt.Println("Verifying modified values in dataset:")
		for _, upd := range updates {
			switch upd.Status {
			case "DELETED":
				// For deleted elements, check they no longer exist
				if ds.HasKeyword(upd.Keyword) {
					fmt.Printf("❌ %s - Still exists in dataset (should be removed)\n", upd.Keyword)
				} else {
					fmt.Printf("✓ %s - Successfully removed\n", upd.Keyword)
				}
			case "CLEARED":
				// For cleared elements, check they're empty
				val := ds.GetStringByKeyword(upd.Keyword)
				if val == "" {
					fmt.Printf("✓ %s - Successfully cleared to empty value\n", upd.Keyword)
				} else {
					fmt.Printf("❌ %s - Value is '%s' (should be empty)\n", upd.Keyword, val)
				}
			default:
				// For updated elements, verify new value
				newVal := ds.GetStringByKeyword(upd.Keyword)
				if newVal == upd.NewValue {
					fmt.Printf("✓ %s - Updated to '%s'\n", upd.Keyword, newVal)
				} else {
					fmt.Printf("❌ %s - Value is '%s' (expected '%s')\n", upd.Keyword, newVal, upd.NewValue)
				}
			}
		}
	}

	// ============================================
	// Display Summary of Changes
	// ============================================
	fmt.Println()
	fmt.Println("=== Summary of Changes ===")
	fmt.Printf("Total updates applied: %d\n\n", len(updates))

	fmt.Println("Change Log:")
	fmt.Println("─────────────────────────────────────────────────────────")
	fmt.Printf("%-25s %-25s %-25s %-10s\n", "Keyword", "Old Value", "New Value", "Status")
	fmt.Println("─────────────────────────────────────────────────────────")

	for _, upd := range updates {
		// Truncate values if too long
		oldVal := upd.OldValue
		if len(oldVal) > 20 {
			oldVal = oldVal[:20] + "..."
		}
		newVal := upd.NewValue
		if len(newVal) > 20 {
			newVal = newVal[:20] + "..."
		}

		fmt.Printf("%-25s %-25s %-25s %-10s\n", upd.Keyword, oldVal, newVal, upd.Status)
	}

	// ============================================
	// Statistics
	// ============================================
	fmt.Println()
	fmt.Println("=== Element Statistics ===")
	fmt.Printf("Original elements: %d\n", len(dicomFile.DataElements))
	fmt.Printf("Modified elements: %d\n", ds.Length())
	fmt.Printf("Elements removed:  %d\n", len(dicomFile.DataElements)-ds.Length())

	// ============================================
	// Save Modified DICOM File
	// ============================================
	fmt.Println()
	fmt.Println("=== Saving Modified DICOM File ===")

	// Create output filename
	outputFilename := "modified_" + filepath.Base(filePath)
	outputPath := filepath.Join(filepath.Dir(filePath), outputFilename)

	fmt.Printf("Output file: %s\n", outputFilename)

	// Create output file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Error creating output file: %v\n", err)
	} else {
		defer outputFile.Close()

		// Write the modified DICOM file
		writer := filebase.NewFileWriter(outputFile)
		dcmWriter := filewriter.NewDCMFileWriter(writer)

		// Set transfer syntax properties based on original file
		dcmWriter.SetExplicitVR(dicomFile.ExplicitVR)
		dcmWriter.SetLittleEndian(dicomFile.IsLittleEndian)

		// Write preamble (128 null bytes)
		if err := dcmWriter.WritePreamble(); err != nil {
			fmt.Printf("Error writing preamble: %v\n", err)
		} else if err := dcmWriter.WriteDICMPrefix(); err != nil {
			fmt.Printf("Error writing DICM prefix: %v\n", err)
		} else {
			// Create file meta info
			fileMetaInfo := &filewriter.FileMetaInfo{
				MediaStorageSOPClassUID:    dicomFile.FileMetaInfo.MediaStorageSOPClassUID,
				MediaStorageSOPInstanceUID: dicomFile.FileMetaInfo.MediaStorageSOPInstanceUID,
				TransferSyntaxUID:          dicomFile.FileMetaInfo.TransferSyntaxUID,
			}

			if err := dcmWriter.WriteFileMetaInfo(fileMetaInfo); err != nil {
				fmt.Printf("Error writing file meta info: %v\n", err)
			} else {
				// Write modified data elements
				// Iterate through original elements and write them with modified values
				successCount := 0
				errorCount := 0

				for _, origElem := range dicomFile.DataElements {
					if origElem == nil {
						continue
					}

					t := tag.New(origElem.Tag.Group(), origElem.Tag.Element())

					// Check if element was deleted
					if !ds.HasKeyword(ds.TagToKeyword(t)) {
						continue // Skip deleted elements
					}

					// Get current value from modified dataset
					currentValue := ds.GetValue(t)
					if currentValue == nil {
						continue
					}

					// Create data element for writing
					writeElem := &filewriter.DataElement{
						Tag:    t,
						VR:     origElem.VR,
						Value:  currentValue,
						Length: uint32(len(currentValue)),
					}

					// Write the element
					if err := dcmWriter.WriteDataElement(writeElem, false); err != nil {
						errorCount++
					} else {
						successCount++
					}
				}

				// Get file size
				fi, err := os.Stat(outputPath)
				if err != nil {
					fmt.Printf("Error getting file info: %v\n", err)
				} else {
					fmt.Printf("✓ Successfully saved modified DICOM file\n")
					fmt.Printf("  File size: %d bytes\n", fi.Size())
					fmt.Printf("  Elements written: %d\n", successCount)
					if errorCount > 0 {
						fmt.Printf("  Write errors: %d\n", errorCount)
					}
					fmt.Printf("  Transfer Syntax: %s\n", fileMetaInfo.TransferSyntaxUID)
				}
			}
		}
	}

	// ============================================
	// Key Concepts
	// ============================================
	fmt.Println()
	fmt.Println("=== Key Concepts for Modifying DICOM (Using Native Keyword API) ===")
	fmt.Println("1. Reading Elements:")
	fmt.Println("   - Use HasKeyword(keyword) to check if element exists")
	fmt.Println("   - Use GetStringByKeyword(keyword) to get text values")
	fmt.Println("   - Keywords are case-sensitive (e.g., PatientName, not patientname)")
	fmt.Println()
	fmt.Println("2. Updating Values:")
	fmt.Println("   - Use SetStringByKeyword(keyword, value) for text values")
	fmt.Println("   - Use SetValueByKeyword(keyword, []byte) for raw bytes")
	fmt.Println("   - Use UpdateElementByKeyword() to modify existing elements")
	fmt.Println()
	fmt.Println("3. Removing Elements:")
	fmt.Println("   - Use RemoveByKeyword(keyword) to delete elements")
	fmt.Println("   - Use RemoveElementsByKeywords() for multiple elements")
	fmt.Println("   - Returns false if keyword not found or element doesn't exist")
	fmt.Println()
	fmt.Println("4. Anonymization:")
	fmt.Println("   - Remove patient identifiable information (PII)")
	fmt.Println("   - RemoveByKeyword() common PII keywords:")
	fmt.Println("     - PatientName, PatientID, PatientBirthDate")
	fmt.Println("     - PerformingPhysicianName, PatientComments")
	fmt.Println("     - PatientPhoneNumber, PatientAddress")
	fmt.Println()
	fmt.Println("5. Common Keywords to Modify:")
	fmt.Println("   - Patient Info: PatientName, PatientID, PatientBirthDate, PatientAge, PatientSex")
	fmt.Println("   - Study Info: StudyDescription, StudyDate, StudyTime, Modality")
	fmt.Println("   - Series Info: SeriesDescription, SeriesInstanceUID")
	fmt.Println("   - Physician: PerformingPhysicianName, ReferringPhysicianName")
	fmt.Println()
	fmt.Println("6. Saving Changes to DICOM File:")
	fmt.Println("   - Use filewriter.DCMFileWriter to write proper DICOM files")
	fmt.Println("   - Write preamble (128 null bytes) first")
	fmt.Println("   - Write 'DICM' prefix after preamble")
	fmt.Println("   - Write FileMetaInfo with SOP Class UID, SOP Instance UID, Transfer Syntax")
	fmt.Println("   - Write each data element with proper VR and Length")
	fmt.Println("   - Skip deleted elements (not in modified dataset)")
	fmt.Println("   - Preserve original VR types and transfer syntax")
	fmt.Println()
	fmt.Println("7. Production DICOM Writing Code:")
	fmt.Println("   writer := filebase.NewFileWriter(file)")
	fmt.Println("   dcmWriter := filewriter.NewDCMFileWriter(writer)")
	fmt.Println("   dcmWriter.WritePreamble()")
	fmt.Println("   dcmWriter.WriteDICMPrefix()")
	fmt.Println("   dcmWriter.WriteFileMetaInfo(metaInfo)")
	fmt.Println("   for each modified element {")
	fmt.Println("     dcmWriter.WriteDataElement(elem, false)")
	fmt.Println("   }")
	fmt.Println()
	fmt.Println("✓ Modification example complete!")
}
