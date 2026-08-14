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
	"github.com/amrshadid/go-dicom/tag"
)

// instance builds a data set with the attributes a store indexes.
type instance struct {
	patientID, patientName string
	studyUID, studyDate    string
	seriesUID, modality    string
	sopInstanceUID         string
	instanceNumber         string
}

func (i instance) dataset() *dataset.Dataset {
	ds := dataset.NewDataset()
	add := func(t tag.Tag, vr dataelem.VR, value string) {
		if value != "" {
			_ = ds.Add(dataelem.NewDataElement(t, vr, value))
		}
	}
	add(tag.New(0x0008, 0x0016), dataelem.UI, "1.2.840.10008.5.1.4.1.1.2")
	add(tag.New(0x0008, 0x0018), dataelem.UI, i.sopInstanceUID)
	add(tag.New(0x0010, 0x0010), dataelem.PN, i.patientName)
	add(tag.New(0x0010, 0x0020), dataelem.LO, i.patientID)
	add(tag.New(0x0020, 0x000D), dataelem.UI, i.studyUID)
	add(tag.New(0x0008, 0x0020), dataelem.DA, i.studyDate)
	add(tag.New(0x0020, 0x000E), dataelem.UI, i.seriesUID)
	add(tag.New(0x0008, 0x0060), dataelem.CS, i.modality)
	add(tag.New(0x0020, 0x0013), dataelem.IS, i.instanceNumber)
	return ds
}

// corpus is two patients: one with a two-series CT study, one with an MR study.
var corpus = []instance{
	{"P1", "SMITH^JOHN", "1.2.1", "20240115", "1.2.1.1", "CT", "1.2.1.1.1", "1"},
	{"P1", "SMITH^JOHN", "1.2.1", "20240115", "1.2.1.1", "CT", "1.2.1.1.2", "2"},
	{"P1", "SMITH^JOHN", "1.2.1", "20240115", "1.2.1.2", "CT", "1.2.1.2.1", "1"},
	{"P2", "JONES^JANE", "1.2.2", "20240220", "1.2.2.1", "MR", "1.2.2.1.1", "1"},
}

func openWithCorpus(t *testing.T) *dcmstore.Store {
	t.Helper()

	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, i := range corpus {
		if _, err := store.Store(context.Background(), i.dataset()); err != nil {
			t.Fatalf("Store(%s): %v", i.sopInstanceUID, err)
		}
	}
	return store
}

// query builds a C-FIND identifier.
func query(level string, keys map[tag.Tag]string) *dataset.Dataset {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, level))
	for t, value := range keys {
		vr := dataelem.LO
		switch t {
		case tag.New(0x0010, 0x0010), tag.New(0x0008, 0x0090):
			vr = dataelem.PN
		case tag.New(0x0020, 0x000D), tag.New(0x0020, 0x000E), tag.New(0x0008, 0x0018),
			tag.New(0x0008, 0x0016):
			vr = dataelem.UI
		case tag.New(0x0008, 0x0020), tag.New(0x0010, 0x0030):
			vr = dataelem.DA
		case tag.New(0x0008, 0x0060), tag.New(0x0008, 0x0061):
			vr = dataelem.CS
		case tag.New(0x0020, 0x0013), tag.New(0x0020, 0x1206),
			tag.New(0x0020, 0x1208), tag.New(0x0020, 0x1209):
			vr = dataelem.IS
		}
		_ = ds.Add(dataelem.NewDataElement(t, vr, value))
	}
	return ds
}

func TestStoreWritesAndIndexes(t *testing.T) {
	store := openWithCorpus(t)

	if got := store.Count(); got != len(corpus) {
		t.Errorf("the store holds %d instances, want %d", got, len(corpus))
	}

	inst, ok := store.Instance("1.2.1.1.1")
	if !ok {
		t.Fatal("the stored instance is not in the index")
	}

	// The tree mirrors the hierarchy, so a study is one directory.
	if !strings.Contains(filepath.ToSlash(inst.Path), "1.2.1/1.2.1.1/") {
		t.Errorf("the instance is filed at %q, not under its study and series", inst.Path)
	}

	// And the file is really there.
	if _, err := os.Stat(filepath.Join(store.Root(), filepath.FromSlash(inst.Path))); err != nil {
		t.Errorf("the indexed file is not on disk: %v", err)
	}
}

