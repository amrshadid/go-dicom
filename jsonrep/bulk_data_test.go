package jsonrep_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/jsonrep"
)

// Test BulkDataHandler creation

func TestNewBulkDataHandlerMemory(t *testing.T) {
	bdh, err := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	if err != nil {
		t.Fatalf("Failed to create bulk data handler: %v", err)
	}
	if bdh == nil {
		t.Error("Expected handler, got nil")
	}
}

func TestNewBulkDataHandlerFile(t *testing.T) {
	tempDir := t.TempDir()
	bdh, err := jsonrep.NewBulkDataHandler(tempDir, jsonrep.StorageFile)
	if err != nil {
		t.Fatalf("Failed to create bulk data handler: %v", err)
	}
	if bdh == nil {
		t.Error("Expected handler, got nil")
	}

	// Verify directory was created
	bulkDir := filepath.Join(tempDir, "bulk_data")
	if _, err := os.Stat(bulkDir); os.IsNotExist(err) {
		t.Error("Bulk data directory was not created")
	}
}

func TestNewBulkDataHandlerExternal(t *testing.T) {
	bdh, err := jsonrep.NewBulkDataHandler("http://external.com", jsonrep.StorageExternal)
	if err != nil {
		t.Fatalf("Failed to create bulk data handler: %v", err)
	}
	if bdh == nil {
		t.Error("Expected handler, got nil")
	}
}

func TestNewBulkDataHandlerEmptyURI(t *testing.T) {
	_, err := jsonrep.NewBulkDataHandler("", jsonrep.StorageMemory)
	if err == nil {
		t.Error("Expected error for empty URI")
	}
}

// Test CreateReference

func TestCreateReferenceMemory(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	data := []byte("test data")

	ref, err := bdh.CreateReference(data, "text/plain")
	if err != nil {
		t.Fatalf("Failed to create reference: %v", err)
	}

	if ref == nil {
		t.Fatal("Expected reference, got nil")
	}
	if ref.ContentType != "text/plain" {
		t.Errorf("Expected text/plain, got %s", ref.ContentType)
	}
	if ref.Length != int64(len(data)) {
		t.Errorf("Expected length %d, got %d", len(data), ref.Length)
	}
	if ref.Hash == "" {
		t.Error("Expected hash, got empty string")
	}
}

func TestCreateReferenceFile(t *testing.T) {
	tempDir := t.TempDir()
	bdh, _ := jsonrep.NewBulkDataHandler(tempDir, jsonrep.StorageFile)
	data := []byte("file test data")

	ref, err := bdh.CreateReference(data, "application/octet-stream")
	if err != nil {
		t.Fatalf("Failed to create reference: %v", err)
	}

	// Verify file was created
	refID := filepath.Base(ref.URI)
	filePath := filepath.Join(tempDir, "bulk_data", refID)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("Bulk data file was not created")
	}
}

func TestCreateReferenceEmptyData(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	_, err := bdh.CreateReference([]byte{}, "text/plain")
	if err == nil {
		t.Error("Expected error for empty data")
	}
}

func TestCreateReferenceDuplicate(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	data := []byte("test data")

	ref1, _ := bdh.CreateReference(data, "text/plain")
	ref2, _ := bdh.CreateReference(data, "text/plain")

	if ref1.Hash != ref2.Hash {
		t.Error("Duplicate data should have same hash")
	}
	if ref1.URI != ref2.URI {
		t.Error("Duplicate data should have same URI")
	}
}

// Test ResolveReference

func TestResolveReferenceMemory(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	originalData := []byte("test data for resolution")

	ref, _ := bdh.CreateReference(originalData, "text/plain")
	resolvedData, err := bdh.ResolveReference(ref)

	if err != nil {
		t.Fatalf("Failed to resolve reference: %v", err)
	}
	if !bytes.Equal(originalData, resolvedData) {
		t.Error("Resolved data does not match original")
	}
}

