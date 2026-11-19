package dcmstore_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/internal/heldvalue"
	"github.com/amrshadid/go-dicom/tag"
)

// withPersonNames holds every person name of ds as a dataelem.PersonName, the
// type the package's own decoders return for PN.
func withPersonNames(ds *dataset.Dataset) {
	for _, elem := range ds.GetAll() {
		if elem.GetVR() != dataelem.PN {
			continue
		}
		raw, ok := elem.Bytes()
		if !ok || strings.Contains(string(raw), `\`) {
			continue
		}
		groups := append(strings.SplitN(string(raw), "=", 3), "", "")
		elem.SetValue(dataelem.PersonName{Alphabetic: groups[0], Ideographic: groups[1], Phonetic: groups[2]})
	}
}

// TestAnInstanceHeldAsGoValuesIndexesAsTheFile proves that an instance built
// in memory with string, []string, number and PersonName values is indexed
// with the same keys as the same instance read from its file.
//
// elementString printed what it did not know with %v, so a Patient's Name
// held as a dataelem.PersonName was indexed as "{DOE^JANE  }" and no query by
// name found it (#420).
func TestAnInstanceHeldAsGoValuesIndexesAsTheFile(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	ctx := context.Background()
	// ASCII files: a Go string is text already and is not decoded by the
	// declared character set, by design, so a file in another repertoire is
	// not the same data set once its bytes are held as strings.
	for _, name := range []string{"CT_small.dcm", "test-SR.dcm"} {
		f, err := os.Open(filepath.Join(corpus, name))
		if err != nil {
			t.Skipf("%s is not present: %v", name, err)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
		_ = f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// A name with its phonetic group, as the file holds it. PersonName's
		// String gives the alphabetic group alone, so %v of a one-group name
		// happened to match the file; this one does not.
		file := df.GetDataset()
		file.Remove(tag.New(0x0010, 0x0010))
		_ = file.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("DOE^JANE==DOE^JANE ")))
		held := heldvalue.AsGoValues(file)
		withPersonNames(held)
		if elem, ok := held.Get(tag.New(0x0010, 0x0010)); !ok {
			t.Fatalf("%s has no Patient's Name", name)
		} else if _, isName := elem.GetValue().(dataelem.PersonName); !isName {
			t.Fatalf("%s: Patient's Name is held as %T, not a PersonName", name, elem.GetValue())
		}

		fromFile, err := dcmstore.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		want, err := fromFile.Store(ctx, file)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		inMemory, err := dcmstore.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		got, err := inMemory.Store(ctx, held)
		if err != nil {
			t.Fatalf("%s held as Go values: %v", name, err)
		}
		for key, w := range want.Keys {
			if g := got.Keys[key]; g != w {
				t.Errorf("%s %s: indexed %+v from the file, %+v held as Go values", name, key, w, g)
			}
		}
		if len(want.Keys) != len(got.Keys) || !reflect.DeepEqual(want.Sequences, got.Sequences) {
			t.Errorf("%s: %d keys and sequences %v from the file, %d keys and %v held as Go values",
				name, len(want.Keys), want.Sequences, len(got.Keys), got.Sequences)
		}
	}
}
