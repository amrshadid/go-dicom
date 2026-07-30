package network_test

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// The N-DIMSE services are the ones this library gets to exercise least against
// a real peer, and they are where its worst defect hid: N-ACTION, N-GET, N-SET
// and N-DELETE all named their target with Affected SOP Instance UID (0000,1000)
// where the standard requires Requested (0000,1001). go-dicom's own SCP read the
// same wrong tag, so the whole suite passed while pynetdicom aborted the
// association. The tag did not exist anywhere in the codebase.
//
// MPPS and print management are built entirely from those primitives, and the
// README claims both. These tests are what make that claim checkable: they run
// the workflows against pynetdicom, which has no reason to agree with our
// mistakes.

// requirePynetdicom skips the test when pynetdicom is not importable — except
// where GODICOM_REQUIRE_INTEROP is set, which makes the absence a failure.
//
// These tests are the only thing standing behind the README's claim that MPPS
// and print management are verified against pynetdicom, and they skip on any
// machine without it. That is right for a contributor's laptop and wrong for CI,
// where a skip is indistinguishable from a pass and the claim would quietly stop
// being true. The interoperability job sets the variable.
func requirePynetdicom(t *testing.T) {
	t.Helper()

	_, pathErr := exec.LookPath("python3")
	importErr := pathErr
	if pathErr == nil {
		importErr = exec.Command("python3", "-c", "import pynetdicom").Run()
	}
	if importErr == nil {
		return
	}
	if os.Getenv("GODICOM_REQUIRE_INTEROP") != "" {
		t.Fatalf("GODICOM_REQUIRE_INTEROP is set but pynetdicom is not importable: %v", importErr)
	}
	t.Skipf("pynetdicom not available (%v); skipping third-party interoperability check", importErr)
}

// startPynetdicomSCP writes the given python source to a temp file, runs it, and
// returns the address it bound. The script must bind port 0 and print
// "PORT <n>" so tests never collide on a fixed port.
func startPynetdicomSCP(t *testing.T, source string) string {
	t.Helper()
	requirePynetdicom(t)

	script := t.TempDir() + "/scp.py"
	if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd := exec.Command("python3", script)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start pynetdicom: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// The port line arrives before the server accepts, so reading it is also how
	// we wait for readiness. Remaining stdout is discarded rather than sent
	// anywhere: a channel here would leave this goroutine blocked on a reader
	// that no longer exists once the test returns.
	ch := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if port, found := strings.CutPrefix(scanner.Text(), "PORT "); found {
				select {
				case ch <- "127.0.0.1:" + strings.TrimSpace(port):
				default:
				}
			}
		}
	}()

	select {
	case addr := <-ch:
		return addr
	case <-time.After(20 * time.Second):
		t.Fatal("pynetdicom SCP did not report a port")
		return ""
	}
}

