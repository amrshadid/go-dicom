// Package compress provides compression and encapsulation handling for DICOM pixel data.
// Supports parsing and generating encapsulated pixel data with Basic Offset Tables,
// fragment extraction, multiple compression formats (JPEG, RLE, DEFLATE, etc.),
// and external decoder support for advanced formats.
package compress

import (
	"io"
	"sync"
)

// Type Definitions

// CompressionType represents the type of compression used in DICOM pixel data.
type CompressionType string

const (
	// UNCOMPRESSED represents uncompressed pixel data
	UNCOMPRESSED CompressionType = "UNCOMPRESSED"
	// DEFLATE represents DEFLATE (zlib) compression
	DEFLATE CompressionType = "DEFLATE"
	// RLE represents Run-Length Encoding compression
	RLE CompressionType = "RLE"
	// JPEG represents JPEG baseline compression (lossy)
	JPEG CompressionType = "JPEG"
	// JPEG_LOSSLESS represents JPEG lossless compression
	JPEG_LOSSLESS CompressionType = "JPEG_LOSSLESS"
	// JPEG_LS represents JPEG-LS compression
	JPEG_LS CompressionType = "JPEG_LS"
	// JPEG_2000 represents JPEG 2000 compression
	JPEG_2000 CompressionType = "JPEG_2000"
)

// Interfaces

// Decompressor is an interface for decompressing pixel data.
// Implementations should be thread-safe and reusable.
type Decompressor interface {
	// Decompress decompresses the given data and returns raw pixel data
	Decompress(data []byte) ([]byte, error)

	// CanDecompress checks if this decompressor can handle the given data
	CanDecompress(data []byte) bool
}

// Compressor is an interface for compressing pixel data.
// Implementations should be thread-safe and reusable.
type Compressor interface {
	// Compress compresses the given data
	Compress(data []byte) ([]byte, error)
}

// Encapsulation Types

// EncapsulatedData represents encapsulated (compressed) DICOM pixel data.
// This follows the DICOM encapsulation format with Basic Offset Table
// and fragment items.
type EncapsulatedData struct {
	// BasicOffsetTable contains byte offsets to the first fragment of each frame
	BasicOffsetTable []uint32

	// Fragments contains the compressed data fragments
	Fragments [][]byte

	// NumberOfFrames is the expected number of frames (from DICOM tag 0028,0008)
	NumberOfFrames int

	// ExtendedOffsetTable contains 64-bit offsets (optional, for large data)
	ExtendedOffsetTable []uint64

	// ExtendedOffsetTableLengths contains lengths of each frame (optional)
	ExtendedOffsetTableLengths []uint64

	// Endianness specifies byte order ("<" for little-endian, ">" for big-endian)
	Endianness string
}

// FragmentInfo holds metadata about a single fragment in encapsulated data.
type FragmentInfo struct {
	// Offset is the absolute byte position of the fragment's item tag
	Offset uint64

	// Length is the length of the fragment data (excluding tag and length fields)
	Length uint32

	// FrameIndex is the frame number this fragment belongs to (0-indexed)
	FrameIndex int
}

// FrameInfo holds metadata about a frame in encapsulated data.
type FrameInfo struct {
	// Index is the frame number (0-indexed)
	Index int

	// Offset is the byte position of the first fragment
	Offset uint64

	// Length is the total length of all fragments for this frame
	Length uint64

	// FragmentCount is the number of fragments that make up this frame
	FragmentCount int

	// Fragments contains references to the fragment data
	Fragments [][]byte
}

// Decompressor Registry

// DecompressorRegistry manages available decompressors for different compression types.
// It provides thread-safe registration and retrieval of decompressors.
type DecompressorRegistry struct {
	decompressors map[CompressionType]Decompressor
	mu            sync.RWMutex
}

// Compression Information

// CompressionInfo holds information about a compression method.
type CompressionInfo struct {
	// Type is the compression type identifier
	Type CompressionType

	// Name is the human-readable name
	Name string

	// IsLossless indicates if this is lossless compression
	IsLossless bool

	// IsSupported indicates if this compression is currently supported
	IsSupported bool

	// RequiresExternal indicates if external libraries are needed
	RequiresExternal bool

	// Description provides details about the compression method
	Description string
}

// Statistics

// Statistics holds compression/decompression statistics.
type Statistics struct {
	// OriginalSize is the size of the uncompressed data in bytes
	OriginalSize uint64

	// CompressedSize is the size of the compressed data in bytes
	CompressedSize uint64

	// CompressionRatio is the ratio of compressed to original size
	CompressionRatio float64

	// CompressionType is the type of compression used
	CompressionType CompressionType

	// CompressionTime is the time taken to compress (nanoseconds)
	CompressionTime int64

	// DecompressionTime is the time taken to decompress (nanoseconds)
	DecompressionTime int64
}

