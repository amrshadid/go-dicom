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

// mutualTLS returns the server and client halves of a mutual TLS setup.
func mutualTLS(pki testPKI) (server, client *TLSConfig) {
	return &TLSConfig{CertFile: pki.serverCert, KeyFile: pki.serverKey, CAFile: pki.caFile},
		&TLSConfig{CertFile: pki.clientCert, KeyFile: pki.clientKey, CAFile: pki.caFile}
}

// TestSCUAssociatesOverTLS: an SCU could not associate over TLS at all.
// DialTLS returned a bare transport no SCU method accepts, so Echo, Store,
// Find, Move and Get were plain TCP only. SCUConfig.TLS and SCPConfig.TLS
// carry it through Associate, ListenAndServe and StartServer (#182).
func TestSCUAssociatesOverTLS(t *testing.T) {
	pki := newTestPKI(t)
	serverTLS, clientTLS := mutualTLS(pki)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	started, err := StartServer(ctx, SCPConfig{
		AETitle: "TLS_SRV", Port: 0, BindAddress: "127.0.0.1", TLS: serverTLS,
	}, &EchoHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer started.Stop()

	// NewSCP reads Port 0 as DefaultPort, so find a free one.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	served := NewSCP(SCPConfig{AETitle: "TLS_SRV", Port: port, BindAddress: "127.0.0.1", TLS: serverTLS})
	served.SetHandler(&EchoHandler{})
	go func() { _ = served.ListenAndServe(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for served.Addr() == "" {
		if time.Now().After(deadline) {
			t.Fatal("ListenAndServe never bound its port")
		}
		time.Sleep(10 * time.Millisecond)
	}

	for name, addr := range map[string]string{"StartServer": started.Addr(), "ListenAndServe": served.Addr()} {
		t.Run(name, func(t *testing.T) {
			scu := NewSCU(SCUConfig{CallingAE: "TLS_SCU", CalledAE: "TLS_SRV", Address: addr, TLS: clientTLS})
			if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
				t.Fatalf("Associate over TLS: %v", err)
			}
			if err := scu.Echo(ctx); err != nil {
				t.Fatalf("Echo over TLS: %v", err)
			}
			_ = scu.Release(ctx)

			// A server that accepts TLS only refuses a plain client, and one
			// with a CAFile refuses a client without a certificate.
			plain := NewSCU(SCUConfig{CallingAE: "TLS_SCU", CalledAE: "TLS_SRV", Address: addr,
				Network: NetworkConfig{NetworkTimeout: 2 * time.Second}})
			if err := plain.Associate(ctx, DefaultVerificationContexts()); err == nil {
				_ = plain.Release(ctx)
				t.Error("a plain TCP client associated with a TLS server")
			}
			anonymous := NewSCU(SCUConfig{CallingAE: "TLS_SCU", CalledAE: "TLS_SRV", Address: addr,
				TLS: &TLSConfig{CAFile: pki.caFile}})
			if err := anonymous.Associate(ctx, DefaultVerificationContexts()); err == nil {
				_ = anonymous.Release(ctx)
				t.Error("a client without a certificate associated with a server that requires one")
			}
		})
	}
}

// TestCMoveSendsOverOutboundTLS: the sub-operations of a C-MOVE are an
// association the SCP opens itself, so they use OutboundTLS, not the TLS the
// SCP accepts with. Here the requestor reaches the source over plain TCP and
// the destination accepts TLS only.
func TestCMoveSendsOverOutboundTLS(t *testing.T) {
	pki := newTestPKI(t)
	serverTLS, clientTLS := mutualTLS(pki)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var mu sync.Mutex
	var stored []string
	destination, err := StartServer(ctx, SCPConfig{
		AETitle: "TLS_DEST", Port: 0, BindAddress: "127.0.0.1", TLS: serverTLS,
	}, &StorageHandler{
		OnStore: func(_ context.Context, _, sopInstance string, _ *dataset.Dataset) uint16 {
			mu.Lock()
			stored = append(stored, sopInstance)
			mu.Unlock()
			return StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer (destination): %v", err)
	}
	defer destination.Stop()
	destination.SetSupportedAbstractSyntaxes([]string{VerificationSOPClassUID, CTImageStorageUID})

	source, err := StartServer(ctx, SCPConfig{
		AETitle: "PLAIN_SRC", Port: 0, BindAddress: "127.0.0.1",
		MoveDestinations: map[string]string{"TLS_DEST": destination.Addr()},
		OutboundTLS:      clientTLS,
	}, &QueryRetrieveHandler{
		OnMoveInstances: func(_ context.Context, _, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return []*dataset.Dataset{makeInstance(CTImageStorageUID, "1.2.9.40", "Tls^Move")}, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer (source): %v", err)
	}
	defer source.Stop()

	scu := NewSCU(SCUConfig{CallingAE: "MOVE_SCU", CalledAE: "PLAIN_SRC", Address: source.Addr()})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")))
	if err := scu.Move(ctx, query, "TLS_DEST"); err != nil {
		t.Fatalf("Move: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(stored) != 1 || stored[0] != "1.2.9.40" {
		t.Fatalf("the TLS destination received %v, want [1.2.9.40]", stored)
	}
}
