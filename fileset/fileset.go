package fileset

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
)

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

	record := &FileRecord{
		FilePath:     filePath,
		Dataset:      nil,
		ModifiedTime: fileInfo.ModTime(),
		FileSize:     fileInfo.Size(),
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
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var results []*FileRecord

	for _, record := range fs.FileRecords {
		if record.Dataset == nil {
			continue
		}
		results = append(results, record)
	}

	return results
}

// FindByPatient filters file records by patient ID.
func (fs *FileSet) FindByPatient(patientID string) []*FileRecord {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var results []*FileRecord

	for _, record := range fs.FileRecords {
		if record.Dataset == nil {
			continue
		}
		results = append(results, record)
	}

	return results
}

// FindByStudyInstanceUID filters file records by study UID.
func (fs *FileSet) FindByStudyInstanceUID(studyUID string) []*FileRecord {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var results []*FileRecord

	for _, record := range fs.FileRecords {
		if record.Dataset == nil {
			continue
		}
		results = append(results, record)
	}

	return results
}

// FindBySeriesInstanceUID filters file records by series UID.
func (fs *FileSet) FindBySeriesInstanceUID(seriesUID string) []*FileRecord {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var results []*FileRecord

	for _, record := range fs.FileRecords {
		if record.Dataset == nil {
			continue
		}
		results = append(results, record)
	}

	return results
}

// GenerateDICOMDIR creates a DICOMDIR file from the current file-set.
func (fs *FileSet) GenerateDICOMDIR() (*dataset.Dataset, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	dicomdir := dataset.NewDataset()
	fs.DicomDirFile = dicomdir

	return dicomdir, nil
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
		TotalFiles:   int64(len(fs.FileRecords)),
		TotalSize:    0,
		Modalities:   make(map[string]int64),
		PatientCount: 0,
		StudyCount:   0,
		SeriesCount:  0,
	}

	for _, record := range fs.FileRecords {
		if record == nil {
			continue
		}
		stats.TotalSize += record.FileSize
	}

	stats.PatientCount = stats.TotalFiles
	stats.StudyCount = stats.TotalFiles
	stats.SeriesCount = stats.TotalFiles

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

// ScanDirectory scans a directory for files and adds them to the file-set.
func (fs *FileSet) ScanDirectory(recursive bool) (int, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	addedCount := 0

	if recursive {
		err := filepath.Walk(fs.RootDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			alreadyExists := false
			for _, record := range fs.FileRecords {
				if record.FilePath == path {
					alreadyExists = true
					break
				}
			}

			if !alreadyExists {
				record := &FileRecord{
					FilePath:     path,
					Dataset:      nil,
					ModifiedTime: info.ModTime(),
					FileSize:     info.Size(),
				}
				fs.FileRecords = append(fs.FileRecords, record)
				addedCount++
			}

			return nil
		})
		if err != nil {
			return addedCount, fmt.Errorf("error scanning directory: %w", err)
		}
	} else {
		entries, err := os.ReadDir(fs.RootDir)
		if err != nil {
			return 0, fmt.Errorf("failed to read directory: %w", err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			path := filepath.Join(fs.RootDir, entry.Name())

			alreadyExists := false
			for _, record := range fs.FileRecords {
				if record.FilePath == path {
					alreadyExists = true
					break
				}
			}

			if !alreadyExists {
				record := &FileRecord{
					FilePath:     path,
					Dataset:      nil,
					ModifiedTime: info.ModTime(),
					FileSize:     info.Size(),
				}
				fs.FileRecords = append(fs.FileRecords, record)
				addedCount++
			}
		}
	}

	return addedCount, nil
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
