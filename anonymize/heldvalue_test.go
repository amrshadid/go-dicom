package anonymize_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/internal/heldvalue"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// everyProfile is each de-identification profile the package offers.
var everyProfile = map[string]anonymize.Profile{
	"Basic":                  anonymize.BasicProfile,
	"CleanDescriptors":       anonymize.CleanDescriptorsProfile,
	"CleanGraphics":          anonymize.CleanGraphicsProfile,
	"CleanStructuredContent": anonymize.CleanStructuredContentProfile,
	"RetainLongFullDates":    anonymize.RetainLongFullDatesProfile,
	"RetainPatientChars":     anonymize.RetainPatientCharsProfile,
	"RetainDeviceIdent":      anonymize.RetainDeviceIdentProfile,
	"RetainUIDs":             anonymize.RetainUIDsProfile,
	"RetainSafePrivate":      anonymize.RetainSafePrivateProfile,
}

// readDataset reads the data set of a DICOM file.
func readDataset(t *testing.T, path string) (*dataset.Dataset, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return nil, err
	}
	return df.GetDataset(), nil
}

// outcome lists, one line per element at any depth, what a data set holds
// after de-identification. Values are compared as the bytes they stand for,
// which is what a writer puts in the file; Patient ID, which the profile
// replaces with a random identifier, is recorded as replaced or not.
func outcome(ds *dataset.Dataset, original map[string][]byte, path string, lines *[]string) {
	for _, elem := range ds.GetAll() {
		tg, _ := elem.Tag()
		at := path + tg.String()
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			*lines = append(*lines, fmt.Sprintf("%s SQ %d items", at, seq.Length()))
			for i, item := range seq.Items() {
				if inner, isDS := item.(*dataset.Dataset); isDS {
					outcome(inner, original, fmt.Sprintf("%s[%d].", at, i), lines)
				}
			}
			continue
		}
		raw, ok := elem.Bytes()
		switch {
		case !ok:
			*lines = append(*lines, fmt.Sprintf("%s holds no bytes (%T)", at, elem.GetValue()))
		case tg == tag.New(0x0010, 0x0020):
			*lines = append(*lines, fmt.Sprintf("%s replaced=%v", at, !bytes.Equal(raw, original[at])))
		default:
			*lines = append(*lines, fmt.Sprintf("%s %q", at, raw))
		}
	}
}

// originalValues records the value bytes of every element, keyed as outcome
// keys them.
func originalValues(ds *dataset.Dataset, path string, into map[string][]byte) {
	for _, elem := range ds.GetAll() {
		tg, _ := elem.Tag()
		at := path + tg.String()
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			for i, item := range seq.Items() {
				if inner, isDS := item.(*dataset.Dataset); isDS {
					originalValues(inner, fmt.Sprintf("%s[%d].", at, i), into)
				}
			}
			continue
		}
		raw, _ := elem.Bytes()
		into[at] = raw
	}
}

