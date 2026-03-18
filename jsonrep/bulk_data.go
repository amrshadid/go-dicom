package jsonrep

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BulkDataReference represents a reference to bulk data in DICOM JSON Model per DICOM Part 18,
// enabling efficient handling of large binary data without embedding in JSON.
type BulkDataReference struct {
	URI         string `json:"uri"`                   // URI to bulk data location
	ContentType string `json:"contentType,omitempty"` // MIME type (e.g., "application/octet-stream")
	Length      int64  `json:"length,omitempty"`      // Size in bytes
	Hash        string `json:"hash,omitempty"`        // SHA-256 hash for integrity verification
	Offset      int64  `json:"offset,omitempty"`      // Offset in bytes for partial references
}

// BulkDataStorage defines storage backend types
type BulkDataStorage string

const (
	StorageFile     BulkDataStorage = "file"
	StorageMemory   BulkDataStorage = "memory"
	StorageExternal BulkDataStorage = "external"
)

// BulkDataHandler manages bulk data references, storage, and retrieval,
// abstracting multiple storage backends (file, memory, external) for flexible DICOM bulk data handling.
type BulkDataHandler struct {
	baseURI        string
	storage        BulkDataStorage
	fileDir        string
	memoryData     map[string][]byte
	references     map[string]*BulkDataReference
	referenceOrder []string // Maintain insertion order to ensure deterministic JSON output and reference consistency
	mu             sync.RWMutex
}

// NewBulkDataHandler creates a new bulk data handler with specified storage backend
func NewBulkDataHandler(baseURI string, storage BulkDataStorage) (*BulkDataHandler, error) {
	if baseURI == "" {
		return nil, fmt.Errorf("baseURI cannot be empty")
	}

	bdh := &BulkDataHandler{
		baseURI:        baseURI,
		storage:        storage,
		memoryData:     make(map[string][]byte),
		references:     make(map[string]*BulkDataReference),
		referenceOrder: make([]string, 0),
	}

	// For file storage, create directory if needed to enable idempotent initialization.
	if storage == StorageFile {
		dir := filepath.Join(baseURI, "bulk_data")
		bdh.fileDir = dir
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create bulk data directory: %w", err)
		}
	}

	return bdh, nil
}

// CreateReference creates a new bulk data reference from raw data,
// storing it according to the configured storage backend and using SHA-256 hashing for deduplication and integrity verification.
func (bdh *BulkDataHandler) CreateReference(data []byte, contentType string) (*BulkDataReference, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	bdh.mu.Lock()
	defer bdh.mu.Unlock()

	// Calculate SHA-256 hash to verify data integrity.
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// Create reference ID using first 16 characters of hash for balance between uniqueness and brevity.
	refID := hashStr[:16]

	// Check if reference already exists for deduplication; return existing reference to avoid storing duplicate data.
	if _, exists := bdh.references[refID]; exists {
		return bdh.references[refID], nil
	}

	ref := &BulkDataReference{
		URI:         fmt.Sprintf("%s/bulk_data/%s", bdh.baseURI, refID),
		ContentType: contentType,
		Length:      int64(len(data)),
		Hash:        hashStr,
	}

	// Store data based on configured storage backend (file, memory, or external).
	switch bdh.storage {
	case StorageFile:
		filePath := filepath.Join(bdh.fileDir, refID)
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return nil, fmt.Errorf("failed to write bulk data to file: %w", err)
		}

	case StorageMemory:
		bdh.memoryData[refID] = data

	case StorageExternal:
		// For external storage, only track the reference; actual data storage is handled elsewhere.

	default:
		return nil, fmt.Errorf("unsupported storage backend: %v", bdh.storage)
	}

	// Store reference
	bdh.references[refID] = ref
	bdh.referenceOrder = append(bdh.referenceOrder, refID)

	return ref, nil
}

// ResolveReference retrieves the data associated with a bulk data reference
func (bdh *BulkDataHandler) ResolveReference(ref *BulkDataReference) ([]byte, error) {
	if ref == nil {
		return nil, fmt.Errorf("reference cannot be nil")
	}

	bdh.mu.RLock()
	defer bdh.mu.RUnlock()

	// Extract reference ID from URI (the final path component).
	refID := filepath.Base(ref.URI)

	switch bdh.storage {
	case StorageFile:
		filePath := filepath.Join(bdh.fileDir, refID)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read bulk data from file: %w", err)
		}
		return data, nil

	case StorageMemory:
		data, exists := bdh.memoryData[refID]
		if !exists {
			return nil, fmt.Errorf("bulk data not found in memory: %s", refID)
		}
		return data, nil

	case StorageExternal:
		return nil, fmt.Errorf("cannot resolve external storage references directly")

	default:
		return nil, fmt.Errorf("unsupported storage backend: %v", bdh.storage)
	}
}

