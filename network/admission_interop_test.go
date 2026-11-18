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

// TestPynetdicomSeesTheChosenRefusal: pynetdicom receives the A-ASSOCIATE-RJ
// with the result, source and reason the admission function chose, names each
// as the standard does, and establishes no association.
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
# What is checked is the PDU pynetdicom received and that no association
# came of it. Its own is_rejected flag is not: on a loaded machine pynetdicom
# 2.1 was seen to hold the A-ASSOCIATE-RJ and leave that flag False for good,
# about once in sixty runs, which is between its DUL and ACSE threads and
# nothing this SCP sends decides (#430).
print("established=%s result=%d source=%d reason=%d" % (assoc.is_established, rj.result, rj.source, rj.reason_diagnostic))
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
		wantNumbers := "established=False result=" + itoa(r.Result) + " source=" + itoa(r.Source) + " reason=" + itoa(r.Reason)
		wantWords := c.pyResult + "|" + c.pySource + "|" + c.pyWhy
		if len(lines) < 2 || lines[len(lines)-2] != wantNumbers || !strings.HasPrefix(lines[len(lines)-1], wantWords) {
			t.Errorf("refused with %s, pynetdicom reported:\n%s\nwant:\n%s\n%s", r, out, wantNumbers, wantWords)
			continue
		}
		t.Logf("refused with %s, pynetdicom reported: %s", r, lines[len(lines)-1])
	}
}

func itoa(b byte) string { return strconv.Itoa(int(b)) }

// TestDcmtkIsRefusedAnotherCalledAE: an admission function that admits
// everyone does not make the SCP answer to every title. echoscu asking for
// the SCP's own title is admitted and its C-ECHO answered; asking for another
// it is told the called AE title is not recognized, and with
// AcceptAnyCalledAE it is admitted under that title too.
func TestDcmtkIsRefusedAnotherCalledAE(t *testing.T) {
	echoscu := dcmtkTool(t, "echoscu")
	admitAll := func(context.Context, *network.AssociationRequest) *network.AssociationRejection { return nil }
	for _, c := range []struct {
		anyCalled bool
		called    string
		admitted  bool
	}{
		{false, "ARCHIVE", true},
		{false, "OTHER", false},
		{true, "OTHER", true},
	} {
		server, err := network.StartServer(context.Background(), network.SCPConfig{AETitle: "ARCHIVE", Port: 0,
			BindAddress: "127.0.0.1", Admit: admitAll, AcceptAnyCalledAE: c.anyCalled}, &network.EchoHandler{})
		if err != nil {
			t.Fatal(err)
		}
		host, port, _ := strings.Cut(server.Addr(), ":")
		out, err := exec.Command(echoscu, "-v", "-aet", "SCANNER", "-aec", c.called, host, port).CombinedOutput()
		server.Stop()
		text := string(out)
		if c.admitted && (err != nil || !strings.Contains(text, "Received Echo Response (Success)")) {
			t.Errorf("AcceptAnyCalledAE %v, called %s: echoscu was not answered: %v\n%s", c.anyCalled, c.called, err, text)
		}
		if !c.admitted && (err == nil || !strings.Contains(text, "Reason: Called AE Title Not Recognized")) {
			t.Errorf("AcceptAnyCalledAE %v, called %s: echoscu printed:\n%s", c.anyCalled, c.called, text)
		}
	}
}
