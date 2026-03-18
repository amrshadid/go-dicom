package fileset_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"

	"github.com/amrshadid/go-dicom/fileset"
)

// Test NewFileSet
func TestNewFileSet(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	testDir := filepath.Join(tmpDir, "dicom_files")

	fs, err := fileset.NewFileSet(testDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	if fs.RootDir != testDir {
		t.Errorf("Expected RootDir %s, got %s", testDir, fs.RootDir)
	}

	if len(fs.FileRecords) != 0 {
		t.Errorf("Expected empty file records, got %d", len(fs.FileRecords))
	}

	// Verify directory was created
	if _, err := os.Stat(testDir); err != nil {
		t.Errorf("Directory was not created: %v", err)
	}
}

// Test AddFile
func TestAddFile(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.dcm")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Add the file
	err = fs.AddFile(testFile)
	if err != nil {
		t.Fatalf("AddFile failed: %v", err)
	}

	// Verify file was added
	files := fs.ListFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 file, got %d", len(files))
	}

	// Try to add the same file again (should error)
	err = fs.AddFile(testFile)
	if err == nil {
		t.Error("Expected error when adding duplicate file")
	}
}

// Test RemoveFile
func TestRemoveFile(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	// Try to remove a non-existent file
	err = fs.RemoveFile("nonexistent.dcm")
	if err == nil {
		t.Error("Expected error when removing non-existent file")
	}

	// Create and add a file
	testFile := filepath.Join(tmpDir, "test.dcm")
	os.WriteFile(testFile, []byte("test"), 0644)
	fs.AddFile(testFile)

	// Verify it was added
	files := fs.ListFiles()
	if len(files) != 1 {
		t.Errorf("Expected 1 file before removal, got %d", len(files))
	}

	// Remove the file
	err = fs.RemoveFile(testFile)
	if err != nil {
		t.Fatalf("RemoveFile failed: %v", err)
	}

	// Verify it was removed
	files = fs.ListFiles()
	if len(files) != 0 {
		t.Errorf("Expected 0 files after removal, got %d", len(files))
	}
}

// Test ListFiles
func TestListFiles(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	files := fs.ListFiles()
	if len(files) != 0 {
		t.Errorf("Expected 0 files, got %d", len(files))
	}
}

// Test FindByModality
func TestFindByModality(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	// Add test records with datasets
	ds1 := dataset.NewDataset()
	ds2 := dataset.NewDataset()

	fs.FileRecords = append(fs.FileRecords, &fileset.FileRecord{
		FilePath: "test1.dcm",
		Dataset:  ds1,
	})
	fs.FileRecords = append(fs.FileRecords, &fileset.FileRecord{
		FilePath: "test2.dcm",
		Dataset:  ds2,
	})

	// Find by modality (currently just returns records with loaded datasets)
	results := fs.FindByModality("CT")
	if len(results) != 2 {
		t.Errorf("Expected 2 CT files, got %d", len(results))
	}
}

// Test FindByPatient
func TestFindByPatient(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	ds1 := dataset.NewDataset()
	ds2 := dataset.NewDataset()

	fs.FileRecords = append(fs.FileRecords, &fileset.FileRecord{
		FilePath: "test1.dcm",
		Dataset:  ds1,
	})
	fs.FileRecords = append(fs.FileRecords, &fileset.FileRecord{
		FilePath: "test2.dcm",
		Dataset:  ds2,
	})

	// Find by patient
	results := fs.FindByPatient("P001")
	if len(results) != 2 {
		t.Errorf("Expected 2 files, got %d", len(results))
	}
}

// Test FindByStudyInstanceUID
func TestFindByStudyInstanceUID(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	ds := dataset.NewDataset()
	fs.FileRecords = append(fs.FileRecords, &fileset.FileRecord{
		FilePath: "test.dcm",
		Dataset:  ds,
	})

	results := fs.FindByStudyInstanceUID("STUDY1")
	if len(results) != 1 {
		t.Errorf("Expected 1 file, got %d", len(results))
	}
}

