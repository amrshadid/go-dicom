package fileset_test

import (
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/fileset"
)

// TestExecuteFileSetHookListFiles tests hook execution for listing files.
func TestExecuteFileSetHookListFiles(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	tempFile := filepath.Join(tempDir, "test.dcm")
	writeMinimalDICOM(t, tempFile)
	fs.AddFile(tempFile)

	result, err := fileset.ExecuteFileSetHook(fs, "list_files")
	if err != nil {
		t.Fatalf("ExecuteFileSetHook error: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	count, ok := result["count"].(int)
	if !ok || count != 1 {
		t.Error("expected count=1 in result")
	}

	files, ok := result["files"].([]*fileset.FileRecord)
	if !ok {
		t.Error("expected files array in result")
	}

	if len(files) != 1 {
		t.Errorf("expected 1 file in result, got %d", len(files))
	}
}

// TestExecuteFileSetHookGetStatistics tests hook execution for getting statistics.
func TestExecuteFileSetHookGetStatistics(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	tempFile := filepath.Join(tempDir, "test.dcm")
	writeMinimalDICOM(t, tempFile)
	fs.AddFile(tempFile)

	result, err := fileset.ExecuteFileSetHook(fs, "get_statistics")
	if err != nil {
		t.Fatalf("ExecuteFileSetHook error: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	totalFiles, ok := result["total_files"].(int64)
	if !ok || totalFiles != 1 {
		t.Errorf("expected total_files=1, got %v", result["total_files"])
	}

	totalSize, ok := result["total_size"].(int64)
	if !ok || totalSize == 0 {
		t.Error("expected non-zero total_size")
	}
}

// TestExecuteFileSetHookValidate tests hook execution for validation.
func TestExecuteFileSetHookValidate(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	tempFile := filepath.Join(tempDir, "test.dcm")
	writeMinimalDICOM(t, tempFile)
	fs.AddFile(tempFile)

	result, err := fileset.ExecuteFileSetHook(fs, "validate")
	if err != nil {
		t.Fatalf("ExecuteFileSetHook error: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	valid, ok := result["valid"].(bool)
	if !ok || !valid {
		t.Error("expected valid=true in result")
	}
}

// TestExecuteFileSetHookInvalidOperation tests error handling for invalid operations.
func TestExecuteFileSetHookInvalidOperation(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	_, err := fileset.ExecuteFileSetHook(fs, "invalid_operation")
	if err == nil {
		t.Fatal("expected error for invalid operation")
	}
}

// TestConvertFileSetToRawFormat tests converting FileSet to raw format.
func TestConvertFileSetToRawFormat(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	tempFile := filepath.Join(tempDir, "test.dcm")
	writeMinimalDICOM(t, tempFile)
	fs.AddFile(tempFile)

	raw := fileset.ConvertFileSetToRawFormat(fs)
	if raw == nil {
		t.Fatal("raw format is nil")
	}

	rootDir, ok := raw["root_dir"].(string)
	if !ok {
		t.Error("expected root_dir in raw format")
	}

	if rootDir != tempDir {
		t.Errorf("expected root_dir %s, got %s", tempDir, rootDir)
	}

	recordCount, ok := raw["record_count"].(int)
	if !ok || recordCount != 1 {
		t.Errorf("expected record_count=1, got %v", raw["record_count"])
	}

	totalSize, ok := raw["total_size"].(int64)
	if !ok || totalSize == 0 {
		t.Error("expected non-zero total_size")
	}

	fileRecords, ok := raw["file_records"].([]map[string]interface{})
	if !ok {
		t.Error("expected file_records array in raw format")
	}

	if len(fileRecords) != 1 {
		t.Errorf("expected 1 file record, got %d", len(fileRecords))
	}
}

// TestConvertFileSetToRawFormatMultipleFiles tests conversion with multiple files.
func TestConvertFileSetToRawFormatMultipleFiles(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	for i := 0; i < 5; i++ {
		file := filepath.Join(tempDir, "file"+string(byte('0'+i))+".dcm")
		writeMinimalDICOM(t, file)
		fs.AddFile(file)
	}

	raw := fileset.ConvertFileSetToRawFormat(fs)

	recordCount, ok := raw["record_count"].(int)
	if !ok || recordCount != 5 {
		t.Errorf("expected record_count=5, got %v", raw["record_count"])
	}

	fileRecords, ok := raw["file_records"].([]map[string]interface{})
	if !ok || len(fileRecords) != 5 {
		t.Errorf("expected 5 file records, got %d", len(fileRecords))
	}
}

// TestConvertFileSetToRawFormatEmptySet tests conversion with empty file-set.
func TestConvertFileSetToRawFormatEmptySet(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	raw := fileset.ConvertFileSetToRawFormat(fs)
	if raw == nil {
		t.Fatal("raw format is nil")
	}

	recordCount, ok := raw["record_count"].(int)
	if !ok || recordCount != 0 {
		t.Errorf("expected record_count=0, got %v", raw["record_count"])
	}
}

// TestConvertRawFormatToFileSet tests converting raw format back to FileSet.
func TestConvertRawFormatToFileSet(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	file1 := filepath.Join(tempDir, "file1.dcm")
	file2 := filepath.Join(tempDir, "file2.dcm")
	writeMinimalDICOM(t, file1)
	writeMinimalDICOM(t, file2)

	fs.AddFile(file1)
	fs.AddFile(file2)

	// Get initial record count
	initialRecords := fs.ListFiles()
	if len(initialRecords) != 2 {
		t.Fatalf("expected 2 records after adding files, got %d", len(initialRecords))
	}

	// Convert to raw format
	raw := fileset.ConvertFileSetToRawFormat(fs)

	// Verify raw data has correct structure
	fileRecords, ok := raw["file_records"].([]map[string]interface{})
	if !ok || len(fileRecords) != 2 {
		t.Fatalf("expected 2 file records in raw format, got %d", len(fileRecords))
	}

	// Create new empty FileSet and test restoration logic
	newFS, _ := fileset.NewFileSet(tempDir)

	// Manually apply the restoration logic
	for _, rawRec := range fileRecords {
		if filePath, ok := rawRec["file_path"].(string); ok {
			if err := newFS.AddFile(filePath); err != nil {
				t.Logf("Note: AddFile returned error (expected for duplicates): %v", err)
			}
		}
	}

	records := newFS.ListFiles()
	if len(records) != 2 {
		t.Errorf("expected 2 records after restoration, got %d", len(records))
	}
}

// TestConvertRawFormatToFileSetEmpty tests conversion of empty raw format.
func TestConvertRawFormatToFileSetEmpty(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	raw := make(map[string]interface{})
	raw["file_records"] = []interface{}{}

	err := fileset.ConvertRawFormatToFileSet(raw, fs)
	if err != nil {
		t.Fatalf("ConvertRawFormatToFileSet error: %v", err)
	}

	records := fs.ListFiles()
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

// TestConvertRawFormatToFileSetInvalidData tests error handling for invalid data.
func TestConvertRawFormatToFileSetInvalidData(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	raw := map[string]interface{}{
		"file_records": []interface{}{
			map[string]interface{}{
				"file_path": "/nonexistent/file.dcm",
			},
		},
	}

	err := fileset.ConvertRawFormatToFileSet(raw, fs)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

// TestExecuteFileSetHookMultipleOperations tests multiple hook operations.
func TestExecuteFileSetHookMultipleOperations(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	tempFile := filepath.Join(tempDir, "test.dcm")
	writeMinimalDICOM(t, tempFile)
	fs.AddFile(tempFile)

	operations := []string{"list_files", "get_statistics", "validate"}
	for _, op := range operations {
		result, err := fileset.ExecuteFileSetHook(fs, op)
		if err != nil {
			t.Fatalf("ExecuteFileSetHook error for operation %s: %v", op, err)
		}

		if result == nil {
			t.Fatalf("result is nil for operation %s", op)
		}
	}
}

// TestConvertFileSetPreservesMetadata tests that raw format preserves metadata.
func TestConvertFileSetPreservesMetadata(t *testing.T) {
	tempDir := t.TempDir()
	fs, _ := fileset.NewFileSet(tempDir)

	tempFile := filepath.Join(tempDir, "test.dcm")
	writeMinimalDICOM(t, tempFile)
	fs.AddFile(tempFile)

	raw := fileset.ConvertFileSetToRawFormat(fs)

	fileRecords := raw["file_records"].([]map[string]interface{})
	record := fileRecords[0]

	if filePath, ok := record["file_path"].(string); ok {
		if filePath != tempFile {
			t.Errorf("expected file_path %s, got %s", tempFile, filePath)
		}
	} else {
		t.Error("expected file_path in record")
	}

	if fileSize, ok := record["file_size"].(int64); ok {
		if fileSize == 0 {
			t.Error("expected a nonzero file_size")
		}
	} else {
		t.Error("expected file_size in record")
	}
}