// ValidateReference verifies that a reference is intact by checking hash
func (bdh *BulkDataHandler) ValidateReference(ref *BulkDataReference) error {
	if ref == nil {
		return fmt.Errorf("reference cannot be nil")
	}

	if ref.Hash == "" {
		return fmt.Errorf("reference has no hash for validation")
	}

	// Retrieve the data
	data, err := bdh.ResolveReference(ref)
	if err != nil {
		return fmt.Errorf("failed to resolve reference for validation: %w", err)
	}

	// Calculate SHA-256 hash to verify data integrity matches the reference.
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	if hashStr != ref.Hash {
		return fmt.Errorf("reference validation failed: hash mismatch (expected %s, got %s)", ref.Hash, hashStr)
	}

	if int64(len(data)) != ref.Length {
		return fmt.Errorf("reference validation failed: size mismatch (expected %d, got %d)", ref.Length, int64(len(data)))
	}

	return nil
}

// ListReferences returns all stored bulk data references
func (bdh *BulkDataHandler) ListReferences() []*BulkDataReference {
	bdh.mu.RLock()
	defer bdh.mu.RUnlock()

	refs := make([]*BulkDataReference, 0, len(bdh.referenceOrder))
	for _, refID := range bdh.referenceOrder {
		if ref, exists := bdh.references[refID]; exists {
			refs = append(refs, ref)
		}
	}
	return refs
}

// DeleteReference removes a bulk data reference and its associated data
func (bdh *BulkDataHandler) DeleteReference(ref *BulkDataReference) error {
	if ref == nil {
		return fmt.Errorf("reference cannot be nil")
	}

	bdh.mu.Lock()
	defer bdh.mu.Unlock()

	refID := filepath.Base(ref.URI)

	switch bdh.storage {
	case StorageFile:
		filePath := filepath.Join(bdh.fileDir, refID)
		if err := os.Remove(filePath); err != nil {
			return fmt.Errorf("failed to delete bulk data file: %w", err)
		}

	case StorageMemory:
		delete(bdh.memoryData, refID)

	case StorageExternal:
		// External storage: just remove reference tracking
	}

	// Remove reference
	delete(bdh.references, refID)
	for i, id := range bdh.referenceOrder {
		if id == refID {
			bdh.referenceOrder = append(bdh.referenceOrder[:i], bdh.referenceOrder[i+1:]...)
			break
		}
	}

	return nil
}

// GetReferenceCount returns the number of stored references
func (bdh *BulkDataHandler) GetReferenceCount() int {
	bdh.mu.RLock()
	defer bdh.mu.RUnlock()

	return len(bdh.references)
}

// GetTotalSize returns the total size of all bulk data
func (bdh *BulkDataHandler) GetTotalSize() int64 {
	bdh.mu.RLock()
	defer bdh.mu.RUnlock()

	var total int64
	for _, ref := range bdh.references {
		total += ref.Length
	}
	return total
}

// JSONElementWithBulk represents a DICOM element that may contain bulk data references instead of inline values,
// reducing JSON size for elements with large binary content.
type JSONElementWithBulk struct {
	VR       string             `json:"vr"`
	Value    interface{}        `json:"value,omitempty"`
	BulkData *BulkDataReference `json:"BulkDataURI,omitempty"`
}

// JSONEncoderWithBulk extends JSON encoding with bulk data support,
// automatically externalizing large values based on configurable size thresholds.
type JSONEncoderWithBulk struct {
	bulkHandler *BulkDataHandler
	threshold   int64 // Switch to bulk data above this size (bytes)
	mu          sync.RWMutex
}

// NewJSONEncoderWithBulk creates a new JSON encoder with bulk data support
func NewJSONEncoderWithBulk(bulkHandler *BulkDataHandler, threshold int64) *JSONEncoderWithBulk {
	if threshold <= 0 {
		threshold = 1024 * 1024 // Default: 1MB
	}

	return &JSONEncoderWithBulk{
		bulkHandler: bulkHandler,
		threshold:   threshold,
	}
}

