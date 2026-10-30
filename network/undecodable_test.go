package network

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
)

// truncatedDataSet is one Implicit VR Little Endian element, (0010,0010),
// declaring 1000 bytes and carrying 4.
var truncatedDataSet = []byte{0x10, 0x00, 0x10, 0x00, 0xE8, 0x03, 0x00, 0x00, 'A', 'B', 'C', 'D'}

// countingStore counts the C-STOREs that reach it.
type countingStore struct {
	BaseHandler
	calls atomic.Int32
}

func (h *countingStore) HandleCStore(context.Context, *CStoreRequest) (*CStoreResponse, error) {
	h.calls.Add(1)
	return &CStoreResponse{Status: StatusSuccess}, nil
}

// sendRaw sends a command and then the given data set bytes as they are, and
// returns the status of the first response.
func sendRaw(ctx context.Context, t *testing.T, scu *SCU, contextID byte, command *dataset.Dataset, data []byte) uint16 {
	t.Helper()
	assoc := scu.Association()
	cmd, err := EncodeCommandDataset(command)
	if err != nil {
		t.Fatal(err)
	}
	if err := assoc.SendPData(ctx, contextID, cmd, true); err != nil {
		t.Fatal(err)
	}
	if err := assoc.SendPData(ctx, contextID, data, false); err != nil {
		t.Fatal(err)
	}
	_, rsp, isCmd, err := assoc.ReceivePData(ctx)
	if err != nil {
		t.Fatalf("no response to a request whose data set does not decode: %v", err)
	}
	if !isCmd {
		t.Fatal("the response was not a command")
	}
	rspDS, err := DecodeCommandDataset(rsp)
	if err != nil {
		t.Fatal(err)
	}
	status, err := getUSValue(rspDS, tagStatus)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

// TestUndecodableDataSetIsRefused: a C-STORE whose data set did not decode was
// handed to the handler as an empty data set and answered Success, so the
// sender believed an instance was stored that never was; a C-FIND whose
// identifier did not decode got no response at all, leaving the requestor
// waiting. Both now answer 0xC000, cannot understand (PS3.4 B.2.3, C.4.1.1.4),
// and the handler is not called (#207).
func TestUndecodableDataSetIsRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	handler := &countingStore{}
	server, err := StartServer(ctx, SCPConfig{AETitle: "UNDECODABLE", Port: 0, BindAddress: "127.0.0.1"}, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "UNDECODABLE", Address: server.Addr()})
	if err := scu.Associate(ctx, []PresentationContextItem{
		{ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ImplicitVRLittleEndianUID}},
		{ID: 3, AbstractSyntax: StudyRootQueryRetrieveFind, TransferSyntaxes: []string{ImplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scu.Release(ctx) }()

	store := BuildCStoreRQ(1, CTImageStorageUID, "1.2.3.888", 0)
	if status := sendRaw(ctx, t, scu, 1, store, truncatedDataSet); status != StatusUnableToProcess {
		t.Errorf("C-STORE with an undecodable data set answered 0x%04X, want 0xC000", status)
	}
	if calls := handler.calls.Load(); calls != 0 {
		t.Errorf("the handler was called %d time(s) with a data set that did not decode", calls)
	}

	find := BuildCFindRQ(2, StudyRootQueryRetrieveFind, 0)
	if status := sendRaw(ctx, t, scu, 3, find, truncatedDataSet); status != StatusUnableToProcess {
		t.Errorf("C-FIND with an undecodable identifier answered 0x%04X, want 0xC000", status)
	}
}
