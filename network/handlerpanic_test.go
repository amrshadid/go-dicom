package network

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// logBuffer is a log output read while the server still writes to it.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// logTo sends what the package logs to the buffer it returns, for the length
// of the test.
func logTo(t *testing.T) *logBuffer {
	t.Helper()
	log := &logBuffer{}
	previous := DefaultLogger
	DefaultLogger = NewLogger(LogLevelWarn, log)
	t.Cleanup(func() { DefaultLogger = previous })
	return log
}

// faultyHandler answers C-ECHO, and panics in the service named by where.
type faultyHandler struct {
	BaseHandler
	where string
}

func (h *faultyHandler) fault(service string) {
	if h.where == service {
		panic("the index is nil in " + service)
	}
}

func (h *faultyHandler) HandleCEcho(ctx context.Context, req *CEchoRequest) (*CEchoResponse, error) {
	if info := AssociationInfoFromContext(ctx); info != nil && info.CallingAE == "FAULTY" {
		h.fault("C-ECHO")
	}
	return h.BaseHandler.HandleCEcho(ctx, req)
}

func (h *faultyHandler) HandleCFind(context.Context, *CFindRequest) ([]*CFindResponse, error) {
	h.fault("C-FIND")
	return nil, nil
}

// faultyStreamer adds the streamed services, whose handlers run on a
// goroutine of their own.
type faultyStreamer struct{ faultyHandler }

func (h *faultyStreamer) StreamCFind(context.Context, *CFindRequest, chan<- *CFindResponse) error {
	h.fault("streamed C-FIND")
	return nil
}

func (h *faultyStreamer) CountCGetMatches(context.Context, *CGetRequest) (int, error) { return 1, nil }

func (h *faultyStreamer) StreamCGet(context.Context, *CGetRequest, chan<- *dataset.Dataset) error {
	h.fault("streamed C-GET")
	return nil
}

func (h *faultyStreamer) CountCMoveMatches(context.Context, *CMoveRequest) (int, error) {
	return 1, nil
}

func (h *faultyStreamer) StreamCMove(context.Context, *CMoveRequest, chan<- *dataset.Dataset) error {
	h.fault("streamed C-MOVE")
	return nil
}

