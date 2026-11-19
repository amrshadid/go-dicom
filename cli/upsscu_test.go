package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amrshadid/go-dicom/network"
)

// upsMemory is a UPSStore in memory, for running the library's own UPS SCP.
type upsMemory struct {
	mu    sync.Mutex
	steps map[string]*network.UPSStep
}

func (m *upsMemory) FindUPS(_ context.Context, uid string) (*network.UPSStep, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	step, ok := m.steps[uid]
	if !ok {
		return nil, false, nil
	}
	copied := *step
	return &copied, true, nil
}

func (m *upsMemory) CreateUPS(_ context.Context, step *network.UPSStep) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.steps[step.SOPInstanceUID] = step
	return nil
}

func (m *upsMemory) UpdateUPS(_ context.Context, step *network.UPSStep) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.steps[step.SOPInstanceUID] = step
	return nil
}

// TestUPSSCUWorksAStep runs upsscu against the library's UPSHandler, which
// keeps PS3.4 CC.1.1's state machine and Transaction UID lock: a step is
// created SCHEDULED, claimed with a Transaction UID the command prints,
// updated and read back, refused completion under another Transaction
// UID, completed under its own, and refused a request to cancel once
// final. Each refusal prints its status and exits nonzero.
func TestUPSSCUWorksAStep(t *testing.T) {
	binary := buildCLI(t)
	store := &upsMemory{steps: map[string]*network.UPSStep{}}
	server, err := network.StartServer(context.Background(), network.SCPConfig{
		AETitle: "UPS", Port: 0, BindAddress: "127.0.0.1",
	}, &network.UPSHandler{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	server.SetSupportedAbstractSyntaxes([]string{network.UnifiedProcedureStepPushUID, network.UnifiedProcedureStepPullUID})

	update := filepath.Join(t.TempDir(), "update.json")
	if err := os.WriteFile(update, []byte(`{"00741204": {"vr": "LO", "Value": ["Planning, revised"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const step = "1.2.826.0.1.3680043.8.498.5000001"
	run := func(args ...string) (string, error) {
		args = append(append([]string{"upsscu", "-aec", "UPS", "-timeout", "10"}, args...), server.Addr())
		out, err := exec.Command(binary, args...).CombinedOutput()
		return string(out), err
	}
	for _, c := range []struct {
		args []string
		ok   bool
		want string
	}{
		{[]string{"-create", "-uid", step, "-patient-id", "P1", "-label", "Planning"}, true, "Procedure step " + step + " is SCHEDULED"},
		{[]string{"-claim", step}, true, "Transaction UID "},
		{[]string{"-complete", step, "-transaction", "1.2.3.4"}, false, "N-ACTION Failure (status 0xC301)"},
	} {
		out, err := run(c.args...)
		if (err == nil) != c.ok || !strings.Contains(out, c.want) {
			t.Fatalf("%v: %v\n%s", c.args, err, out)
		}
	}

	store.mu.Lock()
	lock := store.steps[step].TransactionUID
	state := store.steps[step].State
	store.mu.Unlock()
	if state != network.UPSInProgress || lock == "" {
		t.Fatalf("after -claim the step is %s with Transaction UID %q", state, lock)
	}
	out, _ := run("-claim", "1.2.3.999")
	if !strings.Contains(out, "N-ACTION Failure") {
		t.Errorf("claiming a step that does not exist:\n%s", out)
	}
	for _, c := range []struct {
		args []string
		ok   bool
		want string
	}{
		{[]string{"-set", step, "-transaction", lock, "-json", update}, true, "is updated"},
		{[]string{"-get", step}, true, "Planning, revised"},
		{[]string{"-complete", step, "-transaction", lock}, true, "is COMPLETED"},
		{[]string{"-request-cancel", step}, false, "N-ACTION Failure"},
	} {
		out, err := run(c.args...)
		if (err == nil) != c.ok || !strings.Contains(out, c.want) {
			t.Errorf("%v: %v\n%s", c.args, err, out)
		}
	}
}

// TestUPSSCURefusesWhatIsNotAnOperation checks the flags that name no one
// operation, or one without what it needs, refused before any association.
func TestUPSSCURefusesWhatIsNotAnOperation(t *testing.T) {
	binary := buildCLI(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "give one of"},
		{[]string{"-create", "-query"}, "give one of"},
		{[]string{"-complete", "1.2.3"}, "needs -transaction"},
		{[]string{"-set", "1.2.3", "-transaction", "1.2.4"}, "-set needs -json"},
		{[]string{"-get", "1.2.3", "-transaction", "1.2.4"}, "-transaction is for"},
		{[]string{"-claim", "1.2.3", "-uid", "1.2.4"}, "-uid names the step -create makes"},
		{[]string{"-create", "-priority", "URGENT"}, "HIGH, MEDIUM or LOW"},
	} {
		out, err := exec.Command(binary, append(append([]string{"upsscu"}, c.args...), "127.0.0.1:1")...).CombinedOutput()
		if err == nil || !strings.Contains(string(out), c.want) || strings.Contains(string(out), "Requesting Association") {
			t.Errorf("%v: %v\n%s", c.args, err, out)
		}
	}
}
