// Package encaps provides DICOM encapsulation parsing and frame extraction.
//
// # Overview
//
// The encaps package handles parsing of encapsulated DICOM pixel data, which is used
// for compressed image data. It provides:
//
// - Parser: Reads and parses encapsulated data with Basic Offset Table
// - Extractor: Extracts individual frames from encapsulated data
// - Reframer: Reorganizes fragments to match target frame count
// - Validator: Checks encapsulation structure for validity
// - Statistics: Provides information about encapsulated data
//
// # DICOM Encapsulation Format
//
// Encapsulated pixel data follows the DICOM standard with:
//
// 1. Basic Offset Table (BOT) Item
//   - Item tag: (0xFFFE, 0xE000)
//   - Contains byte offsets to first fragment of each frame
//   - Optional: can be empty or omitted
//
// 2. Data Fragment Items
//   - Item tag: (0xFFFE, 0xE000)
//   - Contains compressed or encoded frame data
//   - Multiple fragments can compose a single frame
//
// 3. Sequence Delimiter (optional)
//   - Item tag: (0xFFFE, 0xE0DD)
//   - Marks end of encapsulated data
//
// # Usage Example
//
//	parser := encaps.NewParser(dataReader, true) // little-endian
//	encData, err := parser.ParseEncapsulatedData()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	extractor := encaps.NewExtractor(encData)
//	frameData, err := extractor.ExtractFrame(0) // Get first frame
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Validate the data
//	validator := encaps.NewValidator()
//	err = validator.ValidateEncapsulation(encData)
//
//	// Get statistics
//	stats := encaps.GetStatistics(encData)
//	fmt.Printf("Frames: %d, Fragments: %d, Total Size: %d bytes\n",
//		stats.FrameCount, stats.FragmentCount, stats.TotalSize)
//
// # Compression Formats
//
// Encapsulated data can contain frames compressed with:
// - JPEG (lossy)
// - JPEG Lossless
// - JPEG-LS
// - JPEG 2000
// - RLE (Run-Length Encoding)
// - DEFLATE (zlib)
// - Other formats supported by external decoders
//
// # Frame Extraction
//
// Frames are extracted using either:
//
// 1. Basic Offset Table (if present)
//   - Direct offsets to each frame's data
//   - Fast and reliable frame access
//
// 2. Fragment-based extraction
//   - Assumes one fragment per frame
//   - Used when BOT is not available
//
// # Thread Safety
//
// The Parser is NOT thread-safe. Create separate instances for concurrent parsing.
// Extractor and Validator are stateless and thread-safe after creation.
package encaps