func TestStoreRefusesADataSetWithoutTheRequiredUIDs(t *testing.T) {
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	cases := map[string]*dataset.Dataset{
		"no SOP Class UID":    instance{sopInstanceUID: "1.2.3"}.dataset(),
		"no SOP Instance UID": func() *dataset.Dataset { return instance{patientID: "P"}.dataset() }(),
		"nil":                 nil,
	}
	// The first case still has the SOP Class from the builder, so remove it.
	cases["no SOP Class UID"].Remove(tag.New(0x0008, 0x0016))

	for name, ds := range cases {
		if _, err := store.Store(context.Background(), ds); err == nil {
			t.Errorf("Store accepted a data set with %s", name)
		}
	}
}

// A UID naming a file has to be validated, or a peer chooses where the file
// goes. This is the store's own check on top of fileutil's.
func TestStoreRefusesUIDsThatWouldEscapeTheStore(t *testing.T) {
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	for _, hostile := range []string{
		"../../etc/cron.d/pwn",
		"../escape",
		"1.2.3/../../..",
	} {
		ds := instance{sopInstanceUID: hostile, studyUID: "1.2.1", seriesUID: "1.2.1.1"}.dataset()
		if _, err := store.Store(context.Background(), ds); err == nil {
			t.Errorf("Store accepted the SOP Instance UID %q", hostile)
		}

		// And the same for the UIDs that become directories.
		ds = instance{sopInstanceUID: "1.2.3.4", studyUID: hostile}.dataset()
		if _, err := store.Store(context.Background(), ds); err == nil {
			t.Errorf("Store accepted the Study Instance UID %q", hostile)
		}
	}
}

func TestQueryAtEachLevel(t *testing.T) {
	store := openWithCorpus(t)
	ctx := context.Background()

	cases := []struct {
		level string
		keys  map[tag.Tag]string
		want  int
		why   string
	}{
		// A universal query returns one response per entity at the level, not one
		// per instance: two patients, two studies, three series, four instances.
		{"PATIENT", map[tag.Tag]string{tag.New(0x0010, 0x0020): ""}, 2, "both patients"},
		{"STUDY", map[tag.Tag]string{tag.New(0x0020, 0x000D): ""}, 2, "both studies"},
		{"SERIES", map[tag.Tag]string{tag.New(0x0020, 0x000E): ""}, 3, "all three series"},
		{"IMAGE", map[tag.Tag]string{tag.New(0x0008, 0x0018): ""}, 4, "all four instances"},

		// Narrowing by a key at the level.
		{"PATIENT", map[tag.Tag]string{tag.New(0x0010, 0x0020): "P1"}, 1, "one patient by ID"},
		{"STUDY", map[tag.Tag]string{tag.New(0x0008, 0x0060): "MR"}, 1, "the MR study"},
		{"SERIES", map[tag.Tag]string{tag.New(0x0008, 0x0060): "CT"}, 2, "the two CT series"},

		// Narrowing by a key from a level above — the hierarchy in action.
		{"SERIES", map[tag.Tag]string{tag.New(0x0020, 0x000D): "1.2.1"}, 2, "series in one study"},
		{"IMAGE", map[tag.Tag]string{tag.New(0x0020, 0x000E): "1.2.1.1"}, 2, "instances in one series"},
		{"IMAGE", map[tag.Tag]string{tag.New(0x0010, 0x0020): "P2"}, 1, "instances for one patient"},

		// Wildcards and ranges reach the store through the same matching rules.
		{"PATIENT", map[tag.Tag]string{tag.New(0x0010, 0x0010): "SMITH*"}, 1, "patient by name prefix"},
		{"STUDY", map[tag.Tag]string{tag.New(0x0008, 0x0020): "20240101-20240131"}, 1, "study by date range"},
		{"STUDY", map[tag.Tag]string{tag.New(0x0008, 0x0020): "20240101-20241231"}, 2, "both studies in the year"},
		{"STUDY", map[tag.Tag]string{tag.New(0x0008, 0x0020): "20230101-20231231"}, 0, "no study in 2023"},

		// A key that matches nothing returns nothing rather than everything.
		{"PATIENT", map[tag.Tag]string{tag.New(0x0010, 0x0020): "NOBODY"}, 0, "no such patient"},
	}

	for _, tc := range cases {
		responses, err := store.Query(ctx, query(tc.level, tc.keys))
		if err != nil {
			t.Errorf("%s query (%s): %v", tc.level, tc.why, err)
			continue
		}
		if len(responses) != tc.want {
			t.Errorf("%s query (%s) returned %d responses, want %d",
				tc.level, tc.why, len(responses), tc.want)
		}
	}
}

