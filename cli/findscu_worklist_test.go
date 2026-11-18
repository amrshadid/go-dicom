package cli

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// worklistEntry is one scheduled MR exam, in the shape dcmtk's sample
// worklist database holds them.
func worklistEntry(t *testing.T) *dataset.Dataset {
	t.Helper()
	add := func(ds *dataset.Dataset, g, e uint16, vr dataelem.VR, v string) {
		if err := ds.Add(dataelem.NewDataElement(tag.New(g, e), vr, []byte(v))); err != nil {
			t.Fatal(err)
		}
	}
	entry := dataset.NewDataset()
	add(entry, 0x0008, 0x0050, dataelem.SH, "ACC0042 ")
	add(entry, 0x0010, 0x0010, dataelem.PN, "VIVALDI^ANTONIO ")
	add(entry, 0x0010, 0x0020, dataelem.LO, "AV35674 ")
	step := dataset.NewDataset()
	add(step, 0x0008, 0x0060, dataelem.CS, "MR")
	add(step, 0x0040, 0x0001, dataelem.AE, "AA32")
	add(step, 0x0040, 0x0002, dataelem.DA, "19951015")
	add(step, 0x0040, 0x0007, dataelem.LO, "EXAM74")
	steps := sequence.New()
	if err := steps.Append(step); err != nil {
		t.Fatal(err)
	}
	if err := entry.AddSequence(tag.New(0x0040, 0x0100), steps); err != nil {
		t.Fatal(err)
	}
	return entry
}

// TestFindscuWorklist runs findscu -worklist against a worklist SCP and checks
// both sides: the identifier is a worklist query, with no Query/Retrieve Level
// and with the matching keys inside the Scheduled Procedure Step Sequence
// (PS3.4 K.6.1.2.2); and the answer is printed with the step it schedules,
// which is held in that sequence and was not printed at all.
func TestFindscuWorklist(t *testing.T) {
	binary := buildCLI(t)

	var mu sync.Mutex
	var identifiers []*dataset.Dataset
	server, err := network.StartServer(context.Background(), network.SCPConfig{
		AETitle: "WLM", Port: 0, BindAddress: "127.0.0.1",
	}, &network.WorklistHandler{
		OnWorklist: func(_ context.Context, query *dataset.Dataset) ([]*dataset.Dataset, error) {
			mu.Lock()
			identifiers = append(identifiers, query)
			mu.Unlock()
			return []*dataset.Dataset{worklistEntry(t)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	server.SetSupportedAbstractSyntaxes([]string{network.ModalityWorklistInformationModelFindUID})

	for _, args := range [][]string{
		{"-worklist", "-modality", "MR", "-station-aet", "AA32", "-date", "19951015"},
		// Naming the model by UID is the same query.
		{"-sop-class", network.ModalityWorklistInformationModelFindUID, "-modality", "MR", "-station-aet", "AA32", "-date", "19951015"},
	} {
		cmd := append([]string{"findscu", "-aec", "WLM", "-timeout", "10"}, args...)
		out, err := exec.Command(binary, append(cmd, server.Addr())...).CombinedOutput()
		if err != nil {
			t.Fatalf("findscu %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		for _, want := range []string{
			"Querying " + server.Addr() + " (Modality Worklist)",
			"Patient's Name                           VIVALDI^ANTONIO",
			"Scheduled Procedure Step Sequence",
			"  Item 1",
			"Modality                                 MR",
			"Scheduled Station AE Title               AA32",
			"Scheduled Procedure Step Description     EXAM74",
			"Found 1 result(s)",
		} {
			if !strings.Contains(string(out), want) {
				t.Errorf("findscu %s: output lacks %q:\n%s", strings.Join(args, " "), want, out)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(identifiers) != 2 {
		t.Fatalf("the SCP was asked %d times, want 2", len(identifiers))
	}
	for _, query := range identifiers {
		if _, ok := query.Get(tag.New(0x0008, 0x0052)); ok {
			t.Error("a worklist query carries a Query/Retrieve Level, which Basic Worklist does not have")
		}
		steps, err := query.GetSequence(tag.New(0x0040, 0x0100))
		if err != nil || steps.Length() != 1 {
			t.Fatalf("no Scheduled Procedure Step Sequence of one item in the query: %v", err)
		}
		raw, _ := steps.Get(0)
		step, ok := raw.(*dataset.Dataset)
		if !ok {
			t.Fatal("the Scheduled Procedure Step item is not a data set")
		}
		for _, k := range []struct {
			tag  tag.Tag
			want string
		}{
			{tag.New(0x0008, 0x0060), "MR"},
			{tag.New(0x0040, 0x0001), "AA32"},
			{tag.New(0x0040, 0x0002), "19951015"},
			{tag.New(0x0040, 0x0009), ""},
		} {
			elem, ok := step.Get(k.tag)
			if !ok {
				t.Errorf("the step item lacks %s", k.tag)
				continue
			}
			value, _ := elem.GetValue().([]byte)
			if got := strings.TrimRight(string(value), " \x00"); got != k.want {
				t.Errorf("%s in the step item = %q, want %q", k.tag, got, k.want)
			}
		}
	}
}

// TestFindscuWorklistFlagsNeedWorklist: the step's keys belong to a worklist
// query, and -level to a query that has levels, so each is refused where it
// would be ignored.
func TestFindscuWorklistFlagsNeedWorklist(t *testing.T) {
	binary := buildCLI(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-modality", "MR"}, "-modality, -date and -station-aet need -worklist"},
		{[]string{"-worklist", "-level", "PATIENT"}, "-level has no meaning in a worklist query"},
		{[]string{"-worklist", "-sop-class", network.StudyRootQueryRetrieveFind}, "-worklist queries Modality Worklist"},
	} {
		out, err := exec.Command(binary, append(append([]string{"findscu"}, c.args...), "127.0.0.1:1")...).CombinedOutput()
		if err == nil || !strings.Contains(string(out), c.want) {
			t.Errorf("findscu %s: exit %v, output:\n%s", strings.Join(c.args, " "), err, out)
		}
	}
}

// TestWorklistDate: "today" is this machine's date; anything else, a range
// included, is the matching value as written.
func TestWorklistDate(t *testing.T) {
	now := time.Date(2026, 11, 18, 23, 0, 0, 0, time.Local)
	for in, want := range map[string]string{"today": "20261118", "TODAY": "20261118",
		"19951015": "19951015", "20261118-20261120": "20261118-20261120"} {
		if got := worklistDate(in, now); got != want {
			t.Errorf("worklistDate(%q) = %q, want %q", in, got, want)
		}
	}
}
