package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/network"
)

// docs/show-json.md and docs/show-json.schema.json say what `show --json`
// prints, and two other programs read it. Every file of the corpus, the
// output of the command that is run, has to be what the schema says, and
// nothing the schema does not describe may appear in it.
func TestShowJSONOfTheCorpusIsWhatTheSchemaSays(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	schema := loadSchema(t, "show-json.schema.json")
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	checked := 0
	for _, path := range paths {
		var out bytes.Buffer
		if err := writeShowJSON(&out, path); err != nil {
			continue
		}
		var document any
		if err := decodeJSON(out.Bytes(), &document); err != nil {
			t.Errorf("%s: not JSON: %v", filepath.Base(path), err)
			continue
		}
		if problems := schema.check(document, true); len(problems) > 0 {
			t.Errorf("%s does not match the schema:\n%s", filepath.Base(path), strings.Join(problems[:min(len(problems), 5)], "\n"))
		}
		checked++
	}
	if checked < 60 {
		t.Errorf("%d files were checked", checked)
	}
}

// The schema says schemaVersion is 1, and the program says what it prints.
func TestShowJSONSchemaVersionIsTheOneDocumented(t *testing.T) {
	schema := loadSchema(t, "show-json.schema.json")
	properties, _ := schema.root["properties"].(map[string]any)
	version, _ := properties["schemaVersion"].(map[string]any)
	if got := version["const"]; got == nil || got.(interface{ String() string }).String() != "1" || showSchemaVersion != 1 {
		t.Errorf("the schema says schemaVersion %v and the command prints %d", got, showSchemaVersion)
	}
}

// docs/events.md and docs/events.schema.json say what each line of the
// -events log is. The lines here are those of real associations over TCP,
// written by the writer the commands use: a store, an echo and a query that
// succeed, an association that is refused, and what a server logs of each.
func TestEventLogLinesAreWhatTheSchemaSays(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The server logs to its own writer, as a storescp or qrscp with -events does.
	var serverLog syncBuffer
	serverEvents := network.NewEventManager()
	serverEvents.OnAny((&eventWriter{out: &serverLog, maxBytes: 65536}).event)
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1", Events: serverEvents,
	}, dcmstore.NewHandler(store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	server.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())
	server.SetSupportedTransferSyntaxes(dcmstore.SupportedTransferSyntaxes())
	addr := server.Addr()

	dir := t.TempDir()
	path := writeInstance(t, dir, "ct.dcm", "1.2.840.10008.5.1.4.1.1.2", "1.2.840.9999.16.1", "1.2.840.10008.1.2.1", make([]byte, 40000))
	log := filepath.Join(dir, "client.jsonl")

	// Truncated PDUs in one run, whole in the next.
	for _, run := range [][]string{
		{"-aec", "ARCHIVE", "-events", log, "-events-pdu-bytes", "64", addr, path},
		{"-aec", "ARCHIVE", "-events", log, addr, path},
	} {
		if err := (&StoreSCUCommand{}).Execute(run); err != nil {
			t.Fatal(err)
		}
	}
	if err := (&EchoSCUCommand{}).Execute([]string{"-aec", "ARCHIVE", "-events", log, addr}); err != nil {
		t.Fatal(err)
	}
	if err := (&FindSCUCommand{}).Execute([]string{"-aec", "ARCHIVE", "-events", log, "-patient-name", "*", addr}); err != nil {
		t.Fatal(err)
	}
	// An association the server refuses: the called AE title is not its own.
	if err := (&EchoSCUCommand{}).Execute([]string{"-aec", "NOBODY", "-events", log, addr}); err == nil {
		t.Fatal("an association to the wrong AE title was accepted")
	}

	// The server writes what it saw of the last association after the client
	// has returned; five were made.
	for deadline := time.Now().Add(10 * time.Second); strings.Count(serverLog.String(), `"EVT_CONN_CLOSE"`) < 5 && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}

	// The command's own line, and an abort, which no happy path produces.
	var other bytes.Buffer
	flagged := eventsFlag{writer: &eventWriter{out: &other}}
	flagged.stored(network.ContextWithAssociationInfo(context.Background(), &network.AssociationInfo{ID: 3, CallingAE: "A", CalledAE: "B"}),
		"1.2.840.10008.5.1.4.1.1.2", "1.2.840.9999.16.1", "/data/1.2.840.9999.16.1.dcm")
	flagged.stored(context.Background(), "1.2.840.10008.5.1.4.1.1.2", "1.2.840.9999.16.1", "x.dcm")
	abort := network.NewEventManager()
	abort.OnAny(flagged.writer.event)
	abort.Emit(&network.Event{Type: network.EVTAssocAborted, PDU: &network.AbortPDU{Source: 2, Reason: 1},
		Detail: &network.EventDetail{PDUBytes: []byte{7, 0, 0, 0, 0, 4, 0, 0, 2, 1}}})

	schema := loadSchema(t, "events.schema.json")
	seen := map[string]bool{}
	lines := 0
	for name, text := range map[string]string{
		"the client's log": readFileText(t, log), "the server's log": serverLog.String(), "the command's own lines": other.String(),
	} {
		scanner := bufio.NewScanner(strings.NewReader(text))
		scanner.Buffer(make([]byte, 1<<20), 1<<26)
		for scanner.Scan() {
			var line any
			if err := decodeJSON(scanner.Bytes(), &line); err != nil {
				t.Fatalf("%s has a line that is not JSON: %v\n%s", name, err, scanner.Text())
			}
			if problems := schema.check(line, true); len(problems) > 0 {
				t.Errorf("%s has a line that does not match the schema:\n%s\n%.300s", name, strings.Join(problems[:min(len(problems), 5)], "\n"), scanner.Text())
			}
			// The schema allows a line without it, as 1.10.0 wrote; the
			// writer puts it on every line, last.
			if !strings.HasSuffix(scanner.Text(), `,"schemaVersion":1}`) {
				t.Errorf("%s has a line that does not end with its schemaVersion:\n%.300s", name, scanner.Text())
			}
			seen[line.(map[string]any)["event"].(string)] = true
			lines++
		}
	}

	// A check of a log that held nothing worth checking would pass anything.
	for _, event := range []string{
		"EVT_CONN_OPEN", "EVT_CONN_CLOSE", "EVT_REQUESTED", "EVT_ACCEPTED", "EVT_ESTABLISHED", "EVT_REJECTED",
		"EVT_RELEASED", "EVT_ABORTED", "EVT_PDU_SENT", "EVT_PDU_RECV", "EVT_DIMSE_SENT", "EVT_DIMSE_RECV",
		"EVT_C_STORE", "EVT_C_ECHO", "EVT_C_FIND", "STORED",
	} {
		if !seen[event] {
			t.Errorf("no %s line was among the %d checked", event, lines)
		}
	}
}