// A response has to carry the unique keys of its own level and every level above,
// or the requestor cannot use it as the query for the next level down — which is
// how a hierarchical retrieval works.
func TestResponsesCarryTheKeysNeededToDescend(t *testing.T) {
	store := openWithCorpus(t)

	responses, err := store.Query(context.Background(),
		query("SERIES", map[tag.Tag]string{tag.New(0x0020, 0x000E): ""}))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(responses) == 0 {
		t.Fatal("no responses")
	}

	for _, want := range []struct {
		t    tag.Tag
		name string
	}{
		{tag.New(0x0008, 0x0052), "QueryRetrieveLevel"},
		{tag.New(0x0010, 0x0020), "PatientID"},
		{tag.New(0x0020, 0x000D), "StudyInstanceUID"},
		{tag.New(0x0020, 0x000E), "SeriesInstanceUID"},
	} {
		if !responses[0].Contains(want.t) {
			t.Errorf("a SERIES response does not carry %s", want.name)
		}
	}

	// And not the level below, which was not asked about.
	if responses[0].Contains(tag.New(0x0008, 0x0018)) {
		t.Error("a SERIES response carries SOPInstanceUID, which belongs to IMAGE level")
	}
}

func TestComputedCountsAreOverTheStoreNotTheMatchedSet(t *testing.T) {
	store := openWithCorpus(t)

	responses, err := store.Query(context.Background(), query("STUDY", map[tag.Tag]string{
		tag.New(0x0020, 0x000D): "1.2.1",
		tag.New(0x0020, 0x1206): "", // NumberOfStudyRelatedSeries
		tag.New(0x0020, 0x1208): "", // NumberOfStudyRelatedInstances
		tag.New(0x0008, 0x0061): "", // ModalitiesInStudy
	}))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("got %d responses for one study", len(responses))
	}

	for _, want := range []struct {
		t     tag.Tag
		value string
		name  string
	}{
		{tag.New(0x0020, 0x1206), "2", "NumberOfStudyRelatedSeries"},
		{tag.New(0x0020, 0x1208), "3", "NumberOfStudyRelatedInstances"},
		{tag.New(0x0008, 0x0061), "CT", "ModalitiesInStudy"},
	} {
		elem, ok := responses[0].Get(want.t)
		if !ok {
			t.Errorf("the response does not carry %s", want.name)
			continue
		}
		if got := valueOf(elem); got != want.value {
			t.Errorf("%s = %q, want %q", want.name, got, want.value)
		}
	}
}

// An attribute the store does not index is an unsupported optional key. C.2.2.1.2
// allows an SCP to ignore it, and it must be returned with a zero-length value
// rather than matched on — matching would return nothing at all, which reads to
// the requestor as an empty archive.
func TestUnindexedKeysAreReturnedEmptyRatherThanMatched(t *testing.T) {
	store := openWithCorpus(t)

	// InstitutionName is not indexed.
	institutionName := tag.New(0x0008, 0x0080)
	responses, err := store.Query(context.Background(), query("STUDY", map[tag.Tag]string{
		tag.New(0x0020, 0x000D): "",
		institutionName:         "SOME HOSPITAL",
	}))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	// Both studies still match: the unsupported key did not filter them out.
	if len(responses) != 2 {
		t.Errorf("an unindexed key reduced the results to %d; it should be ignored", len(responses))
	}
	if len(responses) > 0 {
		elem, ok := responses[0].Get(institutionName)
		if !ok {
			t.Error("the unsupported key is missing from the response entirely")
		} else if got := valueOf(elem); got != "" {
			t.Errorf("the unsupported key came back as %q, want a zero-length value", got)
		}
	}
}

