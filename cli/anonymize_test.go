package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// The anonymize package had no command, so de-identifying a file meant writing
// a program (#232).

const (
	testStudyUID  = "1.2.826.0.1.3680043.10.511.9.100"
	testSeriesUID = "1.2.826.0.1.3680043.10.511.9.101"
)

var (
	tagSOPClassUID    = tag.New(0x0008, 0x0016)
	tagSOPInstanceUID = tag.New(0x0008, 0x0018)
	tagStudyUID       = tag.New(0x0020, 0x000D)
	tagPatientName    = tag.New(0x0010, 0x0010)
	tagIdentityGone   = tag.New(0x0012, 0x0062)
)

// writePatientInstance writes a small CT instance of the test study.
func writePatientInstance(t *testing.T, path, instanceUID string) {
	t.Helper()
	ds := dataset.NewDataset()
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value string
	}{
		{tagSOPClassUID, dataelem.UI, "1.2.840.10008.5.1.4.1.1.2"},
		{tagSOPInstanceUID, dataelem.UI, instanceUID},
		{tagPatientName, dataelem.PN, "Doe^John"},
		{tag.New(0x0010, 0x0020), dataelem.LO, "MRN12345"},
		{tagStudyUID, dataelem.UI, testStudyUID},
		{tag.New(0x0020, 0x000E), dataelem.UI, testSeriesUID},
	} {
		if err := ds.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.2", instanceUID, ds); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// readAnonymized reads a file back as its meta and data set.
func readAnonymized(t *testing.T, path string) (*filereader.FileMetaInfo, *dataset.Dataset) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("the output was not written: %v", err)
	}
	defer f.Close()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return df.FileMetaInfo, df.GetDataset()
}

func text(ds *dataset.Dataset, t tag.Tag) string {
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	raw, _ := elem.GetValue().([]byte)
	return strings.TrimRight(string(raw), " \x00")
}

func runAnonymize(args ...string) error {
	return (&AnonymizeCommand{}).Execute(args)
}