func TestResolveReferenceFile(t *testing.T) {
	tempDir := t.TempDir()
	bdh, _ := jsonrep.NewBulkDataHandler(tempDir, jsonrep.StorageFile)
	originalData := []byte("file test data for resolution")

	ref, _ := bdh.CreateReference(originalData, "application/octet-stream")
	resolvedData, err := bdh.ResolveReference(ref)

	if err != nil {
		t.Fatalf("Failed to resolve reference: %v", err)
	}
	if !bytes.Equal(originalData, resolvedData) {
		t.Error("Resolved data does not match original")
	}
}

func TestResolveReferenceNil(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	_, err := bdh.ResolveReference(nil)
	if err == nil {
		t.Error("Expected error for nil reference")
	}
}

func TestResolveReferenceNotFound(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	fakeRef := &jsonrep.BulkDataReference{
		URI: "http://test.com/bulk_data/nonexistent",
	}

	_, err := bdh.ResolveReference(fakeRef)
	if err == nil {
		t.Error("Expected error for nonexistent reference")
	}
}

// Test ValidateReference

func TestValidateReference(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	data := []byte("data to validate")

	ref, _ := bdh.CreateReference(data, "text/plain")
	err := bdh.ValidateReference(ref)

	if err != nil {
		t.Fatalf("Reference validation failed: %v", err)
	}
}

func TestValidateReferenceCorruptedHash(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	data := []byte("data to validate")

	ref, _ := bdh.CreateReference(data, "text/plain")
	ref.Hash = "invalid_hash"

	err := bdh.ValidateReference(ref)
	if err == nil {
		t.Error("Expected validation error for corrupted hash")
	}
}

func TestValidateReferenceWrongSize(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	data := []byte("data to validate")

	ref, _ := bdh.CreateReference(data, "text/plain")
	ref.Length = 999

	err := bdh.ValidateReference(ref)
	if err == nil {
		t.Error("Expected validation error for wrong size")
	}
}

func TestValidateReferenceNoHash(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	ref := &jsonrep.BulkDataReference{
		URI:    "http://test.com/test",
		Length: 100,
	}

	err := bdh.ValidateReference(ref)
	if err == nil {
		t.Error("Expected error for reference without hash")
	}
}

// Test ListReferences

func TestListReferencesEmpty(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	refs := bdh.ListReferences()
	if len(refs) != 0 {
		t.Errorf("Expected 0 references, got %d", len(refs))
	}
}

func TestListReferencesMultiple(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	for i := 0; i < 5; i++ {
		bdh.CreateReference([]byte(fmt.Sprintf("data %d", i)), "text/plain")
	}

	refs := bdh.ListReferences()
	if len(refs) != 5 {
		t.Errorf("Expected 5 references, got %d", len(refs))
	}
}

func TestListReferencesOrder(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	data := []string{"first", "second", "third"}
	for _, d := range data {
		bdh.CreateReference([]byte(d), "text/plain")
	}

	refs := bdh.ListReferences()
	if len(refs) != 3 {
		t.Fatalf("Expected 3 references, got %d", len(refs))
	}

	// Verify order is maintained
	for i, ref := range refs {
		expectedHash := hashData([]byte(data[i]))
		if ref.Hash != expectedHash {
			t.Errorf("Reference order not maintained at index %d", i)
		}
	}
}

// Test DeleteReference

func TestDeleteReferenceMemory(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	ref, _ := bdh.CreateReference([]byte("data to delete"), "text/plain")

	if bdh.GetReferenceCount() != 1 {
		t.Errorf("Expected 1 reference before delete, got %d", bdh.GetReferenceCount())
	}

	err := bdh.DeleteReference(ref)
	if err != nil {
		t.Fatalf("Failed to delete reference: %v", err)
	}

	if bdh.GetReferenceCount() != 0 {
		t.Errorf("Expected 0 references after delete, got %d", bdh.GetReferenceCount())
	}
}

