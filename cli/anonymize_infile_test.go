package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// De-identifying a file with its pixels left in it writes the same file, byte
// for byte, as reading it whole, for every file of pydicom's corpus under
// every profile. That is what makes leaving them there safe: no attribute
// survives that did not survive before, and none is lost.
//
// It is run with nothing but Pixel Data left in the file (the binary values
// over a megabyte, which the command also leaves, are none in the corpus), and
// with every binary value left there, the most any read leaves.
//
// Patient ID is the one exception, and it is checked to be the only one: it
// becomes a random identifier of twelve characters, so it differs between any
// two runs. Every byte that differs must lie inside a Patient ID's value.
func TestAnonymizeLeavingValuesInTheFileWritesTheSameFile(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	entries, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	profiles := make([]string, 0, len(anonymizeProfiles))
	for name := range anonymizeProfiles {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)

	ways := map[string]anonymizeRead{
		"pixels left in the file":       {threshold: leftInFileAbove},
		"every binary value left there": {threshold: 0},
	}
	dir := t.TempDir()
	written, refused, inFile := 0, 0, 0
	for _, profile := range profiles {
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			input := filepath.Join(corpus, entry.Name())
			// One anonymizer for the runs of a file, as for the files of one
			// run of the command: the UIDs it gives are then the same.
			anonymizer := anonymize.NewAnonymizer(anonymizeProfiles[profile])
			ac := &AnonymizeCommand{allowBurnedIn: true}

			wholePath := filepath.Join(dir, "whole.dcm")
			_ = os.Remove(wholePath)
			wholeErr := ac.anonymizeFileAs(anonymizer, input, wholePath, anonymizeRead{whole: true})
			want, _ := os.ReadFile(wholePath)

			for way, read := range ways {
				path := filepath.Join(dir, "infile.dcm")
				_ = os.Remove(path)
				err := ac.anonymizeFileAs(anonymizer, input, path, read)
				if (err == nil) != (wholeErr == nil) || err != nil && err.Error() != wholeErr.Error() {
					t.Errorf("%s, %s, %s: %v; read whole: %v", profile, entry.Name(), way, err, wholeErr)
					continue
				}
				if err != nil {
					refused++
					continue
				}
				got, _ := os.ReadFile(path)
				if where := differenceOutsidePatientID(t, want, got); where >= 0 {
					t.Errorf("%s, %s, %s: %d bytes, %d read whole; they differ at %d, which is not in a Patient ID",
						profile, entry.Name(), way, len(got), len(want), where)
				}
				written++
				if way == "pixels left in the file" && hasPixelData(input) {
					inFile++
				}
			}
		}
	}
	if written == 0 || inFile == 0 {
		t.Fatalf("%d files written, %d of them with pixels left in the source", written, inFile)
	}
	t.Logf("%d profiles: %d outputs compared (%d with their pixels left in the source), %d refused alike",
		len(profiles), written, inFile, refused)
}

// differenceOutsidePatientID is the first offset at which got differs from
// want other than inside the value of a Patient ID (0010,0020) of want, at
// any depth, or -1 when there is none.
func differenceOutsidePatientID(t *testing.T, want, got []byte) int {
	t.Helper()
	if len(want) != len(got) {
		return min(len(want), len(got))
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(want)))
	if err != nil {
		t.Fatalf("the output read whole does not read back: %v", err)
	}
	var ids [][2]int64
	var walk func([]*filereader.DataElementValue)
	walk = func(elements []*filereader.DataElementValue) {
		for _, e := range elements {
			if e.Tag == tag.New(0x0010, 0x0020) {
				ids = append(ids, [2]int64{e.Position.ValueOffset, e.Position.ValueOffset + int64(e.Length)})
			}
			for _, item := range e.Items {
				walk(item.Elements)
			}
		}
	}
	walk(df.DataElements)

	for i := range want {
		if want[i] == got[i] {
			continue
		}
		inside := false
		for _, id := range ids {
			if int64(i) >= id[0] && int64(i) < id[1] {
				inside = true
			}
		}
		if !inside {
			return i
		}
	}
	return -1
}

// hasPixelData reports whether a file has Pixel Data at its top level.
func hasPixelData(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f), filereader.ReadOptions{SkipBulkData: true})
	if err != nil {
		return false
	}
	for _, e := range df.DataElements {
		if e.Tag == tag.New(0x7FE0, 0x0010) {
			return true
		}
	}
	return false
}

// A deflated file is read with its Pixel Data left in it, like any other: the
// data set holds a reference where the pixels are, and the source the writer
// copies them from is not the file, whose bytes are compressed, but the
// inflated data set the reference counts. The comparison above holds its
// output to the whole read's, image_dfl.dcm included; this holds that it was
// not read whole to get there.
func TestAnonymizeReadsADeflatedFileInPlace(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	in, err := os.Open(filepath.Join(corpus, "image_dfl.dcm"))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	df, ds, source, err := readForAnonymizing(in, anonymizeRead{threshold: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !df.Deflated {
		t.Fatal("image_dfl.dcm is not deflated")
	}
	if source == nil {
		t.Fatal("the deflated file was read whole")
	}
	elem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("no Pixel Data")
	}
	ref, inFile := elem.GetValue().(dataelem.ValueInFile)
	if !inFile {
		t.Fatalf("Pixel Data is %T, want a reference to where it is", elem.GetValue())
	}
	whole, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(mustRead(t, in.Name()))))
	if err != nil {
		t.Fatal(err)
	}
	want := whole.PixelDataElement().Value
	got := make([]byte, ref.Length)
	if _, err := source.ReadAt(got, ref.Offset); err != nil || !bytes.Equal(got, want) {
		t.Errorf("the source gave %d bytes at %d (err %v), not the %d bytes of the pixels", len(got), ref.Offset, err, len(want))
	}
}

// mustRead reads a file whole.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
