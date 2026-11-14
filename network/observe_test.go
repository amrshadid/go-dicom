package network

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The EVT_* set was declared and almost none of it was emitted: an SCP said
// when a connection opened and closed, and an SCU said nothing (#260).

// eventLog collects events as they are emitted.
type eventLog struct {
	mu     sync.Mutex
	events []*Event
}

func newEventLog() (*eventLog, *EventManager) {
	log := &eventLog{}
	manager := NewEventManager()
	manager.OnAny(func(e *Event) {
		log.mu.Lock()
		defer log.mu.Unlock()
		copied := *e
		log.events = append(log.events, &copied)
	})
	return log, manager
}

func (l *eventLog) names() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.events))
	for i, e := range l.events {
		out[i] = EventTypeString(e.Type)
	}
	return out
}

func (l *eventLog) all() []*Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*Event(nil), l.events...)
}

// first returns the first event of a type.
func (l *eventLog) first(t *testing.T, eventType EventType) *Event {
	t.Helper()
	for _, e := range l.all() {
		if e.Type == eventType {
			return e
		}
	}
	t.Fatalf("no %s among %v", EventTypeString(eventType), l.names())
	return nil
}

// waitFor waits until the log holds an event of a type.
func (l *eventLog) waitFor(t *testing.T, eventType EventType) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range l.all() {
			if e.Type == eventType {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s within 5s; have %v", EventTypeString(eventType), l.names())
}

func TestEventsOfAnEcho(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	scpLog, scpEvents := newEventLog()
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "EVT_SCP", Port: 0, BindAddress: "127.0.0.1", Events: scpEvents,
	}, &EchoHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	if server.Events != scpEvents {
		t.Error("the server does not report to the manager it was configured with")
	}

	scuLog, scuEvents := newEventLog()
	scu := NewSCU(SCUConfig{CallingAE: "EVT_SCU", CalledAE: "EVT_SCP", Address: server.Addr(), Events: scuEvents})
	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	if err := scu.Echo(ctx); err != nil {
		t.Fatalf("Echo: %v", err)
	}
	if err := scu.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	scuLog.waitFor(t, EVTConnClose)
	scpLog.waitFor(t, EVTConnClose)

	want := "EVT_CONN_OPEN EVT_PDU_SENT EVT_REQUESTED EVT_PDU_RECV EVT_ACCEPTED EVT_ESTABLISHED " +
		"EVT_PDU_SENT EVT_DIMSE_SENT EVT_PDU_RECV EVT_DIMSE_RECV " +
		"EVT_PDU_SENT EVT_PDU_RECV EVT_RELEASED EVT_CONN_CLOSE"
	if got := strings.Join(scuLog.names(), " "); got != want {
		t.Errorf("the SCU saw\n  %s\nwant\n  %s", got, want)
	}
	want = "EVT_CONN_OPEN EVT_PDU_RECV EVT_REQUESTED EVT_PDU_SENT EVT_ACCEPTED EVT_ESTABLISHED " +
		"EVT_PDU_RECV EVT_DIMSE_RECV EVT_C_ECHO EVT_PDU_SENT EVT_DIMSE_SENT " +
		"EVT_PDU_RECV EVT_PDU_SENT EVT_RELEASED EVT_CONN_CLOSE"
	// The server-started event is the server's own, of no connection.
	var scpNames []string
	for _, e := range scpLog.all() {
		if e.AssociationID != 0 {
			scpNames = append(scpNames, EventTypeString(e.Type))
		}
	}
	if got := strings.Join(scpNames, " "); got != want {
		t.Errorf("the SCP saw\n  %s\nwant\n  %s", got, want)
	}

	// Who is who, on each side.
	established := scuLog.first(t, EVTAssocEstablished)
	if established.Role != "SCU" || established.LocalAE != "EVT_SCU" || established.RemoteAE != "EVT_SCP" ||
		established.AssociationID == 0 || established.RemoteAddr != server.Addr() || established.LocalAddr == "" {
		t.Errorf("the SCU's association is %+v", established)
	}
	accepted := scpLog.first(t, EVTAssocAccepted)
	if accepted.Role != "SCP" || accepted.LocalAE != "EVT_SCP" || accepted.RemoteAE != "EVT_SCU" ||
		accepted.RemoteAddr != established.LocalAddr {
		t.Errorf("the SCP's association is %+v", accepted)
	}

	// The context, as proposed and as answered.
	if len(established.PresentationContexts) != 1 {
		t.Fatalf("contexts: %+v", established.PresentationContexts)
	}
	pc := established.PresentationContexts[0]
	if pc.AbstractSyntax != VerificationSOPClassUID || pc.Result != PCResultAcceptance ||
		pc.TransferSyntax == "" || len(pc.Proposed) == 0 {
		t.Errorf("the context is %+v", pc)
	}

	// The bytes are the PDU: they decode to it, and what one side sent is
	// what the other received.
	requested := scuLog.first(t, EVTAssocRequested)
	decoded, err := DecodePDU(bytes.NewReader(requested.PDUBytes))
	if err != nil {
		t.Fatalf("the request's bytes do not decode: %v", err)
	}
	if rq, ok := decoded.(*AssociateRQ); !ok || rq.CallingAE != "EVT_SCU" || rq.CalledAE != "EVT_SCP" {
		t.Errorf("the request's bytes decode to %+v", decoded)
	}
	if received := scpLog.first(t, EVTAssocRequested); !bytes.Equal(received.PDUBytes, requested.PDUBytes) {
		t.Error("the SCP received other bytes than the SCU sent")
	}

	// The messages.
	sent := scuLog.first(t, EVTDIMSESent).Command
	if sent.Name != "C-ECHO-RQ" || sent.MessageID == 0 || sent.Status != nil || sent.HasDataSet ||
		sent.AffectedSOPClassUID != VerificationSOPClassUID || sent.ContextID != pc.ID {
		t.Errorf("the request is %+v", sent)
	}
	response := scuLog.first(t, EVTDIMSERecv).Command
	if response.Name != "C-ECHO-RSP" || response.Status == nil || *response.Status != StatusSuccess ||
		response.MessageIDBeingRespondedTo != sent.MessageID || !response.IsResponse() {
		t.Errorf("the response is %+v", response)
	}
	if echo := scpLog.first(t, EVTCEcho).Command; echo.MessageID != sent.MessageID {
		t.Errorf("the SCP's C-ECHO event is %+v", echo)
	}
}

