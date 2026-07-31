package network_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// TestCGetCancelFromPynetdicom checks the cancel against a peer that has no
// reason to agree with our idea of one.
//
// The in-package cancellation tests drive both ends, and both ends were written
// here — the failure they guard against was C-GET and C-MOVE never watching for
// a C-CANCEL at all, which our own SCU was equally happy to leave unobserved.
// pynetdicom sends the real thing when a C-GET generator is abandoned.
func TestCGetCancelFromPynetdicom(t *testing.T) {
	const total = 300
	handler := &interopStreamHandler{total: total}

	scp := network.NewSCP(network.SCPConfig{
		AETitle: "GO_CANCEL_SCP", Port: 0, BindAddress: "127.0.0.1",
	})
	scp.SetHandler(handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = scp.ListenAndServe(ctx) }()

	addr := waitForSCP(t, scp)
	host, port := splitHostPort(t, addr)

	runPynetdicomSCU(t, `
from pydicom.dataset import Dataset
from pynetdicom import AE, build_role, evt
from pynetdicom.sop_class import PatientRootQueryRetrieveInformationModelGet, CTImageStorage

received = {"n": 0}

def on_store(event):
    received["n"] += 1
    return 0x0000

ae = AE(ae_title="PY_CANCEL")
ae.add_requested_context(PatientRootQueryRetrieveInformationModelGet)
ae.add_requested_context(CTImageStorage)

# A C-GET returns its instances on the same association, so the requestor has to
# take the SCP role for storage.
roles = [build_role(CTImageStorage, scp_role=True)]

assoc = ae.associate(`+quote(host)+`, `+port+`, ae_title="GO_CANCEL_SCP",
                     ext_neg=roles, evt_handlers=[(evt.EVT_C_STORE, on_store)])
assert assoc.is_established, "association rejected"

query = Dataset()
query.QueryRetrieveLevel = "STUDY"
query.PatientID = "*"

responses = assoc.send_c_get(query, PatientRootQueryRetrieveInformationModelGet)
for status, identifier in responses:
    if received["n"] >= 3:
        # Closing the generator is how pynetdicom issues a C-CANCEL.
        responses.close()
        break

assert received["n"] < 50, "cancel had no effect: %d instances arrived" % received["n"]
assoc.release()
`)

	// The handler stops asynchronously, once the cancel reaches its context.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !handler.stopped.Load() {
		time.Sleep(20 * time.Millisecond)
	}

	produced := handler.produced.Load()
	if produced >= int32(total) {
		t.Fatalf("the handler produced all %d matches; pynetdicom's C-CANCEL was ignored", total)
	}
	if !handler.stopped.Load() {
		t.Fatal("the handler's context was never canceled, so a real archive would have kept searching")
	}
	t.Logf("pynetdicom canceled after 3 instances; the handler stopped at %d of %d", produced, total)
}

// interopStreamHandler produces instances until its context is canceled.
type interopStreamHandler struct {
	network.BaseHandler
	total    int
	produced atomic.Int32
	stopped  atomic.Bool
}

func (h *interopStreamHandler) CountCGetMatches(_ context.Context, _ *network.CGetRequest) (int, error) {
	return h.total, nil
}

func (h *interopStreamHandler) StreamCGet(ctx context.Context, _ *network.CGetRequest,
	out chan<- *dataset.Dataset) error {

	for i := 0; i < h.total; i++ {
		ds := dataset.NewDataset()
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI,
			[]byte(network.CTImageStorageUID)))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI,
			[]byte(fmt.Sprintf("1.2.826.0.1.3680043.10.511.6.%d\x00", i))))
		select {
		case <-ctx.Done():
			h.stopped.Store(true)
			return ctx.Err()
		case out <- ds:
			h.produced.Add(1)
		}
	}
	return nil
}

// splitHostPort separates an address into a quotable host and a bare port.
func splitHostPort(t *testing.T, addr string) (host, port string) {
	t.Helper()
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:]
		}
	}
	t.Fatalf("address %q has no port", addr)
	return "", ""
}