// Test GenerateDICOMDIR
func TestGenerateDICOMDIR(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	ds1 := dataset.NewDataset()
	ds2 := dataset.NewDataset()
	fs.FileRecords = append(fs.FileRecords,
		&fileset.FileRecord{FilePath: "test1.dcm", Dataset: ds1},
		&fileset.FileRecord{FilePath: "test2.dcm", Dataset: ds2},
	)

	dicomdir, err := fs.GenerateDICOMDIR()
	if err != nil {
		t.Fatalf("GenerateDICOMDIR failed: %v", err)
	}

	if dicomdir == nil {
		t.Error("Generated DICOMDIR should not be nil")
	}
}

// Test Validate
func TestValidate(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	// Valid empty fileset should validate
	err = fs.Validate()
	if err != nil {
		t.Fatalf("Empty fileset should validate: %v", err)
	}

	// Add a test file
	testFile := filepath.Join(tmpDir, "test.dcm")
	os.WriteFile(testFile, []byte("test"), 0644)
	fs.AddFile(testFile)

	// Should still validate
	err = fs.Validate()
	if err != nil {
		t.Fatalf("Fileset with files should validate: %v", err)
	}
}

// Test GetStatistics
func TestGetStatistics(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	// Create test files
	file1 := filepath.Join(tmpDir, "test1.dcm")
	file2 := filepath.Join(tmpDir, "test2.dcm")
	file3 := filepath.Join(tmpDir, "test3.dcm")

	os.WriteFile(file1, make([]byte, 1000), 0644)
	os.WriteFile(file2, make([]byte, 2000), 0644)
	os.WriteFile(file3, make([]byte, 1500), 0644)

	fs.AddFile(file1)
	fs.AddFile(file2)
	fs.AddFile(file3)

	stats := fs.GetStatistics()

	if stats.TotalFiles != 3 {
		t.Errorf("Expected 3 total files, got %d", stats.TotalFiles)
	}

	if stats.TotalSize != 4500 {
		t.Errorf("Expected total size 4500, got %d", stats.TotalSize)
	}

	if stats.PatientCount != 3 {
		t.Errorf("Expected 3 patients, got %d", stats.PatientCount)
	}
}

// Test ScanDirectory (recursive=false)
func TestScanDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "test1.dcm"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "test2.dcm"), []byte("test"), 0644)

	// Create subdirectory with file
	subDir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "test3.dcm"), []byte("test"), 0644)

	// Scan non-recursive
	count, err := fs.ScanDirectory(false)
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 files (non-recursive), got %d", count)
	}
}

// Test ScanDirectory (recursive=true)
func TestScanDirectoryRecursive(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "test1.dcm"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "test2.dcm"), []byte("test"), 0644)

	// Create subdirectory with file
	subDir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "test3.dcm"), []byte("test"), 0644)

	// Scan recursive
	count, err := fs.ScanDirectory(true)
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	if count != 3 {
		t.Errorf("Expected 3 files (recursive), got %d", count)
	}
}

// Test SortByPatientName
func TestSortByPatientName(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	// Create files with different names
	file1 := filepath.Join(tmpDir, "zebra.dcm")
	file2 := filepath.Join(tmpDir, "alice.dcm")
	file3 := filepath.Join(tmpDir, "mike.dcm")

	os.WriteFile(file1, []byte("test"), 0644)
	os.WriteFile(file2, []byte("test"), 0644)
	os.WriteFile(file3, []byte("test"), 0644)

	fs.AddFile(file1)
	fs.AddFile(file2)
	fs.AddFile(file3)

	fs.SortByPatientName()

	files := fs.ListFiles()
	names := []string{
		filepath.Base(files[0].FilePath),
		filepath.Base(files[1].FilePath),
		filepath.Base(files[2].FilePath),
	}

	if names[0] != "alice.dcm" || names[1] != "mike.dcm" || names[2] != "zebra.dcm" {
		t.Errorf("Names not sorted correctly: %v", names)
	}
}

