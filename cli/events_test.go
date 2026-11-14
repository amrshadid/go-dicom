package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/network"
)

// No command could say what happened on an association (#260).

// readEvents reads a log back, one object a line.
func readEvents(t *testing.T, path string) []eventRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []eventRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	for scanner.Scan() {
		var record eventRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("a line of the log is not JSON: %v\n%s", err, scanner.Text())
		}
		out = append(out, record)
	}
	return out
}

func eventsNamed(records []eventRecord, name string) []eventRecord {
	var out []eventRecord
	for _, r := range records {
		if r.Event == name {
			out = append(out, r)
		}
	}
	return out
}

func TestStoreSCUWritesAnEventLog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, addr := startArchive(t, ctx, dcmstore.SupportedTransferSyntaxes())

	dir := t.TempDir()
	path := writeInstance(t, dir, "ct.dcm", "1.2.840.10008.5.1.4.1.1.2", "1.2.260.7", "1.2.840.10008.1.2.1", make([]byte, 40000))
	log := filepath.Join(dir, "events.jsonl")

	args := []string{"-aet", "LOGSCU", "-aec", "ARCHIVE", "-events", log, "-events-pdu-bytes", "64", addr, path}
	if err := (&StoreSCUCommand{}).Execute(args); err != nil {
		t.Fatal(err)
	}
	// Appended to, not replaced.
	if err := (&StoreSCUCommand{}).Execute(args); err != nil {
		t.Fatal(err)
	}
	records := readEvents(t, log)

	requested := eventsNamed(records, "EVT_REQUESTED")
	if len(requested) != 2 || requested[0].AssociationID == requested[1].AssociationID {
		t.Fatalf("%d associations were logged, want two with their own ids", len(requested))
	}
	rq := requested[0]
	if rq.Role != "SCU" || rq.LocalAE != "LOGSCU" || rq.RemoteAE != "ARCHIVE" || rq.RemoteAddr != addr ||
		rq.LocalAddr == "" || !strings.HasSuffix(rq.Time, "Z") || !strings.Contains(rq.Time, ".") {
		t.Errorf("the request is %+v", rq)
	}
	if rq.PDU == nil || rq.PDU.Type != "A-ASSOCIATE-RQ" || rq.PDU.MaxPDULength == nil ||
		len(rq.PDU.Proposed) == 0 || rq.PDU.Proposed[0].AbstractSyntax != "1.2.840.10008.5.1.4.1.1.2" {
		t.Errorf("the request's PDU is %+v", rq.PDU)
	}
	// The first bytes of an A-ASSOCIATE-RQ: type 1, a reserved byte, its length.
	raw, err := hex.DecodeString(rq.PDU.Bytes)
	if err != nil || len(raw) != 64 || raw[0] != 0x01 || !rq.PDU.BytesTruncated {
		t.Errorf("the request's bytes are %q (%v)", rq.PDU.Bytes, err)
	}

	accepted := eventsNamed(records, "EVT_ACCEPTED")[0]
	if len(accepted.Contexts) == 0 || accepted.Contexts[0].ResultText != "acceptance" ||
		accepted.Contexts[0].TransferSyntax == "" || len(accepted.Contexts[0].TransferSyntaxes) == 0 {
		t.Errorf("the accepted contexts are %+v", accepted.Contexts)
	}

	var store, response *dimseRecord
	for _, r := range records {
		if r.Event == "EVT_DIMSE_SENT" && store == nil {
			store = r.DIMSE
		}
		if r.Event == "EVT_DIMSE_RECV" && response == nil {
			response = r.DIMSE
		}
	}
	if store == nil || store.Command != "C-STORE-RQ" || store.AffectedSOPInstanceUID != "1.2.260.7" ||
		store.MessageID == nil || store.Status != nil || !store.HasDataSet {
		t.Errorf("the request is %+v", store)
	}
	if response == nil || response.Command != "C-STORE-RSP" || response.Status == nil || *response.Status != 0 ||
		response.MessageIDBeingRespondedTo == nil || *response.MessageIDBeingRespondedTo != *store.MessageID {
		t.Errorf("the response is %+v", response)
	}

	// The data set crossed in several PDUs, each saying what it carried.
	fragments := 0
	for _, r := range eventsNamed(records, "EVT_PDU_SENT") {
		if r.PDU.Type == "P-DATA-TF" && len(r.PDU.PDVs) == 1 && !r.PDU.PDVs[0].Command {
			fragments++
		}
	}
	if fragments < 4 {
		t.Errorf("%d data PDUs were logged for two 40000-byte data sets", fragments)
	}
	if len(eventsNamed(records, "EVT_RELEASED")) != 2 || len(eventsNamed(records, "EVT_CONN_CLOSE")) != 2 {
		t.Errorf("the associations' ends are not both logged")
	}
}

func TestEventsToStandardOutputAndNowhere(t *testing.T) {
	// No -events: no manager, and nothing to close.
	var none eventsFlag
	manager, done, err := none.open()
	if err != nil || manager != nil {
		t.Fatalf("with no -events: %v, %v", manager, err)
	}
	done()
	none.stored("1.2", "3.4", "x")

	var out bytes.Buffer
	writer := &eventWriter{out: &out}
	events := network.NewEventManager()
	events.OnAny(writer.event)
	events.Emit(&network.Event{Type: network.EVTAssocAborted, PDU: &network.AbortPDU{Source: 2, Reason: 1},
		PDUBytes: []byte{7, 0, 0, 0, 0, 4, 0, 0, 2, 1}})
	var record eventRecord
	if err := json.Unmarshal(out.Bytes(), &record); err != nil || !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("not one JSON line: %v %q", err, out.String())
	}
	if record.Event != "EVT_ABORTED" || record.PDU.Type != "A-ABORT" || record.PDU.Length != 4 ||
		*record.PDU.Source != 2 || *record.PDU.Reason != 1 || record.PDU.Bytes != "07000000000400000201" {
		t.Errorf("the abort is %+v, PDU %+v", record, record.PDU)
	}
}

// Every network command takes the option.
func TestEveryNetworkCommandTakesEvents(t *testing.T) {
	for _, cmd := range allCommands() {
		listed := false
		for _, name := range netCommands {
			listed = listed || name == cmd.Name()
		}
		if !listed {
			continue
		}
		fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
		cmd.AddFlags(fs)
		if fs.Lookup("events") == nil || fs.Lookup("events-pdu-bytes") == nil {
			t.Errorf("%s has no -events", cmd.Name())
		}
	}
}