// Buffered Item (Internal)

// BufferedItem represents a buffered encapsulation item.
// This is used internally for lazy loading of encapsulated data.
// This exported type is provided for advanced users who need to work
// with buffered items directly; the buffer field is typically used internally
// by the EncapsulatedBuffer implementation.
type BufferedItem struct {
	// Buffer is the underlying io.ReadSeeker for the item data
	Buffer io.ReadSeeker
	// Length is the total length of the item including tag and padding
	Length uint32
	// Padding indicates if a padding byte is needed
	Padding bool
}

// Constants

const (
	// ItemTag is the DICOM item tag (FFFE,E000)
	// When written as bytes {0xFE, 0xFF, 0x00, 0xE0} and read as little-endian uint32
	ItemTag uint32 = 0xE000FFFE

	// SequenceDelimiterTag is the DICOM sequence delimiter tag (FFFE,E0DD)
	// When written as bytes {0xFE, 0xFF, 0xDD, 0xE0} and read as little-endian uint32
	SequenceDelimiterTag uint32 = 0xE0DDFFFE

	// JPEGEOIMarker is the JPEG End of Image marker (0xFF 0xD9)
	JPEGEOIMarker = "\xff\xd9"

	// DefaultEndianness is the default byte order for DICOM (little-endian)
	DefaultEndianness = "<"
)

// Error Types

// ErrorType represents different categories of compression errors.
type ErrorType int

const (
	// ErrorTypeUnknown represents an unknown error
	ErrorTypeUnknown ErrorType = iota

	// ErrorTypeInvalidData indicates invalid or corrupted input data
	ErrorTypeInvalidData

	// ErrorTypeUnsupportedCompression indicates unsupported compression format
	ErrorTypeUnsupportedCompression

	// ErrorTypeDecompressionFailed indicates decompression operation failed
	ErrorTypeDecompressionFailed

	// ErrorTypeCompressionFailed indicates compression operation failed
	ErrorTypeCompressionFailed

	// ErrorTypeInvalidEncapsulation indicates invalid encapsulation format
	ErrorTypeInvalidEncapsulation
)

// Compression Type Information Map

// compressionInfoMap provides detailed information about each compression type.
// This is used by GetCompressionInfo to return metadata about compression methods.
var compressionInfoMap = map[CompressionType]CompressionInfo{
	UNCOMPRESSED: {
		Type:             UNCOMPRESSED,
		Name:             "Uncompressed",
		IsLossless:       true,
		IsSupported:      true,
		RequiresExternal: false,
		Description:      "No compression applied",
	},
	DEFLATE: {
		Type:             DEFLATE,
		Name:             "DEFLATE",
		IsLossless:       true,
		IsSupported:      true,
		RequiresExternal: false,
		Description:      "DEFLATE compression (zlib)",
	},
	RLE: {
		Type:             RLE,
		Name:             "RLE",
		IsLossless:       true,
		IsSupported:      true,
		RequiresExternal: false,
		Description:      "Run-Length Encoding",
	},
	JPEG: {
		Type:             JPEG,
		Name:             "JPEG",
		IsLossless:       false,
		IsSupported:      true,
		RequiresExternal: false,
		Description:      "JPEG Lossy compression (baseline)",
	},
	JPEG_LOSSLESS: {
		Type:             JPEG_LOSSLESS,
		Name:             "JPEG Lossless",
		IsLossless:       true,
		IsSupported:      false,
		RequiresExternal: true,
		Description:      "JPEG Lossless compression (requires external library)",
	},
	JPEG_LS: {
		Type:             JPEG_LS,
		Name:             "JPEG-LS",
		IsLossless:       true,
		IsSupported:      false,
		RequiresExternal: true,
		Description:      "JPEG-LS lossless compression (requires external library)",
	},
	JPEG_2000: {
		Type:             JPEG_2000,
		Name:             "JPEG 2000",
		IsLossless:       true,
		IsSupported:      false,
		RequiresExternal: true,
		Description:      "JPEG 2000 compression (requires external library)",
	},
}

// GetCompressionInfo returns information about a compression type.
func GetCompressionInfo(compressionType CompressionType) CompressionInfo {
	if info, exists := compressionInfoMap[compressionType]; exists {
		return info
	}

	return CompressionInfo{
		Type:             compressionType,
		Name:             string(compressionType),
		IsLossless:       false,
		IsSupported:      false,
		RequiresExternal: true,
		Description:      "Unknown compression type",
	}
}