// Test SortByModifiedTime
func TestSortByModifiedTime(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	now := time.Now()

	file1 := filepath.Join(tmpDir, "test1.dcm")
	file2 := filepath.Join(tmpDir, "test2.dcm")
	file3 := filepath.Join(tmpDir, "test3.dcm")

	os.WriteFile(file1, []byte("test"), 0644)
	os.WriteFile(file2, []byte("test"), 0644)
	os.WriteFile(file3, []byte("test"), 0644)

	fs.AddFile(file1)
	fs.AddFile(file2)
	fs.AddFile(file3)

	// Modify timestamps
	os.Chtimes(file1, now.Add(2*time.Hour), now.Add(2*time.Hour))
	os.Chtimes(file2, now, now)
	os.Chtimes(file3, now.Add(1*time.Hour), now.Add(1*time.Hour))

	// Re-add files to get updated info
	fs.FileRecords = nil
	fs.AddFile(file1)
	fs.AddFile(file2)
	fs.AddFile(file3)

	fs.SortByModifiedTime()

	files := fs.ListFiles()
	if !files[0].ModifiedTime.Before(files[1].ModifiedTime) {
		t.Error("Files not sorted by modified time")
	}
}

// Test SortByFileSize
func TestSortByFileSize(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	file1 := filepath.Join(tmpDir, "test1.dcm")
	file2 := filepath.Join(tmpDir, "test2.dcm")
	file3 := filepath.Join(tmpDir, "test3.dcm")

	os.WriteFile(file1, make([]byte, 5000), 0644)
	os.WriteFile(file2, make([]byte, 1000), 0644)
	os.WriteFile(file3, make([]byte, 3000), 0644)

	fs.AddFile(file1)
	fs.AddFile(file2)
	fs.AddFile(file3)

	fs.SortByFileSize()

	files := fs.ListFiles()
	if files[0].FileSize != 1000 || files[1].FileSize != 3000 || files[2].FileSize != 5000 {
		t.Error("Files not sorted by size")
	}
}

// Test concurrent access
func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, err := fileset.NewFileSet(tmpDir)
	if err != nil {
		t.Fatalf("NewFileSet failed: %v", err)
	}

	// Create and add a file
	testFile := filepath.Join(tmpDir, "test.dcm")
	os.WriteFile(testFile, []byte("test"), 0644)
	fs.AddFile(testFile)

	done := make(chan bool, 2)

	// Concurrent read operations
	go func() {
		_ = fs.ListFiles()
		done <- true
	}()

	go func() {
		_ = fs.FindByPatient("P001")
		done <- true
	}()

	<-done
	<-done
}

// Benchmark tests

func BenchmarkNewFileSet(b *testing.B) {
	tmpDir := b.TempDir()
	defer os.RemoveAll(tmpDir)

	for i := 0; i < b.N; i++ {
		fileset.NewFileSet(filepath.Join(tmpDir, "test"))
	}
}

func BenchmarkAddFile(b *testing.B) {
	tmpDir := b.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, _ := fileset.NewFileSet(tmpDir)

	// Create test files
	for i := 0; i < b.N; i++ {
		file := filepath.Join(tmpDir, "test_"+string(rune(i))+".dcm")
		os.WriteFile(file, []byte("test"), 0644)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file := filepath.Join(tmpDir, "test_"+string(rune(i))+".dcm")
		fs.AddFile(file)
	}
}

func BenchmarkListFiles(b *testing.B) {
	tmpDir := b.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, _ := fileset.NewFileSet(tmpDir)

	// Add test files
	for i := 0; i < 50; i++ {
		file := filepath.Join(tmpDir, "test_"+string(rune(i))+".dcm")
		os.WriteFile(file, []byte("test"), 0644)
		fs.AddFile(file)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs.ListFiles()
	}
}

func BenchmarkGetStatistics(b *testing.B) {
	tmpDir := b.TempDir()
	defer os.RemoveAll(tmpDir)

	fs, _ := fileset.NewFileSet(tmpDir)

	// Add test files
	for i := 0; i < 50; i++ {
		file := filepath.Join(tmpDir, "test_"+string(rune(i))+".dcm")
		os.WriteFile(file, make([]byte, 1000), 0644)
		fs.AddFile(file)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs.GetStatistics()
	}
}