func TestQueryRequiresALevel(t *testing.T) {
	store := openWithCorpus(t)

	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, "P1"))
	if _, err := store.Query(context.Background(), ds); err == nil {
		t.Error("a query with no QueryRetrieveLevel was accepted")
	}

	if _, err := store.Query(context.Background(), query("FRAME", nil)); err == nil {
		t.Error("a query with an unknown QueryRetrieveLevel was accepted")
	}
}

func TestMatchingInstancesReturnsInstancesAtEveryLevel(t *testing.T) {
	store := openWithCorpus(t)
	ctx := context.Background()

	cases := []struct {
		level string
		keys  map[tag.Tag]string
		want  int
	}{
		// A retrieval at STUDY level transfers every instance in the study, not one
		// per study.
		{"STUDY", map[tag.Tag]string{tag.New(0x0020, 0x000D): "1.2.1"}, 3},
		{"SERIES", map[tag.Tag]string{tag.New(0x0020, 0x000E): "1.2.1.1"}, 2},
		{"IMAGE", map[tag.Tag]string{tag.New(0x0008, 0x0018): "1.2.1.1.1"}, 1},
		{"PATIENT", map[tag.Tag]string{tag.New(0x0010, 0x0020): "P1"}, 3},
	}

	for _, tc := range cases {
		matched, err := store.MatchingInstances(ctx, query(tc.level, tc.keys))
		if err != nil {
			t.Errorf("%s: %v", tc.level, err)
			continue
		}
		if len(matched) != tc.want {
			t.Errorf("%s retrieval matched %d instances, want %d", tc.level, len(matched), tc.want)
		}
	}
}

func TestLoadReadsBackWhatWasStored(t *testing.T) {
	store := openWithCorpus(t)

	inst, ok := store.Instance("1.2.1.1.1")
	if !ok {
		t.Fatal("instance not indexed")
	}

	ds, err := store.Load(context.Background(), inst)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, want := range []struct {
		t     tag.Tag
		value string
		name  string
	}{
		{tag.New(0x0008, 0x0018), "1.2.1.1.1", "SOPInstanceUID"},
		{tag.New(0x0010, 0x0010), "SMITH^JOHN", "PatientName"},
		{tag.New(0x0020, 0x000D), "1.2.1", "StudyInstanceUID"},
	} {
		elem, ok := ds.Get(want.t)
		if !ok {
			t.Errorf("%s is missing from the instance read back", want.name)
			continue
		}
		if got := valueOf(elem); got != want.value {
			t.Errorf("%s read back as %q, want %q", want.name, got, want.value)
		}
	}
}

