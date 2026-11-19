package network

import (
	"bytes"
	"context"
	"io"
	"net"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

// discardingStore streams each data set to io.Discard, so nothing of an
// instance is held by the handler.
func discardingStore() Handler {
	return &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
		_, err := io.Copy(io.Discard, data)
		return &CStoreResponse{Status: StatusSuccess}, err
	}}
}

// storeAllocation is the memory allocated in the process while one streamed
// C-STORE of size bytes is received at addr and answered.
func storeAllocation(t *testing.T, addr string, size int64) uint64 {
	t.Helper()
	ctx := context.Background()
	scu := NewSCU(SCUConfig{CallingAE: "SENDER", CalledAE: "STREAM", Address: addr})
	if err := scu.Associate(ctx, []PresentationContextItem{
		{ID: ctxExplicit, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ExplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scu.Release(ctx) }()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if err := sendBigStore(ctx, scu, 1, size); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := scu.Association().ReceivePData(ctx); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// unobservedSCP starts an SCP that reports to no EventManager, whose
// transports therefore copy nothing, and returns its address.
func unobservedSCP(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	scp := NewSCP(SCPConfig{AETitle: "STREAM", Port: port, BindAddress: "127.0.0.1"})
	scp.SetHandler(discardingStore())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = scp.ListenAndServe(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	deadline := time.Now().Add(5 * time.Second)
	for scp.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return "127.0.0.1:" + strconv.Itoa(port)
}

// TestAssociationEventsDoNotCopyTheData: a server whose only event handlers
// are of association events allocates no more for a 64 MiB store, over what
// a server with no EventManager allocates, than for a 16 MiB one. Every
// received PDU used to be copied whenever there was an EventManager, which a
// Server always has, so the excess grew with the instance: 64 MiB more for a
// 64 MiB store (#469).
func TestAssociationEventsDoNotCopyTheData(t *testing.T) {
	events := NewEventManager()
	var accepted, released int
	var mu sync.Mutex
	events.On(EVTAssocAccepted, func(e *Event) {
		mu.Lock()
		defer mu.Unlock()
		if len(e.PDUBytes()) > 0 {
			accepted++
		}
	})
	events.On(EVTAssocReleased, func(*Event) { mu.Lock(); released++; mu.Unlock() })
	server, err := StartServer(context.Background(), SCPConfig{AETitle: "STREAM", Port: 0, BindAddress: "127.0.0.1", Events: events}, discardingStore())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	plain := unobservedSCP(t)

	excess := func(size int64) int64 {
		// The least of three, against a collection that happens to land
		// inside one measurement.
		least := int64(-1)
		for i := 0; i < 3; i++ {
			e := int64(storeAllocation(t, server.Addr(), size)) - int64(storeAllocation(t, plain, size))
			if least < 0 || e < least {
				least = e
			}
		}
		return least
	}
	small, large := excess(16<<20), excess(64<<20)
	t.Logf("allocated over a server with no EventManager: 16 MiB store %+.1f MiB, 64 MiB store %+.1f MiB",
		float64(small)/(1<<20), float64(large)/(1<<20))
	if grew := large - small; grew > 8<<20 {
		t.Errorf("the excess grew by %.1f MiB from a 16 to a 64 MiB store; the data was copied with no handler registered that is given it",
			float64(grew)/(1<<20))
	}
	mu.Lock()
	defer mu.Unlock()
	if accepted == 0 {
		t.Error("EVTAssocAccepted was given no PDU bytes; association events keep theirs")
	}
}

// TestPDUEventsStillCarryEveryByte: a handler of EVTPDURecv is given every
// PDU the server received, P-DATA-TF included, byte for byte as the client
// sent it.
func TestPDUEventsStillCarryEveryByte(t *testing.T) {
	var mu sync.Mutex
	var sent, received [][]byte
	keep := func(into *[][]byte) EventHandler {
		return func(e *Event) {
			mu.Lock()
			defer mu.Unlock()
			*into = append(*into, append([]byte(nil), e.PDUBytes()...))
		}
	}
	serverEvents, clientEvents := NewEventManager(), NewEventManager()
	serverEvents.On(EVTPDURecv, keep(&received))
	clientEvents.On(EVTPDUSent, keep(&sent))

	server, err := StartServer(context.Background(), SCPConfig{AETitle: "STREAM", Port: 0, BindAddress: "127.0.0.1", Events: serverEvents}, discardingStore())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	ctx := context.Background()
	scu := NewSCU(SCUConfig{CallingAE: "SENDER", CalledAE: "STREAM", Address: server.Addr(), Events: clientEvents})
	if err := scu.Associate(ctx, []PresentationContextItem{
		{ID: ctxExplicit, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ExplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sendBigStore(ctx, scu, 1, 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := scu.Association().ReceivePData(ctx); err != nil {
		t.Fatal(err)
	}
	if err := scu.Release(ctx); err != nil {
		t.Fatal(err)
	}
	server.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(sent) < 60 || len(received) != len(sent) {
		t.Fatalf("the client sent %d PDUs and the server's handler was given %d", len(sent), len(received))
	}
	dataPDUs := 0
	for i := range sent {
		if !bytes.Equal(received[i], sent[i]) {
			t.Fatalf("PDU %d: the handler was given %d bytes starting % x, the client sent %d starting % x",
				i, len(received[i]), head(received[i]), len(sent[i]), head(sent[i]))
		}
		if sent[i][0] == PDUTypeDataTF {
			dataPDUs++
		}
	}
	t.Logf("%d PDUs, %d of them P-DATA-TF, given to the handler byte for byte", len(sent), dataPDUs)
}

func head(b []byte) []byte { return b[:min(len(b), 8)] }
