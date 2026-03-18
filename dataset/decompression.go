package dataset

import (
	"fmt"
	"sync"

	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// PixelDataHandler defines the interface for decompressing pixel data
// based on Transfer Syntax UID.
type PixelDataHandler interface {
	// Decompress decompresses the given pixel data according to the handler's
	// transfer syntax and returns the decompressed bytes.
	Decompress(compressedData []byte, info *PixelDataInfo) ([]byte, error)

	// CanHandle returns true if this handler can decompress the given transfer syntax
	CanHandle(transferSyntax string) bool

	// Name returns a human-readable name for this handler
	Name() string
}

// PixelDataHandlerRegistry manages registered decompression handlers
type PixelDataHandlerRegistry struct {
	mu       sync.RWMutex
	handlers []PixelDataHandler
}

var (
	defaultRegistry *PixelDataHandlerRegistry
	once            sync.Once
)

// GetDefaultRegistry returns the default pixel data handler registry
func GetDefaultRegistry() *PixelDataHandlerRegistry {
	once.Do(func() {
		defaultRegistry = &PixelDataHandlerRegistry{
			handlers: make([]PixelDataHandler, 0),
		}
		// Register built-in handlers
		defaultRegistry.Register(&UncompressedHandler{})
		defaultRegistry.Register(&RLEHandler{})
	})
	return defaultRegistry
}

// Register registers a new pixel data handler
func (r *PixelDataHandlerRegistry) Register(handler PixelDataHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = append(r.handlers, handler)
}

// FindHandler finds a handler that can decompress the given transfer syntax
func (r *PixelDataHandlerRegistry) FindHandler(transferSyntax string) PixelDataHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, handler := range r.handlers {
		if handler.CanHandle(transferSyntax) {
			return handler
		}
	}
	return nil
}

// ListHandlers returns all registered handlers
func (r *PixelDataHandlerRegistry) ListHandlers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.handlers))
	for _, handler := range r.handlers {
		names = append(names, handler.Name())
	}
	return names
}

// UncompressedHandler handles uncompressed pixel data (1.2.840.10008.1.2.1)
type UncompressedHandler struct{}

func (h *UncompressedHandler) Name() string {
	return "Explicit VR Little Endian (Uncompressed)"
}

func (h *UncompressedHandler) CanHandle(transferSyntax string) bool {
	// Uncompressed transfer syntaxes
	uncompressedSyntaxes := []string{
		uid.ExplicitVRLittleEndian().String(),
		uid.BigEndianTransferSyntax().String(),
		uid.ImplicitVRLittleEndian().String(),
		"1.2.840.10008.1.2",   // Implicit VR Little Endian
		"1.2.840.10008.1.2.1", // Explicit VR Little Endian
		"1.2.840.10008.1.2.2", // Explicit VR Big Endian
	}

	for _, syntax := range uncompressedSyntaxes {
		if transferSyntax == syntax {
			return true
		}
	}
	return false
}

func (h *UncompressedHandler) Decompress(compressedData []byte, info *PixelDataInfo) ([]byte, error) {
	// Data is already uncompressed, return as-is
	return compressedData, nil
}

// RLEHandler handles RLE (Run-Length Encoding) compressed pixel data
// (1.2.840.10008.1.2.5)
type RLEHandler struct{}

func (h *RLEHandler) Name() string {
	return "RLE Lossless"
}

func (h *RLEHandler) CanHandle(transferSyntax string) bool {
	// Check if transfer syntax is RLE Lossless using uid module
	u := uid.New(transferSyntax)
	if !u.IsValid() {
		return false
	}
	// RLE Lossless is uncompressed native format with specific UID
	rleUID := uid.New("1.2.840.10008.1.2.5")
	return u.Equals(rleUID)
}

func (h *RLEHandler) Decompress(compressedData []byte, info *PixelDataInfo) ([]byte, error) {
	return decompressRLE(compressedData, info)
}

