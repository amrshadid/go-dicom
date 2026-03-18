// Example: Downsize MRI Image (Conceptual Demonstration)
//
// NOTE: This is a conceptual/educational example that demonstrates the
// algorithm for downsampling DICOM image pixel data. It uses simulated
// pixel data rather than reading from an actual DICOM file. To apply
// this to real files, combine the downsampling logic here with the
// filereader/filewriter packages shown in the other examples.
//
// This example illustrates how to:
// - Read pixel data from a DICOM file
// - Downsize an image by subsampling
// - Update pixel dimensions
// - Understand pixel data format conversions
//

package main

import (
	"fmt"
)

// downsizeImage performs simple downsampling by taking every nth pixel
func downsizeImage(pixelData []uint16, rows, cols, factor uint32) ([]uint16, uint32, uint32) {
	newRows := rows / factor
	newCols := cols / factor

	downsampled := make([]uint16, newRows*newCols)

	idx := 0
	for i := uint32(0); i < rows; i += factor {
		for j := uint32(0); j < cols; j += factor {
			// Take the pixel at position [i*cols + j]
			if i*cols+j < uint32(len(pixelData)) {
				downsampled[idx] = pixelData[i*cols+j]
			}
			idx++
		}
	}

	return downsampled, newRows, newCols
}

func main() {
	fmt.Println("=== Downsize MRI Image Example ===")

	fmt.Println("This example demonstrates image downsampling concepts:")

	// Example with simulated pixel data
	// In a real scenario, you would read from a DICOM file using:
	// file, _ := os.Open(filePath)
	// reader := filebase.NewFileReader(file)
	// dicomFile, _ := filereader.ReadDICOMFile(reader)

	fmt.Println("Original Image:")
	originalRows := uint32(512)
	originalCols := uint32(512)
	fmt.Printf("  Dimensions: %d x %d pixels\n", originalRows, originalCols)
	fmt.Printf("  Total pixels: %d\n", originalRows*originalCols)
	fmt.Printf("  Memory (16-bit): %.2f MB\n", float64(originalRows*originalCols*2)/1024/1024)

	fmt.Println()
	fmt.Println("Downsampling by factor of 8...")
	downsampleFactor := uint32(8)

	// Create simulated pixel data (all zeros in this example)
	// In production, this would come from the DICOM file
	pixelData := make([]uint16, originalRows*originalCols)
	for i := range pixelData {
		pixelData[i] = uint16(i % 4096) // Simulated pixel values
	}

	_, newRows, newCols := downsizeImage(pixelData, originalRows, originalCols, downsampleFactor)

	fmt.Println()
	fmt.Println("Downsampled Image:")
	fmt.Printf("  Dimensions: %d x %d pixels\n", newRows, newCols)
	fmt.Printf("  Total pixels: %d\n", newRows*newCols)
	fmt.Printf("  Memory (16-bit): %.2f MB\n", float64(newRows*newCols*2)/1024/1024)

	fmt.Println()
	fmt.Println("Reduction:")
	fmt.Printf("  Pixels reduced: %d → %d (%.1f%%)\n",
		originalRows*originalCols, newRows*newCols,
		float64(newRows*newCols)/float64(originalRows*originalCols)*100)
	fmt.Printf("  Memory saved: %.2f%%\n",
		(1-float64(newRows*newCols)/float64(originalRows*originalCols))*100)

	fmt.Println()
	fmt.Println("=== How to Use with Real DICOM Files ===")

	fmt.Println()
	fmt.Println("1. Read DICOM file:")
	fmt.Println("   file, _ := os.Open(filePath)")
	fmt.Println("   reader := filebase.NewFileReader(file)")
	fmt.Println("   dicomFile, _ := filereader.ReadDICOMFile(reader)")

	fmt.Println()
	fmt.Println("2. Extract pixel data:")
	fmt.Println("   // Find the pixel data element (0x7FE0, 0x0010)")
	fmt.Println("   var pixelBytes []byte")
	fmt.Println("   for _, elem := range dicomFile.DataElements {")
	fmt.Println("       if elem.Tag.Group() == 0x7FE0 && elem.Tag.Element() == 0x0010 {")
	fmt.Println("           pixelBytes = elem.Value")
	fmt.Println("       }")
	fmt.Println("   }")

	fmt.Println()
	fmt.Println("3. Convert to uint16 array:")
	fmt.Println("   pixelArray := pixelDataToUint16Array(pixelBytes)")

	fmt.Println()
	fmt.Println("4. Get original dimensions:")
	fmt.Println("   // Find Rows (0x0028, 0x0010) and Columns (0x0028, 0x0011)")
	fmt.Println("   rows := // extract from DICOM element")
	fmt.Println("   cols := // extract from DICOM element")

	fmt.Println()
	fmt.Println("5. Downsample:")
	fmt.Println("   downsampled, newRows, newCols := downsizeImage(pixelArray, rows, cols, 8)")

	fmt.Println()
	fmt.Println("6. Update DICOM elements:")
	fmt.Println("   // Update Rows and Columns elements")
	fmt.Println("   // Update Pixel Data element with downsampled bytes")

	fmt.Println()
	fmt.Println("7. Write back to file:")
	fmt.Println("   // Use filewriter to write modified DICOM file")

	fmt.Println()
	fmt.Println("=== Key Concepts ===")

	fmt.Println()
	fmt.Println("Pixel Data Format:")
	fmt.Println("  - Stored as byte array in DICOM")
	fmt.Println("  - Little-endian byte order (standard)")
	fmt.Println("  - Conversion: uint16 = byte[i] | (byte[i+1] << 8)")

	fmt.Println()
	fmt.Println("Downsampling Methods:")
	fmt.Println("  - Central section (this example): Take every nth pixel")
	fmt.Println("  - Averaging: Average nxn blocks of pixels")
	fmt.Println("  - Interpolation: Use weighted average")

	fmt.Println()
	fmt.Println("Dimension Updates:")
	fmt.Println("  - Rows (0x0028, 0x0010): Update to newRows")
	fmt.Println("  - Columns (0x0028, 0x0011): Update to newCols")

	fmt.Println()
	fmt.Println("Memory Considerations:")
	fmt.Println("  - Loading entire image into memory can be expensive")
	fmt.Println("  - Consider streaming for very large datasets")
	fmt.Println("  - Use sampling for statistics on large images")

	fmt.Println()
	fmt.Println("✓ Image downsampling example complete!")
}