// Losing the index should cost a slow start, not the archive.
func TestTheIndexIsRebuiltFromTheFiles(t *testing.T) {
	dir := t.TempDir()

	store, err := dcmstore.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, i := range corpus {
		if _, err := store.Store(context.Background(), i.dataset()); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	// Delete the index and reopen.
	if err := os.Remove(filepath.Join(dir, "index.json")); err != nil {
		t.Fatalf("removing the index: %v", err)
	}

	reopened, err := dcmstore.Open(dir)
	if err != nil {
		t.Fatalf("reopening after losing the index: %v", err)
	}
	if got := reopened.Count(); got != len(corpus) {
		t.Errorf("the rebuilt index holds %d instances, want %d", got, len(corpus))
	}

	// And it can still be queried, which is the point of rebuilding it.
	responses, err := reopened.Query(context.Background(),
		query("STUDY", map[tag.Tag]string{tag.New(0x0020, 0x000D): ""}))
	if err != nil {
		t.Fatalf("Query after a rebuild: %v", err)
	}
	if len(responses) != 2 {
		t.Errorf("a query after the rebuild found %d studies, want 2", len(responses))
	}
}

// A corrupt file should not make the rest of the archive unqueryable.
func TestRebuildSkipsUnreadableFiles(t *testing.T) {
	dir := t.TempDir()

	store, err := dcmstore.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, i := range corpus {
		if _, err := store.Store(context.Background(), i.dataset()); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	// Corrupt one file and drop a junk one in.
	inst, _ := store.Instance("1.2.1.1.1")
	victim := filepath.Join(dir, filepath.FromSlash(inst.Path))
	if err := os.WriteFile(victim, []byte("not a DICOM file at all"), 0o600); err != nil {
		t.Fatalf("corrupting a file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.dcm"), []byte{0x00, 0x01}, 0o600); err != nil {
		t.Fatalf("writing a junk file: %v", err)
	}
	_ = os.Remove(filepath.Join(dir, "index.json"))

	reopened, err := dcmstore.Open(dir)
	if err != nil {
		t.Fatalf("reopening with a corrupt file present: %v", err)
	}

	// The three readable instances survive.
	if got := reopened.Count(); got != len(corpus)-1 {
		t.Errorf("the rebuilt index holds %d instances, want %d", got, len(corpus)-1)
	}
}

func TestRemoveDeletesTheFileAndTheIndexEntry(t *testing.T) {
	store := openWithCorpus(t)

	inst, ok := store.Instance("1.2.1.1.1")
	if !ok {
		t.Fatal("instance not indexed")
	}
	path := filepath.Join(store.Root(), filepath.FromSlash(inst.Path))

	if err := store.Remove("1.2.1.1.1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := store.Instance("1.2.1.1.1"); ok {
		t.Error("the instance is still in the index")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the file is still on disk")
	}
	if got := store.Count(); got != len(corpus)-1 {
		t.Errorf("the store holds %d instances, want %d", got, len(corpus)-1)
	}

	if err := store.Remove("1.2.1.1.1"); err == nil {
		t.Error("removing an instance twice succeeded the second time")
	}
}

// Storing the same SOP Instance UID again replaces it, which is what a repeated
// C-STORE of one instance means.
func TestStoringTheSameInstanceTwiceReplacesIt(t *testing.T) {
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	first := instance{patientID: "P1", patientName: "SMITH^JOHN", studyUID: "1.2.1",
		seriesUID: "1.2.1.1", sopInstanceUID: "1.2.1.1.1", modality: "CT"}
	if _, err := store.Store(ctx, first.dataset()); err != nil {
		t.Fatalf("Store: %v", err)
	}

	second := first
	second.patientName = "SMITH^JOHN^CORRECTED"
	if _, err := store.Store(ctx, second.dataset()); err != nil {
		t.Fatalf("re-storing: %v", err)
	}

	if got := store.Count(); got != 1 {
		t.Errorf("the store holds %d instances after storing one twice", got)
	}

	inst, _ := store.Instance("1.2.1.1.1")
	ds, err := store.Load(ctx, inst)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	elem, _ := ds.Get(tag.New(0x0010, 0x0010))
	if got := valueOf(elem); got != "SMITH^JOHN^CORRECTED" {
		t.Errorf("the replaced instance reads back as %q", got)
	}
}

func TestConcurrentStoreAndQuery(t *testing.T) {
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	// An SCP handles each association in its own goroutine, so a store behind one
	// is written and read concurrently by construction.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 25; i++ {
			ds := instance{
				patientID: "P1", studyUID: "1.2.1", seriesUID: "1.2.1.1",
				sopInstanceUID: "1.2.1.1." + string(rune('0'+i%10)) + "." + string(rune('0'+i/10)),
				modality:       "CT",
			}.dataset()
			_, _ = store.Store(ctx, ds)
		}
	}()

	for i := 0; i < 25; i++ {
		_, _ = store.Query(ctx, query("STUDY", map[tag.Tag]string{tag.New(0x0020, 0x000D): ""}))
		_ = store.Count()
		_ = store.Instances()
	}
	<-done
}

// valueOf renders an element value the way the store does, so a test comparison
// is not sensitive to whether the value is held as a string or as bytes.
func valueOf(elem *dataelem.DataElement) string {
	if elem == nil {
		return ""
	}
	switch v := elem.GetValue().(type) {
	case nil:
		return ""
	case string:
		return strings.Trim(v, " \x00")
	case []byte:
		return strings.Trim(string(v), " \x00")
	default:
		return ""
	}
}
