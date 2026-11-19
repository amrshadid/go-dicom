package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// There was no command to write or list a DICOMDIR, though the library does
// both (#348).

// runDICOMDIR runs the command in the test process and returns what it printed
// on each stream.
func runDICOMDIR(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := &DICOMDIRCommand{out: &out, errOut: &errOut}
	err = cmd.Execute(args)
	return out.String(), errOut.String(), err
}

// listDICOMDIR is the JSON listing of a DICOMDIR.
func listDICOMDIR(t *testing.T, path string) dicomdirDocument {
	t.Helper()
	out, _, err := runDICOMDIR(t, "--list", "--json", path)
	if err != nil {
		t.Fatal(err)
	}
	var doc dicomdirDocument
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	return doc
}

// instances returns the File IDs of the records of a listing that reference a
// file, in tree order.
func (d dicomdirDocument) instances() []string {
	var out []string
	var walk func(rs []dicomdirRecord)
	walk = func(rs []dicomdirRecord) {
		for _, r := range rs {
			if len(r.FileID) > 0 {
				out = append(out, strings.Join(r.FileID, "/"))
			}
			walk(r.Children)
		}
	}
	walk(d.Records)
	return out
}

// writeInstances writes one instance of each uid into dir, named as given.
func writeInstances(t *testing.T, dir string, names map[string]string) {
	t.Helper()
	for name, uid := range names {
		writePatientInstance(t, filepath.Join(dir, filepath.FromSlash(name)), uid)
	}
}

// A directory whose files are named as File IDs get a DICOMDIR at its root
// that lists each of them, with the hierarchy of PS3.3 F.5 and the Referenced
// File ID of each one.
func TestDICOMDIRIndexesADirectory(t *testing.T) {
	dir := t.TempDir()
	writeInstances(t, dir, map[string]string{
		"CT000001":      "1.2.826.0.1.3680043.10.511.9.200",
		"SERIES1/IM_2":  "1.2.826.0.1.3680043.10.511.9.201",
		"SERIES1/IM_10": "1.2.826.0.1.3680043.10.511.9.202",
	})
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("not DICOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := runDICOMDIR(t, "--file-set-id", "TESTSET", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1 patient, 1 study, 1 series, 3 image") {
		t.Errorf("the summary does not count the records: %q", out)
	}
	if !strings.Contains(errOut, "README (not a DICOM instance)") {
		t.Errorf("the file that was left out is not named: %q", errOut)
	}

	doc := listDICOMDIR(t, filepath.Join(dir, "DICOMDIR"))
	if doc.SchemaVersion != 1 || doc.FileSetID != "TESTSET" || !doc.OffsetsWereUsed || doc.Unreachable != 0 {
		t.Errorf("listing: %+v", doc)
	}
	if got := strings.Join(doc.instances(), " "); got != "CT000001 SERIES1/IM_10 SERIES1/IM_2" {
		t.Errorf("instances = %q, want the three files by path", got)
	}
	if doc.Counts["PATIENT"] != 1 || doc.Counts["STUDY"] != 1 || doc.Counts["SERIES"] != 1 || doc.Counts["IMAGE"] != 3 {
		t.Errorf("counts = %v", doc.Counts)
	}
	patient := doc.Records[0]
	found := map[string]string{}
	for _, a := range patient.Attributes {
		found[a.Keyword] = a.Value
	}
	if found["PatientName"] != "Doe^John" || found["PatientID"] != "MRN12345" {
		t.Errorf("the patient record's keys = %v", found)
	}

	text, _, err := runDICOMDIR(t, "--list", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"File-set ID: TESTSET", "PATIENT  PatientName=Doe^John  PatientID=MRN12345",
		"      IMAGE  SERIES1/IM_2  ", "1 patient, 1 study, 1 series, 3 image"} {
		if !strings.Contains(text, want) {
			t.Errorf("the text listing lacks %q:\n%s", want, text)
		}
	}
}

