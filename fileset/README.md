# FileSet

DICOM file-set management: directory scanning, the Patient/Study/Series hierarchy, search, statistics, and DICOMDIR reading and generation.

A DICOMDIR stores its records in one flat sequence and describes a tree. The links are byte offsets into the file, not sequence order, so `ReadDICOMDIR` builds the tree from the offsets and falls back to order only for a file that has none. `WriteDICOMDIR` computes those offsets by writing the file twice — once to learn the layout, once with the real values — and re-reads its own output before returning it, because a file with wrong offsets is worse than no file: a reader accepts it and follows it into a tree that is not there.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/fileset"

fs, _ := fileset.NewFileSet("/data/dicom/studies")
count, _ := fs.ScanDirectory(true) // recursive scan

// Search
ctFiles := fs.FindByModality("CT")
patientFiles := fs.FindByPatient("P12345")
studyFiles := fs.FindByStudyInstanceUID("1.2.3.4.5")

// Sort and list
fs.SortByModifiedTime()
records := fs.ListFiles()

// Statistics and DICOMDIR
stats := fs.GetStatistics()
dicomdir, _ := fs.GenerateDICOMDIR()
```

## API Reference

```go
func NewFileSet(rootDir string) (*FileSet, error)
func (fs *FileSet) ScanDirectory(recursive bool) (int, error)
func (fs *FileSet) AddFile(filePath string) error
func (fs *FileSet) RemoveFile(filePath string) error
func (fs *FileSet) ListFiles() []*FileRecord
func (fs *FileSet) FindByModality(modality string) []*FileRecord
func (fs *FileSet) FindByPatient(patientID string) []*FileRecord
func (fs *FileSet) FindByStudyInstanceUID(studyUID string) []*FileRecord
func (fs *FileSet) FindBySeriesInstanceUID(seriesUID string) []*FileRecord
func (fs *FileSet) SortByPatientName() / SortByModifiedTime() / SortByFileSize()
func (fs *FileSet) GetStatistics() FileSetStatistics
func (fs *FileSet) Validate() error
func (fs *FileSet) GenerateDICOMDIR() (*dataset.Dataset, error)

type FileRecord struct {
    FilePath string; Dataset *dataset.Dataset
    ModifiedTime time.Time; FileSize int64
}

type FileSetStatistics struct {
    TotalFiles, TotalSize int64
    Modalities map[string]int64
    PatientCount, StudyCount, SeriesCount int64
}
```

## References

- [DICOM PS3.11](https://dicom.nema.org/medical/dicom/current/output/html/part11.html) - Media Storage and File-Set Organization
