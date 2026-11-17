package dcmstore_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// An instance arrives from the network as the bytes its sender wrote, in the
// character set it declares. The index kept those bytes, and a C-FIND answer
// carried them with no Specific Character Set, so a peer could not read a
// name outside ASCII: pynetdicom read none of seven such names correctly
// (#297). The tests below hold the three parts of the fix: the index is
// UTF-8, a query is read in its own character set, and an answer says what it
// is written in.

var (
	tagCharacterSet   = tag.New(0x0008, 0x0005)
	tagName           = tag.New(0x0010, 0x0010)
	tagStudyUID       = tag.New(0x0020, 0x000D)
	tagLevel          = tag.New(0x0008, 0x0052)
	tagRequestAttrs   = tag.New(0x0040, 0x0275)
	tagStepDescriptor = tag.New(0x0040, 0x0007)
)

// latin1Instance is an instance as a peer sends it: ISO_IR 100, the name
// "Buc^Jérôme" in that set's bytes, and a scheduled step described in it.
func latin1Instance() *dataset.Dataset {
	ds := instance{"P9", "", "1.2.9", "20240301", "1.2.9.1", "CT", "1.2.9.1.1", "1"}.dataset()
	_ = ds.Add(dataelem.NewDataElement(tagCharacterSet, dataelem.CS, []byte("ISO_IR 100")))
	_ = ds.Add(dataelem.NewDataElement(tagName, dataelem.PN, []byte("Buc^J\xe9r\xf4me")))

	// The item declares nothing of its own, so it is in its data set's
	// character set (PS3.3 C.12.1.1.2).
	item := dataset.NewDataset()
	_ = item.Add(dataelem.NewDataElement(tagStepDescriptor, dataelem.LO, []byte("Thorax apr\xe8s injection")))
	seq := sequence.New()
	_ = seq.Append(item)
	_ = ds.Add(dataelem.NewDataElement(tagRequestAttrs, dataelem.SQ, seq))
	return ds
}

// studyQuery asks at STUDY level for the name and the study, with any other
// elements given.
func studyQuery(extra ...*dataelem.DataElement) *dataset.Dataset {
	q := dataset.NewDataset()
	_ = q.Add(dataelem.NewDataElement(tagLevel, dataelem.CS, "STUDY"))
	_ = q.Add(dataelem.NewDataElement(tagName, dataelem.PN, ""))
	_ = q.Add(dataelem.NewDataElement(tagStudyUID, dataelem.UI, ""))
	for _, elem := range extra {
		_ = q.Add(elem)
	}
	return q
}