// A file whose name is not a File ID is refused by name, every such file at
// once and not the first alone, and nothing is written: a DICOMDIR of names
// a reader of the media cannot follow is wrong.
func TestDICOMDIRRefusesNamesThatAreNotFileIDs(t *testing.T) {
	dir := t.TempDir()
	writeInstances(t, dir, map[string]string{
		"GOOD":           "1.2.826.0.1.3680043.10.511.9.300",
		"ct.dcm":         "1.2.826.0.1.3680043.10.511.9.301",
		"SUB/IMG0001.DC": "1.2.826.0.1.3680043.10.511.9.302",
		"TOOLONGNAME":    "1.2.826.0.1.3680043.10.511.9.303",
	})
	_, _, err := runDICOMDIR(t, dir)
	if err == nil {
		t.Fatal("files that are not File IDs were indexed")
	}
	for _, name := range []string{"ct.dcm", "SUB/IMG0001.DC", "TOOLONGNAME", "--copy-to"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal does not name %s: %v", name, err)
		}
	}
	if strings.Contains(err.Error(), "\n  GOOD") {
		t.Errorf("the refusal blames a name that is fine: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "DICOMDIR")); statErr == nil {
		t.Error("a DICOMDIR was written though the command refused")
	}
}

// --copy-to indexes files whose names are not File IDs by copying them under
// names that are: the originals are as they were, the copies are DICOM/IM000001
// onward in the order of the source paths, and the DICOMDIR is beside them.
func TestDICOMDIRCopyToNamesTheCopiesAsFileIDs(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "media")
	writeInstances(t, src, map[string]string{
		"b.dcm":     "1.2.826.0.1.3680043.10.511.9.401",
		"a.dcm":     "1.2.826.0.1.3680043.10.511.9.400",
		"sub/c.dcm": "1.2.826.0.1.3680043.10.511.9.402",
	})
	before := hashTree(t, src)

	if _, _, err := runDICOMDIR(t, "--copy-to", dst, src); err != nil {
		t.Fatal(err)
	}
	if after := hashTree(t, src); !equalHashes(before, after) {
		t.Errorf("the originals changed: %v then %v", before, after)
	}

	doc := listDICOMDIR(t, dst)
	if got := strings.Join(doc.instances(), " "); got != "DICOM/IM000001 DICOM/IM000002 DICOM/IM000003" {
		t.Errorf("instances = %q", got)
	}
	// In the order of the source paths: a.dcm, b.dcm, sub/c.dcm.
	for i, original := range []string{"a.dcm", "b.dcm", "sub/c.dcm"} {
		copied, err := os.ReadFile(filepath.Join(dst, "DICOM", "IM00000"+string(rune('1'+i))))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(original)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(copied, want) {
			t.Errorf("IM00000%d is not a copy of %s", i+1, original)
		}
	}

	// A destination with something in it is not mixed into.
	if _, _, err := runDICOMDIR(t, "--copy-to", dst, src); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("copying into a directory that has files in it: %v", err)
	}
}

