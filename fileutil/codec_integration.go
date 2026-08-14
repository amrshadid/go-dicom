package fileutil

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// CodecIntegration handles codec selection and pixel data decompression
type CodecIntegration struct {
	registry *compress.DecompressorRegistry
	cache    *PixelDataCache
}

// NewCodecIntegration creates a new codec integration manager
func NewCodecIntegration() *CodecIntegration {
	return &CodecIntegration{
		registry: compress.NewDecompressorRegistry(),
		cache:    NewPixelDataCache(100), // Default cache size
	}
}

// DeferredPixelDataReader allows lazy loading of pixel data
type DeferredPixelDataReader struct {
	filePath        string
	pixelDataTag    uint32
	compressionType compress.CompressionType
	offset          int64
	length          int64
	integration     *CodecIntegration
	loaded          bool
	data            []byte
	mu              sync.RWMutex
}

// NewDeferredPixelDataReader creates a new deferred pixel data reader
func NewDeferredPixelDataReader(filePath string, compressionType compress.CompressionType,
	offset int64, length int64, integration *CodecIntegration) *DeferredPixelDataReader {
	return &DeferredPixelDataReader{
		filePath:        filePath,
		pixelDataTag:    uint32(tag.New(0x7FE0, 0x0010)), // PixelData tag
		compressionType: compressionType,
		offset:          offset,
		length:          length,
		integration:     integration,
		loaded:          false,
	}
}

// Load loads the pixel data from file (only on first access)
func (dpdr *DeferredPixelDataReader) Load() error {
	dpdr.mu.Lock()
	defer dpdr.mu.Unlock()

	// Check if already loaded
	if dpdr.loaded {
		return nil
	}

	// Try to get from cache first
	cacheKey := fmt.Sprintf("%s:%d:%d", dpdr.filePath, dpdr.offset, dpdr.length)
	if cached, exists := dpdr.integration.cache.Get(cacheKey); exists {
		if data, ok := cached.([]byte); ok {
			dpdr.data = data
			dpdr.loaded = true
			return nil
		}
		// A cache entry of another type means the key collided with something
		// else. Fall through and read the file rather than panicking on the
		// assertion.
	}

	data, err := dpdr.integration.loadFromFile(dpdr.filePath, dpdr.offset, dpdr.length, cacheKey)
	if err != nil {
		return err
	}

	dpdr.data = data
	dpdr.loaded = true
	return nil
}

// Get returns the decompressed pixel data, reading it on first access.
func (dpdr *DeferredPixelDataReader) Get() ([]byte, error) {
	// Read dpdr.loaded through IsLoaded rather than directly: Load writes it
	// under the write lock, so an unsynchronised read here races with a
	// concurrent Get. Load re-checks the flag under the lock, so the worst a
	// stale answer costs is one extra call into it.
	if !dpdr.IsLoaded() {
		if err := dpdr.Load(); err != nil {
			return nil, err
		}
	}

	dpdr.mu.RLock()
	defer dpdr.mu.RUnlock()

	// Decompress if necessary
	if dpdr.compressionType == compress.UNCOMPRESSED {
		return dpdr.data, nil
	}

	return dpdr.integration.Decompress(dpdr.data, dpdr.compressionType)
}

// IsLoaded returns whether the pixel data has been loaded
func (dpdr *DeferredPixelDataReader) IsLoaded() bool {
	dpdr.mu.RLock()
	defer dpdr.mu.RUnlock()
	return dpdr.loaded
}

// PixelDataCache caches decompressed pixel data to avoid redundant decompression
type PixelDataCache struct {
	mu      sync.RWMutex
	cache   map[string][]byte
	maxSize int
	size    int
}

// NewPixelDataCache creates a new pixel data cache
func NewPixelDataCache(maxSize int) *PixelDataCache {
	return &PixelDataCache{
		cache:   make(map[string][]byte),
		maxSize: maxSize,
		size:    0,
	}
}

// Get retrieves cached pixel data
func (pdc *PixelDataCache) Get(key string) (interface{}, bool) {
	pdc.mu.RLock()
	defer pdc.mu.RUnlock()

	data, exists := pdc.cache[key]
	return data, exists
}

// Set stores pixel data in the cache
func (pdc *PixelDataCache) Set(key string, data []byte) {
	pdc.mu.Lock()
	defer pdc.mu.Unlock()

	// Simple eviction: if cache is full, clear half of it
	if len(pdc.cache) >= pdc.maxSize {
		count := 0
		for k := range pdc.cache {
			delete(pdc.cache, k)
			count++
			if count >= pdc.maxSize/2 {
				break
			}
		}
	}

	pdc.cache[key] = data
	pdc.size += len(data)
}

