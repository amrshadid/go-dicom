package dcmstore_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// The point of the package is that a working archive is a few lines. This is
// those few lines, exercised over a real association: store four instances with
// C-STORE, find them with C-FIND at three levels, and retrieve them with C-GET.
//
// Everything else in this package is tested against the store directly. This is
// the test that would catch the store and the network layer disagreeing — a
// handler that satisfies the interface and answers nothing useful over the wire.
func TestAnArchiveOverARealAssociation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	scp := network.NewSCP(network.SCPConfig{
		AETitle: "ARCHIVE", BindAddress: "127.0.0.1", Port: freePort(t),
	})
	scp.SetHandler(dcmstore.NewHandler(store))
	scp.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())

	addr := serve(ctx, t, scp)

	// Store.
	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "MODALITY", CalledAE: "ARCHIVE", Address: addr,
	})
	if err := scu.Associate(ctx, storageContexts()); err != nil {
		t.Fatalf("Associate for storage: %v", err)
	}
	for _, i := range corpus {
		if err := scu.Store(ctx, i.dataset()); err != nil {
			t.Fatalf("Store(%s): %v", i.sopInstanceUID, err)
		}
	}
	if err := scu.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if got := store.Count(); got != len(corpus) {
		t.Fatalf("the archive holds %d instances after storing %d", got, len(corpus))
	}

	// Find, at each level, over a fresh association.
	finder := network.NewSCU(network.SCUConfig{
		CallingAE: "WORKSTATION", CalledAE: "ARCHIVE", Address: addr,
	})
	if err := finder.Associate(ctx, queryContexts()); err != nil {
		t.Fatalf("Associate for query: %v", err)
	}
	defer func() { _ = finder.Release(ctx) }()

	for _, tc := range []struct {
		level string
		want  int
	}{
		{"PATIENT", 2},
		{"STUDY", 2},
		{"SERIES", 3},
		{"IMAGE", 4},
	} {
		results, err := finder.FindWithSOPClass(ctx, network.StudyRootQueryRetrieveFind,
			query(tc.level, map[tag.Tag]string{
				tag.New(0x0010, 0x0020): "",
				tag.New(0x0020, 0x000D): "",
				tag.New(0x0020, 0x000E): "",
				tag.New(0x0008, 0x0018): "",
			}))
		if err != nil {
			t.Errorf("Find at %s: %v", tc.level, err)
			continue
		}

		count := 0
		for result := range results {
			if result.Err != nil {
				t.Errorf("Find at %s: %v", tc.level, result.Err)
				break
			}
			if result.DataSet != nil {
				count++
			}
		}
		if count != tc.want {
			t.Errorf("C-FIND at %s returned %d matches, want %d", tc.level, count, tc.want)
		}
	}
}

// C-GET retrieves over the same association, so the archive has to send back what
// it stored — the whole loop, out to disk and back over the wire.
func TestRetrievingAStudyWithCGet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, i := range corpus {
		if _, err := store.Store(ctx, i.dataset()); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	scp := network.NewSCP(network.SCPConfig{
		AETitle: "ARCHIVE", BindAddress: "127.0.0.1", Port: freePort(t),
	})
	scp.SetHandler(dcmstore.NewHandler(store))
	scp.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())

	addr := serve(ctx, t, scp)

	received := make(map[string]bool)
	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "WORKSTATION", CalledAE: "ARCHIVE", Address: addr,
		OnCStore: func(_ context.Context, _, sopInstanceUID string, ds *dataset.Dataset) uint16 {
			// Confirm the instance arrived with its attributes, not just its UID:
			// an archive that returns empty instances would otherwise pass.
			if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); !ok || valueOf(elem) == "" {
				return network.StatusUnableToProcess
			}
			received[sopInstanceUID] = true
			return network.StatusSuccess
		},
	})

	contexts := append(queryContexts(), retrieveContexts()...)
	if err := scu.Associate(ctx, contexts); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// Retrieve the three-instance CT study.
	if err := scu.Get(ctx, query("STUDY", map[tag.Tag]string{
		tag.New(0x0020, 0x000D): "1.2.1",
	})); err != nil {
		t.Fatalf("Get: %v", err)
	}

	want := []string{"1.2.1.1.1", "1.2.1.1.2", "1.2.1.2.1"}
	for _, uid := range want {
		if !received[uid] {
			t.Errorf("the C-GET did not deliver %s", uid)
		}
	}
	if len(received) != len(want) {
		t.Errorf("the C-GET delivered %d instances, want %d — a STUDY level retrieval "+
			"transfers every instance in the study", len(received), len(want))
	}
}

