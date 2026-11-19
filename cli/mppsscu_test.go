package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// mppsSCP is an MPPS SCP that keeps the steps it is told of, as an RIS
// would: a step is created IN PROGRESS, and set while it still is.
type mppsSCP struct {
	network.BaseHandler
	mu      sync.Mutex
	created map[string]*dataset.Dataset
	sets    map[string][]*dataset.Dataset
}

func (h *mppsSCP) HandleNCreate(_ context.Context, req *network.NCreateRequest) (*network.NCreateResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.created[req.AffectedSOPInstance] = req.DataSet
	return &network.NCreateResponse{AffectedSOPClass: req.AffectedSOPClass,
		AffectedSOPInstance: req.AffectedSOPInstance, Status: 0x0000}, nil
}

func (h *mppsSCP) HandleNSet(_ context.Context, req *network.NSetRequest) (*network.NSetResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	status := uint16(0x0000)
	if h.created[req.RequestedSOPInstance] == nil {
		status = 0x0112 // No Such Object Instance
	} else {
		h.sets[req.RequestedSOPInstance] = append(h.sets[req.RequestedSOPInstance], req.DataSet)
	}
	return &network.NSetResponse{AffectedSOPClass: req.RequestedSOPClass,
		AffectedSOPInstance: req.RequestedSOPInstance, Status: status}, nil
}

// valueAt returns the text of the attribute at a path of tags, through the
// first item of each sequence on the way.
func valueAt(t *testing.T, ds *dataset.Dataset, path ...tag.Tag) string {
	t.Helper()
	for _, step := range path[:len(path)-1] {
		items, err := ds.GetSequence(step)
		if err != nil || items.Length() == 0 {
			t.Fatalf("no item in %s", step)
		}
		raw, _ := items.Get(0)
		ds = raw.(*dataset.Dataset)
	}
	elem, ok := ds.Get(path[len(path)-1])
	if !ok {
		t.Fatalf("%s is not in the data set", path[len(path)-1])
	}
	value, _ := elem.GetValue().([]byte)
	return strings.TrimRight(string(value), " \x00")
}

