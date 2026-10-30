package network

import (
	"context"
	"testing"
	"time"
)

// A peer could make the SCP hold as much as it liked: a P-DATA-TF over the
// Maximum Length the SCP advertised was accepted, and the fragments of a
// message were appended for as long as none was marked last (#208).

const advertisedMaxPDU = 16384

// boundedServer starts an SCP that advertises advertisedMaxPDU and returns an
// SCU associated with it.
func boundedServer(t *testing.T, cfg NetworkConfig) (*SCU, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	cfg.MaxPDUSize = advertisedMaxPDU
	server, err := StartServer(context.Background(), SCPConfig{
		AETitle: "BOUNDED", Port: 0, BindAddress: "127.0.0.1", Network: cfg,
	}, &countingStore{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)

	scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "BOUNDED", Address: server.Addr()})
	if err := scu.Associate(ctx, []PresentationContextItem{
		{ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ImplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scu.Association().transport.Close() })
	return scu, ctx
}

// sendPDVs writes each PDV in a P-DATA-TF of its own, as a peer that ignores
// the negotiated length would. Write errors are expected once the SCP aborts.
func sendPDVs(ctx context.Context, scu *SCU, pdvs ...PDVItem) {
	for _, pdv := range pdvs {
		if err := scu.Association().transport.WritePDU(ctx, &PDataTF{PDVItems: []PDVItem{pdv}}); err != nil {
			return
		}
	}
}

// expectAbort reads until the SCP's A-ABORT, failing if anything else, or
// nothing, arrives first.
func expectAbort(ctx context.Context, t *testing.T, scu *SCU, why string) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	pdu, err := scu.Association().transport.ReadPDU(readCtx)
	if err != nil {
		t.Fatalf("%s: no A-ABORT from the SCP: %v", why, err)
	}
	if _, ok := pdu.(*AbortPDU); !ok {
		t.Fatalf("%s: the SCP sent %T, want an A-ABORT", why, pdu)
	}
}

// TestPDUOverTheAdvertisedLengthIsAborted: PS3.8 D.1 makes the Maximum Length
// a receiver advertises the bound on what it accepts. A 64 KiB P-DATA-TF sent
// to an SCP that advertised 16 KiB was read and processed.
func TestPDUOverTheAdvertisedLengthIsAborted(t *testing.T) {
	scu, ctx := boundedServer(t, NetworkConfig{})
	sendPDVs(ctx, scu, PDVItem{PresentationContextID: 1, IsCommand: true, Data: make([]byte, 64<<10)})
	expectAbort(ctx, t, scu, "a 64 KiB PDU against a 16 KiB maximum")
}

// TestUnendedCommandIsAborted: command PDVs never marked last grew the
// assembly buffer without limit. A command set holds group 0000 alone, a few
// hundred bytes in practice.
func TestUnendedCommandIsAborted(t *testing.T) {
	scu, ctx := boundedServer(t, NetworkConfig{})
	pdv := PDVItem{PresentationContextID: 1, IsCommand: true, Data: make([]byte, 16000)}
	pdvs := make([]PDVItem, 100) // 1.6 MB
	for i := range pdvs {
		pdvs[i] = pdv
	}
	go sendPDVs(ctx, scu, pdvs...)
	expectAbort(ctx, t, scu, "1.6 MB of unended command PDVs")
}

// TestFragmentsOfOneMessageAgree: a message whose fragments switch between
// command and data, or between presentation contexts, is not one message,
// and switching let a command grow past its bound as data.
func TestFragmentsOfOneMessageAgree(t *testing.T) {
	scu, ctx := boundedServer(t, NetworkConfig{})
	sendPDVs(ctx, scu,
		PDVItem{PresentationContextID: 1, IsCommand: true, Data: make([]byte, 100)},
		PDVItem{PresentationContextID: 1, IsCommand: false, IsLast: true, Data: make([]byte, 100)})
	expectAbort(ctx, t, scu, "a command fragment followed by a data fragment")
}

// TestDataSetOverTheLimitIsAborted: MaxDataSetSize bounds what one data set
// may hold the SCP to, since the whole of it is held in memory.
func TestDataSetOverTheLimitIsAborted(t *testing.T) {
	scu, ctx := boundedServer(t, NetworkConfig{MaxDataSetSize: 64 << 10})
	pdv := PDVItem{PresentationContextID: 1, Data: make([]byte, 16000)}
	go sendPDVs(ctx, scu, pdv, pdv, pdv, pdv, pdv, pdv)
	expectAbort(ctx, t, scu, "96 KB of data set against a 64 KiB limit")
}