// Clear clears the cache
func (pdc *PixelDataCache) Clear() {
	pdc.mu.Lock()
	defer pdc.mu.Unlock()

	pdc.cache = make(map[string][]byte)
	pdc.size = 0
}

// Decompress decompresses pixel data using the appropriate codec
func (ci *CodecIntegration) Decompress(data []byte, compressionType compress.CompressionType) ([]byte, error) {
	if compressionType == compress.UNCOMPRESSED {
		return data, nil
	}

	decompressor, err := ci.registry.Get(compressionType)
	if err != nil {
		return nil, fmt.Errorf("decompressor not available for %s: %w", compressionType, err)
	}

	return decompressor.Decompress(data)
}

// GetCompressionInfo returns information about a compression method
func (ci *CodecIntegration) GetCompressionInfo(compressionType compress.CompressionType) compress.CompressionInfo {
	return compress.GetCompressionInfo(compressionType)
}

// IsCompressionSupported checks if a compression type is supported
func (ci *CodecIntegration) IsCompressionSupported(compressionType compress.CompressionType) bool {
	info := compress.GetCompressionInfo(compressionType)
	return info.IsSupported
}

// GetSupportedCompressions returns list of supported compression types
func (ci *CodecIntegration) GetSupportedCompressions() []compress.CompressionType {
	return []compress.CompressionType{
		compress.UNCOMPRESSED,
		compress.DEFLATE,
		compress.RLE,
		compress.JPEG,
	}
}

// RegisterCustomCodec allows registration of custom decompressors
func (ci *CodecIntegration) RegisterCustomCodec(compressionType compress.CompressionType,
	decompressor compress.Decompressor) error {
	return ci.registry.Register(compressionType, decompressor)
}

// DecompressSegmentedPixelData handles multi-segment pixel data decompression
func (ci *CodecIntegration) DecompressSegmentedPixelData(segments [][]byte,
	compressionType compress.CompressionType) ([]byte, error) {

	if len(segments) == 0 {
		return nil, fmt.Errorf("no segments to decompress")
	}

	if len(segments) == 1 {
		return ci.Decompress(segments[0], compressionType)
	}

	// For multiple segments, decompress each and concatenate
	var result []byte
	for i, segment := range segments {
		decompressed, err := ci.Decompress(segment, compressionType)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress segment %d: %w", i, err)
		}
		result = append(result, decompressed...)
	}

	return result, nil
}

// ValidatePixelData validates pixel data integrity
func (ci *CodecIntegration) ValidatePixelData(data []byte, compressionType compress.CompressionType,
	expectedLength int) error {

	if len(data) == 0 {
		return fmt.Errorf("pixel data is empty")
	}

	if compressionType != compress.UNCOMPRESSED {
		// For compressed data, just verify it can be decompressed
		_, err := ci.Decompress(data, compressionType)
		if err != nil {
			return fmt.Errorf("pixel data decompression failed: %w", err)
		}
	}

	// For uncompressed, verify length matches expected
	if compressionType == compress.UNCOMPRESSED && expectedLength > 0 {
		if len(data) != expectedLength {
			return fmt.Errorf("pixel data length mismatch: got %d, expected %d", len(data), expectedLength)
		}
	}

	return nil
}

// MaxDeferredPixelDataLength bounds a single deferred read.
//
// The length comes from an element header, which is attacker-controlled: a file
// declaring 4 GiB of pixel data would otherwise have that allocated up front,
// before any of it is read. 1 GiB is above any legitimate single instance —
// PS3.5 caps a value length at 2^32-2 bytes, and the largest real pixel data is
// orders of magnitude below this — while keeping a hostile file cheap to reject.
const MaxDeferredPixelDataLength int64 = 1 << 30