// TestEventLogSchemaVersionIsTheOneDocumented: the schema says
// schemaVersion is 1, and the writer says what it puts on each line.
func TestEventLogSchemaVersionIsTheOneDocumented(t *testing.T) {
	schema := loadSchema(t, "events.schema.json")
	properties, _ := schema.root["properties"].(map[string]any)
	version, _ := properties["schemaVersion"].(map[string]any)
	if got := version["const"]; got == nil || got.(interface{ String() string }).String() != "1" || eventsSchemaVersion != 1 {
		t.Errorf("the schema says schemaVersion %v and the writer writes %d", got, eventsSchemaVersion)
	}
	if required, _ := schema.root["required"].([]any); len(required) != 2 {
		t.Errorf("required is %v; a line of go-dicom 1.10.0, without schemaVersion, is version 1 and must still match", required)
	}
}

// TestEventLogLineIsTheOldLineWithItsVersionAdded proves that adding
// schemaVersion changed nothing else: a line is, byte for byte, the line
// go-dicom 1.10.0 wrote for the same event, with ,"schemaVersion":1 before
// its closing brace. A reader that matched on a line's start or on any
// property sees what it saw.
func TestEventLogLineIsTheOldLineWithItsVersionAdded(t *testing.T) {
	var out bytes.Buffer
	w := &eventWriter{out: &out, maxBytes: 4}
	w.event(&network.Event{
		Type: network.EVTAssocAborted, Timestamp: time.Date(2026, 11, 18, 19, 14, 7, 82782000, time.UTC),
		AssociationID: 2, Role: "SCU", LocalAE: "ECHOSCU", RemoteAE: "ECHOSCP",
		LocalAddr: "127.0.0.1:65005", RemoteAddr: "127.0.0.1:41296",
		PDU:    &network.AbortPDU{Source: 2, Reason: 1},
		Detail: &network.EventDetail{PDUBytes: []byte{7, 0, 0, 0, 0, 4, 0, 0, 2, 1}},
	})
	// What go-dicom 1.10.0 wrote for this event.
	const old = `{"event":"EVT_ABORTED","time":"2026-11-18T19:14:07.082782Z","associationId":2,"role":"SCU",` +
		`"localAE":"ECHOSCU","remoteAE":"ECHOSCP","localAddr":"127.0.0.1:65005","remoteAddr":"127.0.0.1:41296",` +
		`"pdu":{"type":"A-ABORT","typeCode":7,"length":4,"bytes":"07000000","bytesTruncated":true,"source":2,"reason":1}}`
	want := strings.TrimSuffix(old, "}") + `,"schemaVersion":1}` + "\n"
	if out.String() != want {
		t.Errorf("the line is\n%s\nwant\n%s", out.String(), want)
	}
}

// syncBuffer is a buffer a server's goroutines write to while a test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func readFileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