func TestDeleteReferenceFile(t *testing.T) {
	tempDir := t.TempDir()
	bdh, _ := jsonrep.NewBulkDataHandler(tempDir, jsonrep.StorageFile)
	ref, _ := bdh.CreateReference([]byte("data to delete"), "text/plain")

	err := bdh.DeleteReference(ref)
	if err != nil {
		t.Fatalf("Failed to delete reference: %v", err)
	}

	// Verify file was deleted
	refID := filepath.Base(ref.URI)
	filePath := filepath.Join(tempDir, "bulk_data", refID)
	if _, err := os.Stat(filePath); err == nil {
		t.Error("File was not deleted")
	}
}

func TestDeleteReferenceNil(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	err := bdh.DeleteReference(nil)
	if err == nil {
		t.Error("Expected error for nil reference")
	}
}

// Test GetReferenceCount

func TestGetReferenceCountZero(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	count := bdh.GetReferenceCount()
	if count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}
}

func TestGetReferenceCountMultiple(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	for i := 0; i < 10; i++ {
		bdh.CreateReference([]byte(fmt.Sprintf("data %d", i)), "text/plain")
	}

	count := bdh.GetReferenceCount()
	if count != 10 {
		t.Errorf("Expected count 10, got %d", count)
	}
}

// Test GetTotalSize

func TestGetTotalSizeZero(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	size := bdh.GetTotalSize()
	if size != 0 {
		t.Errorf("Expected size 0, got %d", size)
	}
}

func TestGetTotalSizeMultiple(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	expectedSize := int64(0)
	for i := 0; i < 5; i++ {
		data := []byte(fmt.Sprintf("data number %d", i))
		bdh.CreateReference(data, "text/plain")
		expectedSize += int64(len(data))
	}

	size := bdh.GetTotalSize()
	if size != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, size)
	}
}

// Test JSONEncoderWithBulk

func TestNewJSONEncoderWithBulk(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 1024)

	if encoder == nil {
		t.Error("Expected encoder, got nil")
	}
}

func TestNewJSONEncoderWithBulkDefaultThreshold(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 0)

	if encoder == nil {
		t.Error("Expected encoder, got nil")
	}
}

// Test EncodeDatasetWithBulk

func TestEncodeDatasetWithBulk(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 100)

	dataset := &jsonrep.DicomDataset{
		PatientID:   "12345",
		PatientName: "Test^Patient",
	}

	elements := map[string]interface{}{
		"PatientID":   "12345",
		"PatientName": "Test^Patient",
	}

	encoded, err := encoder.EncodeDatasetWithBulk(dataset, elements)
	if err != nil {
		t.Fatalf("Failed to encode dataset: %v", err)
	}

	if len(encoded) == 0 {
		t.Error("Expected encoded data, got empty")
	}
}

func TestEncodeDatasetWithBulkData(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 50)

	dataset := &jsonrep.DicomDataset{
		PatientID: "12345",
	}

	largeData := make([]byte, 100)
	elements := map[string]interface{}{
		"PixelData": largeData,
	}

	encoded, err := encoder.EncodeDatasetWithBulk(dataset, elements)
	if err != nil {
		t.Fatalf("Failed to encode dataset: %v", err)
	}

	// Verify large data was converted to bulk reference
	pixelDataElem, exists := encoded["PixelData"]
	if !exists {
		t.Error("PixelData element not found")
	}

	if pixelDataElem.BulkData == nil {
		t.Error("Expected bulk data reference, got inline value")
	}
}

func TestEncodeDatasetNil(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 100)

	_, err := encoder.EncodeDatasetWithBulk(nil, nil)
	if err == nil {
		t.Error("Expected error for nil dataset")
	}
}

// Test DecodeDatasetWithBulk

