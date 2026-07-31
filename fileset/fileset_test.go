package fileset_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/fileset"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
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
	writeMinimalDICOM(t, testFile)

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
	writeMinimalDICOM(t, testFile)
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

	// This test used to build two empty data sets, search for CT, and expect
	// both back — matching a FindByModality that ignored its argument and
	// returned every record that had a data set at all. Its own comment said
	// so. The records below carry a modality, and the search has to use it.
	withModality := func(modality string) *dataset.Dataset {
		ds := dataset.NewDataset()
		if err := ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060),
			dataelem.CS, []byte(modality))); err != nil {
			t.Fatalf("building a record: %v", err)
		}
		return ds
	}

	fs.FileRecords = append(fs.FileRecords,
		&fileset.FileRecord{FilePath: "ct1.dcm", Dataset: withModality("CT")},
		&fileset.FileRecord{FilePath: "ct2.dcm", Dataset: withModality("CT")},
		&fileset.FileRecord{FilePath: "mr1.dcm", Dataset: withModality("MR")},
	)

	if got := len(fs.FindByModality("CT")); got != 2 {
		t.Errorf("FindByModality(CT) returned %d records, want 2", got)
	}
	if got := len(fs.FindByModality("MR")); got != 1 {
		t.Errorf("FindByModality(MR) returned %d records, want 1", got)
	}
	if got := len(fs.FindByModality("US")); got != 0 {
		t.Errorf("FindByModality(US) returned %d records, want 0", got)
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

	// Two empty data sets and a search for P001 used to match both, because the
	// search returned every record that had a data set at all.
	withPatient := func(id string) *dataset.Dataset {
		ds := dataset.NewDataset()
		if err := ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020),
			dataelem.LO, []byte(id))); err != nil {
			t.Fatalf("building a record: %v", err)
		}
		return ds
	}

	fs.FileRecords = append(fs.FileRecords,
		&fileset.FileRecord{FilePath: "a.dcm", Dataset: withPatient("P001")},
		&fileset.FileRecord{FilePath: "b.dcm", Dataset: withPatient("P001")},
		&fileset.FileRecord{FilePath: "c.dcm", Dataset: withPatient("P002")},
	)

	if got := len(fs.FindByPatient("P001")); got != 2 {
		t.Errorf("FindByPatient(P001) returned %d records, want 2", got)
	}
	if got := len(fs.FindByPatient("P999")); got != 0 {
		t.Errorf("FindByPatient(P999) returned %d records, want 0", got)
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

	// Previously an empty data set and a search for STUDY1 were expected to
	// match, because the search ignored what it was given.
	withStudy := func(uid string) *dataset.Dataset {
		ds := dataset.NewDataset()
		if err := ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D),
			dataelem.UI, []byte(uid))); err != nil {
			t.Fatalf("building a record: %v", err)
		}
		return ds
	}

	fs.FileRecords = append(fs.FileRecords,
		&fileset.FileRecord{FilePath: "a.dcm", Dataset: withStudy("STUDY1")},
		&fileset.FileRecord{FilePath: "b.dcm", Dataset: withStudy("STUDY2")},
	)

	if got := len(fs.FindByStudyInstanceUID("STUDY1")); got != 1 {
		t.Errorf("FindByStudyInstanceUID(STUDY1) returned %d records, want 1", got)
	}
	if got := len(fs.FindByStudyInstanceUID("STUDY3")); got != 0 {
		t.Errorf("FindByStudyInstanceUID(STUDY3) returned %d records, want 0", got)
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

	// This used to append two empty data sets and assert the result was not
	// nil, which an empty data set satisfies — so a GenerateDICOMDIR that
	// produced nothing at all passed.
	for _, name := range []string{"test1.dcm", "test2.dcm"} {
		path := filepath.Join(tmpDir, name)
		writeMinimalDICOM(t, path)
		if err := fs.AddFile(path); err != nil {
			t.Fatalf("AddFile(%s): %v", name, err)
		}
	}

	dicomdir, err := fs.GenerateDICOMDIR()
	if err != nil {
		t.Fatalf("GenerateDICOMDIR failed: %v", err)
	}
	if dicomdir == nil {
		t.Fatal("Generated DICOMDIR should not be nil")
	}

	// A DICOMDIR with no records in it is not a DICOMDIR.
	if _, ok := dicomdir.Get(tag.New(0x0004, 0x1220)); !ok {
		t.Fatal("the generated DICOMDIR has no Directory Record Sequence")
	}
	if _, ok := dicomdir.Get(tag.New(0x0004, 0x1200)); !ok {
		t.Error("the generated DICOMDIR has no offset to its first root record")
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
	writeMinimalDICOM(t, testFile)
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

	// Three files of one patient, one study, one series. Every count used to
	// be the file count, so this reported three patients and three studies.
	for _, name := range []string{"test1.dcm", "test2.dcm", "test3.dcm"} {
		path := filepath.Join(tmpDir, name)
		writeMinimalDICOM(t, path)
		if err := fs.AddFile(path); err != nil {
			t.Fatalf("AddFile(%s): %v", name, err)
		}
	}

	stats := fs.GetStatistics()

	if stats.TotalFiles != 3 {
		t.Errorf("Expected 3 total files, got %d", stats.TotalFiles)
	}
	if stats.TotalSize == 0 {
		t.Error("Expected a nonzero total size")
	}
	if stats.PatientCount != 1 {
		t.Errorf("Expected 1 distinct patient, got %d", stats.PatientCount)
	}
	if stats.StudyCount != 1 {
		t.Errorf("Expected 1 distinct study, got %d", stats.StudyCount)
	}
	if stats.Modalities["OT"] != 3 {
		t.Errorf("Expected 3 files of modality OT, got %d", stats.Modalities["OT"])
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
	writeMinimalDICOM(t, filepath.Join(tmpDir, "test1.dcm"))
	writeMinimalDICOM(t, filepath.Join(tmpDir, "test2.dcm"))

	// Create subdirectory with file
	subDir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subDir, 0755)
	writeMinimalDICOM(t, filepath.Join(subDir, "test3.dcm"))

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
	writeMinimalDICOM(t, filepath.Join(tmpDir, "test1.dcm"))
	writeMinimalDICOM(t, filepath.Join(tmpDir, "test2.dcm"))

	// Create subdirectory with file
	subDir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subDir, 0755)
	writeMinimalDICOM(t, filepath.Join(subDir, "test3.dcm"))

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

	writeMinimalDICOM(t, file1)
	writeMinimalDICOM(t, file2)
	writeMinimalDICOM(t, file3)

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

	writeMinimalDICOM(t, file1)
	writeMinimalDICOM(t, file2)
	writeMinimalDICOM(t, file3)

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

	// The sizes came from files padded to 1000, 3000 and 5000 bytes, which are
	// not DICOM and are no longer accepted into a file-set. Real files have the
	// sizes they have, so the order is what is checked.
	for _, name := range []string{"test1.dcm", "test2.dcm", "test3.dcm"} {
		path := filepath.Join(tmpDir, name)
		writeMinimalDICOM(t, path)
		if err := fs.AddFile(path); err != nil {
			t.Fatalf("AddFile(%s): %v", name, err)
		}
	}

	fs.SortByFileSize()

	files := fs.ListFiles()
	for i := 1; i < len(files); i++ {
		if files[i-1].FileSize > files[i].FileSize {
			t.Fatalf("files are not sorted by size: %d before %d",
				files[i-1].FileSize, files[i].FileSize)
		}
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
	writeMinimalDICOM(t, testFile)
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
		writeMinimalDICOM(b, file)
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
		writeMinimalDICOM(b, file)
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
		writeMinimalDICOM(b, file)
		fs.AddFile(file)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs.GetStatistics()
	}
}

