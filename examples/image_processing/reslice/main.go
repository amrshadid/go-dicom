// Example: Reslice CT Images in Different Planes (Conceptual Demonstration)
//
// NOTE: This is a conceptual/educational example that explains the
// principles behind multi-slice volume reconstruction and multiplanar
// reformatting. It does not perform actual file I/O. To apply these
// concepts to real DICOM files, use the filereader package to load
// each slice and the approach described here to build the 3D volume.
//
// This example illustrates how to:
// - Load multiple DICOM files
// - Sort slices by slice location
// - Build a 3D image from 2D slices
// - Extract axial, sagittal, and coronal planes
// - Access pixel spacing and slice thickness
//

package main

import (
	"fmt"
)

// SliceInfo holds metadata for a slice
type SliceInfo struct {
	FilePath      string
	SliceLocation float64
	PixelArray    [][]uint16
}

// SliceCollection is a sortable collection of slices
type SliceCollection []SliceInfo

func (s SliceCollection) Len() int           { return len(s) }
func (s SliceCollection) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s SliceCollection) Less(i, j int) bool { return s[i].SliceLocation < s[j].SliceLocation }

func main() {
	fmt.Println("=== Reslice CT Images in Different Planes ===")

	fmt.Println("This example demonstrates multi-slice volume reconstruction:")

	fmt.Println("Key Concepts:")
	fmt.Println("  1. Load multiple DICOM files from a series")
	fmt.Println("  2. Sort slices by SliceLocation (0x0020, 0x1041)")
	fmt.Println("  3. Build a 3D volume from 2D slices")
	fmt.Println("  4. Extract different reformatted planes")
	fmt.Println()

	fmt.Println("=== Loading Multiple DICOM Files ===")

	fmt.Println("The process involves:")
	fmt.Println("  1. Use glob pattern to load files: glob.glob(\"*.dcm\")")
	fmt.Println("  2. For each file:")
	fmt.Println("     - Read DICOM file using filereader.ReadDICOMFile()")
	fmt.Println("     - Check for SliceLocation tag (0x0020, 0x1041)")
	fmt.Println("     - Skip if not present (scout views, etc.)")
	fmt.Println("     - Extract pixel data")
	fmt.Println("     - Store SliceInfo with location and pixel array")

	fmt.Println()
	fmt.Println("=== Sorting Slices ===")

	fmt.Println()
	fmt.Println("Sort by SliceLocation:")
	fmt.Println("  slices.Sort()")
	fmt.Println("  // Ensures correct order for 3D reconstruction")

	fmt.Println()
	fmt.Println("=== Building 3D Volume ===")

	// Demonstrate with simulated data
	fmt.Println("Example with 128 slices of 512x512 pixels:")

	const numSlices = 128
	const sliceRows = 512
	const sliceCols = 512

	fmt.Printf("  Volume dimensions: %d x %d x %d\n", sliceRows, sliceCols, numSlices)
	fmt.Printf("  Memory per slice: %.2f MB (16-bit)\n",
		float64(sliceRows*sliceCols*2)/1024/1024)
	fmt.Printf("  Total volume memory: %.2f MB\n",
		float64(sliceRows*sliceCols*numSlices*2)/1024/1024)

	fmt.Println()
	fmt.Println("=== Volume Indexing ===")

	fmt.Println()
	fmt.Println("3D volume structure: volume[row][col][slice]")
	fmt.Println("  - Rows: 0 to 511 (head to feet)")
	fmt.Println("  - Cols: 0 to 511 (left to right)")
	fmt.Println("  - Slices: 0 to 127 (superior to inferior)")

	fmt.Println()
	fmt.Println("=== Extracting Planes ===")

	midSlice := numSlices / 2
	midRow := sliceRows / 2
	midCol := sliceCols / 2

	fmt.Printf("1. Axial (transverse) at slice %d:\n", midSlice)
	fmt.Printf("   - View: Looking up from feet (axial view)\n")
	fmt.Printf("   - Dimensions: %d x %d\n", sliceRows, sliceCols)
	fmt.Printf("   - Access: volume[:, :, %d]\n", midSlice)

	fmt.Println()
	fmt.Printf("2. Sagittal (left-right) at column %d:\n", midCol)
	fmt.Printf("   - View: Looking from right side\n")
	fmt.Printf("   - Dimensions: %d x %d\n", sliceRows, numSlices)
	fmt.Printf("   - Access: volume[:, %d, :]\n", midCol)

	fmt.Println()
	fmt.Printf("3. Coronal (front-back) at row %d:\n", midRow)
	fmt.Printf("   - View: Looking from front\n")
	fmt.Printf("   - Dimensions: %d x %d\n", numSlices, sliceCols)
	fmt.Printf("   - Access: volume[%d, :, :] (transposed)\n", midRow)

	fmt.Println()
	fmt.Println("=== Pixel Spacing ===")

	fmt.Println("Get spacing information from first slice:")
	fmt.Println("  - Pixel Spacing (0x0028, 0x0030): [row_spacing, col_spacing] in mm")
	fmt.Println("  - Slice Thickness (0x0018, 0x0050): distance between slices in mm")

	fmt.Println()
	fmt.Println("Example values:")
	fmt.Println("  - Pixel Spacing: [1.0, 1.0] mm")
	fmt.Println("  - Slice Thickness: 5.0 mm")

	fmt.Println()
	fmt.Println("=== Aspect Ratio Corrections ===")

	fmt.Println("When displaying reformatted planes, apply aspect ratio corrections:")
	fmt.Println("  - Axial aspect: pixel_spacing[1] / pixel_spacing[0]")
	fmt.Println("    Example: 1.0 / 1.0 = 1.0 (square pixels)")
	fmt.Println("  - Sagittal aspect: pixel_spacing[1] / slice_thickness")
	fmt.Println("    Example: 1.0 / 5.0 = 0.2 (tall pixels)")
	fmt.Println("  - Coronal aspect: slice_thickness / pixel_spacing[0]")
	fmt.Println("    Example: 5.0 / 1.0 = 5.0 (wide pixels)")

	fmt.Println()
	fmt.Println("=== Code Pattern ===")

	fmt.Println("// Load files")
	fmt.Println("var slices []SliceInfo")
	fmt.Println("for _, fname := range glob.glob(\"*.dcm\") {")
	fmt.Println("    file, _ := os.Open(fname)")
	fmt.Println("    reader := filebase.NewFileReader(file)")
	fmt.Println("    dicomFile, _ := filereader.ReadDICOMFile(reader)")
	fmt.Println("    // Extract SliceLocation and pixel data")
	fmt.Println("    slices = append(slices, SliceInfo{...})")
	fmt.Println("}")
	fmt.Println()
	fmt.Println("// Sort by location")
	fmt.Println("sort.Sort(SliceCollection(slices))")
	fmt.Println()
	fmt.Println("// Build 3D volume")
	fmt.Println("volume := build3DImage(slices)")
	fmt.Println()
	fmt.Println("// Extract planes")
	fmt.Println("axial := getAxialSlice(volume, midSlice)")
	fmt.Println("sagittal := getSagittalSlice(volume, midCol)")
	fmt.Println("coronal := getCoronalSlice(volume, midRow)")

	fmt.Println()
	fmt.Println("=== Performance Considerations ===")

	fmt.Println()
	fmt.Println("Memory usage:")
	fmt.Printf("  - 512x512x128 at 16-bit: %.2f MB\n",
		float64(512*512*128*2)/1024/1024)
	fmt.Printf("  - 512x512x256 at 16-bit: %.2f MB\n",
		float64(512*512*256*2)/1024/1024)
	fmt.Printf("  - 512x512x512 at 16-bit: %.2f MB\n",
		float64(512*512*512*2)/1024/1024)

	fmt.Println()
	fmt.Println("For large datasets:")
	fmt.Println("  - Consider streaming/chunked processing")
	fmt.Println("  - Load slices on-demand")
	fmt.Println("  - Use memory-mapped files")
	fmt.Println("  - Compress data between slices")

	fmt.Println()
	fmt.Println("✓ Reslicing example structure demonstrated!")
}
