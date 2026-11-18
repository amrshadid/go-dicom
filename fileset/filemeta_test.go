package fileset_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/fileset"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// corpusFile returns the bytes of a file of pydicom's corpus, skipping the
// test when the corpus is not configured.
func corpusFile(t *testing.T, name string) []byte {
	t.Helper()
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	data, err := os.ReadFile(filepath.Join(corpus, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// addAsPart10 writes a corpus file that is a bare data set into the file-set's
// directory as a Part 10 file, with the File Meta Information it lacks, and
// adds it. rtstruct.dcm is pydicom's only RT Structure Set, and it has none.
func addAsPart10(t *testing.T, fs *fileset.FileSet, name, id string) {
	t.Helper()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(corpusFile(t, name))))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	ds := df.GetDataset()
	text := func(t tag.Tag) string {
		e, _ := ds.Get(t)
		v, _ := e.GetValue().([]byte)
		return string(bytes.TrimRight(v, "\x00 "))
	}
	path := filepath.Join(fs.RootDir, id)
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    text(tag.New(0x0008, 0x0016)),
		MediaStorageSOPInstanceUID: text(tag.New(0x0008, 0x0018)),
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})
	if err := w.AddDataElements(filewriter.ElementsFromDataset(ds)); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fs.AddFile(path); err != nil {
		t.Fatalf("adding %s written as a Part 10 file: %v", name, err)
	}
}

// AddFile refuses a data set with no File Meta Information, rtstruct.dcm: no
// preamble, no DICM, no group 0002. Every file of a File-set is in the DICOM
// File Format (PS3.10 7.1, 8.1); dcmmkdir refuses this one ("file not in part
// 10 format (no file meta information)") and pydicom's FileSet cannot load it.
// It was added, because it carries the SOP Class and Instance UIDs, and the
// DICOMDIR named it. The error says why and can be told apart from a file that
// is not DICOM at all.
func TestAFileWithoutFileMetaIsNotAdded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "RTSTRUCT")
	if err := os.WriteFile(path, corpusFile(t, "rtstruct.dcm"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, err := fileset.NewFileSet(dir)
	if err != nil {
		t.Fatal(err)
	}

	err = fs.AddFile(path)
	if !errors.Is(err, fileset.ErrNoFileMeta) {
		t.Fatalf("AddFile of a bare data set gave %v, want ErrNoFileMeta", err)
	}
	if errors.Is(err, fileset.ErrNotDICOM) {
		t.Error("a bare data set with an instance's UIDs is reported as not DICOM")
	}
	if n := len(fs.ListFiles()); n != 0 {
		t.Errorf("%d files in the file-set after the refusal", n)
	}
}

// A directory scan leaves out the bare data set and a text file, says which
// and why, and does not fail: a directory holds whatever was put in it, and a
// scan that stopped at the first stray file would index nothing. The DICOMDIR
// written from the scan names the two Part 10 files only.
func TestScanReportsTheFilesItLeavesOut(t *testing.T) {
	dir := t.TempDir()
	for name, id := range map[string]string{
		"CT_small.dcm": "CTSMALL",
		"MR_small.dcm": "MRSMALL",
		"rtstruct.dcm": "RTSTRUCT",
	} {
		if err := os.WriteFile(filepath.Join(dir, id), corpusFile(t, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("not a DICOM file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fs, err := fileset.NewFileSet(dir)
	if err != nil {
		t.Fatal(err)
	}
	added, skipped, err := fs.Scan(false)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if added != 2 {
		t.Errorf("Scan added %d files, want the two Part 10 files", added)
	}
	reasons := map[string]error{}
	for _, s := range skipped {
		reasons[filepath.Base(s.Path)] = s.Err
	}
	if len(reasons) != 2 {
		t.Errorf("Scan left out %v, want RTSTRUCT and README", reasons)
	}
	if !errors.Is(reasons["RTSTRUCT"], fileset.ErrNoFileMeta) {
		t.Errorf("RTSTRUCT left out with %v, want ErrNoFileMeta", reasons["RTSTRUCT"])
	}
	if !errors.Is(reasons["README"], fileset.ErrNotDICOM) {
		t.Errorf("README left out with %v, want ErrNotDICOM", reasons["README"])
	}

	data, err := fs.WriteDICOMDIR(fileset.DICOMDIROptions{})
	if err != nil {
		t.Fatalf("WriteDICOMDIR: %v", err)
	}
	if bytes.Contains(data, []byte("RTSTRUCT")) {
		t.Error("the DICOMDIR names RTSTRUCT, a file with no File Meta Information")
	}
	for _, id := range []string{"CTSMALL", "MRSMALL"} {
		if !bytes.Contains(data, []byte(id)) {
			t.Errorf("the DICOMDIR does not name %s", id)
		}
	}

	// ScanDirectory is the same scan, and leaving files out is not an error.
	again, err := fileset.NewFileSet(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := again.ScanDirectory(false); n != 2 || err != nil {
		t.Errorf("ScanDirectory = %d, %v; want 2, nil", n, err)
	}
}
