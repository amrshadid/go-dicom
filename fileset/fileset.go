package fileset

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// ErrNoFileMeta is the reason a data set with no File Meta Information is
// not taken into a file-set: no preamble, no "DICM", no group 0002. Every
// file of a File-set is in the DICOM File Format (PS3.10 7.1, 8.1), and a
// DICOMDIR naming one that is not sends a reader to a file it cannot open as
// one. AddFile returns an error wrapping it, and Scan reports the file with it.
var ErrNoFileMeta = errors.New("a data set without the file meta information of PS3.10")

// ErrNotDICOM is the reason a file that is not a DICOM instance is not taken
// into a file-set.
var ErrNotDICOM = errors.New("not a DICOM instance")

// FileRecord represents a single DICOM file in the file-set.
type FileRecord struct {
	FilePath     string
	Dataset      *dataset.Dataset
	ModifiedTime time.Time
	FileSize     int64
}

// FileSet represents a collection of DICOM files in a directory structure.
type FileSet struct {
	RootDir      string
	DicomDirFile *dataset.Dataset
	FileRecords  []*FileRecord
	mu           sync.RWMutex
}

// NewFileSet creates a new empty file-set for the given root directory.
func NewFileSet(rootDir string) (*FileSet, error) {
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}

	return &FileSet{
		RootDir:     rootDir,
		FileRecords: make([]*FileRecord, 0),
	}, nil
}

