package dcmstore_test

import (
	"context"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

var (
	tagScheduledProcedureStepSequence = tag.New(0x0040, 0x0100)
	tagScheduledStationAETitle        = tag.New(0x0040, 0x0001)
	tagScheduledStepStartDate         = tag.New(0x0040, 0x0002)
	tagScheduledStepStartTime         = tag.New(0x0040, 0x0003)
	tagScheduledStepID                = tag.New(0x0040, 0x0009)
	tagModalityInStep                 = tag.New(0x0008, 0x0060)
)

// step is one item of a Scheduled Procedure Step Sequence.
type step struct {
	station   string
	startDate string
	startTime string
	modality  string
	stepID    string
}

func (s step) dataset() *dataset.Dataset {
	ds := dataset.NewDataset()
	add := func(t tag.Tag, vr dataelem.VR, value string) {
		if value != "" {
			_ = ds.Add(dataelem.NewDataElement(t, vr, value))
		}
	}
	add(tagScheduledStationAETitle, dataelem.AE, s.station)
	add(tagScheduledStepStartDate, dataelem.DA, s.startDate)
	add(tagScheduledStepStartTime, dataelem.TM, s.startTime)
	add(tagModalityInStep, dataelem.CS, s.modality)
	add(tagScheduledStepID, dataelem.SH, s.stepID)
	return ds
}

// instanceWithSteps builds an instance carrying a Scheduled Procedure Step
// Sequence with the given items.
func instanceWithSteps(sopInstanceUID string, steps ...step) *dataset.Dataset {
	ds := instance{
		patientID: "P1", patientName: "SMITH^JOHN",
		studyUID: "1.2.1", seriesUID: "1.2.1.1",
		sopInstanceUID: sopInstanceUID, modality: "CT",
	}.dataset()

	seq := sequence.New()
	for _, s := range steps {
		_ = seq.Append(s.dataset())
	}
	_ = ds.Add(dataelem.NewDataElement(tagScheduledProcedureStepSequence, dataelem.SQ, seq))

	return ds
}

// querySequence builds a query carrying a sequence key with one item.
func querySequence(level string, item *dataset.Dataset) *dataset.Dataset {
	ds := query(level, nil)
	seq := sequence.New()
	if item != nil {
		_ = seq.Append(item)
	}
	_ = ds.Add(dataelem.NewDataElement(tagScheduledProcedureStepSequence, dataelem.SQ, seq))
	return ds
}

func openWithSteps(t *testing.T) *dcmstore.Store {
	t.Helper()

	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Three instances. The third carries two steps, which is what makes the
	// "any item matches" rule observable.
	for _, ds := range []*dataset.Dataset{
		instanceWithSteps("1.2.1.1.1",
			step{station: "CT_ROOM_1", startDate: "20240115", startTime: "090000", modality: "CT", stepID: "S1"}),
		instanceWithSteps("1.2.1.1.2",
			step{station: "MR_ROOM_2", startDate: "20240220", startTime: "140000", modality: "MR", stepID: "S2"}),
		instanceWithSteps("1.2.1.1.3",
			step{station: "CT_ROOM_1", startDate: "20240301", startTime: "080000", modality: "CT", stepID: "S3"},
			step{station: "MR_ROOM_2", startDate: "20240302", startTime: "160000", modality: "MR", stepID: "S4"}),
	} {
		if _, err := store.Store(context.Background(), ds); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	return store
}

func TestSequenceMatchingFiltersOnNestedAttributes(t *testing.T) {
	store := openWithSteps(t)
	ctx := context.Background()

	cases := []struct {
		name string
		item *dataset.Dataset
		want int
	}{
		{
			// A universal sequence key: no item at all matches everything, and asks
			// for the sequence to be returned.
			name: "no item is universal matching",
			item: nil,
			want: 3,
		},
		{
			// An item whose attributes are all zero-length is universal too.
			name: "an item of empty values is universal matching",
			item: step{}.dataset(),
			want: 3,
		},
		{
			name: "one station",
			item: step{station: "CT_ROOM_1"}.dataset(),
			want: 2, // the first instance and the third, whose first step is there
		},
		{
			name: "the other station",
			item: step{station: "MR_ROOM_2"}.dataset(),
			want: 2, // the second instance and the third, whose second step is there
		},
		{
			name: "a station nothing is scheduled at",
			item: step{station: "XR_ROOM_9"}.dataset(),
			want: 0,
		},
		{
			name: "a wildcard on the station",
			item: step{station: "CT_*"}.dataset(),
			want: 2,
		},
		{
			name: "a date range inside the sequence",
			item: step{startDate: "20240101-20240131"}.dataset(),
			want: 1,
		},
		{
			name: "a wider date range",
			item: step{startDate: "20240101-20241231"}.dataset(),
			want: 3,
		},
		{
			name: "the step ID",
			item: step{stepID: "S4"}.dataset(),
			want: 1, // only the third instance has it, in its second item
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses, err := store.Query(ctx, querySequence("IMAGE", tc.item))
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(responses) != tc.want {
				t.Errorf("matched %d instances, want %d", len(responses), tc.want)
			}
		})
	}
}

// Every criterion in a query item must be satisfied by the *same* stored item.
// An instance with a step at CT_ROOM_1 on one date and a step at MR_ROOM_2 on
// another must not match a query for CT_ROOM_1 on the second date — matching the
// attributes across different items would return a step nobody scheduled.
func TestSequenceCriteriaMustBeMetByOneItem(t *testing.T) {
	store := openWithSteps(t)
	ctx := context.Background()

	// The third instance has CT_ROOM_1 on 20240301 and MR_ROOM_2 on 20240302.
	matchingCombination := step{station: "CT_ROOM_1", startDate: "20240301"}.dataset()
	crossedCombination := step{station: "CT_ROOM_1", startDate: "20240302"}.dataset()

	matched, err := store.Query(ctx, querySequence("IMAGE", matchingCombination))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(matched) != 1 {
		t.Errorf("a station and date from the same item matched %d instances, want 1", len(matched))
	}

	crossed, err := store.Query(ctx, querySequence("IMAGE", crossedCombination))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(crossed) != 0 {
		t.Errorf("a station from one item and a date from another matched %d instances, want 0; "+
			"criteria must be satisfied by a single item", len(crossed))
	}
}

// An instance that does not carry the sequence cannot satisfy a query for values
// inside it — but a universal sequence key still matches it, since that key asks
// for the attribute to be returned rather than selecting on it.
func TestAnInstanceWithoutTheSequence(t *testing.T) {
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	plain := instance{
		patientID: "P2", studyUID: "1.2.2", seriesUID: "1.2.2.1",
		sopInstanceUID: "1.2.2.1.1", modality: "CT",
	}.dataset()
	if _, err := store.Store(ctx, plain); err != nil {
		t.Fatalf("Store: %v", err)
	}

	withValues, err := store.Query(ctx, querySequence("IMAGE", step{station: "CT_ROOM_1"}.dataset()))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(withValues) != 0 {
		t.Errorf("an instance with no sequence matched a query for values inside it")
	}

	universal, err := store.Query(ctx, querySequence("IMAGE", nil))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(universal) != 1 {
		t.Errorf("a universal sequence key matched %d instances, want 1", len(universal))
	}
}

// The response carries the item that matched, not whichever came first, so the
// requestor sees the step it asked about.
func TestTheResponseCarriesTheMatchedItem(t *testing.T) {
	store := openWithSteps(t)

	// The third instance's *second* step is at MR_ROOM_2.
	responses, err := store.Query(context.Background(),
		querySequence("IMAGE", step{station: "MR_ROOM_2", stepID: ""}.dataset()))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(responses) == 0 {
		t.Fatal("no matches")
	}

	// Find the response for the two-step instance.
	var target *dataset.Dataset
	for _, response := range responses {
		elem, ok := response.Get(tag.New(0x0008, 0x0018))
		if ok && valueOf(elem) == "1.2.1.1.3" {
			target = response
			break
		}
	}
	if target == nil {
		t.Fatal("the two-step instance is not among the matches")
	}

	seq, err := target.GetSequence(tagScheduledProcedureStepSequence)
	if err != nil {
		t.Fatalf("the response carries no Scheduled Procedure Step Sequence: %v", err)
	}
	if seq.Length() != 1 {
		t.Fatalf("the response sequence has %d items, want 1", seq.Length())
	}

	item, err := seq.Get(0)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	itemDS, ok := item.(*dataset.Dataset)
	if !ok {
		t.Fatalf("the item is %T, not a data set", item)
	}

	station, ok := itemDS.Get(tagScheduledStationAETitle)
	if !ok {
		t.Fatal("the returned item carries no ScheduledStationAETitle")
	}
	if got := valueOf(station); got != "MR_ROOM_2" {
		t.Errorf("the returned item is for station %q, want the one that matched, MR_ROOM_2", got)
	}
}

// A sequence outside the indexed set is an unsupported optional key, like any
// other: ignored for matching rather than matched on, since matching would return
// nothing and read as an empty archive.
func TestAnUnindexedSequenceIsIgnoredForMatching(t *testing.T) {
	store := openWithSteps(t)

	// (0008,1110) Referenced Study Sequence is not indexed.
	unindexed := tag.New(0x0008, 0x1110)
	item := dataset.NewDataset()
	_ = item.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1150), dataelem.UI, "1.2.999"))

	seq := sequence.New()
	_ = seq.Append(item)

	q := query("IMAGE", nil)
	_ = q.Add(dataelem.NewDataElement(unindexed, dataelem.SQ, seq))

	responses, err := store.Query(context.Background(), q)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(responses) != 3 {
		t.Errorf("an unindexed sequence key reduced the results to %d; it should be ignored",
			len(responses))
	}
}