// waitForLines collects lines the python SCP printed until it has n of them.
func collectLines(t *testing.T, path string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			got := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(got) >= want && got[0] != "" {
				return got
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("pynetdicom recorded fewer than %d events; got:\n%s", want, data)
	return nil
}

// TestMPPSAgainstPynetdicom drives a modality performed procedure step the way a
// modality does: N-CREATE to announce the step is in progress, N-SET to complete
// it. pynetdicom is the SCP, so the attribute values it reports back are its
// reading of our encoding, not ours.
func TestMPPSAgainstPynetdicom(t *testing.T) {
	record := t.TempDir() + "/events.txt"
	addr := startPynetdicomSCP(t, `
import sys
from pynetdicom import AE, evt, ALL_TRANSFER_SYNTAXES
from pynetdicom.sop_class import ModalityPerformedProcedureStep

record = open(`+quote(record)+`, "w", buffering=1)

def on_create(event):
    ds = event.attribute_list
    record.write("CREATE %s %s\n" % (ds.PerformedProcedureStepStatus, ds.Modality))
    return 0x0000, ds

def on_set(event):
    ds = event.modification_list
    record.write("SET %s\n" % ds.PerformedProcedureStepStatus)
    return 0x0000, ds

ae = AE(ae_title="PY_MPPS")
ae.add_supported_context(ModalityPerformedProcedureStep, ALL_TRANSFER_SYNTAXES)
server = ae.start_server(("127.0.0.1", 0), block=False,
                         evt_handlers=[(evt.EVT_N_CREATE, on_create), (evt.EVT_N_SET, on_set)])
print("PORT %d" % server.server_address[1], flush=True)
import time
time.sleep(120)  # idles until the test kills it
`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "GO_MPPS", CalledAE: "PY_MPPS", Address: addr,
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: network.ModalityPerformedProcedureStepUID,
		TransferSyntaxes: []string{network.ExplicitVRLittleEndianUID, network.ImplicitVRLittleEndianUID},
	}}); err != nil {
		t.Fatalf("associate for MPPS: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	const instance = "1.2.826.0.1.3680043.10.511.9.1"

	inProgress := dataset.NewDataset()
	add(t, inProgress, 0x0040, 0x0252, dataelem.CS, "IN PROGRESS ")
	add(t, inProgress, 0x0008, 0x0060, dataelem.CS, "CT")
	created, err := scu.NCreate(ctx, network.ModalityPerformedProcedureStepUID, instance, inProgress)
	if err != nil {
		t.Fatalf("N-CREATE: %v", err)
	}
	if created.Status != network.StatusSuccess {
		t.Errorf("N-CREATE status = 0x%04X, want success", created.Status)
	}

	completed := dataset.NewDataset()
	add(t, completed, 0x0040, 0x0252, dataelem.CS, "COMPLETED   ")
	set, err := scu.NSet(ctx, network.ModalityPerformedProcedureStepUID, instance, completed)
	if err != nil {
		t.Fatalf("N-SET: %v", err)
	}
	if set.Status != network.StatusSuccess {
		t.Errorf("N-SET status = 0x%04X, want success", set.Status)
	}

	events := collectLines(t, record, 2)
	if got := strings.TrimSpace(events[0]); got != "CREATE IN PROGRESS CT" {
		t.Errorf("pynetdicom read the N-CREATE as %q, want %q", got, "CREATE IN PROGRESS CT")
	}
	if got := strings.TrimSpace(events[1]); got != "SET COMPLETED" {
		t.Errorf("pynetdicom read the N-SET as %q, want %q", got, "SET COMPLETED")
	}
}

// TestMPPSSCPAgainstPynetdicom is the same workflow with the roles reversed:
// pynetdicom is the modality and go-dicom serves the procedure step. Both
// directions matter, because the SCU and SCP paths build their command sets
// separately — the (0000,1001) defect was present in both and visible in
// neither until a third party read the bytes.
func TestMPPSSCPAgainstPynetdicom(t *testing.T) {
	events := make(chan mppsEvent, 4)

	scp := network.NewSCP(network.SCPConfig{
		AETitle: "GO_MPPS_SCP", Port: 0, BindAddress: "127.0.0.1",
	})
	scp.SetHandler(&mppsRecorder{events: events})
	scp.SetSupportedAbstractSyntaxes([]string{network.ModalityPerformedProcedureStepUID})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = scp.ListenAndServe(ctx) }()

	addr := waitForSCP(t, scp)

	runPynetdicomSCU(t, `
import sys
from pydicom.dataset import Dataset
from pynetdicom import AE
from pynetdicom.sop_class import ModalityPerformedProcedureStep

ae = AE(ae_title="PY_MPPS")
ae.add_requested_context(ModalityPerformedProcedureStep)
assoc = ae.associate(`+quote(strings.Split(addr, ":")[0])+`, `+strings.Split(addr, ":")[1]+`,
                     ae_title="GO_MPPS_SCP")
if not assoc.is_established:
    sys.exit("association rejected")

instance = "1.2.826.0.1.3680043.10.511.9.2"
ds = Dataset()
ds.PerformedProcedureStepStatus = "IN PROGRESS"
status, _ = assoc.send_n_create(ds, ModalityPerformedProcedureStep, instance)
assert status.Status == 0x0000, "N-CREATE 0x%04X" % status.Status

ds2 = Dataset()
ds2.PerformedProcedureStepStatus = "COMPLETED"
status, _ = assoc.send_n_set(ds2, ModalityPerformedProcedureStep, instance)
assert status.Status == 0x0000, "N-SET 0x%04X" % status.Status
assoc.release()
`)

	for _, want := range []mppsEvent{{"N-CREATE", "IN PROGRESS"}, {"N-SET", "COMPLETED"}} {
		select {
		case got := <-events:
			if got.kind != want.kind || strings.TrimSpace(got.status) != want.status {
				t.Errorf("go-dicom SCP saw %s %q, want %s %q",
					got.kind, strings.TrimSpace(got.status), want.kind, want.status)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("go-dicom SCP never received %s from pynetdicom", want.kind)
		}
	}
}

// TestPrintManagementAgainstPynetdicom runs the basic grayscale print workflow:
// a film session holds a film box, the film box holds an image box, and an
// N-ACTION prints it. It is four SOP classes and four different N-services on
// one association, which is why it is worth running end to end rather than
// asserting the UIDs exist.
func TestPrintManagementAgainstPynetdicom(t *testing.T) {
	record := t.TempDir() + "/events.txt"
	addr := startPynetdicomSCP(t, `
import time
from pydicom.dataset import Dataset
from pynetdicom import AE, evt, ALL_TRANSFER_SYNTAXES
from pynetdicom.sop_class import BasicFilmSession, BasicFilmBox, BasicGrayscaleImageBox

record = open(`+quote(record)+`, "w", buffering=1)

def on_create(event):
    record.write("CREATE %s\n" % event.request.AffectedSOPClassUID)
    return 0x0000, event.attribute_list

def on_set(event):
    record.write("SET %s\n" % event.request.RequestedSOPClassUID)
    return 0x0000, event.modification_list

def on_action(event):
    record.write("ACTION %d\n" % event.request.ActionTypeID)
    return 0x0000, Dataset()

def on_delete(event):
    record.write("DELETE %s\n" % event.request.RequestedSOPClassUID)
    return 0x0000

ae = AE(ae_title="PY_PRINT")
for sop in (BasicFilmSession, BasicFilmBox, BasicGrayscaleImageBox):
    ae.add_supported_context(sop, ALL_TRANSFER_SYNTAXES)
server = ae.start_server(("127.0.0.1", 0), block=False, evt_handlers=[
    (evt.EVT_N_CREATE, on_create), (evt.EVT_N_SET, on_set),
    (evt.EVT_N_ACTION, on_action), (evt.EVT_N_DELETE, on_delete)])
print("PORT %d" % server.server_address[1], flush=True)
time.sleep(120)
`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ts := []string{network.ExplicitVRLittleEndianUID, network.ImplicitVRLittleEndianUID}
	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "GO_PRINT", CalledAE: "PY_PRINT", Address: addr,
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{
		{ID: 1, AbstractSyntax: network.BasicFilmSessionSOPClassUID, TransferSyntaxes: ts},
		{ID: 3, AbstractSyntax: network.BasicFilmBoxSOPClassUID, TransferSyntaxes: ts},
		{ID: 5, AbstractSyntax: network.BasicGrayscaleImageBoxSOPClassUID, TransferSyntaxes: ts},
	}); err != nil {
		t.Fatalf("associate for print management: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	const (
		sessionInstance = "1.2.826.0.1.3680043.10.511.8.1"
		boxInstance     = "1.2.826.0.1.3680043.10.511.8.2"
		imageInstance   = "1.2.826.0.1.3680043.10.511.8.3"
	)

	session := dataset.NewDataset()
	add(t, session, 0x2000, 0x0010, dataelem.IS, "1 ") // number of copies
	if r, err := scu.NCreate(ctx, network.BasicFilmSessionSOPClassUID, sessionInstance, session); err != nil {
		t.Fatalf("film session N-CREATE: %v", err)
	} else if r.Status != network.StatusSuccess {
		t.Fatalf("film session N-CREATE status 0x%04X", r.Status)
	}

	box := dataset.NewDataset()
	add(t, box, 0x2010, 0x0010, dataelem.ST, "STANDARD\\1,1") // image display format
	if r, err := scu.NCreate(ctx, network.BasicFilmBoxSOPClassUID, boxInstance, box); err != nil {
		t.Fatalf("film box N-CREATE: %v", err)
	} else if r.Status != network.StatusSuccess {
		t.Fatalf("film box N-CREATE status 0x%04X", r.Status)
	}

	image := dataset.NewDataset()
	_ = image.Add(dataelem.NewDataElement(tag.New(0x2020, 0x0010), dataelem.US, []byte{0x01, 0x00}))
	if r, err := scu.NSet(ctx, network.BasicGrayscaleImageBoxSOPClassUID, imageInstance, image); err != nil {
		t.Fatalf("image box N-SET: %v", err)
	} else if r.Status != network.StatusSuccess {
		t.Fatalf("image box N-SET status 0x%04X", r.Status)
	}

	if r, err := scu.NAction(ctx, network.BasicFilmBoxSOPClassUID, boxInstance, 1, nil); err != nil {
		t.Fatalf("print N-ACTION: %v", err)
	} else if r.Status != network.StatusSuccess {
		t.Fatalf("print N-ACTION status 0x%04X", r.Status)
	}

	if r, err := scu.NDelete(ctx, network.BasicFilmSessionSOPClassUID, sessionInstance); err != nil {
		t.Fatalf("film session N-DELETE: %v", err)
	} else if r.Status != network.StatusSuccess {
		t.Fatalf("film session N-DELETE status 0x%04X", r.Status)
	}

	want := []string{
		"CREATE " + network.BasicFilmSessionSOPClassUID,
		"CREATE " + network.BasicFilmBoxSOPClassUID,
		"SET " + network.BasicGrayscaleImageBoxSOPClassUID,
		"ACTION 1",
		"DELETE " + network.BasicFilmSessionSOPClassUID,
	}
	got := collectLines(t, record, len(want))
	for i, w := range want {
		if i >= len(got) || strings.TrimSpace(got[i]) != w {
			t.Errorf("print step %d: pynetdicom recorded %q, want %q", i+1, got[i], w)
		}
	}
}

// mppsRecorder serves the two N-services MPPS is built from, which is all an
// MPPS SCP is: there is no dedicated service class, in this library or in
// pynetdicom.
type mppsEvent struct {
	kind   string
	status string
}

type mppsRecorder struct {
	network.BaseHandler
	events chan<- mppsEvent
}

func (h *mppsRecorder) HandleNCreate(_ context.Context, req *network.NCreateRequest) (*network.NCreateResponse, error) {
	h.record("N-CREATE", req.DataSet)
	return &network.NCreateResponse{Status: network.StatusSuccess}, nil
}

func (h *mppsRecorder) HandleNSet(_ context.Context, req *network.NSetRequest) (*network.NSetResponse, error) {
	h.record("N-SET", req.DataSet)
	return &network.NSetResponse{Status: network.StatusSuccess}, nil
}

func (h *mppsRecorder) record(kind string, ds *dataset.Dataset) {
	status := ""
	if ds != nil {
		if elem, ok := ds.Get(tag.New(0x0040, 0x0252)); ok {
			if value, ok := elem.GetValue().([]byte); ok {
				status = string(value)
			}
		}
	}
	h.events <- mppsEvent{kind, status}
}

func add(t *testing.T, ds *dataset.Dataset, group, element uint16, vr dataelem.VR, value string) {
	t.Helper()
	if err := ds.Add(dataelem.NewDataElement(tag.New(group, element), vr, []byte(value))); err != nil {
		t.Fatalf("add (%04X,%04X): %v", group, element, err)
	}
}

// quote renders a Go string as a python string literal.
func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// waitForSCP blocks until the SCP has bound a port and returns its address.
func waitForSCP(t *testing.T, scp *network.SCP) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if addr := scp.Addr(); addr != "" && !strings.HasSuffix(addr, ":0") {
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("go-dicom SCP never bound a port")
	return ""
}

// runPynetdicomSCU runs a python SCU script to completion and fails the test if
// it exits non-zero, so an association pynetdicom rejected cannot pass silently.
func runPynetdicomSCU(t *testing.T, source string) {
	t.Helper()
	requirePynetdicom(t)

	script := t.TempDir() + "/scu.py"
	if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	out, err := exec.Command("python3", script).CombinedOutput()
	if err != nil {
		t.Fatalf("pynetdicom SCU failed: %v\n%s", err, out)
	}
}

// TestUPSAgainstPynetdicom drives a Unified Procedure Step through its whole
// life against a peer that did not learn the state machine from this code.
//
// The Transaction UID lock is the part worth checking against a third party: it
// only works if both sides agree on which attribute carries it and when it is
// required, and a disagreement produces success responses on both sides rather
// than an error.
func TestUPSAgainstPynetdicom(t *testing.T) {
	store := newMemoryUPSStore()
	scp := network.NewSCP(network.SCPConfig{
		AETitle: "GO_UPS_SCP", Port: 0, BindAddress: "127.0.0.1",
	})
	scp.SetHandler(&network.UPSHandler{Store: store})
	scp.SetSupportedAbstractSyntaxes([]string{network.UnifiedProcedureStepPushUID})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = scp.ListenAndServe(ctx) }()

	addr := waitForSCP(t, scp)
	host, port := splitHostPort(t, addr)

	runPynetdicomSCU(t, `
import sys
from pydicom.dataset import Dataset
from pynetdicom import AE
from pynetdicom.sop_class import UnifiedProcedureStepPush

INSTANCE = "1.2.826.0.1.3680043.10.511.4.1"
TXN      = "1.2.826.0.1.3680043.10.511.4.99"
OTHER    = "1.2.826.0.1.3680043.10.511.4.98"

ae = AE(ae_title="PY_UPS")
ae.add_requested_context(UnifiedProcedureStepPush)
assoc = ae.associate(`+quote(host)+`, `+port+`, ae_title="GO_UPS_SCP")
assert assoc.is_established, "association rejected"

def action(state, txn, expect, what):
    ds = Dataset()
    ds.ProcedureStepState = state
    if txn:
        ds.TransactionUID = txn
    status, _ = assoc.send_n_action(ds, 1, UnifiedProcedureStepPush, INSTANCE)
    got = status.Status
    assert got == expect, "%s: got 0x%04X, want 0x%04X" % (what, got, expect)

# Scheduled.
create = Dataset()
create.ProcedureStepState = "SCHEDULED"
create.Modality = "CT"
status, _ = assoc.send_n_create(create, UnifiedProcedureStepPush, INSTANCE)
assert status.Status == 0x0000, "N-CREATE 0x%04X" % status.Status

# Cannot finish without starting.
action("COMPLETED", TXN, 0xC310, "completing a scheduled step")

# Claim it.
action("IN PROGRESS", TXN, 0x0000, "starting the step")

# Another performer cannot finish it.
action("COMPLETED", OTHER, 0xC301, "a second performer completing it")

# Nor can one presenting no Transaction UID.
action("CANCELED", None, 0xC301, "canceling without a Transaction UID")

# The holder can update it.
upd = Dataset()
upd.TransactionUID = TXN
upd.PerformedProcedureStepStartDateTime = "20260730120000"
status, _ = assoc.send_n_set(upd, UnifiedProcedureStepPush, INSTANCE)
assert status.Status == 0x0000, "N-SET 0x%04X" % status.Status

# And finish it.
action("COMPLETED", TXN, 0x0000, "completing the step")

# After which nothing more is allowed.
action("CANCELED", TXN, 0xC311, "canceling a completed step")

assoc.release()
`)

	step, found, _ := store.FindUPS(context.Background(), "1.2.826.0.1.3680043.10.511.4.1")
	if !found {
		t.Fatal("the step pynetdicom created is not in the store")
	}
	if step.State != network.UPSCompleted {
		t.Errorf("the step ended in state %q, want COMPLETED", step.State)
	}
	if step.TransactionUID != "" {
		t.Errorf("a completed step still holds Transaction UID %q", step.TransactionUID)
	}
}
