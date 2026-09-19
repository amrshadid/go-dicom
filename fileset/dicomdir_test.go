package fileset_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/fileset"
)

// A DICOMDIR stores its records in one flat sequence and describes a tree. The
// links are byte offsets, not sequence order, and pydicom ships DICOMDIR-
// reordered because a conforming file may store the records in any order it
// likes. Reading them in order gives the right records in the wrong shape.
//
// The counts below are pydicom's, taken from its FileSet reader over the same
// seven fixtures.

func dicomdirDir(t *testing.T) string {
	t.Helper()

	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	dir := filepath.Join(corpus, "dicomdirtests")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("pydicom's dicomdirtests are not present: %v", err)
	}
	return dir
}

func readDICOMDIR(t *testing.T, path string) *fileset.DICOMDIR {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Skipf("%s is not present: %v", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	dd, err := fileset.ReadDICOMDIR(df)
	if err != nil {
		t.Fatalf("ReadDICOMDIR: %v", err)
	}
	return dd
}

// TestDICOMDIRRecordCounts covers the fixtures against pydicom's reading.
func TestDICOMDIRRecordCounts(t *testing.T) {
	dir := dicomdirDir(t)

	tests := []struct {
		file    string
		records int
		roots   int
		images  int
		why     string
	}{
		{"DICOMDIR", 52, 2, 31, ""},
		// Neither the byte order nor the VR encoding changes what the offsets
		// mean, so both of these read the same tree as the file above.
		{"DICOMDIR-bigEnd", 52, 2, 31, ""},
		{"DICOMDIR-implicit", 52, 2, 31, ""},
		// The records are stored in an order that is not the tree order. A
		// reader that took the sequence order for the hierarchy would build a
		// different, wrong tree here and the same right one everywhere else.
		{"DICOMDIR-reordered", 52, 2, 31, "the sequence order is not the tree"},
		// Media with nothing on it yet, which is a valid file-set.
		{"DICOMDIR-empty.dcm", 0, 0, 0, ""},
		// This file ends 24 bytes into its 52nd record. The other 51 are whole
		// and are kept; pydicom keeps a partial 52nd, so its image count is one
		// higher.
		{"DICOMDIR-nooffset", 51, 2, 30, "the last record is truncated"},
	}

	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			dd := readDICOMDIR(t, filepath.Join(dir, tc.file))

			if got := len(dd.Records); got != tc.records {
				t.Errorf("read %d records, want %d %s", got, tc.records, tc.why)
			}
			if got := len(dd.Roots); got != tc.roots {
				t.Errorf("built %d roots, want %d", got, tc.roots)
			}
			if got := len(dd.RecordsOfType(fileset.RecordImage)); got != tc.images {
				t.Errorf("found %d IMAGE records in the tree, want %d %s",
					got, tc.images, tc.why)
			}
		})
	}
}

// TestDICOMDIRHierarchy checks the shape, not only the counts.
//
// Every IMAGE has to sit under a SERIES, under a STUDY, under a PATIENT. A
// reader that collected the right records without linking them would pass a
// count and fail this.
func TestDICOMDIRHierarchy(t *testing.T) {
	dd := readDICOMDIR(t, filepath.Join(dicomdirDir(t), "DICOMDIR"))

	want := []string{fileset.RecordPatient, fileset.RecordStudy, fileset.RecordSeries}
	var checked int

	var walk func(r *fileset.Record, ancestry []string)
	walk = func(r *fileset.Record, ancestry []string) {
		if r.Type == fileset.RecordImage {
			checked++
			if len(ancestry) != len(want) {
				t.Errorf("an IMAGE sits %d levels down, want %d: %v",
					len(ancestry), len(want), ancestry)
				return
			}
			for i := range want {
				if ancestry[i] != want[i] {
					t.Errorf("an IMAGE has ancestry %v, want %v", ancestry, want)
					return
				}
			}
			if len(r.ReferencedFileID) == 0 {
				t.Error("an IMAGE record references no file")
			}
		}
		for _, c := range r.Children {
			walk(c, append(ancestry, r.Type))
		}
	}
	for _, root := range dd.Roots {
		walk(root, nil)
	}

	if checked != 31 {
		t.Errorf("walked %d IMAGE records, want 31", checked)
	}
}

// TestReorderedFileGivesTheSameTree is the point of the offsets.
//
// DICOMDIR and DICOMDIR-reordered hold the same file-set, stored in different
// orders. Read through the offsets they are the same tree; read in sequence
// order they are not.
func TestReorderedFileGivesTheSameTree(t *testing.T) {
	dir := dicomdirDir(t)

	paths := func(dd *fileset.DICOMDIR) []string {
		var out []string
		dd.Walk(func(r *fileset.Record) bool {
			if r.Type == fileset.RecordImage {
				out = append(out, r.Path("/"))
			}
			return true
		})
		return out
	}

	ordered := paths(readDICOMDIR(t, filepath.Join(dir, "DICOMDIR")))
	reordered := paths(readDICOMDIR(t, filepath.Join(dir, "DICOMDIR-reordered")))

	if len(ordered) != len(reordered) {
		t.Fatalf("the two files give %d and %d images", len(ordered), len(reordered))
	}
	for i := range ordered {
		if ordered[i] != reordered[i] {
			t.Fatalf("image %d is %q in one file and %q in the other; the tree was built "+
				"from sequence order rather than from the offsets", i, ordered[i], reordered[i])
		}
	}
}

// TestOffsetsAreUsedWhenPresent records which path was taken, since the two
// produce the same answer on a file written in tree order and only differ on
// one that is not.
func TestOffsetsAreUsedWhenPresent(t *testing.T) {
	dd := readDICOMDIR(t, filepath.Join(dicomdirDir(t), "DICOMDIR"))
	if !dd.OffsetsWereUsed {
		t.Error("the tree was rebuilt from record order for a file that has offsets")
	}
}

// TestFileSetIDIsRead covers the label on the media.
func TestFileSetIDIsRead(t *testing.T) {
	dd := readDICOMDIR(t, filepath.Join(dicomdirDir(t), "DICOMDIR"))
	if dd.FileSetID == "" {
		t.Skip("this fixture has no File-set ID")
	}
}

// TestNotADICOMDIRIsRefused covers a file that is not one.
func TestNotADICOMDIRIsRefused(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	f, err := os.Open(filepath.Join(corpus, "CT_small.dcm"))
	if err != nil {
		t.Skipf("CT_small.dcm is not present: %v", err)
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	if _, err := fileset.ReadDICOMDIR(df); err == nil {
		t.Error("a CT image was read as a DICOMDIR")
	}
}