func TestDecodeDatasetWithBulk(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 100)

	dataset := &jsonrep.DicomDataset{
		PatientID: "12345",
	}

	elements := map[string]interface{}{
		"PatientID": "12345",
	}

	encoded, _ := encoder.EncodeDatasetWithBulk(dataset, elements)
	decoded, err := encoder.DecodeDatasetWithBulk(encoded)

	if err != nil {
		t.Fatalf("Failed to decode dataset: %v", err)
	}

	if len(decoded) == 0 {
		t.Error("Expected decoded data, got empty")
	}
}

func TestDecodeDatasetWithBulkData(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 50)

	dataset := &jsonrep.DicomDataset{
		PatientID: "12345",
	}

	originalData := make([]byte, 100)
	copy(originalData, "test bulk data")

	elements := map[string]interface{}{
		"PixelData": originalData,
	}

	encoded, _ := encoder.EncodeDatasetWithBulk(dataset, elements)
	decoded, err := encoder.DecodeDatasetWithBulk(encoded)

	if err != nil {
		t.Fatalf("Failed to decode dataset: %v", err)
	}

	pixelData, exists := decoded["PixelData"]
	if !exists {
		t.Error("PixelData not found in decoded data")
	}

	if decodedBytes, ok := pixelData.([]byte); ok {
		if !bytes.Equal(decodedBytes, originalData) {
			t.Error("Decoded data does not match original")
		}
	} else {
		t.Error("Expected []byte, got different type")
	}
}

func TestDecodeDatasetEmpty(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 100)

	_, err := encoder.DecodeDatasetWithBulk(make(map[string]jsonrep.JSONElementWithBulk))
	if err == nil {
		t.Error("Expected error for empty encoded data")
	}
}

// Test ValidateReferences

func TestValidateReferencesEmpty(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 100)

	err := encoder.ValidateReferences()
	if err != nil {
		t.Fatalf("Validation failed for empty references: %v", err)
	}
}

func TestValidateReferencesMultiple(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 50)

	for i := 0; i < 5; i++ {
		bdh.CreateReference([]byte(fmt.Sprintf("data %d", i)), "text/plain")
	}

	err := encoder.ValidateReferences()
	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}
}

// Test CreateCompactJSON

func TestCreateCompactJSON(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 100)

	dataset := &jsonrep.DicomDataset{
		PatientID:   "12345",
		PatientName: "Test^Patient",
	}

	elements := map[string]interface{}{
		"PatientID":   "12345",
		"PatientName": "Test^Patient",
	}

	data, err := encoder.CreateCompactJSON(dataset, elements)
	if err != nil {
		t.Fatalf("Failed to create compact JSON: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected JSON data, got empty")
	}

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Invalid JSON generated: %v", err)
	}

	if _, exists := parsed["metadata"]; !exists {
		t.Error("Expected metadata in compact JSON")
	}
}

// Test CreateRoundTripJSON

func TestCreateRoundTripJSON(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 100)

	dataset := &jsonrep.DicomDataset{
		PatientID: "12345",
	}

	elements := map[string]interface{}{
		"PatientID": "12345",
	}

	data, err := encoder.CreateRoundTripJSON(dataset, elements)
	if err != nil {
		t.Fatalf("Failed to create round-trip JSON: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected JSON data, got empty")
	}
}

// Test GetStats

func TestGetStats(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	for i := 0; i < 3; i++ {
		bdh.CreateReference([]byte(fmt.Sprintf("data %d", i)), "text/plain")
	}

	stats := bdh.GetStats()

	if stats.TotalReferences != 3 {
		t.Errorf("Expected 3 references, got %d", stats.TotalReferences)
	}

	if stats.TotalSize <= 0 {
		t.Errorf("Expected positive total size, got %d", stats.TotalSize)
	}

	if stats.StorageBackend != "memory" {
		t.Errorf("Expected memory backend, got %s", stats.StorageBackend)
	}
}

