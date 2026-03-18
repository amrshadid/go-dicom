// Package fileset provides comprehensive management of DICOM file collections organized in directory structures.
//
// This package implements functionality for organizing, indexing, and managing collections of DICOM files
// as file-sets with support for directory scanning, file record management, searching by patient/study/series,
// DICOMDIR generation, and statistical analysis. Includes deferred dataset loading for efficient memory usage.
//
// # Core Concepts
//
// ## FileRecord
//
// Represents a single DICOM file in the file-set with metadata including file path, dataset reference,
// modification time, and file size. Supports deferred loading of dataset information.
//
// ## FileSet
//
// Manages a collection of FileRecords organized in a root directory structure. Provides operations for:
//   - Adding/removing files
//   - Directory scanning (recursive and non-recursive)
//   - Searching by patient/study/series/modality
//   - DICOMDIR generation
//   - Statistics collection
//   - Sorting by various criteria
//   - Validation
//
// ## FileSetStatistics
//
// Aggregates summary statistics about the file-set including total file count, total size,
// count of modalities, patients, studies, and series.
//
// # Basic Usage
//
// ## Creating a FileSet
//
//	import (
//	    "log"
//	    "github.com/amrshadid/go-dicom/fileset"
//	)
//
//	func main() {
//	    // Create new file-set for directory
//	    fs, err := fileset.NewFileSet("/path/to/dicom/files")
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	}
//
// ## Adding Files
//
//	// Add single file
//	err := fs.AddFile("/path/to/file.dcm")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Or scan directory for multiple files
//	count, err := fs.ScanDirectory(true)  // recursive=true
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Added %d files\n", count)
//
// ## Listing Files
//
//	records := fs.ListFiles()
//	for _, record := range records {
//	    fmt.Printf("File: %s, Size: %d bytes\n", record.FilePath, record.FileSize)
//	}
//
// ## Finding Files by Criteria
//
//	// Find by patient ID (requires loaded datasets)
//	results := fs.FindByPatient("P001")
//
//	// Find by study UID
//	studyResults := fs.FindByStudyInstanceUID("1.2.3.4.5")
//
//	// Find by series UID
//	seriesResults := fs.FindBySeriesInstanceUID("1.2.3.4.5.6")
//
//	// Find by modality
//	modalityResults := fs.FindByModality("CT")
//
// ## Getting Statistics
//
//	stats := fs.GetStatistics()
//	fmt.Printf("Total files: %d\n", stats.TotalFiles)
//	fmt.Printf("Total size: %d bytes\n", stats.TotalSize)
//	fmt.Printf("Patients: %d\n", stats.PatientCount)
//	fmt.Printf("Studies: %d\n", stats.StudyCount)
//	fmt.Printf("Series: %d\n", stats.SeriesCount)
//
// ## Sorting Files
//
//	// Sort by patient name (file basename)
//	fs.SortByPatientName()
//
//	// Sort by modification time
//	fs.SortByModifiedTime()
//
//	// Sort by file size
//	fs.SortByFileSize()
//
// ## Validation and DICOMDIR
//
//	// Validate file-set integrity
//	err := fs.Validate()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Generate DICOMDIR file
//	dicomdir, err := fs.GenerateDICOMDIR()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Advanced Usage
//
// ## Directory Scanning
//
//	// Non-recursive scan (only immediate children)
//	count, err := fs.ScanDirectory(false)
//
//	// Recursive scan (all subdirectories)
//	count, err := fs.ScanDirectory(true)
//
// ## Deferred Dataset Loading
//
// FileSet supports deferred loading of DICOM datasets to minimize memory usage.
// Files can be added with nil Dataset references and loaded on demand.
//
//	// File added with nil dataset (no parsing yet)
//	fs.AddFile("/path/to/file.dcm")
//
//	// Dataset loaded when needed (external to this package)
//	record := fs.ListFiles()[0]
//	if record.Dataset == nil {
//	    // Load dataset externally using filereader package
//	}
//
// ## Concurrent Access
//
// FileSet is thread-safe for concurrent reads through sync.RWMutex:
//
//	go func() {
//	    records := fs.ListFiles()
//	    // Read operations
//	}()
//
//	go func() {
//	    stats := fs.GetStatistics()
//	    // Read operations
//	}()
//
//	// Write operations lock out readers
//	fs.AddFile(path)
//	fs.RemoveFile(path)
//
// # Data Structures
//
// ## FileRecord
//
//	type FileRecord struct {
//	    FilePath     string
//	    Dataset      *dataset.Dataset
//	    ModifiedTime time.Time
//	    FileSize     int64
//	}
//
// Represents a single DICOM file with metadata and optional dataset.
//
// ## FileSet
//
//	type FileSet struct {
//	    RootDir      string
//	    DicomDirFile *dataset.Dataset
//	    FileRecords  []*FileRecord
//	    mu           sync.RWMutex
//	}
//
// Manages collection of DICOM files in a directory structure with thread-safe access.
//
// ## FileSetStatistics
//
//	type FileSetStatistics struct {
//	    TotalFiles   int64
//	    TotalSize    int64
//	    Modalities   map[string]int64
//	    PatientCount int64
//	    StudyCount   int64
//	    SeriesCount  int64
//	}
//
// Summary statistics about the file-set.
//
// # API Reference
//
// ## Creation
//
// ### NewFileSet
//
//	func NewFileSet(rootDir string) (*FileSet, error)
//
// Creates a new file-set for the specified root directory. Creates the directory if it doesn't exist.
//
// **Parameters:**
// - `rootDir`: Root directory path for file-set
//
// **Returns:** FileSet pointer and error
//
// **Example:**
// ```go
// fs, err := fileset.NewFileSet("/data/dicom")
//
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ```
//
// ## File Management
//
// ### AddFile
//
//	func (fs *FileSet) AddFile(filePath string) error
//
// Adds a single DICOM file to the file-set by storing file information.
// File must exist on disk. Dataset is deferred for later loading.
//
// **Parameters:**
// - `filePath`: Path to DICOM file
//
// **Returns:** Error if file not found or already in set
//
// **Example:**
// ```go
// err := fs.AddFile("/data/dicom/patient001.dcm")
//
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ```
//
// ### RemoveFile
//
//	func (fs *FileSet) RemoveFile(filePath string) error
//
// Removes a DICOM file from the file-set by path.
//
// **Parameters:**
// - `filePath`: Path to file to remove
//
// **Returns:** Error if file not found in set
//
// **Example:**
// ```go
// err := fs.RemoveFile("/data/dicom/patient001.dcm")
// ```
//
// ### ListFiles
//
//	func (fs *FileSet) ListFiles() []*FileRecord
//
// Returns all file records in the file-set. Returns a copy to prevent external modification.
//
// **Returns:** Slice of FileRecord pointers
//
// **Example:**
// ```go
// records := fs.ListFiles()
//
//	for _, record := range records {
//	    fmt.Printf("%s (%d bytes)\n", record.FilePath, record.FileSize)
//	}
//
// ```
//
// ## Searching
//
// ### FindByModality
//
//	func (fs *FileSet) FindByModality(modality string) []*FileRecord
//
// Filters file records by DICOM modality (e.g., "CT", "MR"). Only returns records with loaded datasets.
//
// **Parameters:**
// - `modality`: DICOM modality string
//
// **Returns:** Filtered FileRecord slice
//
// ### FindByPatient
//
//	func (fs *FileSet) FindByPatient(patientID string) []*FileRecord
//
// Filters file records by patient ID. Only returns records with loaded datasets.
//
// **Parameters:**
// - `patientID`: Patient identifier
//
// **Returns:** Filtered FileRecord slice
//
// ### FindByStudyInstanceUID
//
//	func (fs *FileSet) FindByStudyInstanceUID(studyUID string) []*FileRecord
//
// Filters file records by study instance UID.
//
// **Parameters:**
// - `studyUID`: Study instance UID
//
// **Returns:** Filtered FileRecord slice
//
// **Example:**
// ```go
// studyFiles := fs.FindByStudyInstanceUID("1.2.840.113619.2.55.3.1234567")
// ```
//
// ### FindBySeriesInstanceUID
//
//	func (fs *FileSet) FindBySeriesInstanceUID(seriesUID string) []*FileRecord
//
// Filters file records by series instance UID.
//
// **Parameters:**
// - `seriesUID`: Series instance UID
//
// **Returns:** Filtered FileRecord slice
//
// ## Directory Operations
//
// ### ScanDirectory
//
//	func (fs *FileSet) ScanDirectory(recursive bool) (int, error)
//
// Scans directory for files and adds them to the file-set.
//
// **Parameters:**
// - `recursive`: If true, scans subdirectories; if false, only immediate children
//
// **Returns:** Number of files added and error
//
// **Example:**
// ```go
// count, err := fs.ScanDirectory(true)  // Recursive scan
//
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// fmt.Printf("Added %d files\n", count)
// ```
//
// ## Sorting
//
// ### SortByPatientName
//
//	func (fs *FileSet) SortByPatientName()
//
// Sorts file records by file name (proxy for patient name).
//
// **Example:**
// ```go
// fs.SortByPatientName()
// records := fs.ListFiles()
// // Now sorted alphabetically by filename
// ```
//
// ### SortByModifiedTime
//
//	func (fs *FileSet) SortByModifiedTime()
//
// Sorts file records by modification time in ascending order.
//
// ### SortByFileSize
//
//	func (fs *FileSet) SortByFileSize()
//
// Sorts file records by file size in ascending order.
//
// ## Statistics and Validation
//
// ### GetStatistics
//
//	func (fs *FileSet) GetStatistics() FileSetStatistics
//
// Returns summary statistics about the file-set.
//
// **Returns:** FileSetStatistics struct with aggregate data
//
// **Example:**
// ```go
// stats := fs.GetStatistics()
// fmt.Printf("Files: %d, Total: %d bytes\n", stats.TotalFiles, stats.TotalSize)
// ```
//
// ### Validate
//
//	func (fs *FileSet) Validate() error
//
// Validates file-set integrity and completeness. Checks directory accessibility and file records.
//
// **Returns:** Error if validation fails
//
// **Example:**
// ```go
//
//	if err := fs.Validate(); err != nil {
//	    log.Fatal(err)
//	}
//
// ```
//
// ### GenerateDICOMDIR
//
//	func (fs *FileSet) GenerateDICOMDIR() (*dataset.Dataset, error)
//
// Generates a DICOMDIR file from the current file-set structure.
//
// **Returns:** Dataset pointer representing DICOMDIR and error
//
// **Example:**
// ```go
// dicomdir, err := fs.GenerateDICOMDIR()
//
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ```
//
// # Performance Characteristics
//
// | Operation | Complexity | Description |
// |-----------|-----------|-------------|
// | NewFileSet | O(1) | Directory creation |
// | AddFile | O(n) | n = current files (duplicate check) |
// | RemoveFile | O(n) | n = current files (linear search) |
// | ListFiles | O(n) | n = file count (returns copy) |
// | FindByPatient/Study/Series/Modality | O(n) | n = file count |
// | ScanDirectory (non-recursive) | O(m) | m = files in directory |
// | ScanDirectory (recursive) | O(m) | m = total files in tree |
// | SortByPatientName | O(n log n) | n = file count |
// | SortByModifiedTime | O(n log n) | n = file count |
// | SortByFileSize | O(n log n) | n = file count |
// | GetStatistics | O(n) | n = file count |
// | Validate | O(n) | n = file count |
// | GenerateDICOMDIR | O(n) | n = file count |
//
// # Thread Safety
//
// FileSet is thread-safe through sync.RWMutex:
//   - **Concurrent Reads**: ListFiles, FindBy*, GetStatistics, ScanDirectory
//   - **Exclusive Writes**: AddFile, RemoveFile, SortBy*, GenerateDICOMDIR
//   - **Blocked by Reads**: Write operations wait for all readers to complete
//
// # Error Handling
//
// | Operation | Error Condition |
// |-----------|-----------------|
// | NewFileSet | Root directory creation fails |
// | AddFile | File not found, file already in set |
// | RemoveFile | File not in set |
// | ScanDirectory | Directory read fails |
// | Validate | Root directory invalid or inaccessible, file records invalid |
// | GenerateDICOMDIR | File-set not properly loaded |
//
// # Use Cases
//
// ## Organizing DICOM Studies
//
// Create file-set for patient studies in a directory tree and manage file collection.
//
// ## Bulk File Operations
//
// Scan directory recursively and perform statistics, sorting, or searching operations.
//
// ## DICOMDIR Generation
//
// Create DICOMDIR file index from managed file collection.
//
// ## File Validation
//
// Validate integrity of DICOM file collection and metadata consistency.
//
// ## Memory-Efficient Processing
//
// Use deferred dataset loading to manage large collections without loading all data into memory.
//
// # Related Packages
//
//   - dataset: DICOM dataset structure and operations
//   - filereader: Reading DICOM files and loading datasets
//   - tag: DICOM tag definitions
//
// # DICOM Compliance
//
// Implements DICOM standard (PS3.11) for:
//   - File-set structure organization
//   - DICOMDIR file generation
//   - Patient/study/series hierarchy
//   - File record management
//
// See: https://www.dicomstandard.org/
//
// # Limitations
//
// - Search operations (FindBy*) require datasets to be loaded (not just metadata)
// - Modality filtering currently returns all records with loaded datasets
// - DICOMDIR generation is simplified and may not include all DICOM elements
package fileset
