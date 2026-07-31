package fileset_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/fileset"
	"github.com/amrshadid/go-dicom/tag"
)

// GenerateDICOMDIR used to return an empty data set. Its test asserted the
// result was not nil, which an empty data set satisfies, so a function that
// produced nothing passed for as long as it existed.
//
// Writing a real one is awkward for one reason: the records point at each other
// with byte offsets into the finished file, so nothing can be filled in until
// the layout is known and the layout depends on what is filled in. The file is
// written once with the offsets zeroed, read back to learn where each record
// landed, and written again — which works only because the offsets are UL and
// four bytes wide whatever they hold.

// scannedFileSet copies real DICOM files into a temporary directory and scans it.
func scannedFileSet(t *testing.T, names ...string) *fileset.FileSet {
	t.Helper()

	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	dir := t.TempDir()
	copied := 0
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(corpus, name))
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		copied++
	}
	if copied == 0 {
		t.Skip("none of the fixtures are present")
	}

	fs, err := fileset.NewFileSet(dir)
	if err != nil {
		t.Fatalf("NewFileSet: %v", err)
	}
	if _, err := fs.ScanDirectory(true); err != nil {
		t.Fatalf("ScanDirectory: %v", err)
	}
	return fs
}

// TestScanDirectoryParsesWhatItFinds covers the step everything else depends on.
//
// Scanning used to record a path and a nil data set for every file it met,
// which left every search and every statistic with nothing to work from, and
// put non-DICOM files in the file-set alongside the images.
func TestScanDirectoryParsesWhatItFinds(t *testing.T) {
	fs := scannedFileSet(t, "CT_small.dcm", "MR_small.dcm")

	// Something that is not DICOM, which must not be taken for part of the set.
	if err := os.WriteFile(filepath.Join(fs.RootDir, "README.txt"),
		[]byte("not a DICOM file"), 0o600); err != nil {
		t.Fatalf("writing README.txt: %v", err)
	}
	again, err := fileset.NewFileSet(fs.RootDir)
	if err != nil {
		t.Fatalf("NewFileSet: %v", err)
	}
	n, err := again.ScanDirectory(true)
	if err != nil {
		t.Fatalf("ScanDirectory: %v", err)
	}
	if n != 2 {
		t.Errorf("scanning found %d files, want 2; a text file was taken for DICOM", n)
	}
	for _, rec := range again.ListFiles() {
		if rec.Dataset == nil {
			t.Errorf("%s was recorded without a data set", rec.FilePath)
		}
	}
}

// TestFindActuallyFilters covers four methods that ignored their argument.
//
// Each returned every record with a data set, so a search for a modality that
// is not in the file-set returned all of it.
func TestFindActuallyFilters(t *testing.T) {
	fs := scannedFileSet(t, "CT_small.dcm", "MR_small.dcm")
	if len(fs.ListFiles()) != 2 {
		t.Skip("both fixtures are needed for this")
	}

	if got := len(fs.FindByModality("CT")); got != 1 {
		t.Errorf("FindByModality(CT) returned %d records, want 1", got)
	}
	if got := len(fs.FindByModality("MR")); got != 1 {
		t.Errorf("FindByModality(MR) returned %d records, want 1", got)
	}
	if got := len(fs.FindByModality("US")); got != 0 {
		t.Errorf("FindByModality(US) returned %d records, want 0 — no ultrasound is in "+
			"this file-set", got)
	}
	if got := len(fs.FindByPatient("no such patient")); got != 0 {
		t.Errorf("FindByPatient returned %d records for a patient that is not here", got)
	}
}

// TestStatisticsCountDistinctEntities covers counts that were all the file count.
//
// Forty slices of one series reported forty patients, forty studies and forty
// series, and the modality breakdown was always empty.
func TestStatisticsCountDistinctEntities(t *testing.T) {
	fs := scannedFileSet(t, "CT_small.dcm", "MR_small.dcm")
	if len(fs.ListFiles()) != 2 {
		t.Skip("both fixtures are needed for this")
	}

	stats := fs.GetStatistics()
	if stats.TotalFiles != 2 {
		t.Fatalf("TotalFiles is %d, want 2", stats.TotalFiles)
	}
	if len(stats.Modalities) == 0 {
		t.Error("the modality breakdown is empty")
	}
	if stats.PatientCount != 2 {
		t.Errorf("PatientCount is %d, want 2 distinct patients", stats.PatientCount)
	}
	if stats.SeriesCount != 2 {
		t.Errorf("SeriesCount is %d, want 2 distinct series", stats.SeriesCount)
	}
}