// Test CopyBulkDataHandler

func TestCopyBulkDataHandler(t *testing.T) {
	srcBdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	dstBdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	ref, _ := srcBdh.CreateReference([]byte("test data"), "text/plain")

	err := dstBdh.CopyBulkDataHandler(srcBdh)
	if err != nil {
		t.Fatalf("Failed to copy handler: %v", err)
	}

	if dstBdh.GetReferenceCount() != 1 {
		t.Errorf("Expected 1 reference in destination, got %d", dstBdh.GetReferenceCount())
	}

	// Verify data was copied
	resolvedData, _ := dstBdh.ResolveReference(ref)
	if !bytes.Equal(resolvedData, []byte("test data")) {
		t.Error("Copied data does not match original")
	}
}

// Test StreamBulkData

func TestStreamBulkData(t *testing.T) {
	srcBdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	dstBdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	ref, _ := srcBdh.CreateReference([]byte("streaming test data"), "text/plain")

	err := jsonrep.StreamBulkData(srcBdh, dstBdh, ref)
	if err != nil {
		t.Fatalf("Failed to stream bulk data: %v", err)
	}

	// Verify data was streamed
	if dstBdh.GetReferenceCount() != 1 {
		t.Errorf("Expected 1 reference in destination, got %d", dstBdh.GetReferenceCount())
	}
}

// Test StreamBulkDataReader

func TestStreamBulkDataReader(t *testing.T) {
	tempDir := t.TempDir()
	bdh, _ := jsonrep.NewBulkDataHandler(tempDir, jsonrep.StorageFile)

	testData := []byte("streaming test data content")
	ref, _ := bdh.CreateReference(testData, "text/plain")

	reader, err := jsonrep.NewStreamBulkDataReader(bdh, ref)
	if err != nil {
		t.Fatalf("Failed to create stream reader: %v", err)
	}
	defer reader.Close()

	// Read data
	buf := make([]byte, len(testData))
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read from stream: %v", err)
	}

	if n != len(testData) {
		t.Errorf("Expected %d bytes read, got %d", len(testData), n)
	}

	if !bytes.Equal(buf[:n], testData) {
		t.Error("Stream data does not match original")
	}
}

func TestStreamBulkDataReaderCopy(t *testing.T) {
	tempDir := t.TempDir()
	bdh, _ := jsonrep.NewBulkDataHandler(tempDir, jsonrep.StorageFile)

	testData := []byte("copy test data")
	ref, _ := bdh.CreateReference(testData, "text/plain")

	reader, _ := jsonrep.NewStreamBulkDataReader(bdh, ref)
	defer reader.Close()

	buf := new(bytes.Buffer)
	n, err := reader.Copy(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to copy from stream: %v", err)
	}

	if n != int64(len(testData)) {
		t.Errorf("Expected %d bytes copied, got %d", len(testData), n)
	}

	if !bytes.Equal(buf.Bytes(), testData) {
		t.Error("Copied data does not match original")
	}
}

func TestStreamBulkDataReaderMemoryStorage(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	ref, _ := bdh.CreateReference([]byte("test"), "text/plain")

	_, err := jsonrep.NewStreamBulkDataReader(bdh, ref)
	if err == nil {
		t.Error("Expected error for memory storage streaming")
	}
}

// Test ExtractBulkDataReferences

func TestExtractBulkDataReferencesEmpty(t *testing.T) {
	encoded := make(map[string]jsonrep.JSONElementWithBulk)
	refs := jsonrep.ExtractBulkDataReferences(encoded)

	if len(refs) != 0 {
		t.Errorf("Expected 0 references, got %d", len(refs))
	}
}

