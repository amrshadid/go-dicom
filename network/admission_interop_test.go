package network_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/network"
)

// A refusal is only worth choosing if the peer reads it as chosen. These run
// dcmtk and pynetdicom against an SCP whose admission function refuses, and
// read the result, source and reason from what each of them reports.

// dcmtkTool returns the path of a dcmtk program: from DCMTK_BIN when it is
// set, otherwise from PATH. pynetdicom's console scripts share dcmtk's names,
// so a program found on PATH is checked to be dcmtk's. Absent, the test is
// skipped, or fails where GODICOM_REQUIRE_INTEROP is set.
func dcmtkTool(t *testing.T, name string) string {
	t.Helper()
	path := name
	if dir := os.Getenv("DCMTK_BIN"); dir != "" {
		path = filepath.Join(dir, name)
	}
	resolved, err := exec.LookPath(path)
	if err == nil {
		out, _ := exec.Command(resolved, "--version").CombinedOutput()
		if !strings.Contains(string(out), "dcmtk") {
			err = &exec.Error{Name: resolved, Err: exec.ErrNotFound}
		}
	}
	if err == nil {
		return resolved
	}
	if os.Getenv("GODICOM_REQUIRE_INTEROP") != "" {
		t.Fatalf("GODICOM_REQUIRE_INTEROP is set but dcmtk's %s is not available: %v", name, err)
	}
	t.Skipf("dcmtk's %s not available (%v); skipping third-party interoperability check", name, err)
	return ""
}

// refusingServer starts an SCP that refuses every caller with r.
func refusingServer(t *testing.T, r network.AssociationRejection) (host, port string) {
	t.Helper()
	server, err := network.StartServer(context.Background(), network.SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1",
		Admit: func(context.Context, *network.AssociationRequest) *network.AssociationRejection {
			return &r
		},
	}, &network.EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	host, port, _ = strings.Cut(server.Addr(), ":")
	return host, port
}

// The refusals the peers are shown, with the words dcmtk and pynetdicom print
// for each part.
var refusalsForPeers = []struct {
	rejection                 network.AssociationRejection
	dcmtkResult, dcmtkReason  string
	pyResult, pySource, pyWhy string // pyWhy is how pynetdicom's reason begins
}{
	{*network.RejectCallingAE(), "Rejected Permanent, Source: Service User", "Calling AE Title Not Recognized",
		"Rejected (Permanent)", "DUL service-user", "Calling AE title not recogni"},
	{*network.RejectCalledAE(), "Rejected Permanent, Source: Service User", "Called AE Title Not Recognized",
		"Rejected (Permanent)", "DUL service-user", "Called AE title not recogni"},
	{*network.RejectBusy(), "Rejected Transient, Source: Service Provider (Presentation Related)", "Temporary Congestion",
		"Rejected (Transient)", "DUL service-provider (presentation related)", "Temporary congestion"},
}

// TestDcmtkSeesTheChosenRefusal: echoscu and storescu each print the result,
// source and reason the admission function chose.
func TestDcmtkSeesTheChosenRefusal(t *testing.T) {
	echoscu := dcmtkTool(t, "echoscu")
	storescu := dcmtkTool(t, "storescu")
	dump2dcm := dcmtkTool(t, "dump2dcm")

	// A file for storescu to offer, written by dcmtk itself. It is never
	// sent: the association is refused first.
	dir := t.TempDir()
	dump := filepath.Join(dir, "sc.txt")
	file := filepath.Join(dir, "sc.dcm")
	if err := os.WriteFile(dump, []byte(
		"(0008,0016) UI =SecondaryCaptureImageStorage\n"+
			"(0008,0018) UI [1.2.826.0.1.3680043.10.511.316.1]\n"+
			"(0010,0020) LO [SYNTHETIC]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(dump2dcm, "+te", dump, file).CombinedOutput(); err != nil {
		t.Fatalf("dump2dcm: %v\n%s", err, out)
	}

	for _, c := range refusalsForPeers {
		host, port := refusingServer(t, c.rejection)
		for name, args := range map[string][]string{
			"echoscu":  {echoscu, "-aet", "SCANNER", "-aec", "ARCHIVE", host, port},
			"storescu": {storescu, "-aet", "SCANNER", "-aec", "ARCHIVE", host, port, file},
		} {
			out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
			if err == nil {
				t.Errorf("%s exited 0 against a refusing SCP:\n%s", name, out)
			}
			text := string(out)
			if !strings.Contains(text, "Result: "+c.dcmtkResult) || !strings.Contains(text, "Reason: "+c.dcmtkReason) {
				t.Errorf("%s, refused with %s, printed:\n%s", name, c.rejection, text)
				continue
			}
			t.Logf("%s, refused with %s:\n%s", name, c.rejection, strings.TrimSpace(text))
		}
	}
}

// TestPynetdicomSeesTheChosenRefusal: pynetdicom reports the association
// rejected, with the result, source and reason the admission function chose.
func TestPynetdicomSeesTheChosenRefusal(t *testing.T) {
	requirePynetdicom(t)
	script := filepath.Join(t.TempDir(), "associate.py")
	if err := os.WriteFile(script, []byte(`
import sys
from pynetdicom import AE, evt
from pynetdicom.pdu import A_ASSOCIATE_RJ
from pynetdicom.sop_class import Verification

seen = []
def on_pdu(event):
    if isinstance(event.pdu, A_ASSOCIATE_RJ):
        seen.append(event.pdu)

ae = AE(ae_title="SCANNER")
ae.add_requested_context(Verification)
assoc = ae.associate(sys.argv[1], int(sys.argv[2]), ae_title="ARCHIVE",
                     evt_handlers=[(evt.EVT_PDU_RECV, on_pdu)])
rj = seen[0]
print("rejected=%s result=%d source=%d reason=%d" % (assoc.is_rejected, rj.result, rj.source, rj.reason_diagnostic))
print("%s|%s|%s" % (rj.result_str, rj.source_str, rj.reason_str))
`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, c := range refusalsForPeers {
		host, port := refusingServer(t, c.rejection)
		out, err := exec.Command("python3", script, host, port).CombinedOutput()
		if err != nil {
			t.Fatalf("pynetdicom: %v\n%s", err, out)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		r := c.rejection
		wantNumbers := "rejected=True result=" + itoa(r.Result) + " source=" + itoa(r.Source) + " reason=" + itoa(r.Reason)
		wantWords := c.pyResult + "|" + c.pySource + "|" + c.pyWhy
		if len(lines) < 2 || lines[len(lines)-2] != wantNumbers || !strings.HasPrefix(lines[len(lines)-1], wantWords) {
			t.Errorf("refused with %s, pynetdicom reported:\n%s\nwant:\n%s\n%s", r, out, wantNumbers, wantWords)
			continue
		}
		t.Logf("refused with %s, pynetdicom reported: %s", r, lines[len(lines)-1])
	}
}

func itoa(b byte) string { return strconv.Itoa(int(b)) }