// The store refuses an instance rather than aborting the association, so the rest
// of what a peer is sending still gets through.
func TestARefusedInstanceDoesNotEndTheAssociation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	scp := network.NewSCP(network.SCPConfig{
		AETitle: "ARCHIVE", BindAddress: "127.0.0.1", Port: freePort(t),
	})
	scp.SetHandler(dcmstore.NewHandler(store))
	scp.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())

	addr := serve(ctx, t, scp)

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "MODALITY", CalledAE: "ARCHIVE", Address: addr,
	})
	if err := scu.Associate(ctx, storageContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// A UID that cannot name a file. The C-STORE fails...
	hostile := instance{
		patientID: "P9", studyUID: "1.2.9", seriesUID: "1.2.9.1",
		sopInstanceUID: "../../escape", modality: "CT",
	}
	if err := scu.Store(ctx, hostile.dataset()); err == nil {
		t.Error("the archive accepted an instance whose UID cannot name a file")
	}

	// ...and the association survives, so a good instance still stores.
	good := corpus[0]
	if err := scu.Store(ctx, good.dataset()); err != nil {
		t.Fatalf("a valid instance failed after a refused one: %v", err)
	}
	if got := store.Count(); got != 1 {
		t.Errorf("the archive holds %d instances, want 1", got)
	}
}

// freePort returns a port nothing is listening on.
//
// SCPConfig.Port defaults to 11112 when it is zero, so there is no way to ask for
// an ephemeral port through the public API. Binding one and releasing it leaves a
// small window where something else could take it, which is preferable to several
// tests in this file contending for the standard DICOM port.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the probed port: %v", err)
	}
	return port
}

// serve starts scp and returns the address it bound.
func serve(ctx context.Context, t *testing.T, scp *network.SCP) string {
	t.Helper()

	go func() { _ = scp.ListenAndServe(ctx) }()
	t.Cleanup(func() { _ = scp.Close() })

	// ListenAndServe binds asynchronously, so wait for the address rather than
	// racing the first connection against the listener.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if addr := scp.Addr(); addr != "" {
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the SCP did not bind within five seconds")
	return ""
}

func storageContexts() []network.PresentationContextItem {
	return []network.PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   "1.2.840.10008.5.1.4.1.1.2", // CT Image Storage
		TransferSyntaxes: network.DefaultTransferSyntaxes(),
	}}
}

func queryContexts() []network.PresentationContextItem {
	return []network.PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   network.StudyRootQueryRetrieveFind,
		TransferSyntaxes: network.DefaultTransferSyntaxes(),
	}, {
		ID:               3,
		AbstractSyntax:   network.StudyRootQueryRetrieveGet,
		TransferSyntaxes: network.DefaultTransferSyntaxes(),
	}}
}

// retrieveContexts adds the storage context a C-GET needs, with the SCP role
// negotiated: the peer sends C-STORE back over the same association, so this side
// has to act as an SCP for that SOP class.
func retrieveContexts() []network.PresentationContextItem {
	return []network.PresentationContextItem{{
		ID:               5,
		AbstractSyntax:   "1.2.840.10008.5.1.4.1.1.2",
		TransferSyntaxes: network.DefaultTransferSyntaxes(),
	}}
}