// acceptingStore answers every C-STORE with success.
type acceptingStore struct{ BaseHandler }

func (acceptingStore) HandleCStore(_ context.Context, req *CStoreRequest) (*CStoreResponse, error) {
	return &CStoreResponse{MessageIDRespondedTo: req.MessageID, Status: StatusSuccess}, nil
}

func TestEventsOfAStore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	scpLog, scpEvents := newEventLog()
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "EVT_SCP", Port: 0, BindAddress: "127.0.0.1", Events: scpEvents,
	}, &acceptingStore{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scuLog, scuEvents := newEventLog()
	scu := NewSCU(SCUConfig{CallingAE: "EVT_SCU", CalledAE: "EVT_SCP", Address: server.Addr(), Events: scuEvents})
	contexts := []PresentationContextItem{{ID: 1, AbstractSyntax: CTImageStorageUID,
		TransferSyntaxes: []string{ExplicitVRLittleEndianUID}}}
	if err := scu.Associate(ctx, contexts); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(CTImageStorageUID+"\x00")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.260.1\x00")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, make([]byte, 20000)))
	if err := scu.Store(ctx, ds); err != nil {
		t.Fatalf("Store: %v", err)
	}
	_ = scu.Release(ctx)
	scpLog.waitFor(t, EVTConnClose)

	store := scpLog.first(t, EVTCStore).Command
	if store.Name != "C-STORE-RQ" || store.AffectedSOPInstanceUID != "1.2.260.1" ||
		store.AffectedSOPClassUID != CTImageStorageUID || !store.HasDataSet {
		t.Errorf("the C-STORE event is %+v", store)
	}
	// One message, however many PDUs carried it: the data set is larger
	// than a PDU.
	messages, dataPDUs := 0, 0
	for _, e := range scuLog.all() {
		switch e.Type {
		case EVTDIMSESent:
			messages++
		case EVTPDUSent:
			if _, ok := e.PDU.(*PDataTF); ok {
				dataPDUs++
			}
		}
	}
	if messages != 1 || dataPDUs < 3 {
		t.Errorf("%d message(s) in %d P-DATA-TF PDUs, want one in several", messages, dataPDUs)
	}
	if rsp := scuLog.first(t, EVTDIMSERecv).Command; rsp.Name != "C-STORE-RSP" || rsp.Status == nil || *rsp.Status != 0 {
		t.Errorf("the response is %+v", rsp)
	}
}

// A rejection and an abort, at the transport: neither needs a peer that
// behaves.
func TestEventsOfARejectionAndAnAbort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, b := net.Pipe()
	near, far := NewTransport(a, 0), NewTransport(b, 0)
	log, events := newEventLog()
	near.observe(events)

	go func() {
		_, _ = far.ReadPDU(ctx)
		_ = far.WritePDU(ctx, &AssociateRJ{Result: RJResultRejectedPermanent, Source: RJSourceServiceUser, Reason: 7})
		_ = far.WritePDU(ctx, &AbortPDU{Source: AbortSourceServiceProvider, Reason: 2})
	}()
	if err := near.WritePDU(ctx, &AssociateRQ{CallingAE: "A", CalledAE: "B"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := near.ReadPDU(ctx); err != nil {
			t.Fatal(err)
		}
	}
	_ = near.Close()
	_ = near.Close()

	want := "EVT_CONN_OPEN EVT_PDU_SENT EVT_REQUESTED EVT_PDU_RECV EVT_REJECTED EVT_PDU_RECV EVT_ABORTED EVT_CONN_CLOSE"
	if got := strings.Join(log.names(), " "); got != want {
		t.Errorf("saw\n  %s\nwant\n  %s", got, want)
	}
	if rj, ok := log.first(t, EVTAssocRejected).PDU.(*AssociateRJ); !ok || rj.Result != RJResultRejectedPermanent || rj.Reason != 7 {
		t.Errorf("the rejection is %+v", log.first(t, EVTAssocRejected).PDU)
	}
	if abort, ok := log.first(t, EVTAssocAborted).PDU.(*AbortPDU); !ok || abort.Source != AbortSourceServiceProvider || abort.Reason != 2 {
		t.Errorf("the abort is %+v", log.first(t, EVTAssocAborted).PDU)
	}
}

// With nowhere to report to, nothing is kept and nothing changes.
func TestNoEventsWithoutAManager(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close(); _ = b.Close() }()
	transport := NewTransport(a, 0)
	transport.observe(nil)
	if transport.observed() != nil {
		t.Error("a transport with no manager has an observer")
	}
	transport.emit(EVTConnOpen)
}