// TestMPPSSCUReportsAStep runs mppsscu against an MPPS SCP through a step's
// life: the N-CREATE carries the patient, the scheduled step, the station
// and IN PROGRESS, with the Type 2 attributes of PS3.4 Table F.7.2-1
// present though empty, and a -json object's attributes over them; the
// N-SET makes it COMPLETED with its end time and the series and images it
// made; and a step the SCP does not know is a refusal, printed with its
// status, with a nonzero exit.
func TestMPPSSCUReportsAStep(t *testing.T) {
	binary := buildCLI(t)
	scp := &mppsSCP{created: map[string]*dataset.Dataset{}, sets: map[string][]*dataset.Dataset{}}
	server, err := network.StartServer(context.Background(), network.SCPConfig{
		AETitle: "MPPS", Port: 0, BindAddress: "127.0.0.1",
	}, scp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	server.SetSupportedAbstractSyntaxes([]string{network.ModalityPerformedProcedureStepUID})

	extra := filepath.Join(t.TempDir(), "extra.json")
	if err := os.WriteFile(extra, []byte(`{"00400242": {"vr": "SH", "Value": ["SCANNER-3"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const step = "1.2.826.0.1.3680043.8.498.4950001"
	run := func(args ...string) (string, error) {
		args = append(append([]string{"mppsscu", "-aec", "MPPS", "-timeout", "10"}, args...), server.Addr())
		out, err := exec.Command(binary, args...).CombinedOutput()
		return string(out), err
	}

	out, err := run("-uid", step, "-patient-name", "DOE^JANE", "-patient-id", "P123", "-accession", "A42",
		"-scheduled-step-id", "SPS1", "-modality", "MR", "-json", extra)
	if err != nil || !strings.Contains(out, "N-CREATE Success (status 0x0000)") ||
		!strings.Contains(out, "Performed Procedure Step "+step+" is IN PROGRESS") {
		t.Fatalf("create: %v\n%s", err, out)
	}
	scp.mu.Lock()
	created := scp.created[step]
	scp.mu.Unlock()
	if created == nil {
		t.Fatal("the SCP was not given the step")
	}
	scheduled := tag.New(0x0040, 0x0270)
	for _, c := range []struct {
		path []tag.Tag
		want string
	}{
		{[]tag.Tag{tag.New(0x0010, 0x0010)}, "DOE^JANE"},
		{[]tag.Tag{tag.New(0x0010, 0x0020)}, "P123"},
		{[]tag.Tag{tag.New(0x0008, 0x0060)}, "MR"},
		{[]tag.Tag{tag.New(0x0040, 0x0252)}, "IN PROGRESS"},
		{[]tag.Tag{tag.New(0x0040, 0x0241)}, "MPPSSCU"},
		{[]tag.Tag{tag.New(0x0040, 0x0242)}, "SCANNER-3"},
		{[]tag.Tag{tag.New(0x0040, 0x0250)}, ""},
		{[]tag.Tag{scheduled, tag.New(0x0008, 0x0050)}, "A42"},
		{[]tag.Tag{scheduled, tag.New(0x0040, 0x0009)}, "SPS1"},
	} {
		if got := valueAt(t, created, c.path...); got != c.want {
			t.Errorf("created %v = %q, want %q", c.path, got, c.want)
		}
	}
	if valueAt(t, created, scheduled, tag.New(0x0020, 0x000D)) == "" {
		t.Error("the scheduled step has no Study Instance UID")
	}

	out, err = run("-complete", step, "-series", "1.2.3.4.5", "-protocol", "T1 AX",
		"-instance", "1.2.840.10008.5.1.4.1.1.4:1.2.3.4.5.1")
	if err != nil || !strings.Contains(out, "N-SET Success") || !strings.Contains(out, "is COMPLETED") {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	scp.mu.Lock()
	set := scp.sets[step][0]
	scp.mu.Unlock()
	series := tag.New(0x0040, 0x0340)
	for _, c := range []struct {
		path []tag.Tag
		want string
	}{
		{[]tag.Tag{tag.New(0x0040, 0x0252)}, "COMPLETED"},
		{[]tag.Tag{series, tag.New(0x0020, 0x000E)}, "1.2.3.4.5"},
		{[]tag.Tag{series, tag.New(0x0018, 0x1030)}, "T1 AX"},
		{[]tag.Tag{series, tag.New(0x0008, 0x1140), tag.New(0x0008, 0x1155)}, "1.2.3.4.5.1"},
	} {
		if got := valueAt(t, set, c.path...); got != c.want {
			t.Errorf("set %v = %q, want %q", c.path, got, c.want)
		}
	}
	if len(valueAt(t, set, tag.New(0x0040, 0x0250))) != 8 {
		t.Error("COMPLETED without an end date")
	}

	out, err = run("-discontinue", "1.2.3.999")
	if err == nil || !strings.Contains(out, "N-SET Failure (status 0x0112)") {
		t.Errorf("an unknown step: %v\n%s", err, out)
	}
}

// TestMPPSSCURefusesWhatIsNotAStep checks the flags that cannot make a
// report, refused before any association: two ways to end a step, images
// with no series or with several, and series without Protocol Name, which
// each Performed Series Sequence item requires.
func TestMPPSSCURefusesWhatIsNotAStep(t *testing.T) {
	binary := buildCLI(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-complete", "1.2.3", "-discontinue", "1.2.3"}, "give one"},
		{[]string{"-complete", "1.2.3", "-instance", "1.2:3.4"}, "in the one -series"},
		{[]string{"-complete", "1.2.3", "-series", "1.2.4"}, "-series needs -protocol"},
		{[]string{"-series", "1.2.4", "-protocol", "P"}, "with -complete or -discontinue"},
		{[]string{"-complete", "1.2.3", "-uid", "1.2.4"}, "-uid names the step to create"},
	} {
		out, err := exec.Command(binary, append(append([]string{"mppsscu"}, c.args...), "127.0.0.1:1")...).CombinedOutput()
		if err == nil || !strings.Contains(string(out), c.want) || strings.Contains(string(out), "Requesting Association") {
			t.Errorf("%v: %v\n%s", c.args, err, out)
		}
	}
}
