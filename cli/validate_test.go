package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// brokenCT writes CT_small.dcm with the backlog's eight changes: seven
// attributes deleted and Bits Stored set to 99.
func brokenCT(t *testing.T) string {
	t.Helper()
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	f, err := os.Open(filepath.Join(corpus, "CT_small.dcm"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatal(err)
	}
	ds := df.GetDataset()
	for _, gone := range []tag.Tag{
		tag.New(0x0008, 0x0060), tag.New(0x0010, 0x0020), tag.New(0x0020, 0x000D), tag.New(0x0020, 0x000E),
		tag.New(0x0020, 0x0032), tag.New(0x0028, 0x0030), tag.New(0x0028, 0x1053),
	} {
		ds.Remove(gone)
	}
	if err := ds.SetValue(tag.New(0x0028, 0x0101), []byte{99, 0}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "broken_ct.dcm")
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.2", datasetText(ds, tag.New(0x0008, 0x0018)), ds); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestValidateReportsEachDefect: the text names each of the eight changes
// with its tag and rule, counts them, and the run ends with exit status 3,
// for errors. The file unbroken ends with none.
func TestValidateReportsEachDefect(t *testing.T) {
	path := brokenCT(t)
	out, err := captured(t, func() error { return (&ValidateCommand{}).Execute([]string{path}) })
	var status *ExitStatusError
	if !errors.As(err, &status) || status.ExitCode() != validateErrors {
		t.Fatalf("the run ended with %v; want exit status %d", err, validateErrors)
	}
	for _, want := range []string{
		"checked as the CT Image IOD (PS3.3 A.3",
		"error       (0008,0060) Modality: Type 1: absent",
		"error       (0010,0020) Patient ID: Type 2: absent",
		"error       (0020,000D) Study Instance UID: Type 1: absent",
		"error       (0020,000E) Series Instance UID: Type 1: absent",
		"error       (0020,0032) Image Position (Patient): Type 1: absent",
		"error       (0028,0030) Pixel Spacing: Type 1: absent",
		`error       (0028,0101) Bits Stored: Enumerated Values: the value is "99"; the Enumerated Values are 12, 13, 14, 15, 16`,
		"error       (0028,1053) Rescale Slope: Type 1: absent",
		"not checked Synchronization: Usage C",
		"8 error(s), 0 warning(s),",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}

	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	out, err = captured(t, func() error {
		return (&ValidateCommand{}).Execute([]string{filepath.Join(corpus, "CT_small.dcm")})
	})
	if err != nil || !strings.Contains(out, "0 error(s), 0 warning(s),") {
		t.Errorf("CT_small.dcm: %v\n%s", err, out)
	}
}

// TestValidateJSON: --json is one object with the file, the IOD, the edition,
// every finding with its severity by name, and the counts.
func TestValidateJSON(t *testing.T) {
	path := brokenCT(t)
	out, _ := captured(t, func() error { return (&ValidateCommand{}).Execute([]string{"--json", path}) })
	var got struct {
		File     string `json:"file"`
		IOD      string `json:"iod"`
		Edition  string `json:"edition"`
		Findings []struct {
			Severity, Tag, Path, Module, Rule, Reference, Message string
		} `json:"findings"`
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.File != path || got.IOD != "CT Image" || got.Edition == "" || got.Counts["error"] != 8 {
		t.Errorf("file %q, IOD %q, edition %q, counts %v", got.File, got.IOD, got.Edition, got.Counts)
	}
	found := false
	for _, f := range got.Findings {
		if f.Tag == "(0008,0060)" && f.Severity == "error" && f.Module == "General Series" && f.Rule == "Type 1" &&
			strings.HasPrefix(f.Reference, "PS3.3 C.7.3.1") {
			found = true
		}
	}
	if !found {
		t.Errorf("Modality's finding is not there as expected:\n%s", out)
	}
}

// TestValidateRefusesWhatItCannotCheck: a file that is not DICOM, or one of
// no known SOP class, is an error of the run (exit status 1), not a pass.
func TestValidateRefusesWhatItCannotCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "text.dcm")
	if err := os.WriteFile(path, []byte("not a DICOM file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := captured(t, func() error { return (&ValidateCommand{}).Execute([]string{path}) })
	var status *ExitStatusError
	if err == nil || errors.As(err, &status) {
		t.Errorf("a file that is not DICOM gave %v", err)
	}
}
