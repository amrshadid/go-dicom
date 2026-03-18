// Package pixels provides comprehensive access to and manipulation of DICOM pixel data.
//
// # Overview
//
// The pixels package handles uncompressed DICOM pixel arrays with support for:
//
// - Multiple sample depths (8-bit, 16-bit, 32-bit)
// - Signed and unsigned integer representations
// - Multi-frame (3D) images
// - Multiple color spaces (grayscale, RGB)
// - Bit allocation specifications
// - Byte order handling (little-endian, big-endian)
//
// # Main Types
//
// - PixelData: Core structure representing raw pixel image data
// - Accessor: Provides methods to read individual pixel values
// - Calculator: Computes statistical properties of the image
// - Validator: Validates pixel data consistency
//
// # Usage Example
//
//	// Create pixel data from raw bytes
//	pd := pixels.NewPixelData(pixelBytes, 512, 512)
//	pd.BitsAllocated = 16
//	pd.BitsStored = 16
//	pd.PixelRepresentation = 0 // unsigned
//	pd.LittleEndian = true
//
//	// Access individual pixels
//	accessor := pixels.NewAccessor(pd)
//	value, err := accessor.GetPixelValue(100, 200, 0) // row, col, frame
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("Pixel value: %v\n", value)
//
//	// Calculate statistics
//	calculator := pixels.NewCalculator(pd)
//	stats, err := calculator.CalculateStatistics()
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("Min: %.2f, Max: %.2f, Mean: %.2f\n",
//		stats.MinValue, stats.MaxValue, stats.MeanValue)
//
//	// Validate pixel data
//	validator := pixels.NewValidator()
//	err = validator.ValidatePixelData(pd)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # Pixel Data Structure
//
// DICOM pixel data includes metadata describing the image:
//
// - Rows, Columns: Image dimensions (width, height)
// - NumberOfFrames: Count of frames for multi-frame images (default 1)
// - BitsAllocated: Bits per sample (usually 8, 16, or 32)
// - BitsStored: Actual bits used (≤ BitsAllocated)
// - HighBit: Highest bit position (typically BitsStored - 1)
// - PixelRepresentation: 0=unsigned, 1=signed
// - SamplesPerPixel: 1=grayscale, 3=RGB, etc.
// - PhotometricInterpretation: Color space ("MONOCHROME2", "RGB", etc.)
// - LittleEndian: Byte order for multi-byte values
// - PlanarConfiguration: 0=interleaved (RGB), 1=planar (RRR...GGG...BBB...)
//
// # Byte Order
//
// The accessor respects the LittleEndian flag when reading multi-byte values.
// Ensure this is set correctly based on the DICOM transfer syntax:
// - Most modern DICOM uses little-endian (default)
// - Explicit VR Big Endian transfer syntax uses big-endian
//
// # Multi-Frame Images
//
// For multi-frame images, NumberOfFrames indicates the total count.
// Frames are stored sequentially in the pixel data.
// Use the frame index parameter when accessing pixels from specific frames.
//
// # Performance Considerations
//
// - For large images (>10 megapixels), use CalculateStatisticsSampled
// - Accessor is stateless and reusable
// - Calculator is CPU-intensive; consider caching results
// - Validator checks consistency but is fast
//
// # Thread Safety
//
// PixelData is immutable. Accessor, Calculator, and Validator are thread-safe
// as long as the underlying PixelData doesn't change.
package pixels