// AddFile adds a DICOM file to the file-set with deferred dataset loading.
func (fs *FileSet) AddFile(filePath string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, record := range fs.FileRecords {
		if record.FilePath == filePath {
			return fmt.Errorf("file already exists in fileset: %s", filePath)
		}
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	// Parsed, not merely listed. A record with no data set is invisible to
	// every search and every statistic, and cannot be placed in a DICOMDIR.
	record, err := readFileRecord(filePath, fileInfo)
	if err != nil {
		return fmt.Errorf("failed to add %s to the file-set: %w", filePath, err)
	}

	fs.FileRecords = append(fs.FileRecords, record)
	return nil
}

// RemoveFile removes a DICOM file from the file-set by path.
func (fs *FileSet) RemoveFile(filePath string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for i, record := range fs.FileRecords {
		if record.FilePath == filePath {
			fs.FileRecords = append(fs.FileRecords[:i], fs.FileRecords[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("file not found in fileset: %s", filePath)
}

// ListFiles returns all file records in the file-set.
func (fs *FileSet) ListFiles() []*FileRecord {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	records := make([]*FileRecord, len(fs.FileRecords))
	copy(records, fs.FileRecords)
	return records
}

// FindByModality filters file records by DICOM modality.
func (fs *FileSet) FindByModality(modality string) []*FileRecord {
	return fs.matchingRecords(tag.New(0x0008, 0x0060), modality)
}

// FindByPatient filters file records by patient ID.
func (fs *FileSet) FindByPatient(patientID string) []*FileRecord {
	return fs.matchingRecords(tag.New(0x0010, 0x0020), patientID)
}

// FindByStudyInstanceUID filters file records by study UID.
func (fs *FileSet) FindByStudyInstanceUID(studyUID string) []*FileRecord {
	return fs.matchingRecords(tag.New(0x0020, 0x000D), studyUID)
}

// FindBySeriesInstanceUID filters file records by series UID.
func (fs *FileSet) FindBySeriesInstanceUID(seriesUID string) []*FileRecord {
	return fs.matchingRecords(tag.New(0x0020, 0x000E), seriesUID)
}

// matchingRecords returns the records whose data set has value at tag t.
//
// The four Find methods below used to ignore their argument and return every
// record with a data set, which made them look like they worked on a file-set
// whose files were all wanted anyway. Searching for a modality that is not
// present returned everything.
func (fs *FileSet) matchingRecords(t tag.Tag, want string) []*FileRecord {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	want = strings.TrimRight(want, " \x00")
	var results []*FileRecord
	for _, record := range fs.FileRecords {
		if record == nil || record.Dataset == nil {
			continue
		}
		if recordString(record.Dataset, t) == want {
			results = append(results, record)
		}
	}
	return results
}

// Validate checks the integrity and completeness of the file-set.
func (fs *FileSet) Validate() error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.RootDir == "" {
		return fmt.Errorf("fileset has no root directory")
	}

	info, err := os.Stat(fs.RootDir)
	if err != nil {
		return fmt.Errorf("root directory is not accessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("root path is not a directory: %s", fs.RootDir)
	}

	validCount := 0
	for i, record := range fs.FileRecords {
		if err := validateFileRecord(record, i); err != nil {
			return fmt.Errorf("invalid file record at index %d: %w", i, err)
		}
		if record.Dataset != nil {
			validCount++
		}
	}

	return nil
}

// GetStatistics returns summary statistics about the file-set.
func (fs *FileSet) GetStatistics() FileSetStatistics {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	stats := FileSetStatistics{
		TotalFiles: int64(len(fs.FileRecords)),
		Modalities: make(map[string]int64),
	}

	// Counted by distinct identifier, not by file. Every count used to be the
	// file count, so a file-set of forty slices from one series reported forty
	// patients, forty studies and forty series.
	patients := map[string]struct{}{}
	studies := map[string]struct{}{}
	series := map[string]struct{}{}

	for _, record := range fs.FileRecords {
		if record == nil {
			continue
		}
		stats.TotalSize += record.FileSize
		if record.Dataset == nil {
			continue
		}
		if v := recordString(record.Dataset, tag.New(0x0008, 0x0060)); v != "" {
			stats.Modalities[v]++
		}
		if v := recordString(record.Dataset, tag.New(0x0010, 0x0020)); v != "" {
			patients[v] = struct{}{}
		}
		if v := recordString(record.Dataset, tag.New(0x0020, 0x000D)); v != "" {
			studies[v] = struct{}{}
		}
		if v := recordString(record.Dataset, tag.New(0x0020, 0x000E)); v != "" {
			series[v] = struct{}{}
		}
	}

	stats.PatientCount = int64(len(patients))
	stats.StudyCount = int64(len(studies))
	stats.SeriesCount = int64(len(series))
	return stats
}

// FileSetStatistics contains summary statistics about a file-set.
type FileSetStatistics struct {
	TotalFiles   int64
	TotalSize    int64
	Modalities   map[string]int64
	PatientCount int64
	StudyCount   int64
	SeriesCount  int64
}

// ScanDirectory scans the root directory for files and adds the DICOM files
// among them to the file-set, returning how many it added.
//
// A file that cannot be a member is left out without failing the scan: a
// file that is not DICOM, and a bare data set with no File Meta Information
// (ErrNoFileMeta). A directory holds whatever was put in it, and a scan that
// stopped at the first README would index nothing. Scan does the same and
// also says which files were left out and why.
func (fs *FileSet) ScanDirectory(recursive bool) (int, error) {
	added, _, err := fs.Scan(recursive)
	return added, err
}

// SkippedFile is a file a scan found in the directory and did not add to the
// file-set, and why.
type SkippedFile struct {
	Path string
	// Err is the reason: it wraps ErrNoFileMeta for a data set that is not
	// in the File Format, ErrNotDICOM for anything else that is not an
	// instance, and is the error from opening it for a file that could not
	// be opened.
	Err error
}

// Scan is ScanDirectory, and returns as well the files it left out, in the
// order it met them, each with the reason.
//
// Leaving a file out is a report, not an error: the error is for a directory
// that could not be read.
func (fs *FileSet) Scan(recursive bool) (added int, skipped []SkippedFile, err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	known := make(map[string]bool, len(fs.FileRecords))
	for _, record := range fs.FileRecords {
		known[record.FilePath] = true
	}
	consider := func(path string, info os.FileInfo) {
		if known[path] {
			return
		}
		record, err := readFileRecord(path, info)
		if err != nil {
			skipped = append(skipped, SkippedFile{Path: path, Err: err})
			return
		}
		fs.FileRecords = append(fs.FileRecords, record)
		known[path] = true
		added++
	}

	if recursive {
		err := filepath.Walk(fs.RootDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				consider(path, info)
			}
			return nil
		})
		if err != nil {
			return added, skipped, fmt.Errorf("error scanning directory: %w", err)
		}
		return added, skipped, nil
	}

	entries, err := os.ReadDir(fs.RootDir)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		consider(filepath.Join(fs.RootDir, entry.Name()), info)
	}
	return added, skipped, nil
}

// SortByPatientName sorts file records by file name.
func (fs *FileSet) SortByPatientName() {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	sort.Slice(fs.FileRecords, func(i, j int) bool {
		nameI := filepath.Base(fs.FileRecords[i].FilePath)
		nameJ := filepath.Base(fs.FileRecords[j].FilePath)
		return nameI < nameJ
	})
}

// SortByModifiedTime sorts file records by modification time.
func (fs *FileSet) SortByModifiedTime() {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	sort.Slice(fs.FileRecords, func(i, j int) bool {
		return fs.FileRecords[i].ModifiedTime.Before(fs.FileRecords[j].ModifiedTime)
	})
}

// SortByFileSize sorts file records by file size.
func (fs *FileSet) SortByFileSize() {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	sort.Slice(fs.FileRecords, func(i, j int) bool {
		return fs.FileRecords[i].FileSize < fs.FileRecords[j].FileSize
	})
}

func validateFileRecord(record *FileRecord, index int) error {
	if record == nil {
		return fmt.Errorf("record is nil")
	}

	if record.FilePath == "" {
		return fmt.Errorf("file path is empty")
	}

	info, err := os.Stat(record.FilePath)
	if err != nil {
		return fmt.Errorf("file does not exist or is inaccessible: %w", err)
	}

	if record.FileSize != info.Size() {
		return fmt.Errorf("file size mismatch (stored: %d, actual: %d)", record.FileSize, info.Size())
	}

	return nil
}

// ExecuteFileSetHook executes a hook for file-set operations.
func ExecuteFileSetHook(fs *FileSet, operation string) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	switch operation {
	case "list_files":
		records := fs.ListFiles()
		result["count"] = len(records)
		result["files"] = records
	case "get_statistics":
		stats := fs.GetStatistics()
		result["total_files"] = stats.TotalFiles
		result["total_size"] = stats.TotalSize
		result["modalities"] = stats.Modalities
		result["patient_count"] = stats.PatientCount
		result["study_count"] = stats.StudyCount
		result["series_count"] = stats.SeriesCount
	case "validate":
		if err := fs.Validate(); err != nil {
			return result, fmt.Errorf("validation failed: %w", err)
		}
		result["valid"] = true
	default:
		return result, fmt.Errorf("unknown operation: %s", operation)
	}

	return result, nil
}

// ConvertFileSetToRawFormat converts a FileSet to a format suitable for hook processing.
func ConvertFileSetToRawFormat(fs *FileSet) map[string]interface{} {
	records := fs.ListFiles()
	rawRecords := make([]map[string]interface{}, 0)

	for _, record := range records {
		rawRecord := map[string]interface{}{
			"file_path":     record.FilePath,
			"file_size":     record.FileSize,
			"modified_time": record.ModifiedTime,
			"has_dataset":   record.Dataset != nil,
		}
		rawRecords = append(rawRecords, rawRecord)
	}

	stats := fs.GetStatistics()
	return map[string]interface{}{
		"root_dir":      fs.RootDir,
		"file_records":  rawRecords,
		"record_count":  len(records),
		"total_size":    stats.TotalSize,
		"total_files":   stats.TotalFiles,
		"patient_count": stats.PatientCount,
		"study_count":   stats.StudyCount,
		"series_count":  stats.SeriesCount,
	}
}

// ConvertRawFormatToFileSet converts raw format data back to FileSet state.
func ConvertRawFormatToFileSet(rawData map[string]interface{}, fs *FileSet) error {
	if rawRecords, ok := rawData["file_records"].([]interface{}); ok {
		for _, rawRecord := range rawRecords {
			if record, ok := rawRecord.(map[string]interface{}); ok {
				if filePath, ok := record["file_path"].(string); ok {
					if err := fs.AddFile(filePath); err != nil {
						return fmt.Errorf("failed to add file %s: %w", filePath, err)
					}
				}
			}
		}
	}
	return nil
}

// readFileRecord parses one file into a record.
//
// Scanning used to record the path and a nil data set, which left every search
// and every statistic with nothing to work from — and added README files and
// thumbnails to the file-set alongside the images.
//
// Pixel data is dropped from the retained data set. A file-set index needs the
// identifiers and the descriptive attributes; keeping the pixels would mean
// holding every image in the directory in memory at once, which is the
// difference between indexing a study and indexing a hospital.
func readFileRecord(path string, info os.FileInfo) (*FileRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotDICOM, err)
	}
	ds := df.GetDataset()
	if ds == nil {
		return nil, fmt.Errorf("%w: it parsed to no data set", ErrNotDICOM)
	}

	// Parsing succeeding is not evidence that the file is DICOM. A stream with
	// no meta header is read as a raw data set, and arbitrary bytes can be read
	// that way without producing an error — a README in the directory would
	// otherwise be indexed as an instance. The two UIDs that identify an
	// instance tell the two apart.
	//
	// Being DICOM is not enough either. A file in a File-set is in the DICOM
	// File Format: the preamble, "DICM" and the File Meta Information (PS3.10
	// 7.1, 8.1). A bare data set used to be indexed when it had the UIDs, and
	// the DICOMDIR then named a file dcmtk's dcmmkdir refuses ("file not in
	// part 10 format (no file meta information)") and pydicom's FileSet cannot
	// load. The dicomdir command checked for it on its own; this is now the one
	// place that knows.
	if !df.HasPreamble || len(df.MetaElements) == 0 {
		if recordString(ds, tag.New(0x0008, 0x0016)) == "" ||
			recordString(ds, tag.New(0x0008, 0x0018)) == "" {
			return nil, fmt.Errorf("%w: it has neither a DICM preamble nor a SOP "+
				"Class and Instance UID", ErrNotDICOM)
		}
		return nil, ErrNoFileMeta
	}
	_ = ds.Remove(tag.New(0x7FE0, 0x0010))

	return &FileRecord{
		FilePath:     path,
		Dataset:      ds,
		ModifiedTime: info.ModTime(),
		FileSize:     info.Size(),
	}, nil
}