// TestAnonymizeFile: the identity is gone, the data set says so, and the file
// meta carries the same new UID as the data set, not the original.
func TestAnonymizeFile(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.dcm"), filepath.Join(dir, "out.dcm")
	writePatientInstance(t, in, "1.2.826.0.1.3680043.10.511.9.1")

	if err := runAnonymize(in, out); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	meta, ds := readAnonymized(t, out)

	if name := text(ds, tagPatientName); name != "" {
		t.Errorf("Patient's Name is still %q", name)
	}
	if got := text(ds, tagIdentityGone); got != "YES" {
		t.Errorf("Patient Identity Removed is %q, want YES", got)
	}
	instance := text(ds, tagSOPInstanceUID)
	if instance == "" || instance == "1.2.826.0.1.3680043.10.511.9.1" {
		t.Errorf("SOP Instance UID is %q, want a new one", instance)
	}
	if meta.MediaStorageSOPInstanceUID != instance {
		t.Errorf("the meta's instance UID is %q and the data set's %q", meta.MediaStorageSOPInstanceUID, instance)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, phi := range []string{"Doe^John", "MRN12345", "1.2.826.0.1.3680043.10.511.9.1"} {
		if strings.Contains(string(data), phi) {
			t.Errorf("%q is still in the output file", phi)
		}
	}
	// The input is read, never written.
	if _, source := readAnonymized(t, in); text(source, tagPatientName) != "Doe^John" {
		t.Error("the input file was changed")
	}
}

// TestAnonymizeDirectoryKeepsAStudyTogether: one Anonymizer serves the whole
// run, so two instances of a study get the same new Study Instance UID and
// stay one study. The layout under the output is the input's.
func TestAnonymizeDirectoryKeepsAStudyTogether(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	writePatientInstance(t, filepath.Join(in, "a.dcm"), "1.2.826.0.1.3680043.10.511.9.1")
	writePatientInstance(t, filepath.Join(in, "series2", "b.dcm"), "1.2.826.0.1.3680043.10.511.9.2")

	if err := runAnonymize(in, out); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	_, a := readAnonymized(t, filepath.Join(out, "a.dcm"))
	_, b := readAnonymized(t, filepath.Join(out, "series2", "b.dcm"))

	studyA, studyB := text(a, tagStudyUID), text(b, tagStudyUID)
	if studyA == "" || studyA == testStudyUID || studyA != studyB {
		t.Errorf("study UIDs %q and %q: want one new UID for both", studyA, studyB)
	}
	if text(a, tagSOPInstanceUID) == text(b, tagSOPInstanceUID) {
		t.Error("two instances were given the same SOP Instance UID")
	}
}

// TestAnonymizeProfile: -profile selects the option, and a name that is not
// one is refused rather than read as the Basic Profile.
func TestAnonymizeProfile(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.dcm"), filepath.Join(dir, "out.dcm")
	writePatientInstance(t, in, "1.2.826.0.1.3680043.10.511.9.1")

	if err := runAnonymize("-profile", "retain-uids", in, out); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	_, ds := readAnonymized(t, out)
	if got := text(ds, tagStudyUID); got != testStudyUID {
		t.Errorf("retain-uids changed the Study Instance UID to %q", got)
	}
	if name := text(ds, tagPatientName); name != "" {
		t.Errorf("retain-uids kept Patient's Name %q", name)
	}

	err := runAnonymize("-profile", "everything", in, filepath.Join(dir, "other.dcm"))
	if err == nil || !strings.Contains(err.Error(), "retain-safe-private") {
		t.Errorf("an unknown profile gave %v, want an error listing the profiles", err)
	}
	for name := range anonymizeProfiles {
		if err := runAnonymize("-profile", name, in, filepath.Join(dir, name+".dcm")); err != nil {
			t.Errorf("-profile %s: %v", name, err)
		}
	}
}

// TestAnonymizeUIDMap: the mapping is the link back to the patient, so it is
// written only when asked for.
func TestAnonymizeUIDMap(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.dcm"), filepath.Join(dir, "out.dcm")
	mapFile := filepath.Join(dir, "uids.json")
	writePatientInstance(t, in, "1.2.826.0.1.3680043.10.511.9.1")

	if err := runAnonymize(in, out); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("%d files after a run without -uid-map, want the input and the output", len(entries))
	}

	if err := runAnonymize("-uid-map", mapFile, in, out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(mapFile)
	if err != nil {
		t.Fatalf("the UID map was not written: %v", err)
	}
	var mapping map[string]string
	if err := json.Unmarshal(raw, &mapping); err != nil {
		t.Fatalf("the UID map is not JSON: %v", err)
	}
	_, ds := readAnonymized(t, out)
	if mapping[testStudyUID] != text(ds, tagStudyUID) {
		t.Errorf("the map gives %q for the study and the file has %q", mapping[testStudyUID], text(ds, tagStudyUID))
	}
}

// TestAnonymizeSkipsWhatIsNotDICOM: a directory holds other things too. They
// are left out of the output, where a copy would carry whatever they hold. A
// single input that is not DICOM is an error, since nothing was done.
func TestAnonymizeSkipsWhatIsNotDICOM(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	writePatientInstance(t, filepath.Join(in, "a.dcm"), "1.2.826.0.1.3680043.10.511.9.1")
	notes := filepath.Join(in, "notes.txt")
	if err := os.WriteFile(notes, []byte("Doe^John, seen Tuesday"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runAnonymize(in, out); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "a.dcm")); err != nil {
		t.Errorf("the DICOM file was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "notes.txt")); err == nil {
		t.Error("a file that is not DICOM was copied to the output")
	}

	if err := runAnonymize(notes, filepath.Join(dir, "notes.dcm")); err == nil {
		t.Error("a text file was accepted as the input")
	}
}

// TestAnonymizeRefusesToWriteOverItsInput: the output replacing the input
// loses the original, and an output directory inside the input is walked as
// input.
func TestAnonymizeRefusesToWriteOverItsInput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	file := filepath.Join(in, "a.dcm")
	writePatientInstance(t, file, "1.2.826.0.1.3680043.10.511.9.1")

	for name, args := range map[string][]string{
		"file onto itself":       {file, file},
		"directory onto itself":  {in, in},
		"output inside input":    {in, filepath.Join(in, "out")},
		"missing input":          {filepath.Join(dir, "nothing"), filepath.Join(dir, "out")},
		"one argument":           {in},
		"file into its own home": {file, in},
	} {
		if err := runAnonymize(args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, ds := readAnonymized(t, file); text(ds, tagPatientName) != "Doe^John" {
		t.Error("the input was changed by a refused run")
	}
}

// TestAnonymizeKeepsCompressedPixelData: an encapsulated instance keeps its
// transfer syntax, which is the only record of the codec its fragments are in.
func TestAnonymizeKeepsCompressedPixelData(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	in := filepath.Join(corpus, "JPEG2000.dcm")
	if _, err := os.Stat(in); err != nil {
		t.Skipf("JPEG2000.dcm is not present: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.dcm")
	if err := runAnonymize(in, out); err != nil {
		t.Fatalf("anonymize: %v", err)
	}

	sourceMeta, source := readAnonymized(t, in)
	meta, ds := readAnonymized(t, out)
	if meta.TransferSyntaxUID != sourceMeta.TransferSyntaxUID {
		t.Errorf("transfer syntax %q, want the source's %q", meta.TransferSyntaxUID, sourceMeta.TransferSyntaxUID)
	}
	want, err := source.PixelArray()
	if err != nil {
		t.Fatalf("decoding the source: %v", err)
	}
	got, err := ds.PixelArray()
	if err != nil {
		t.Fatalf("the anonymized pixel data does not decode: %v", err)
	}
	if !equalPixels(want, got) {
		t.Error("the pixels changed")
	}
}

func equalPixels(a, b interface{}) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return len(x) > 2 && string(x) == string(y)
}