func textOf(t *testing.T, ds *dataset.Dataset, at tag.Tag) string {
	t.Helper()
	elem, ok := ds.Get(at)
	if !ok {
		return ""
	}
	switch v := elem.GetValue().(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	t.Fatalf("%s holds a %T", at, elem.GetValue())
	return ""
}

func storeLatin1(t *testing.T) *dcmstore.Store {
	t.Helper()
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Store(context.Background(), latin1Instance()); err != nil {
		t.Fatalf("Store: %v", err)
	}
	return store
}

// TestAnAnswerIsUTF8AndSaysSo checks that a name stored in ISO_IR 100 is
// answered in UTF-8 under ISO_IR 192, inside a sequence as well.
func TestAnAnswerIsUTF8AndSaysSo(t *testing.T) {
	store := storeLatin1(t)

	wanted := sequence.New()
	_ = wanted.Append(dataset.NewDataset())
	answers, err := store.Query(context.Background(),
		studyQuery(dataelem.NewDataElement(tagRequestAttrs, dataelem.SQ, wanted)))
	if err != nil || len(answers) != 1 {
		t.Fatalf("Query: %d answers, %v", len(answers), err)
	}
	answer := answers[0]

	if got := textOf(t, answer, tagName); got != "Buc^Jérôme" {
		t.Errorf("the name is %q, want %q", got, "Buc^Jérôme")
	}
	if got := textOf(t, answer, tagCharacterSet); got != "ISO_IR 192" {
		t.Errorf("Specific Character Set is %q, want ISO_IR 192", got)
	}

	seq, err := answer.GetSequence(tagRequestAttrs)
	if err != nil || seq.Length() != 1 {
		t.Fatalf("the answer's Request Attributes Sequence: %v", err)
	}
	item, _ := seq.Get(0)
	if got := textOf(t, item.(*dataset.Dataset), tagStepDescriptor); got != "Thorax après injection" {
		t.Errorf("the step description is %q", got)
	}
}

// TestAQueryIsReadInItsOwnCharacterSet checks that the same accented name
// matches whether the query writes it in ISO_IR 100 or in UTF-8, and that it
// is not matched by its bytes read in the wrong set.
func TestAQueryIsReadInItsOwnCharacterSet(t *testing.T) {
	store := storeLatin1(t)

	cases := []struct {
		name    string
		charset string
		value   []byte
		matches int
	}{
		{"ISO_IR 100", "ISO_IR 100", []byte("Buc^J\xe9r*"), 1},
		{"UTF-8", "ISO_IR 192", []byte("Buc^Jér*"), 1},
		{"no character set, plain ASCII", "", []byte("Buc^J*"), 1},
		{"another name", "ISO_IR 192", []byte("Buc^Jèr*"), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := dataset.NewDataset()
			_ = q.Add(dataelem.NewDataElement(tagLevel, dataelem.CS, []byte("STUDY")))
			if c.charset != "" {
				_ = q.Add(dataelem.NewDataElement(tagCharacterSet, dataelem.CS, []byte(c.charset)))
			}
			_ = q.Add(dataelem.NewDataElement(tagName, dataelem.PN, c.value))
			answers, err := store.Query(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			if len(answers) != c.matches {
				t.Errorf("%d answers, want %d", len(answers), c.matches)
			}
		})
	}
}

// TestAnASCIIAnswerDeclaresNoCharacterSet checks that an answer with nothing
// outside ASCII is as it was: no Specific Character Set, even when the query
// declared one. The default repertoire needs no declaration, and an empty
// element would say there is one to read.
func TestAnASCIIAnswerDeclaresNoCharacterSet(t *testing.T) {
	store := openWithCorpus(t)

	answers, err := store.Query(context.Background(),
		studyQuery(dataelem.NewDataElement(tagCharacterSet, dataelem.CS, "ISO_IR 192")))
	if err != nil || len(answers) == 0 {
		t.Fatalf("Query: %d answers, %v", len(answers), err)
	}
	for _, answer := range answers {
		if answer.Contains(tagCharacterSet) {
			t.Errorf("the answer for %s carries Specific Character Set %q",
				textOf(t, answer, tagStudyUID), textOf(t, answer, tagCharacterSet))
		}
	}
}

// TestRebuildAndArrivalIndexAlike checks that an instance indexed as it
// arrived and the same instance indexed from its file hold the same text.
// They did not: the file reader decodes, and arrival kept the bytes.
func TestRebuildAndArrivalIndexAlike(t *testing.T) {
	store := storeLatin1(t)
	arrived, _ := store.Instance("1.2.9.1.1")
	before := arrived.Keys[tagName.String()].Value

	if err := store.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	rebuilt, _ := store.Instance("1.2.9.1.1")
	if after := rebuilt.Keys[tagName.String()].Value; after != before || after != "Buc^Jérôme" {
		t.Errorf("indexed on arrival %q, from the file %q", before, after)
	}
}

// TestAnIndexFromBeforeIsRebuilt checks that an index file in the form
// written before this, which may hold text in any character set, is not
// trusted: the store reads the files again.
func TestAnIndexFromBeforeIsRebuilt(t *testing.T) {
	store := storeLatin1(t)
	dir := store.Root()
	inst, _ := store.Instance("1.2.9.1.1")

	// The old form: the instances alone, by SOP Instance UID.
	old := `{"1.2.9.1.1": {"sopClassUID": "1.2.840.10008.5.1.4.1.1.2", "sopInstanceUID": "1.2.9.1.1", ` +
		`"path": "` + inst.Path + `", "keys": {"(0010,0010)": {"vr": "PN", "value": "STALE"}, ` +
		`"(0020,000D)": {"vr": "UI", "value": "1.2.9"}}}}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := dcmstore.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	again, ok := reopened.Instance("1.2.9.1.1")
	if !ok {
		t.Fatal("the instance is gone after reopening")
	}
	if got := again.Keys[tagName.String()].Value; got != "Buc^Jérôme" {
		t.Errorf("the name is %q: the old index was kept", got)
	}

	// And the index it leaves behind is one the next start reads as it is.
	data, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version"`) {
		t.Error("the index written after the rebuild carries no version")
	}
}