// identifyingFile writes a file holding what the corpus has little of: a
// multi-valued UID the profile replaces, and a private attribute Table
// E.3.10-1 lists as safe, with its creator.
func identifyingFile(t *testing.T) string {
	t.Helper()
	const class, instance = "1.2.840.10008.5.1.4.1.1.7", "1.2.826.0.1.3680043.10.511.420.1"
	ds := dataset.NewDataset()
	add := func(tg tag.Tag, vr dataelem.VR, value string) {
		if len(value)%2 == 1 {
			value += " "
		}
		if err := ds.Add(dataelem.NewDataElement(tg, vr, []byte(value))); err != nil {
			t.Fatal(err)
		}
	}
	add(tag.New(0x0008, 0x0016), dataelem.UI, class)
	add(tag.New(0x0008, 0x0018), dataelem.UI, instance)
	// Failed SOP Instance UID List (0008,0058), U in Table E.1-1, VM 1-n.
	add(tag.New(0x0008, 0x0058), dataelem.UI,
		"1.2.826.0.1.3680043.10.511.420.2\\1.2.826.0.1.3680043.10.511.420.3")
	// An earlier pass that redacted text automatically says so beside its
	// method (0012,0063), LO VM 1-n; a later pass keeps the note.
	add(tag.New(0x0012, 0x0063), dataelem.LO,
		"Basic Application Confidentiality Profile\\"+anonymize.AutomaticRedactionNote)
	add(tag.New(0x0010, 0x0010), dataelem.PN, "DOE^JANE")
	add(tag.New(0x0010, 0x0020), dataelem.LO, "PID12345")
	// ELSCINT1 (00E1,xx21) is safe; (00E1,xx99) is not.
	add(tag.New(0x00E1, 0x0010), dataelem.LO, "ELSCINT1")
	add(tag.New(0x00E1, 0x1021), dataelem.DS, "1.5")
	add(tag.New(0x00E1, 0x1099), dataelem.LO, "DOE^JANE PID12345")

	path := filepath.Join(t.TempDir(), "identifying.dcm")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID: class, MediaStorageSOPInstanceUID: instance,
		TransferSyntaxUID: "1.2.840.10008.1.2.1",
	})
	if err := w.AddDataElements(filewriter.ElementsFromDataset(ds)); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestEveryProfileTreatsAStringHeldDataSetAsTheFile proves that each profile
// removes, empties and replaces the same attributes in a data set built in
// memory with string and []string values as in the same data set read from
// its file, over pydicom's corpus and a file with a multi-valued UID and a
// safe private attribute.
//
// The de-identifier read values with a []byte assertion or a switch that knew
// []byte and string. A UID list held as []string was taken for no UID and kept
// its original values, which link the result back to the source instances,
// and Retain Safe Private read a creator held as a string as no creator and
// dropped the safe attribute it owned.
func TestEveryProfileTreatsAStringHeldDataSetAsTheFile(t *testing.T) {
	paths := []string{identifyingFile(t)}
	if dir := os.Getenv("GODICOM_PYDICOM_DATA"); dir != "" {
		corpus, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
		for _, p := range corpus {
			// The large files add time and nothing a small one does not.
			if info, err := os.Stat(p); err == nil && info.Size() < 1<<20 {
				paths = append(paths, p)
			}
		}
	}

	names := make([]string, 0, len(everyProfile))
	for name := range everyProfile {
		names = append(names, name)
	}
	sort.Strings(names)

	compared := 0
	for _, path := range paths {
		if _, err := readDataset(t, path); err != nil {
			continue // a file this library refuses is not this test's subject
		}
		compared++
		for _, name := range names {
			fromFile, _ := readDataset(t, path)
			inMemory := heldvalue.AsGoValues(fromFile)
			original := map[string][]byte{}
			originalValues(fromFile, "", original)

			// One anonymizer for both, so a UID maps to the same replacement.
			a := anonymize.NewAnonymizer(everyProfile[name])
			if err := a.Anonymize(fromFile); err != nil {
				t.Fatalf("%s %s, from the file: %v", filepath.Base(path), name, err)
			}
			if err := a.Anonymize(inMemory); err != nil {
				t.Fatalf("%s %s, held as strings: %v", filepath.Base(path), name, err)
			}

			var want, got []string
			outcome(fromFile, original, "", &want)
			outcome(inMemory, original, "", &got)
			if diff := firstDifference(want, got); diff != "" {
				t.Errorf("%s, %s profile: the data set held as strings came out different from the file: %s",
					filepath.Base(path), name, diff)
			}
		}
	}
	if compared == 0 {
		t.Fatal("no file was compared")
	}
	t.Logf("%d files, %d profiles", compared, len(names))
}

// firstDifference describes where two outcomes first part, or "" when they
// are the same.
func firstDifference(want, got []string) string {
	for i := 0; i < len(want) || i < len(got); i++ {
		var w, g string
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return fmt.Sprintf("file has %q, memory has %q", w, g)
		}
	}
	return ""
}