// The index is JSON, so it survives a restart — including the nested items, which
// are the part a naive index format would lose.
func TestSequencesSurviveAnIndexReload(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store, err := dcmstore.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Store(ctx, instanceWithSteps("1.2.1.1.1",
		step{station: "CT_ROOM_1", startDate: "20240115", modality: "CT", stepID: "S1"})); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Reopened from the index file.
	reopened, err := dcmstore.Open(dir)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	fromIndex, err := reopened.Query(ctx, querySequence("IMAGE", step{station: "CT_ROOM_1"}.dataset()))
	if err != nil {
		t.Fatalf("Query after reload: %v", err)
	}
	if len(fromIndex) != 1 {
		t.Errorf("a sequence query after an index reload matched %d instances, want 1", len(fromIndex))
	}

	// And after a rebuild from the files, which is the other path that has to
	// produce the same index.
	if err := reopened.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	fromFiles, err := reopened.Query(ctx, querySequence("IMAGE", step{station: "CT_ROOM_1"}.dataset()))
	if err != nil {
		t.Fatalf("Query after rebuild: %v", err)
	}
	if len(fromFiles) != 1 {
		t.Errorf("a sequence query after a rebuild matched %d instances, want 1; "+
			"the two index paths disagree about nested attributes", len(fromFiles))
	}
}
