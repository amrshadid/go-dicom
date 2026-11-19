package dataset_test

import (
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

// valueAnswers is what every accessor that returns an element's bytes says about
// one element of one data set.
type valueAnswers struct {
	getValue, byKeyword, stringByKeyword, withDefault, stringWithDefault string
}

func answersFor(ds *dataset.Dataset, keyword string, t tag.Tag) valueAnswers {
	return valueAnswers{
		getValue:          trimPad(ds.GetValue(t)),
		byKeyword:         trimPad(ds.GetValueByKeyword(keyword)),
		stringByKeyword:   ds.GetStringByKeyword(keyword),
		withDefault:       trimPad(ds.GetByKeywordWithDefault(keyword, []byte("DEFAULT"))),
		stringWithDefault: ds.GetStringByKeywordWithDefault(keyword, "DEFAULT"),
	}
}

// TestElementValueReadsTheSameHoweverItIsHeld proves that the accessors answer
// the same for a value held as a Go string, as bytes, read from a corpus file
// and decoded from DICOM JSON. Before, a string-held value (which is what
// de-identification records Patient Identity Removed as) read as absent from
// GetValue, GetValueByKeyword, GetStringByKeyword and the two default forms.
func TestElementValueReadsTheSameHoweverItIsHeld(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	path := filepath.Join(dir, "CT_small.dcm")
	if _, err := os.Stat(path); err != nil {
		t.Skip("CT_small.dcm not present in the corpus")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}

	const keyword = "PatientName"
	patientName := tag.New(0x0010, 0x0010)
	const want = "CompressedSamples^CT1"

	corpus := df.GetDataset()
	if got := corpus.GetStringByKeyword(keyword); got != want {
		t.Fatalf("corpus file: PatientName = %q, want %q", got, want)
	}

	asBytes := dataset.NewDataset()
	_ = asBytes.Add(dataelem.NewDataElement(patientName, dataelem.PN, []byte(want)))
	asString := dataset.NewDataset()
	_ = asString.Add(dataelem.NewDataElement(patientName, dataelem.PN, want))
	asStrings := dataset.NewDataset()
	_ = asStrings.Add(dataelem.NewDataElement(patientName, dataelem.PN, []string{want}))

	fromJSON := dataset.NewDataset()
	if err := fromJSON.FromDICOMJSONString(`{"00100010":{"vr":"PN","Value":[{"Alphabetic":"` + want + `"}]}}`); err != nil {
		t.Fatalf("FromDICOMJSONString: %v", err)
	}

	reference := answersFor(asBytes, keyword, patientName)
	if reference.stringByKeyword != want || reference.getValue != want {
		t.Fatalf("the bytes-held reference reads %+v", reference)
	}
	for name, ds := range map[string]*dataset.Dataset{
		"corpus file": corpus, "string": asString, "[]string": asStrings, "DICOM JSON": fromJSON,
	} {
		got := answersFor(ds, keyword, patientName)
		if got != reference {
			t.Errorf("%s: accessors read %+v, want %+v", name, got, reference)
		}
	}
}

// TestStringHeldPrivateValuesAreFound proves a private creator and a private
// value held as strings are found by the private accessors, which share the
// helper with the keyword accessors.
func TestStringHeldPrivateValuesAreFound(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0009, 0x0010), dataelem.LO, "ACME_PRIVATE"))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0009, 0x1001), dataelem.LO, "payload"))

	if !ds.HasPrivateCreator(0x0009, "ACME_PRIVATE") {
		t.Error("a creator held as a string was not found")
	}
	if got := ds.GetAllPrivateCreators()[0x0009]; len(got) != 1 || got[0] != "ACME_PRIVATE" {
		t.Errorf("GetAllPrivateCreators = %v, want [ACME_PRIVATE]", got)
	}
	got, err := ds.GetPrivateValue(0x0009, 0x01, "ACME_PRIVATE")
	if err != nil || string(got) != "payload" {
		t.Errorf("GetPrivateValue = %q, %v; want \"payload\"", got, err)
	}
}

// TestPatientIdentityRemovedHeldAsStringReadsBack is the case of the report:
// the attribute the library itself records as a string.
func TestPatientIdentityRemovedHeldAsStringReadsBack(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0012, 0x0062), dataelem.CS, "YES"))
	if !ds.ContainsByKeyword("PatientIdentityRemoved") {
		t.Fatal("the element is not there")
	}
	if got := ds.GetStringByKeyword("PatientIdentityRemoved"); got != "YES" {
		t.Errorf("GetStringByKeyword = %q, want \"YES\"", got)
	}
}

// trimPad drops the padding a file adds to make a value an even length, which
// is the one difference between a stored value and a built one.
func trimPad(b []byte) string { return strings.TrimRight(string(b), " \x00") }