// RegisterPixelHandler registers a custom pixel data handler globally
func RegisterPixelHandler(handler PixelDataHandler) {
	GetDefaultRegistry().Register(handler)
}

// DecompressPixelData decompresses pixel data based on the transfer syntax.
// Automatically detects the transfer syntax from the dataset and applies
// the appropriate decompression handler.
func (ds *Dataset) DecompressPixelData() ([]byte, error) {
	// Get transfer syntax from dataset
	transferSyntax, err := ds.GetTransferSyntax()
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer syntax: %w", err)
	}

	// Get raw pixel data
	compressedData, err := ds.RawPixelData()
	if err != nil {
		return nil, fmt.Errorf("failed to get pixel data: %w", err)
	}

	// Get pixel data info
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get pixel data info: %w", err)
	}

	// Find and use appropriate handler
	registry := GetDefaultRegistry()
	handler := registry.FindHandler(transferSyntax)
	if handler == nil {
		return nil, fmt.Errorf("no decompression handler found for transfer syntax: %s", transferSyntax)
	}

	// Decompress
	decompressed, err := handler.Decompress(compressedData, info)
	if err != nil {
		return nil, fmt.Errorf("decompression failed using %s: %w", handler.Name(), err)
	}

	return decompressed, nil
}

// DecompressPixelDataWithHandler uses a specific handler to decompress pixel data
func (ds *Dataset) DecompressPixelDataWithHandler(handler PixelDataHandler) ([]byte, error) {
	// Get raw pixel data
	compressedData, err := ds.RawPixelData()
	if err != nil {
		return nil, fmt.Errorf("failed to get pixel data: %w", err)
	}

	// Get pixel data info
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get pixel data info: %w", err)
	}

	// Decompress using provided handler
	return handler.Decompress(compressedData, info)
}

// GetTransferSyntax retrieves the Transfer Syntax UID from the dataset.
// Returns the UID string or error if not found.
func (ds *Dataset) GetTransferSyntax() (string, error) {
	// Transfer Syntax UID is in tag (0002,0010)
	tsElem, ok := ds.Get(tag.New(0x0002, 0x0010))
	if !ok {
		// Try common location (0008,0016 for SOP Class UID)
		// or default to uncompressed
		return uid.ExplicitVRLittleEndian().String(), nil
	}

	ts, err := extractStringValue(tsElem)
	if err != nil {
		return "", fmt.Errorf("failed to extract transfer syntax: %w", err)
	}

	return ts, nil
}

// RLE Decompression Implementation
// Run-Length Encoding is a simple compression scheme where repeated values
// are stored as a count followed by the value(s).

// decompressRLE decompresses RLE-encoded pixel data
func decompressRLE(compressedData []byte, info *PixelDataInfo) ([]byte, error) {
	if len(compressedData) == 0 {
		return nil, fmt.Errorf("compressed data is empty")
	}

	// RLE format: header + encoded segments
	// Header contains offsets to segment starts
	expectedSize := info.NumberOfFrames * info.BytesPerFrame

	decompressed := make([]byte, 0, expectedSize)

	// RLE segments are typically one per frame
	offset := 0

	for frame := 0; frame < info.NumberOfFrames; frame++ {
		// Decompress one frame
		frameData, bytesRead, err := decompressRLESegment(compressedData, offset, info)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress RLE frame %d: %w", frame, err)
		}

		decompressed = append(decompressed, frameData...)
		offset += bytesRead
	}

	return decompressed, nil
}