// hashTree is the SHA-256 of every file under dir, by relative path.
func hashTree(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[rel] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func equalHashes(a, b map[string][32]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// A DICOMDIR that is there is not replaced without --force, and is not itself
// indexed when it is: writing twice gives the same index.
func TestDICOMDIRReplacesOnlyWithForce(t *testing.T) {
	dir := t.TempDir()
	writeInstances(t, dir, map[string]string{"ONE": "1.2.826.0.1.3680043.10.511.9.500"})
	if _, _, err := runDICOMDIR(t, dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runDICOMDIR(t, dir); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("a second write: %v", err)
	}
	writeInstances(t, dir, map[string]string{"TWO": "1.2.826.0.1.3680043.10.511.9.501"})
	if _, _, err := runDICOMDIR(t, "--force", dir); err != nil {
		t.Fatal(err)
	}
	doc := listDICOMDIR(t, dir)
	if got := strings.Join(doc.instances(), " "); got != "ONE TWO" {
		t.Errorf("instances = %q, want the two files and not the DICOMDIR", got)
	}
}

// The command is for directories, and --list for DICOMDIRs: each says so when
// handed the other, and the options of one are not accepted by the other.
func TestDICOMDIRRejectsWhatItCannotDo(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "A")
	writePatientInstance(t, file, "1.2.826.0.1.3680043.10.511.9.600")
	for name, args := range map[string][]string{
		"a file to index":               {file},
		"no argument":                   {},
		"two arguments":                 {dir, dir},
		"a missing directory":           {filepath.Join(dir, "none")},
		"an empty directory":            {t.TempDir()},
		"--json without --list":         {"--json", dir},
		"--force with --list":           {"--list", "--force", dir},
		"a file that is not a DICOMDIR": {"--list", file},
		"a long File-set ID":            {"--file-set-id", "0123456789ABCDEFG", dir},
	} {
		if out, _, err := runDICOMDIR(t, args...); err == nil {
			t.Errorf("%s was accepted, printing %q", name, out)
		}
	}
}

// The DICOMDIRs of pydicom's corpus are listed with the records pydicom's
// FileSet finds in them (2 patients, 6 studies, 13 series, 31 images), whatever
// order the records are stored in and even where they have no offsets.
func TestDICOMDIRListsPydicomsDICOMDIRs(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for _, name := range []string{"DICOMDIR", "DICOMDIR-reordered", "DICOMDIR-nooffset", "DICOMDIR-implicit", "DICOMDIR-bigEnd"} {
		path := filepath.Join(corpus, "dicomdirtests", name)
		if _, err := os.Stat(path); err != nil {
			t.Skipf("%s is not present", name)
		}
		doc := listDICOMDIR(t, path)
		if doc.Counts["PATIENT"] != 2 || doc.Counts["STUDY"] != 6 || doc.Counts["SERIES"] != 13 || doc.Counts["IMAGE"] != 31 {
			t.Errorf("%s: counts %v", name, doc.Counts)
		}
		if n := len(doc.instances()); n != 31 || doc.Unreachable != 0 {
			t.Errorf("%s: %d instances in the tree, %d unreachable", name, n, doc.Unreachable)
		}
	}
	text, _, err := runDICOMDIR(t, "--list", filepath.Join(corpus, "dicomdirtests"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "File-set ID: PYDICOM_TEST") || !strings.Contains(text, "IMAGE  98892003/MR700/4648") {
		t.Errorf("the listing of a directory holding a DICOMDIR:\n%s", text)
	}
}

// Real files, copied from pydicom's corpus under File IDs: each is found in the
// DICOMDIR by the UIDs the files themselves hold, an instance that is not an
// image under its own record type, and a data set without the file meta
// information of Part 10 is left out by name where the library would index it.
func TestDICOMDIRIndexesCorpusFiles(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	dir := t.TempDir()
	for from, to := range map[string]string{
		"CT_small.dcm":     "CT_SMALL",
		"MR_small.dcm":     "MR_SMALL",
		"rtplan.dcm":       "RTPLAN",
		"waveform_ecg.dcm": "ECG",
		"rtstruct.dcm":     "RTSTRUCT", // a bare data set: no preamble
	} {
		data, err := os.ReadFile(filepath.Join(corpus, from))
		if err != nil {
			t.Skipf("%s is not present", from)
		}
		if err := os.WriteFile(filepath.Join(dir, to), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, errOut, err := runDICOMDIR(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "RTSTRUCT (a data set without the file meta information of PS3.10)") {
		t.Errorf("the bare data set is not named: %q", errOut)
	}
	doc := listDICOMDIR(t, dir)
	want := map[string]int{"PATIENT": 4, "STUDY": 4, "SERIES": 4, "IMAGE": 2, "RT PLAN": 1, "WAVEFORM": 1}
	for kind, n := range want {
		if doc.Counts[kind] != n {
			t.Errorf("%s records = %d, want %d (%v)", kind, doc.Counts[kind], n, doc.Counts)
		}
	}
	if got := strings.Join(doc.instances(), " "); got != "CT_SMALL ECG MR_SMALL RTPLAN" {
		t.Errorf("instances = %q", got)
	}
}
