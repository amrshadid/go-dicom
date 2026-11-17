package network

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// A handler could not tell which association a request came on: the events
// carried an id and the handler's context did not, so a program that kept
// an instance could not say on which association it arrived (#284).

// idRecorder notes the association id each C-STORE arrives with.
type idRecorder struct {
	BaseHandler
	mu  sync.Mutex
	ids []uint64
}

func (h *idRecorder) HandleCStore(ctx context.Context, req *CStoreRequest) (*CStoreResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if info := AssociationInfoFromContext(ctx); info != nil {
		h.ids = append(h.ids, info.ID)
	}
	return &CStoreResponse{MessageIDRespondedTo: req.MessageID, Status: StatusSuccess}, nil
}

func storeOne(t *testing.T, ctx context.Context, addr, instance string) {
	t.Helper()
	scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "IDS", Address: addr})
	if err := scu.Associate(ctx, []PresentationContextItem{
		{ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ImplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(CTImageStorageUID+"\x00")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte(instance)))
	if err := scu.Store(ctx, ds); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestHandlerKnowsItsAssociation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log, events := newEventLog()
	handler := &idRecorder{}
	server, err := StartServer(ctx, SCPConfig{AETitle: "IDS", Port: 0, BindAddress: "127.0.0.1", Events: events}, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	storeOne(t, ctx, server.Addr(), "1.2.284.1\x00")
	storeOne(t, ctx, server.Addr(), "1.2.284.2\x00")

	// The id each request was handled with is the id of its EVT_C_STORE.
	var fromEvents []uint64
	for _, e := range log.all() {
		if e.Type == EVTCStore {
			fromEvents = append(fromEvents, e.AssociationID)
		}
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if len(handler.ids) != 2 || len(fromEvents) != 2 {
		t.Fatalf("the handler saw %v, the events %v", handler.ids, fromEvents)
	}
	for i := range fromEvents {
		if handler.ids[i] == 0 || handler.ids[i] != fromEvents[i] {
			t.Errorf("request %d was handled as association %d, and its event says %d", i, handler.ids[i], fromEvents[i])
		}
	}
	if handler.ids[0] == handler.ids[1] {
		t.Errorf("two associations share the id %d", handler.ids[0])
	}
}

// A connection that reports no events has no id to give. A Server always
// has somewhere to report to, so this is a connection on its own.
func TestConnectionHasNoIDWithoutEvents(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close(); _ = b.Close() }()
	transport := NewTransport(a, 0)
	if id := transport.associationID(); id != 0 {
		t.Errorf("a connection with nowhere to report has the id %d", id)
	}
	_, events := newEventLog()
	transport.observe(events)
	if transport.associationID() == 0 {
		t.Error("a connection that reports events has no id")
	}
}