func TestExtractBulkDataReferencesMultiple(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	ref1, _ := bdh.CreateReference([]byte("data1"), "text/plain")
	ref2, _ := bdh.CreateReference([]byte("data2"), "text/plain")

	encoded := map[string]jsonrep.JSONElementWithBulk{
		"elem1": {VR: "UN", BulkData: ref1},
		"elem2": {VR: "UN", BulkData: ref2},
		"elem3": {VR: "LO", Value: "inline"},
	}

	refs := jsonrep.ExtractBulkDataReferences(encoded)

	if len(refs) != 2 {
		t.Errorf("Expected 2 references, got %d", len(refs))
	}
}

// Test CompressForTransmission

func TestCompressForTransmission(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 50)

	dataset := &jsonrep.DicomDataset{
		PatientID: "12345",
	}

	elements := map[string]interface{}{
		"PatientID": "12345",
		"PixelData": make([]byte, 100),
	}

	data, err := encoder.CompressForTransmission(dataset, elements)
	if err != nil {
		t.Fatalf("Failed to compress for transmission: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected compressed data, got empty")
	}

	var manifest map[string]interface{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("Invalid manifest JSON: %v", err)
	}

	if _, exists := manifest["bulkDataRefs"]; !exists {
		t.Error("Expected bulkDataRefs in manifest")
	}
}

// Test DetermineVR

func TestDetermineVR(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"PatientName", "PN"},
		{"PatientID", "LO"},
		{"StudyInstanceUID", "UI"},
		{"Modality", "CS"},
		{"StudyDate", "DA"},
		{"StudyTime", "TM"},
		{"InstanceNumber", "IS"},
		{"UnknownField", "UN"},
	}

	for _, test := range tests {
		vr := jsonrep.DetermineVR(test.key)
		if vr != test.expected {
			t.Errorf("For key %s: expected %s, got %s", test.key, test.expected, vr)
		}
	}
}

// Test concurrent operations

func TestConcurrentCreateReference(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			_, err := bdh.CreateReference(
				[]byte(fmt.Sprintf("concurrent data %d", index)),
				"text/plain",
			)
			done <- err
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent create failed: %v", err)
		}
	}

	if bdh.GetReferenceCount() != 10 {
		t.Errorf("Expected 10 references, got %d", bdh.GetReferenceCount())
	}
}

func TestConcurrentResolveReference(t *testing.T) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)

	ref, _ := bdh.CreateReference([]byte("shared data"), "text/plain")

	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_, err := bdh.ResolveReference(ref)
			done <- err
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent resolve failed: %v", err)
		}
	}
}

// Benchmark tests

func BenchmarkCreateReference(b *testing.B) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	data := []byte("benchmark data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uniqueData := append(data, byte(i%256))
		bdh.CreateReference(uniqueData, "text/plain")
	}
}

func BenchmarkResolveReference(b *testing.B) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	ref, _ := bdh.CreateReference([]byte("benchmark data"), "text/plain")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bdh.ResolveReference(ref)
	}
}

func BenchmarkValidateReference(b *testing.B) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	ref, _ := bdh.CreateReference([]byte("benchmark data"), "text/plain")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bdh.ValidateReference(ref)
	}
}

func BenchmarkEncodeDatasetWithBulk(b *testing.B) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 100)

	dataset := &jsonrep.DicomDataset{
		PatientID: "12345",
	}

	elements := map[string]interface{}{
		"PatientID":   "12345",
		"PatientName": "Test^Patient",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoder.EncodeDatasetWithBulk(dataset, elements)
	}
}

func BenchmarkCreateCompactJSON(b *testing.B) {
	bdh, _ := jsonrep.NewBulkDataHandler("http://test.com", jsonrep.StorageMemory)
	encoder := jsonrep.NewJSONEncoderWithBulk(bdh, 100)

	dataset := &jsonrep.DicomDataset{
		PatientID: "12345",
	}

	elements := map[string]interface{}{
		"PatientID":   "12345",
		"PatientName": "Test^Patient",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoder.CreateCompactJSON(dataset, elements)
	}
}

// Helper function

func hashData(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
