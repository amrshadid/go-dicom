// Example: Work with DICOM Sequences
//
// This example illustrates how to:
// - Create DICOM sequences using the sequence package
// - Add Dataset items to sequences
// - Attach sequences to a parent dataset
// - Access and iterate sequence items
// - Remove items from sequences
// - Nest sequences inside sequence items
//
// Sequences (VR = SQ) are containers for ordered collections of items,
// where each item is itself a dataset. They are used throughout DICOM
// for treatment plans, referenced images, and other hierarchical data.

package main

import (
	"fmt"
	"log"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

func main() {
	fmt.Println("=== Working with DICOM Sequences ===")
	fmt.Println()

	// Create a parent dataset that will hold our sequences
	parentDS := dataset.NewDataset()

	// Add some top-level elements to the parent
	parentDS.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("RTPLAN")))
	parentDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))

	// ============================================
	// Step 1: Create a BeamSequence (300A,00B0)
	// ============================================
	fmt.Println("--- Creating BeamSequence ---")

	// Create two beam items as datasets
	beam1 := dataset.NewDataset()
	beam1.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00C0), dataelem.IS, []byte("1")))        // BeamNumber
	beam1.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00C2), dataelem.LO, []byte("Anterior"))) // BeamName
	beam1.Add(dataelem.NewDataElement(tag.New(0x300A, 0x0114), dataelem.DS, []byte("0")))        // NominalBeamEnergy (placeholder tag)

	beam2 := dataset.NewDataset()
	beam2.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00C0), dataelem.IS, []byte("2")))       // BeamNumber
	beam2.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00C2), dataelem.LO, []byte("Lateral"))) // BeamName

	// Create a sequence from the beam items
	beamSeq := sequence.NewFromItems(beam1, beam2)
	fmt.Printf("BeamSequence created with %d items\n", beamSeq.Length())

	// Add the BeamSequence to the parent dataset using AddSequence
	beamSeqTag := tag.New(0x300A, 0x00B0) // BeamSequence tag
	if err := parentDS.AddSequence(beamSeqTag, beamSeq); err != nil {
		log.Fatalf("Error adding BeamSequence: %v", err)
	}
	fmt.Println("BeamSequence added to parent dataset")

	// ============================================
	// Step 2: Nest a BlockSequence inside Beam 1
	// ============================================
	fmt.Println()
	fmt.Println("--- Nesting BlockSequence inside Beam 1 ---")

	// Create block items
	block1 := dataset.NewDataset()
	block1.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00F6), dataelem.IS, []byte("1")))
	block1.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00F8), dataelem.CS, []byte("APERTURE")))

	block2 := dataset.NewDataset()
	block2.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00F6), dataelem.IS, []byte("2")))
	block2.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00F8), dataelem.CS, []byte("SHIELDING")))

	// Create the BlockSequence and attach it to beam1
	blockSeqTag := tag.New(0x300A, 0x00F4) // BlockSequence tag
	blockSeq := sequence.NewFromItems(block1, block2)
	if err := beam1.AddSequence(blockSeqTag, blockSeq); err != nil {
		log.Fatalf("Error adding BlockSequence: %v", err)
	}
	fmt.Printf("BlockSequence with %d items added to Beam 1\n", blockSeq.Length())

	// ============================================
	// Step 3: Access sequence items
	// ============================================
	fmt.Println()
	fmt.Println("--- Accessing Sequence Items ---")

	// Retrieve the BeamSequence from the parent dataset
	retrievedSeq, err := parentDS.GetSequence(beamSeqTag)
	if err != nil {
		log.Fatalf("Error retrieving BeamSequence: %v", err)
	}
	fmt.Printf("Retrieved BeamSequence: %d items\n", retrievedSeq.Length())

	// Iterate through beam items
	for i := 0; i < retrievedSeq.Length(); i++ {
		item, err := retrievedSeq.Get(i)
		if err != nil {
			log.Printf("Error getting beam %d: %v", i, err)
			continue
		}
		if beamDS, ok := item.(*dataset.Dataset); ok {
			beamName := beamDS.GetStringByKeyword("BeamName")
			beamNumber := beamDS.GetStringByKeyword("BeamNumber")
			fmt.Printf("  Beam %s: %s (%d elements)\n", beamNumber, beamName, beamDS.Length())

			// Check for nested BlockSequence
			if beamDS.HasSequence(blockSeqTag) {
				nestedSeq, _ := beamDS.GetSequence(blockSeqTag)
				fmt.Printf("    -> Contains BlockSequence with %d blocks\n", nestedSeq.Length())
			}
		}
	}

	// ============================================
	// Step 4: Append a new item to a sequence
	// ============================================
	fmt.Println()
	fmt.Println("--- Appending a New Beam ---")

	beam3 := dataset.NewDataset()
	beam3.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00C0), dataelem.IS, []byte("3")))
	beam3.Add(dataelem.NewDataElement(tag.New(0x300A, 0x00C2), dataelem.LO, []byte("Posterior")))

	// AppendToSequence adds to an existing sequence (or creates one if it doesn't exist)
	if err := parentDS.AppendToSequence(beamSeqTag, beam3); err != nil {
		log.Fatalf("Error appending beam: %v", err)
	}
	fmt.Printf("BeamSequence now has %d items\n", beamSeq.Length())

	// ============================================
	// Step 5: Remove an item from a sequence
	// ============================================
	fmt.Println()
	fmt.Println("--- Removing Beam at Index 1 ---")

	if err := beamSeq.Remove(1); err != nil {
		log.Fatalf("Error removing beam: %v", err)
	}
	fmt.Printf("BeamSequence now has %d items\n", beamSeq.Length())

	// ============================================
	// Summary
	// ============================================
	fmt.Println()
	fmt.Println("--- Final Summary ---")
	fmt.Printf("Parent dataset: %d elements\n", parentDS.Length())

	finalSeq, _ := parentDS.GetSequence(beamSeqTag)
	fmt.Printf("BeamSequence: %d beams\n", finalSeq.Length())
	for i := 0; i < finalSeq.Length(); i++ {
		item, _ := finalSeq.Get(i)
		if beamDS, ok := item.(*dataset.Dataset); ok {
			name := beamDS.GetStringByKeyword("BeamName")
			fmt.Printf("  Beam %d: %s\n", i, name)
		}
	}

	fmt.Println()
	fmt.Println("Done!")
}