// loadFromFile reads length bytes at offset from filePath and caches them.
//
// The read is bounded twice: against MaxDeferredPixelDataLength before
// allocating, and against the file's actual size, so a declared length longer
// than the file is refused rather than returning a short buffer the caller
// cannot tell is short. io.ReadFull enforces that the whole extent was present.
func (ci *CodecIntegration) loadFromFile(filePath string, offset int64, length int64,
	cacheKey string) ([]byte, error) {

	if offset < 0 {
		return nil, fmt.Errorf("offset %d is negative", offset)
	}
	if length <= 0 {
		return nil, fmt.Errorf("length %d is not positive", length)
	}
	if length > MaxDeferredPixelDataLength {
		return nil, fmt.Errorf("declared length %d exceeds the %d byte limit on a deferred read",
			length, MaxDeferredPixelDataLength)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	// Check the extent against the file before allocating for it. Without this a
	// file declaring a gigabyte and holding a kilobyte still costs a gigabyte.
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", filePath, err)
	}
	if end := offset + length; end > info.Size() {
		return nil, fmt.Errorf("%s is %d bytes; pixel data at offset %d for %d bytes ends at %d",
			filePath, info.Size(), offset, length, end)
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seeking to %d in %s: %w", offset, filePath, err)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, fmt.Errorf("reading %d bytes at offset %d from %s: %w",
			length, offset, filePath, err)
	}

	ci.cache.Set(cacheKey, data)
	return data, nil
}

// CompressionStatistics calculates compression statistics
type CompressionStatistics struct {
	OriginalSize     int64
	CompressedSize   int64
	CompressionRatio float64
	CompressionType  compress.CompressionType
	IsLossless       bool
}

// CalculateCompressionStatistics calculates statistics for compressed data
func (ci *CodecIntegration) CalculateCompressionStatistics(
	originalData []byte,
	compressedData []byte,
	compressionType compress.CompressionType) CompressionStatistics {

	var ratio float64
	if len(originalData) > 0 {
		ratio = float64(len(compressedData)) / float64(len(originalData))
	}

	info := compress.GetCompressionInfo(compressionType)

	return CompressionStatistics{
		OriginalSize:     int64(len(originalData)),
		CompressedSize:   int64(len(compressedData)),
		CompressionRatio: ratio,
		CompressionType:  compressionType,
		IsLossless:       info.IsLossless,
	}
}

// TransferSyntaxSupport provides information about transfer syntax support
type TransferSyntaxSupport struct {
	UID              string
	Name             string
	CompressionType  compress.CompressionType
	IsSupported      bool
	IsLossless       bool
	RequiresExternal bool
	Description      string
}

// GetTransferSyntaxSupport returns support information for a transfer syntax
// Uses uid module to get transfer syntax metadata
func (ci *CodecIntegration) GetTransferSyntaxSupport(uidStr string) TransferSyntaxSupport {
	// Validate and classify transfer syntax using uid module
	u := uid.New(uidStr)

	// Get UID metadata from uid module
	uidInfo := uid.GetUIDInfo(uidStr)

	// Determine compression type based on transfer syntax
	compressionType := compress.UNCOMPRESSED
	name := "Unknown"
	description := "Unknown transfer syntax"

	if uidInfo != nil {
		name = uidInfo.Name
		description = uidInfo.Description

		// Map transfer syntaxes to compression types
		if uid.IsCompressed(u) {
			// Determine specific compression type for known compressed syntaxes
			switch uidStr {
			case "1.2.840.10008.1.2.5": // RLE Lossless
				compressionType = compress.RLE
			case "1.2.840.10008.1.2.4.50", "1.2.840.10008.1.2.4.51", // JPEG Baseline, Extended
				"1.2.840.10008.1.2.4.57", "1.2.840.10008.1.2.4.70": // JPEG Lossless
				compressionType = compress.JPEG
			case "1.2.840.10008.1.2.1.99": // Deflated Explicit VR
				compressionType = compress.DEFLATE
			default:
				// Other compressed syntaxes (may need additional handlers)
				compressionType = compress.UNCOMPRESSED // Default to uncompressed if not recognized
			}
		} else {
			compressionType = compress.UNCOMPRESSED
		}
	} else if !u.IsValid() {
		// Invalid UID
		return TransferSyntaxSupport{
			UID:              uidStr,
			Name:             "Invalid",
			CompressionType:  compress.UNCOMPRESSED,
			IsSupported:      false,
			IsLossless:       false,
			RequiresExternal: true,
			Description:      "Invalid transfer syntax UID",
		}
	}

	// Get compression info
	info := compress.GetCompressionInfo(compressionType)

	return TransferSyntaxSupport{
		UID:              uidStr,
		Name:             name,
		CompressionType:  compressionType,
		IsSupported:      info.IsSupported,
		IsLossless:       info.IsLossless,
		RequiresExternal: info.RequiresExternal,
		Description:      description,
	}
}