// decompressRLESegment decompresses a single RLE segment
func decompressRLESegment(data []byte, offset int, info *PixelDataInfo) ([]byte, int, error) {
	output := make([]byte, 0, info.BytesPerFrame)
	currentOffset := offset
	bytesProcessed := 0

	expectedOutputSize := info.BytesPerFrame
	bytesPerSample := info.BitsAllocated / 8

	for len(output) < expectedOutputSize && currentOffset < len(data) {
		header := data[currentOffset]
		currentOffset++
		bytesProcessed++

		if header <= 127 {
			// Literal run: copy next (header+1) bytes
			runLength := int(header) + 1
			requiredBytes := runLength * bytesPerSample

			if currentOffset+requiredBytes > len(data) {
				return nil, 0, fmt.Errorf("insufficient data for literal run")
			}

			output = append(output, data[currentOffset:currentOffset+requiredBytes]...)
			currentOffset += requiredBytes
			bytesProcessed += requiredBytes
		} else if header >= 128 {
			// Repeat run: repeat next value (129-header+1) times
			runLength := 257 - int(header)

			if currentOffset+bytesPerSample > len(data) {
				return nil, 0, fmt.Errorf("insufficient data for repeat run")
			}

			repeatValue := data[currentOffset : currentOffset+bytesPerSample]
			currentOffset += bytesPerSample
			bytesProcessed += bytesPerSample

			for i := 0; i < runLength; i++ {
				output = append(output, repeatValue...)
			}
		}
	}

	if len(output) != expectedOutputSize {
		return nil, 0, fmt.Errorf("decompressed size mismatch: got %d, expected %d", len(output), expectedOutputSize)
	}

	return output, bytesProcessed, nil
}

// SupportedCompressions returns a list of supported compression formats
func SupportedCompressions() []string {
	registry := GetDefaultRegistry()
	return registry.ListHandlers()
}

// IsCompressionSupported checks if a transfer syntax is supported
func IsCompressionSupported(transferSyntax string) bool {
	registry := GetDefaultRegistry()
	return registry.FindHandler(transferSyntax) != nil
}

// GetCompressionInfo returns detailed information about supported compressions
type CompressionInfo struct {
	Name           string
	TransferSyntax string
	Lossless       bool
	Lossy          bool
}

// GetSupportedCompressions returns information about all supported compressions
// Uses uid module constants for transfer syntax UIDs
func GetSupportedCompressions() []CompressionInfo {
	return []CompressionInfo{
		{
			Name:           "Explicit VR Little Endian",
			TransferSyntax: uid.ExplicitVRLittleEndian().String(),
			Lossless:       true,
			Lossy:          false,
		},
		{
			Name:           "Implicit VR Little Endian",
			TransferSyntax: uid.ImplicitVRLittleEndian().String(),
			Lossless:       true,
			Lossy:          false,
		},
		{
			Name:           "Explicit VR Big Endian",
			TransferSyntax: uid.BigEndianTransferSyntax().String(),
			Lossless:       true,
			Lossy:          false,
		},
		{
			Name:           "RLE Lossless",
			TransferSyntax: uid.New("1.2.840.10008.1.2.5").String(),
			Lossless:       true,
			Lossy:          false,
		},
	}
}

// DecompressionStats tracks decompression performance
type DecompressionStats struct {
	InputSize        int64
	OutputSize       int64
	CompressionRatio float64
	Handler          string
}

// DecompressPixelDataWithStats decompresses pixel data and returns statistics
func (ds *Dataset) DecompressPixelDataWithStats() ([]byte, *DecompressionStats, error) {
	transferSyntax, err := ds.GetTransferSyntax()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get transfer syntax: %w", err)
	}

	compressedData, err := ds.RawPixelData()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get pixel data: %w", err)
	}

	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get pixel data info: %w", err)
	}

	registry := GetDefaultRegistry()
	handler := registry.FindHandler(transferSyntax)
	if handler == nil {
		return nil, nil, fmt.Errorf("no decompression handler found for transfer syntax: %s", transferSyntax)
	}

	decompressed, err := handler.Decompress(compressedData, info)
	if err != nil {
		return nil, nil, fmt.Errorf("decompression failed: %w", err)
	}

	stats := &DecompressionStats{
		InputSize:  int64(len(compressedData)),
		OutputSize: int64(len(decompressed)),
		Handler:    handler.Name(),
	}

	if stats.InputSize > 0 {
		stats.CompressionRatio = float64(stats.OutputSize) / float64(stats.InputSize)
	}

	return decompressed, stats, nil
}