// TestGeneratedDICOMDIRHasWorkingOffsets is the point of the two passes.
func TestGeneratedDICOMDIRHasWorkingOffsets(t *testing.T) {
	fs := scannedFileSet(t, "CT_small.dcm", "MR_small.dcm", "rtplan.dcm")
	count := len(fs.ListFiles())

	data, err := fs.WriteDICOMDIR(fileset.DICOMDIROptions{FileSetID: "TESTSET"})
	if err != nil {
		t.Fatalf("WriteDICOMDIR: %v", err)
	}

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatalf("the generated file does not read back: %v", err)
	}
	dd, err := fileset.ReadDICOMDIR(df)
	if err != nil {
		t.Fatalf("ReadDICOMDIR: %v", err)
	}

	if !dd.OffsetsWereUsed {
		t.Error("the tree had to be rebuilt from record order, so the offsets written " +
			"do not resolve to the records they name")
	}
	if dd.FileSetID != "TESTSET" {
		t.Errorf("File-set ID is %q, want TESTSET", dd.FileSetID)
	}
	if got := len(dd.RecordsOfType(fileset.RecordImage)); got != count {
		t.Errorf("the index has %d IMAGE records for %d files", got, count)
	}

	// Every IMAGE has to name a file and sit three levels down.
	for _, img := range dd.RecordsOfType(fileset.RecordImage) {
		if len(img.ReferencedFileID) == 0 {
			t.Error("an IMAGE record references no file")
		}
		if img.StringValue(tag.New(0x0004, 0x1511)) == "" {
			t.Error("an IMAGE record has no Referenced SOP Instance UID in File")
		}
	}
}

// TestGeneratedDICOMDIRIsSelfConsistent checks the guard that stops a wrong file
// from being written at all.
//
// The second pass has to lay out identically to the first. If it ever does not,
// the offsets describe the earlier layout — and a file with wrong offsets is
// worse than no file, because a reader accepts it and follows it into a tree
// that is not there. WriteDICOMDIR re-reads its own output and refuses to
// return one that disagrees, so reaching this point at all means it agreed.
func TestGeneratedDICOMDIRIsSelfConsistent(t *testing.T) {
	fs := scannedFileSet(t, "CT_small.dcm", "MR_small.dcm")

	// A File-set ID of odd length pads to even, and one of even length does
	// not. Both have to produce a file whose offsets check out.
	for _, id := range []string{"ODD", "EVEN"} {
		t.Run(id, func(t *testing.T) {
			if _, err := fs.WriteDICOMDIR(fileset.DICOMDIROptions{FileSetID: id}); err != nil {
				t.Fatalf("WriteDICOMDIR: %v", err)
			}
		})
	}
}

// TestGenerateIsDeterministic checks the same file-set gives the same file.
//
// It cannot be byte-identical without pinning the instance UID, which is a new
// instance each time by design, so the UID is supplied here and the rest must
// match.
func TestGenerateIsDeterministic(t *testing.T) {
	fs := scannedFileSet(t, "CT_small.dcm", "MR_small.dcm")

	opts := fileset.DICOMDIROptions{
		FileSetID:      "TESTSET",
		SOPInstanceUID: "1.2.826.0.1.3680043.10.511.5.99",
	}
	first, err := fs.WriteDICOMDIR(opts)
	if err != nil {
		t.Fatalf("WriteDICOMDIR: %v", err)
	}
	second, err := fs.WriteDICOMDIR(opts)
	if err != nil {
		t.Fatalf("WriteDICOMDIR: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two generations of the same file-set differ")
	}
}

// TestFilesThatCannotBePlacedAreReported covers the case an index must not hide.
//
// A file with no Study or Series Instance UID has nowhere to go in the
// hierarchy. Dropping it quietly would leave an index that looks complete and
// is not, which is the one thing an index must never be.
func TestFilesThatCannotBePlacedAreReported(t *testing.T) {
	dir := t.TempDir()
	fs, err := fileset.NewFileSet(dir)
	if err != nil {
		t.Fatalf("NewFileSet: %v", err)
	}
	fs.FileRecords = append(fs.FileRecords, &fileset.FileRecord{
		FilePath: filepath.Join(dir, "nowhere.dcm"),
	})

	if _, err := fs.WriteDICOMDIR(fileset.DICOMDIROptions{}); err == nil {
		t.Error("a file that cannot be placed in the hierarchy was silently omitted")
	}
}