// EncodeDatasetWithBulk encodes a DICOM dataset with bulk data support,
// returning a map of elements with either inline values or bulk data references (for values exceeding threshold).
func (je *JSONEncoderWithBulk) EncodeDatasetWithBulk(dataset *DicomDataset, elements map[string]interface{}) (map[string]JSONElementWithBulk, error) {
	if dataset == nil {
		return nil, fmt.Errorf("dataset cannot be nil")
	}

	je.mu.RLock()
	defer je.mu.RUnlock()

	result := make(map[string]JSONElementWithBulk)

	// Process elements
	for key, value := range elements {
		elem := JSONElementWithBulk{}

		// Determine VR (Value Representation) based on element key per DICOM standard.
		elem.VR = DetermineVR(key)

		// Check if value exceeds threshold for bulk data storage.
		data, isBulk := isBulkData(value, je.threshold)
		if isBulk && len(data) > 0 {
			// Create bulk data reference
			ref, err := je.bulkHandler.CreateReference(data, "application/octet-stream")
			if err != nil {
				return nil, fmt.Errorf("failed to create bulk data reference for %s: %w", key, err)
			}
			elem.BulkData = ref
		} else {
			elem.Value = value
		}

		result[key] = elem
	}

	return result, nil
}

// DecodeDatasetWithBulk decodes a DICOM dataset from JSON with bulk data resolution
func (je *JSONEncoderWithBulk) DecodeDatasetWithBulk(encodedData map[string]JSONElementWithBulk) (map[string]interface{}, error) {
	if len(encodedData) == 0 {
		return nil, fmt.Errorf("encoded data cannot be empty")
	}

	je.mu.RLock()
	defer je.mu.RUnlock()

	result := make(map[string]interface{})

	for key, elem := range encodedData {
		if elem.BulkData != nil {
			// Resolve bulk data reference
			data, err := je.bulkHandler.ResolveReference(elem.BulkData)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve bulk data for %s: %w", key, err)
			}
			result[key] = data
		} else {
			result[key] = elem.Value
		}
	}

	return result, nil
}

// ValidateReferences validates all bulk data references
func (je *JSONEncoderWithBulk) ValidateReferences() error {
	je.mu.RLock()
	defer je.mu.RUnlock()

	refs := je.bulkHandler.ListReferences()
	for _, ref := range refs {
		if err := je.bulkHandler.ValidateReference(ref); err != nil {
			return fmt.Errorf("validation failed for reference %s: %w", ref.URI, err)
		}
	}

	return nil
}

// CreateCompactJSON creates a minimal JSON representation with bulk data references
func (je *JSONEncoderWithBulk) CreateCompactJSON(dataset *DicomDataset, elements map[string]interface{}) ([]byte, error) {
	if dataset == nil {
		return nil, fmt.Errorf("dataset cannot be nil")
	}

	// Encode with bulk data
	encoded, err := je.EncodeDatasetWithBulk(dataset, elements)
	if err != nil {
		return nil, fmt.Errorf("failed to encode dataset: %w", err)
	}

	// Create wrapper structure
	wrapper := map[string]interface{}{
		"metadata": map[string]interface{}{
			"timestamp":     time.Now(),
			"totalBulkData": je.bulkHandler.GetReferenceCount(),
			"totalSize":     je.bulkHandler.GetTotalSize(),
		},
		"elements": encoded,
	}

	return json.Marshal(wrapper)
}

// CreateRoundTripJSON creates JSON that can be round-tripped (encoded then decoded)
func (je *JSONEncoderWithBulk) CreateRoundTripJSON(dataset *DicomDataset, elements map[string]interface{}) ([]byte, error) {
	encoded, err := je.EncodeDatasetWithBulk(dataset, elements)
	if err != nil {
		return nil, fmt.Errorf("failed to encode for round-trip: %w", err)
	}

	return json.MarshalIndent(encoded, "", "  ")
}

// GetBulkDataStats returns statistics about bulk data storage
type BulkDataStats struct {
	TotalReferences int64
	TotalSize       int64
	AverageSize     int64
	StorageBackend  string
	CreatedAt       time.Time
}

// GetStats returns statistics about bulk data
func (bdh *BulkDataHandler) GetStats() BulkDataStats {
	bdh.mu.RLock()
	defer bdh.mu.RUnlock()

	stats := BulkDataStats{
		TotalReferences: int64(len(bdh.references)),
		TotalSize:       0,
		StorageBackend:  string(bdh.storage),
		CreatedAt:       time.Now(),
	}

	if len(bdh.references) > 0 {
		for _, ref := range bdh.references {
			stats.TotalSize += ref.Length
		}
		stats.AverageSize = stats.TotalSize / stats.TotalReferences
	}

	return stats
}

// CopyBulkDataHandler creates a deep copy of the handler's references
func (bdh *BulkDataHandler) CopyBulkDataHandler(other *BulkDataHandler) error {
	if other == nil {
		return fmt.Errorf("source handler cannot be nil")
	}

	other.mu.RLock()
	defer other.mu.RUnlock()

	bdh.mu.Lock()
	defer bdh.mu.Unlock()

	for _, ref := range other.references {
		// Copy reference metadata only, not the actual data
		// Actual data would need explicit resolve and create calls
		refCopy := *ref
		refID := filepath.Base(ref.URI)
		bdh.references[refID] = &refCopy
		bdh.referenceOrder = append(bdh.referenceOrder, refID)

		// For memory storage, also copy the data
		if other.storage == StorageMemory {
			if data, exists := other.memoryData[refID]; exists {
				dataCopy := make([]byte, len(data))
				copy(dataCopy, data)
				bdh.memoryData[refID] = dataCopy
			}
		}
	}

	return nil
}

