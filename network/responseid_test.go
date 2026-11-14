package network

import (
	"context"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The SCP answered a C-STORE and a C-ECHO with the Message ID the handler
// put in its response. A handler that left it unset answered message 0,
// which the requestor never matched: it waited out its timeout for an
// instance that had been stored (#266).

// bareHandler answers with a status and nothing else, or with nothing.
type bareHandler struct {
	BaseHandler
	none bool
}

func (h *bareHandler) HandleCEcho(context.Context, *CEchoRequest) (*CEchoResponse, error) {
	if h.none {
		return nil, nil
	}
	return &CEchoResponse{Status: StatusSuccess}, nil
}

func (h *bareHandler) HandleCStore(context.Context, *CStoreRequest) (*CStoreResponse, error) {
	if h.none {
		return nil, nil
	}
	return &CStoreResponse{Status: StatusSuccess}, nil
}

func bareExchange(t *testing.T, handler Handler) (echo, store error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server, err := StartServer(ctx, SCPConfig{AETitle: "BARE", Port: 0, BindAddress: "127.0.0.1"}, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "BARE", Address: server.Addr()})
	if err := scu.Associate(ctx, []PresentationContextItem{
		{ID: 1, AbstractSyntax: VerificationSOPClassUID, TransferSyntaxes: []string{ImplicitVRLittleEndianUID}},
		{ID: 3, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ImplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	echo = scu.Echo(ctx)
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(CTImageStorageUID+"\x00")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.266.1\x00")))
	store = scu.Store(ctx, ds)
	return echo, store
}

func TestResponseCarriesTheRequestsMessageID(t *testing.T) {
	echo, store := bareExchange(t, &bareHandler{})
	if echo != nil {
		t.Errorf("Echo against a handler that set no Message ID: %v", echo)
	}
	if store != nil {
		t.Errorf("Store against a handler that set no Message ID: %v", store)
	}
}

// A handler that returns nothing has not answered. The requestor is told so,
// and the server keeps running.
func TestNilResponseIsAFailureNotAPanic(t *testing.T) {
	echo, store := bareExchange(t, &bareHandler{none: true})
	if echo == nil || store == nil {
		t.Errorf("a handler that returned no response was reported as success: echo %v, store %v", echo, store)
	}
}
