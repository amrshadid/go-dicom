package network

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestIntegrationSCUEchoViaSCP tests a full end-to-end C-ECHO:
// Start a real SCP on a random port, connect an SCU, send C-ECHO, release.
func TestIntegrationSCUEchoViaSCP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start SCP
	scp := NewSCP(SCPConfig{
		AETitle:     "TEST_SCP",
		BindAddress: "127.0.0.1",
	})
	scp.SetHandler(&EchoHandler{})

	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()

	scpCtx, scpCancel := context.WithCancel(ctx)
	defer scpCancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			transport, err := ln.Accept(scpCtx)
			if err != nil {
				return
			}
			go scp.handleConnection(scpCtx, transport)
		}
	}()

	// Connect SCU
	scu := NewSCU(SCUConfig{
		CallingAE: "TEST_SCU",
		CalledAE:  "TEST_SCP",
		Address:   addr,
	})

	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}

	// C-ECHO
	if err := scu.Echo(ctx); err != nil {
		t.Fatalf("Echo: %v", err)
	}

	// Release
	if err := scu.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	scpCancel()
	ln.Close()
	wg.Wait()
}

// TestIntegrationCStoreRoundTrip tests a full C-STORE:
// SCU sends a dataset, SCP receives it, verifies content.
func TestIntegrationCStoreRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Track what the SCP receives
	var receivedMu sync.Mutex
	var receivedSOPClass string
	var receivedSOPInstance string
	receivedCount := 0

	// Start SCP
	scp := NewSCP(SCPConfig{
		AETitle:     "STORE_SCP",
		BindAddress: "127.0.0.1",
	})
	scp.SetHandler(&StorageHandler{
		OnStore: func(_ context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
			receivedMu.Lock()
			defer receivedMu.Unlock()
			receivedSOPClass = sopClass
			receivedSOPInstance = sopInstance
			receivedCount++
			return StatusSuccess
		},
	})

	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()

	scpCtx, scpCancel := context.WithCancel(ctx)
	defer scpCancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			transport, err := ln.Accept(scpCtx)
			if err != nil {
				return
			}
			go scp.handleConnection(scpCtx, transport)
		}
	}()

	// Build a dataset to send
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(CTImageStorageUID)))
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.3.4.5.6.7.8.9")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("TestPatient^Integration")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("INTEG-001")))

	// Connect SCU with storage contexts
	scu := NewSCU(SCUConfig{
		CallingAE: "STORE_SCU",
		CalledAE:  "STORE_SCP",
		Address:   addr,
	})

	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}

	// C-STORE
	if err := scu.Store(ctx, ds); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Release
	if err := scu.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	scpCancel()
	ln.Close()
	wg.Wait()

	// Verify SCP received correctly
	receivedMu.Lock()
	defer receivedMu.Unlock()

	if receivedCount != 1 {
		t.Errorf("expected 1 received, got %d", receivedCount)
	}
	if receivedSOPClass != CTImageStorageUID {
		t.Errorf("expected SOP class %s, got %s", CTImageStorageUID, receivedSOPClass)
	}
	if receivedSOPInstance != "1.2.3.4.5.6.7.8.9" {
		t.Errorf("expected SOP instance '1.2.3.4.5.6.7.8.9', got '%s'", receivedSOPInstance)
	}
}

// TestIntegrationMultipleEchoes tests sending multiple C-ECHOs on one association.
func TestIntegrationMultipleEchoes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	echoCount := 0
	var mu sync.Mutex

	scp := NewSCP(SCPConfig{
		AETitle:     "MULTI_SCP",
		BindAddress: "127.0.0.1",
	})
	scp.SetHandler(&BaseHandler{})

	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()

	scpCtx, scpCancel := context.WithCancel(ctx)
	defer scpCancel()

	go func() {
		for {
			transport, err := ln.Accept(scpCtx)
			if err != nil {
				return
			}
			go scp.handleConnection(scpCtx, transport)
		}
	}()

	scu := NewSCU(SCUConfig{
		CallingAE: "MULTI_SCU",
		CalledAE:  "MULTI_SCP",
		Address:   addr,
	})

	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}

	// Send 10 C-ECHOs on the same association
	for i := 0; i < 10; i++ {
		if err := scu.Echo(ctx); err != nil {
			t.Fatalf("Echo %d: %v", i, err)
		}
		mu.Lock()
		echoCount++
		mu.Unlock()
	}

	if err := scu.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	mu.Lock()
	if echoCount != 10 {
		t.Errorf("expected 10 echoes, got %d", echoCount)
	}
	mu.Unlock()

	scpCancel()
	ln.Close()
}

// TestIntegrationAbort tests aborting an association.
func TestIntegrationAbort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	scp := NewSCP(SCPConfig{
		AETitle:     "ABORT_SCP",
		BindAddress: "127.0.0.1",
	})
	scp.SetHandler(&EchoHandler{})

	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()

	scpCtx, scpCancel := context.WithCancel(ctx)
	defer scpCancel()

	go func() {
		for {
			transport, err := ln.Accept(scpCtx)
			if err != nil {
				return
			}
			go scp.handleConnection(scpCtx, transport)
		}
	}()

	scu := NewSCU(SCUConfig{
		CallingAE: "ABORT_SCU",
		CalledAE:  "ABORT_SCP",
		Address:   addr,
	})

	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}

	// Echo first to verify connection works
	if err := scu.Echo(ctx); err != nil {
		t.Fatalf("Echo: %v", err)
	}

	// Abort instead of release
	if err := scu.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	if scu.IsAssociated() {
		t.Error("should not be associated after abort")
	}

	scpCancel()
	ln.Close()
}

// TestIntegrationConcurrentAssociations tests multiple concurrent SCU connections.
func TestIntegrationConcurrentAssociations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var echoMu sync.Mutex
	echoTotal := 0

	scp := NewSCP(SCPConfig{
		AETitle:     "CONC_SCP",
		BindAddress: "127.0.0.1",
	})
	scp.SetHandler(&EchoHandler{})

	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()

	scpCtx, scpCancel := context.WithCancel(ctx)
	defer scpCancel()

	go func() {
		for {
			transport, err := ln.Accept(scpCtx)
			if err != nil {
				return
			}
			go scp.handleConnection(scpCtx, transport)
		}
	}()

	// Launch 5 concurrent SCUs
	var wg sync.WaitGroup
	numClients := 5

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			scu := NewSCU(SCUConfig{
				CallingAE: "CONC_SCU",
				CalledAE:  "CONC_SCP",
				Address:   addr,
			})

			if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
				t.Errorf("SCU %d Associate: %v", id, err)
				return
			}

			if err := scu.Echo(ctx); err != nil {
				t.Errorf("SCU %d Echo: %v", id, err)
				scu.Abort(ctx)
				return
			}

			echoMu.Lock()
			echoTotal++
			echoMu.Unlock()

			scu.Release(ctx)
		}(i)
	}

	wg.Wait()

	echoMu.Lock()
	if echoTotal != numClients {
		t.Errorf("expected %d successful echoes, got %d", numClients, echoTotal)
	}
	echoMu.Unlock()

	scpCancel()
	ln.Close()
}