// writeMinimalDICOM writes a real DICOM file, small but valid.
//
// These tests used to write the four bytes "test" and expect them scanned as
// DICOM instances, which worked only because scanning never opened the files it
// listed. Now that it parses them, a file-set has to contain files.
func writeMinimalDICOM(t testing.TB, path string) {
	t.Helper()

	out := &seekableTestBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.7",
		MediaStorageSOPInstanceUID: "1.2.826.0.1.3680043.10.511.9." + filepath.Base(path),
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})
	add := func(group, element uint16, vr, value string) {
		if len(value)%2 == 1 {
			value += " "
		}
		if err := w.AddDataElement(&filewriter.DataElement{
			Tag: tag.New(group, element), VR: vr,
			Value: []byte(value), Length: uint32(len(value)),
		}); err != nil {
			t.Fatalf("AddDataElement: %v", err)
		}
	}
	add(0x0008, 0x0016, "UI", "1.2.840.10008.5.1.4.1.1.7")
	add(0x0008, 0x0018, "UI", "1.2.826.0.1.3680043.10.511.9."+filepath.Base(path))
	add(0x0008, 0x0060, "CS", "OT")
	add(0x0010, 0x0020, "LO", "PID1")
	add(0x0020, 0x000D, "UI", "1.2.826.0.1.3680043.10.511.9.1")
	add(0x0020, 0x000E, "UI", "1.2.826.0.1.3680043.10.511.9.2")

	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := os.WriteFile(path, out.data, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// seekableTestBuffer is an in-memory io.WriteSeeker for the writer.
type seekableTestBuffer struct {
	data []byte
	pos  int64
}

func (b *seekableTestBuffer) Write(p []byte) (int, error) {
	end := b.pos + int64(len(p))
	if end > int64(len(b.data)) {
		grown := make([]byte, end)
		copy(grown, b.data)
		b.data = grown
	}
	copy(b.data[b.pos:], p)
	b.pos = end
	return len(p), nil
}

func (b *seekableTestBuffer) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		b.pos = offset
	case io.SeekCurrent:
		b.pos += offset
	case io.SeekEnd:
		b.pos = int64(len(b.data)) + offset
	}
	return b.pos, nil
}