// TestHandlerPanicEndsOnlyItsAssociation: nothing in the package recovered
// from a panic, so one in any handler ended the process: every association
// in progress, and the listener. It now ends the association it happened on,
// with an A-ABORT, and is logged with its stack. An association open beside
// it goes on, its slot is given back, and the next caller is served.
func TestHandlerPanicEndsOnlyItsAssociation(t *testing.T) {
	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")))
	retrieve := func(abstractSyntax string) []PresentationContextItem {
		return []PresentationContextItem{{ID: 1, AbstractSyntax: abstractSyntax,
			TransferSyntaxes: []string{ExplicitVRLittleEndianUID}}}
	}
	find := func(ctx context.Context, scu *SCU) error {
		results, err := scu.Find(ctx, query)
		if err != nil {
			return err
		}
		for r := range results {
			if r.Err != nil {
				err = r.Err
			}
		}
		return err
	}

	for _, c := range []struct {
		service  string
		handler  func(where string) Handler
		contexts []PresentationContextItem
		run      func(ctx context.Context, scu *SCU) error
	}{
		{"C-ECHO", func(w string) Handler { return &faultyHandler{where: w} }, nil,
			func(ctx context.Context, scu *SCU) error { return scu.Echo(ctx) }},
		{"C-FIND", func(w string) Handler { return &faultyHandler{where: w} }, nil, find},
		{"streamed C-FIND", func(w string) Handler { return &faultyStreamer{faultyHandler{where: w}} }, nil, find},
		{"streamed C-GET", func(w string) Handler { return &faultyStreamer{faultyHandler{where: w}} },
			retrieve(PatientRootQueryRetrieveGet),
			func(ctx context.Context, scu *SCU) error { return scu.Get(ctx, query) }},
		{"streamed C-MOVE", func(w string) Handler { return &faultyStreamer{faultyHandler{where: w}} },
			retrieve(PatientRootQueryRetrieveMove),
			func(ctx context.Context, scu *SCU) error { return scu.Move(ctx, query, "ELSEWHERE") }},
	} {
		t.Run(c.service, func(t *testing.T) {
			log := logTo(t)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			server, err := StartServer(ctx, SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1",
				MaxAssociations: 2,
				// Never dialed: the handler panics before an instance is sent.
				MoveDestinations: map[string]string{"ELSEWHERE": "127.0.0.1:1"}}, c.handler(c.service))
			if err != nil {
				t.Fatal(err)
			}
			defer server.Stop()
			associate := func(calling string, contexts []PresentationContextItem) *SCU {
				t.Helper()
				scu := NewSCU(SCUConfig{CallingAE: calling, CalledAE: "ARCHIVE", Address: server.Addr()})
				if err := scu.Associate(ctx, contexts); err != nil {
					t.Fatalf("%s: %v", calling, err)
				}
				return scu
			}

			steady := associate("STEADY", nil)
			if err := steady.Echo(ctx); err != nil {
				t.Fatal(err)
			}

			faulty := associate("FAULTY", c.contexts)
			opCtx, cancelOp := context.WithTimeout(ctx, 5*time.Second)
			err = c.run(opCtx, faulty)
			leftWaiting := opCtx.Err() != nil
			cancelOp()
			if err == nil {
				t.Fatalf("the %s whose handler panicked was answered as though it had not", c.service)
			}
			if leftWaiting {
				t.Fatalf("the peer was left waiting 5s for an answer to its %s: %v", c.service, err)
			}
			if !strings.Contains(err.Error(), "ABORTED") {
				t.Errorf("the peer was not sent an A-ABORT: %v", err)
			}
			t.Logf("the peer was told: %v", err)

			// The association beside it, and the server, go on.
			if err := steady.Echo(ctx); err != nil {
				t.Errorf("the association open beside the panic: %v", err)
			}
			_ = steady.Release(ctx)
			// Both slots are free again: two more callers are taken.
			for _, calling := range []string{"NEXT", "AFTER"} {
				scu := associate(calling, nil)
				if err := scu.Echo(ctx); err != nil {
					t.Errorf("%s: %v", calling, err)
				}
				defer func() { _ = scu.Release(ctx) }()
			}

			text := log.String()
			if !strings.Contains(text, "panic") || !strings.Contains(text, "the index is nil in "+c.service) {
				t.Errorf("the panic was not logged with what it said:\n%s", text)
			}
			if !strings.Contains(text, "handlerpanic_test.go") {
				t.Errorf("the log does not say where the handler panicked:\n%s", text)
			}
		})
	}
}

// TestEventHandlerPanicDoesNotStopTheServer: an event handler is called from
// the accept loop and from each association, so a panic in one ended the
// process too. It is logged, the handlers registered after it are still
// called, and the association it was reporting on is served.
func TestEventHandlerPanicDoesNotStopTheServer(t *testing.T) {
	log := logTo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	events := NewEventManager()
	var mu sync.Mutex
	seen := map[EventType]int{}
	events.OnAny(func(e *Event) { panic("the metrics client is nil, on " + EventTypeString(e.Type)) })
	events.OnAny(func(e *Event) {
		mu.Lock()
		seen[e.Type]++
		mu.Unlock()
	})
	server, err := StartServer(ctx, SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1", Events: events}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	for i := 0; i < 2; i++ {
		scu := NewSCU(SCUConfig{CallingAE: "SCANNER", CalledAE: "ARCHIVE", Address: server.Addr()})
		if err := scu.Associate(ctx, nil); err != nil {
			t.Fatalf("association %d: %v", i, err)
		}
		if err := scu.Echo(ctx); err != nil {
			t.Fatalf("association %d: %v", i, err)
		}
		_ = scu.Release(ctx)
	}
	mu.Lock()
	accepted := seen[EVTAssocAccepted]
	mu.Unlock()
	if accepted != 2 {
		t.Errorf("the handler after the one that panics saw %d associations accepted, want 2", accepted)
	}
	if !strings.Contains(log.String(), "the metrics client is nil") {
		t.Errorf("the event handler's panic was not logged:\n%s", log.String())
	}
}
