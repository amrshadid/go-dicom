package dataset

import (
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Dataset represents an in-memory DICOM dataset.
// It holds a collection of data elements indexed by tag for fast access.
// All operations are thread-safe using sync.RWMutex.
//
// A Dataset can be:
//   - A root-level dataset (standalone DICOM file)
//   - A nested dataset within a sequence (child dataset)
//
// Example usage:
//
//	ds := dataset.NewDataset()
//	ds.SetStringByKeyword("PatientName", "John Doe")
//	name := ds.GetStringByKeyword("PatientName")
type DatasetInterface interface {
	// Core Operations
	Add(elem *dataelem.DataElement) error
	Get(t tag.Tag) (*dataelem.DataElement, bool)
	Contains(t tag.Tag) bool
	Remove(t tag.Tag) bool
	Clear()
	Length() int

	// Value Operations
	GetValue(t tag.Tag) []byte
	SetValue(t tag.Tag, value []byte) error
	UpdateElement(t tag.Tag, value []byte) error

	// Collection Operations
	GetAll() []*dataelem.DataElement
	Tags() []tag.Tag
	ForEach(fn func(*dataelem.DataElement) error) error

	// Query Operations
	GetByGroup(group uint16) []*dataelem.DataElement
	GetByVR(vr dataelem.VR) []*dataelem.DataElement
	GetByTagRange(start, end tag.Tag) []*dataelem.DataElement
	FilteredElements(filter func(*dataelem.DataElement) bool) []*dataelem.DataElement

	// Batch Operations
	GetElements(tags ...tag.Tag) []*dataelem.DataElement
	RemoveElements(tags ...tag.Tag) int
	Merge(other *Dataset) error

	// Hierarchy Operations
	Parent() *Dataset
	IsRoot() bool
	Top() *Dataset
	Depth() int
	Path() string

	// Sequence Operations
	AddSequence(t tag.Tag, seq *sequence.Sequence) error
	GetSequence(t tag.Tag) (*sequence.Sequence, error)
	HasSequence(t tag.Tag) bool

	// Clone & Comparison
	Clone() *Dataset
	CloneWithSequences() *Dataset
	Equals(other *Dataset) bool

	// String Representation
	String() string
}

// PixelDataInfo contains metadata about pixel data in the dataset.
// This information is extracted from standard DICOM tags.
type PixelDataInfo struct {
	BitsAllocated             int     // 8, 16, 32, or 64
	BitsStored                int     // Actual bits used
	HighBit                   int     // Position of highest bit
	Rows                      int     // Height in pixels
	Columns                   int     // Width in pixels
	NumberOfFrames            int     // For multi-frame images, 1 if single frame
	SamplesPerPixel           int     // 1 for grayscale, 3+ for color
	PhotometricInterpretation string  // RGB, YCbCr, MONOCHROME1/2, etc.
	PixelRepresentation       int     // 0 for unsigned, 1 for signed
	PlanarConfiguration       int     // 0 = R1G1B1R2G2B2, 1 = R1R2...G1G2...B1B2
	BytesPerFrame             int     // Calculated: (Rows * Columns * SamplesPerPixel * BitsAllocated) / 8
	RescaleIntercept          float64 // Linear transformation intercept for pixel values
	RescaleSlope              float64 // Linear transformation slope for pixel values
}

// WindowingParameters represents VOI (Value of Interest) window settings.
// Window center and width are used to map stored pixel values to display values.
type WindowingParameters struct {
	Center float64 // Window center (level)
	Width  float64 // Window width
}

// VOILUTParameters represents Value of Interest Lookup Table parameters.
// Used for non-linear transformations of pixel values for display.
type VOILUTParameters struct {
	LUTData        []uint16  // The lookup table data
	LUTDescriptor  [3]uint16 // [number of entries, first mapped value, bits per entry]
	LUTExplanation string    // Explanation of the LUT
}

// PhotometricInterpretation represents different DICOM photometric interpretations.
// This is different from ColorSpace (in imaging.go) which is for color space conversions.
type PhotometricInterpretation string

// Standard DICOM photometric interpretations
const (
	PhotometricRGB             PhotometricInterpretation = "RGB"
	PhotometricYBR_FULL        PhotometricInterpretation = "YBR_FULL"
	PhotometricYBR_FULL_422    PhotometricInterpretation = "YBR_FULL_422"
	PhotometricYBR_PARTIAL_420 PhotometricInterpretation = "YBR_PARTIAL_420"
	PhotometricYBR_ICT         PhotometricInterpretation = "YBR_ICT"
	PhotometricYBR_RCT         PhotometricInterpretation = "YBR_RCT"
	PhotometricMONOCHROME1     PhotometricInterpretation = "MONOCHROME1"
	PhotometricMONOCHROME2     PhotometricInterpretation = "MONOCHROME2"
	PhotometricPALETTE_COLOR   PhotometricInterpretation = "PALETTE COLOR"
)

// ImageFormat represents output image formats.
type ImageFormat int

const (
	FormatPNG ImageFormat = iota
	FormatJPEG
	FormatBMP
	FormatTIFF
)

// String returns the string representation of the image format.
func (f ImageFormat) String() string {
	switch f {
	case FormatPNG:
		return "PNG"
	case FormatJPEG:
		return "JPEG"
	case FormatBMP:
		return "BMP"
	case FormatTIFF:
		return "TIFF"
	default:
		return "UNKNOWN"
	}
}

// FileExtension returns the file extension for the image format.
func (f ImageFormat) FileExtension() string {
	switch f {
	case FormatPNG:
		return ".png"
	case FormatJPEG:
		return ".jpg"
	case FormatBMP:
		return ".bmp"
	case FormatTIFF:
		return ".tif"
	default:
		return ""
	}
}

// Statistics contains statistical information about a dataset.
type Statistics struct {
	TotalElements int            // Total number of data elements
	TotalBytes    int            // Total size of all element values in bytes
	ByVR          map[string]int // Count of elements by Value Representation
	ByGroup       map[uint16]int // Count of elements by group number
}

// ExportOptions contains options for exporting images from DICOM datasets.
type ExportOptions struct {
	Format         ImageFormat // Output format
	WindowCenter   float64     // Window center for windowing
	WindowWidth    float64     // Window width for windowing
	ApplyWindowing bool        // Whether to apply windowing
	ApplyRescale   bool        // Whether to apply rescale slope/intercept
	FrameIndex     int         // Which frame to export (for multi-frame images)
	Quality        int         // JPEG quality (1-100), ignored for other formats
	ConvertToRGB   bool        // Convert to RGB color space
}

// DefaultExportOptions returns sensible default export options.
func DefaultExportOptions() ExportOptions {
	return ExportOptions{
		Format:         FormatPNG,
		ApplyWindowing: false,
		ApplyRescale:   true,
		FrameIndex:     0,
		Quality:        90,
		ConvertToRGB:   false,
	}
}

// StringFormatOptions contains options for string representation formatting.
type StringFormatOptions struct {
	ShowValues      bool // Include element values in output
	MaxValueLength  int  // Maximum length of values to display
	ShowHierarchy   bool // Show hierarchical structure for sequences
	IndentSize      int  // Number of spaces per indent level
	ShowPrivateTags bool // Include private tags in output
	ShowRetiredTags bool // Include retired tags in output
	Compact         bool // Compact single-line output
}

// DefaultStringFormatOptions returns sensible default formatting options.
func DefaultStringFormatOptions() StringFormatOptions {
	return StringFormatOptions{
		ShowValues:      true,
		MaxValueLength:  64,
		ShowHierarchy:   true,
		IndentSize:      2,
		ShowPrivateTags: true,
		ShowRetiredTags: true,
		Compact:         false,
	}
}

// JSONDataset represents a dataset in JSON format.
// Used for marshaling/unmarshaling DICOM data to/from JSON.
type JSONDataset struct {
	Elements map[string]JSONElement `json:"elements"`
	Metadata JSONMetadata           `json:"metadata,omitempty"`
}

// JSONElement represents a single DICOM element in JSON format.
type JSONElement struct {
	Tag     string      `json:"tag"`               // Tag in (GGGG,EEEE) format
	VR      string      `json:"vr"`                // Value Representation
	Value   interface{} `json:"value"`             // Element value (varies by VR)
	Keyword string      `json:"keyword,omitempty"` // DICOM keyword if known
	Name    string      `json:"name,omitempty"`    // Human-readable name
}

// JSONMetadata contains metadata about the dataset.
type JSONMetadata struct {
	TransferSyntaxUID string `json:"transferSyntaxUID,omitempty"`
	CharacterSet      string `json:"characterSet,omitempty"`
	ElementCount      int    `json:"elementCount"`
	HasPixelData      bool   `json:"hasPixelData"`
	HasSequences      bool   `json:"hasSequences"`
}

// IterationCallback is a function type for iterating over elements.
// Return an error to stop iteration early.
type IterationCallback func(*Dataset, *dataelem.DataElement) error

// FilterPredicate is a function type for filtering elements.
// Return true to include the element in results.
type FilterPredicate func(*dataelem.DataElement) bool

// DatasetPredicate is a function type for filtering datasets.
// Used in sequence searches. Return true to include the dataset in results.
type DatasetPredicate func(*Dataset) bool