// DetermineVR determines the Value Representation from an element key
func DetermineVR(key string) string {
	switch key {
	case "PatientName":
		return "PN"
	case "PatientID", "StudyID", "SeriesNumber":
		return "LO"
	case "StudyInstanceUID", "SeriesInstanceUID", "SOPInstanceUID", "SOPClassUID":
		return "UI"
	case "Modality":
		return "CS"
	case "StudyDate", "SeriesDate", "ContentDate":
		return "DA"
	case "StudyTime", "SeriesTime", "ContentTime":
		return "TM"
	case "InstanceNumber":
		return "IS"
	default:
		return "UN" // Unknown
	}
}

// isBulkData checks if a value should be stored as bulk data based on configured threshold
func isBulkData(value interface{}, threshold int64) ([]byte, bool) {
	switch v := value.(type) {
	case []byte:
		return v, int64(len(v)) > threshold
	case string:
		data := []byte(v)
		return data, int64(len(data)) > threshold
	default:
		// For other types, marshal to JSON and check size
		if data, err := json.Marshal(v); err == nil {
			return data, int64(len(data)) > threshold
		}
	}
	return nil, false
}

// CompressForTransmission creates a transmission-optimized JSON format with a manifest including metadata and bulk data references
func (je *JSONEncoderWithBulk) CompressForTransmission(dataset *DicomDataset, elements map[string]interface{}) ([]byte, error) {
	encoded, err := je.EncodeDatasetWithBulk(dataset, elements)
	if err != nil {
		return nil, fmt.Errorf("failed to encode for transmission: %w", err)
	}

	// Create transmission manifest
	manifest := map[string]interface{}{
		"version":      "1.0",
		"timestamp":    time.Now(),
		"bulkDataRefs": je.bulkHandler.GetReferenceCount(),
		"totalSize":    je.bulkHandler.GetTotalSize(),
		"elements":     encoded,
	}

	return json.Marshal(manifest)
}

// ExtractBulkDataReferences gets all bulk data references from encoded data
func ExtractBulkDataReferences(encodedData map[string]JSONElementWithBulk) []*BulkDataReference {
	refs := make([]*BulkDataReference, 0)

	for _, elem := range encodedData {
		if elem.BulkData != nil {
			refs = append(refs, elem.BulkData)
		}
	}

	return refs
}

// StreamBulkData streams bulk data from one handler to another
func StreamBulkData(src, dst *BulkDataHandler, ref *BulkDataReference) error {
	if src == nil || dst == nil {
		return fmt.Errorf("source and destination handlers cannot be nil")
	}

	// Read from source
	data, err := src.ResolveReference(ref)
	if err != nil {
		return fmt.Errorf("failed to read from source: %w", err)
	}

	// Write to destination
	_, err = dst.CreateReference(data, ref.ContentType)
	if err != nil {
		return fmt.Errorf("failed to write to destination: %w", err)
	}

	return nil
}

// StreamBulkDataReader implements io.Reader for streaming large bulk data without loading entire content into memory
type StreamBulkDataReader struct {
	handler *BulkDataHandler
	ref     *BulkDataReference
	file    *os.File
	mu      sync.RWMutex
}

// NewStreamBulkDataReader creates a new streaming reader for bulk data
func NewStreamBulkDataReader(handler *BulkDataHandler, ref *BulkDataReference) (*StreamBulkDataReader, error) {
	if handler.storage != StorageFile {
		return nil, fmt.Errorf("streaming is only supported for file storage")
	}

	refID := filepath.Base(ref.URI)
	filePath := filepath.Join(handler.fileDir, refID)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open bulk data file: %w", err)
	}

	return &StreamBulkDataReader{
		handler: handler,
		ref:     ref,
		file:    file,
	}, nil
}

// Read implements io.Reader interface
func (sr *StreamBulkDataReader) Read(p []byte) (n int, err error) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	if sr.file == nil {
		return 0, fmt.Errorf("reader is closed")
	}

	return sr.file.Read(p)
}

// Close closes the reader
func (sr *StreamBulkDataReader) Close() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if sr.file == nil {
		return nil
	}

	return sr.file.Close()
}

// Copy implements io.Copy-like functionality
func (sr *StreamBulkDataReader) Copy(w io.Writer) (n int64, err error) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	return io.Copy(w, sr.file)
}
