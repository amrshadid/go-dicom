package network

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// gatedStore is a C-STORE handler that signals when a store begins and
// takes as long as it is told to, unless its context ends first.
type gatedStore struct {
	BaseHandler
	took     time.Duration
	started  chan struct{}
	calls    atomic.Int32
	canceled atomic.Bool
}

func newGatedStore(took time.Duration) *gatedStore {
	return &gatedStore{took: took, started: make(chan struct{}, 8)}
}

func (h *gatedStore) HandleCStore(ctx context.Context, _ *CStoreRequest) (*CStoreResponse, error) {
	h.calls.Add(1)
	h.started <- struct{}{}
	select {
	case <-time.After(h.took):
		return &CStoreResponse{Status: StatusSuccess}, nil
	case <-ctx.Done():
		h.canceled.Store(true)
		return nil, ctx.Err()
	}
}

// corpusInstance is CT_small.dcm from pydicom's test data when
// GODICOM_PYDICOM_DATA names it, and a generated CT instance otherwise: what
// is stored does not matter to a stop, and these run where the corpus is not.
func corpusInstance(t *testing.T) *dataset.Dataset {
	t.Helper()
	if dir := os.Getenv("GODICOM_PYDICOM_DATA"); dir != "" {
		f, err := os.Open(filepath.Join(dir, "CT_small.dcm"))
		if err == nil {
			defer func() { _ = f.Close() }()
			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatalf("reading CT_small.dcm: %v", err)
			}
			return df.GetDataset()
		}
	}
	return buildTestCTDataset("1.2.826.0.1.3680043.2.1125.464", "STOP^TEST", "464")
}

// shutdownServer starts a server for these tests and an SCU associated with
// it for CT storage, whose events report the A-ABORTs it receives.
func shutdownServer(t *testing.T, handler Handler) (*Server, *SCU, <-chan *AbortPDU) {
	t.Helper()
	server, err := StartServer(context.Background(), SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1"}, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	aborts := make(chan *AbortPDU, 4)
	events := NewEventManager()
	events.On(EVTAssocAborted, func(e *Event) {
		if p, ok := e.PDU.(*AbortPDU); ok && e.Role == "SCU" {
			aborts <- p
		}
	})
	scu := NewSCU(SCUConfig{CallingAE: "SCANNER", CalledAE: "ARCHIVE", Address: server.Addr(), Events: events})
	if err := scu.Associate(context.Background(), []PresentationContextItem{
		{ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ExplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatal(err)
	}
	return server, scu, aborts
}

// receivedAbort waits for the A-ABORT the SCU was sent, and fails unless it
// is the service-user's (PS3.8 Table 9-26, source 0, reason 0).
func receivedAbort(t *testing.T, aborts <-chan *AbortPDU) {
	t.Helper()
	select {
	case p := <-aborts:
		if p.Source != AbortSourceServiceUser || p.Reason != 0 {
			t.Errorf("A-ABORT source %d reason %d, want the service-user's (0, 0)", p.Source, p.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the SCU was sent no A-ABORT")
	}
}

// TestShutdownLetsAStoreInFlightFinish: a C-STORE whose handler is running
// when Shutdown begins is answered Success, its handler's context is not
// canceled, and Shutdown returns nil once the requestor releases. Stop cut
// such a store with 0xC000 and the instance had arrived whole (#464).
func TestShutdownLetsAStoreInFlightFinish(t *testing.T) {
	base := runtime.NumGoroutine()
	handler := newGatedStore(500 * time.Millisecond)
	server, scu, _ := shutdownServer(t, handler)

	stored := make(chan error, 1)
	go func() { stored <- scu.Store(context.Background(), corpusInstance(t)) }()
	<-handler.started

	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stopped <- server.Shutdown(ctx)
	}()

	if err := <-stored; err != nil {
		t.Fatalf("the store in flight when Shutdown began: %v, want Success", err)
	}
	if handler.canceled.Load() {
		t.Error("the handler's context was canceled while the store was in flight")
	}
	if err := scu.Release(context.Background()); err != nil {
		t.Errorf("release after the store: %v", err)
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("Shutdown returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return after the association was released")
	}
	goroutinesSettle(t, base)
}

// TestShutdownAbortsAStoreThatOutlivesItsBound: a store still running when
// Shutdown's context ends is aborted, its handler's context is canceled, and
// Shutdown returns the context's error within a second or so of the bound.
func TestShutdownAbortsAStoreThatOutlivesItsBound(t *testing.T) {
	base := runtime.NumGoroutine()
	handler := newGatedStore(time.Minute)
	server, scu, aborts := shutdownServer(t, handler)

	stored := make(chan error, 1)
	go func() { stored <- scu.Store(context.Background(), corpusInstance(t)) }()
	<-handler.started

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	began := time.Now()
	err := server.Shutdown(ctx)
	took := time.Since(began)
	var cut *ShutdownError
	if !errors.As(err, &cut) || cut.Aborted != 1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown returned %v, want a *ShutdownError counting 1 association, unwrapping to context.DeadlineExceeded", err)
	}
	if took > 2*time.Second {
		t.Errorf("Shutdown took %v past a 300ms bound", took)
	}
	receivedAbort(t, aborts)
	if err := <-stored; err == nil {
		t.Error("the store cut at the bound reported Success")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !handler.canceled.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !handler.canceled.Load() {
		t.Error("the handler's context was not canceled at the bound")
	}
	goroutinesSettle(t, base)
}

// requestAssociation sends an A-ASSOCIATE-RQ on conn and returns the ten bytes
// of the answer.
func requestAssociation(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	raw, err := EncodePDU(associateRQ("SCANNER", "ARCHIVE"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(raw); err != nil {
		t.Fatal(err)
	}
	rj := make([]byte, 10)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, rj); err != nil {
		t.Fatalf("reading the answer to the A-ASSOCIATE-RQ: %v", err)
	}
	return rj
}

// wantTransientRefusal fails unless rj is an A-ASSOCIATE-RJ rejected-transient,
// service-provider (presentation), temporary-congestion (PS3.8 Table 9-21).
func wantTransientRefusal(t *testing.T, who string, rj []byte) {
	t.Helper()
	if rj[0] != 0x03 || rj[7] != RJResultRejectedTransient || rj[8] != RJSourceServiceProviderPresentation || rj[9] != RJReasonTemporaryCongestion {
		t.Errorf("%s: answered % x, want an A-ASSOCIATE-RJ 02 03 01", who, rj)
	}
}

// waitDraining waits until a Shutdown has begun.
func waitDraining(t *testing.T, server *Server) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !server.scp.draining() {
		if time.Now().After(deadline) {
			t.Fatal("the server never began draining")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestShutdownRefusesNewAssociations: while Shutdown drains, a caller that
// connects is answered rejected-transient, service-provider (presentation),
// temporary-congestion (PS3.8 Table 9-21), as one whose connection was
// already open is, so that a caller told to try later does. It was met with
// a refused TCP connection, which a modality may take for a failure. The
// listener is closed when the drain ends, and no goroutine is left.
func TestShutdownRefusesNewAssociations(t *testing.T) {
	base := runtime.NumGoroutine()
	handler := newGatedStore(time.Second)
	server, scu, _ := shutdownServer(t, handler)
	addr := server.Addr()

	early, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer early.Close()
	// Dial returns when the kernel has the connection, which is before the
	// server has accepted it, and the server counts the connections it serves
	// when it has: the SCU's and this one.
	served := func() int {
		d := server.scp.drain()
		d.mu.Lock()
		defer d.mu.Unlock()
		return len(d.conns)
	}
	for accepted := time.Now().Add(5 * time.Second); served() < 2; {
		if time.Now().After(accepted) {
			t.Fatalf("the server serves %d connections, want the SCU's and the early one", served())
		}
		time.Sleep(time.Millisecond)
	}
	// A store in flight keeps the server draining while the callers are tried.
	stored := make(chan error, 1)
	go func() { stored <- scu.Store(context.Background(), corpusInstance(t)) }()
	<-handler.started
	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stopped <- server.Shutdown(ctx)
	}()
	waitDraining(t, server)

	// A caller that connects now, in the drain, is accepted and told to come
	// back; each of several is.
	for i := 0; i < 3; i++ {
		late, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			t.Fatalf("a connection made while Shutdown drains was refused: %v", err)
		}
		wantTransientRefusal(t, "a caller during the drain", requestAssociation(t, late))
		late.Close()
	}
	wantTransientRefusal(t, "a connection opened before Shutdown", requestAssociation(t, early))
	early.Close()

	// The same through an SCU, which is how a modality asks: it learns the
	// refusal is transient, not that the connection failed.
	retry := NewSCU(SCUConfig{CallingAE: "SCANNER", CalledAE: "ARCHIVE", Address: addr})
	var rejected *AssociationError
	err = retry.Associate(context.Background(), DefaultVerificationContexts())
	if !errors.As(err, &rejected) || rejected.Result != RJResultRejectedTransient ||
		rejected.Source != RJSourceServiceProviderPresentation || rejected.Reason != RJReasonTemporaryCongestion {
		t.Errorf("an SCU associating during the drain got %v, want the refusal with its three values", err)
	}

	if err := <-stored; err != nil {
		t.Errorf("the store in flight: %v", err)
	}
	_ = scu.Release(context.Background())
	if err := <-stopped; err != nil {
		t.Errorf("Shutdown returned %v", err)
	}
	// The drain is over, so the listener is closed.
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		conn.Close()
		t.Error("a connection made after the drain ended was accepted")
	}
	goroutinesSettle(t, base)
}

// TestShutdownClosesTheListenerWhenNothingIsLeft: a server with no
// association closes its listener as soon as Shutdown begins, which returns
// nil at once, and the accept loop is gone.
func TestShutdownClosesTheListenerWhenNothingIsLeft(t *testing.T) {
	base := runtime.NumGoroutine()
	server, err := StartServer(context.Background(), SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1"}, newGatedStore(0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	addr := server.Addr()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned %v", err)
	}
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		conn.Close()
		t.Error("the listener was left open")
	}
	server.Wait()
	goroutinesSettle(t, base)
}

// TestShutdownAtItsBoundClosesTheListener: when the bound ends the drain, the
// callers refused up to then are the last, the associations left are aborted,
// and the listener is closed.
func TestShutdownAtItsBoundClosesTheListener(t *testing.T) {
	base := runtime.NumGoroutine()
	handler := newGatedStore(time.Minute)
	server, scu, _ := shutdownServer(t, handler)
	addr := server.Addr()
	stored := make(chan error, 1)
	go func() { stored <- scu.Store(context.Background(), corpusInstance(t)) }()
	<-handler.started

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- server.Shutdown(ctx) }()
	waitDraining(t, server)
	late, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("a connection made while Shutdown drains was refused: %v", err)
	}
	wantTransientRefusal(t, "a caller before the bound", requestAssociation(t, late))
	late.Close()

	var cut *ShutdownError
	if err := <-stopped; !errors.As(err, &cut) || cut.Aborted != 1 {
		t.Errorf("Shutdown returned %v, want a *ShutdownError counting 1 association", err)
	}
	<-stored
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		conn.Close()
		t.Error("the listener was left open after the bound")
	}
	goroutinesSettle(t, base)
}

// TestShutdownAbortsAnIdleAssociationAtOnce: an association waiting for its
// next request when Shutdown begins is aborted then, not at the bound, and
// Shutdown returns nil long before its context ends.
func TestShutdownAbortsAnIdleAssociationAtOnce(t *testing.T) {
	base := runtime.NumGoroutine()
	server, scu, aborts := shutdownServer(t, newGatedStore(0))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	began := time.Now()
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown returned %v, want nil", err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("Shutdown took %v with only an idle association", took)
	}
	// The SCU learns of the abort when it next reads.
	if _, _, _, err := scu.Association().ReceivePData(context.Background()); err == nil {
		t.Error("the idle association was not ended")
	}
	receivedAbort(t, aborts)
	goroutinesSettle(t, base)
}

// TestShutdownStartsNoRequestAfterAnOperation: an association that finished
// an operation during Shutdown is waited on for a release, and a request it
// sends instead is aborted without reaching the handler.
func TestShutdownStartsNoRequestAfterAnOperation(t *testing.T) {
	base := runtime.NumGoroutine()
	handler := newGatedStore(300 * time.Millisecond)
	server, scu, aborts := shutdownServer(t, handler)

	stored := make(chan error, 1)
	go func() { stored <- scu.Store(context.Background(), corpusInstance(t)) }()
	<-handler.started
	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stopped <- server.Shutdown(ctx)
	}()
	if err := <-stored; err != nil {
		t.Fatalf("the store in flight: %v", err)
	}

	if err := scu.Store(context.Background(), corpusInstance(t)); err == nil {
		t.Error("a store sent after the operation finished during Shutdown reported Success")
	}
	receivedAbort(t, aborts)
	if n := handler.calls.Load(); n != 1 {
		t.Errorf("the handler was called %d times, want once", n)
	}
	if err := <-stopped; err != nil {
		t.Errorf("Shutdown returned %v", err)
	}
	goroutinesSettle(t, base)
}

// TestShutdownAbortSaysTheServerIsStopping: the A-ABORT that Shutdown sends
// is reported to the server's events with the cause AbortCauseStopping. It
// was reported as AbortCauseRequested, the cause of any abort the application
// asks for, so a listener could not tell a server being stopped from a
// handler that gave up on an association.
func TestShutdownAbortSaysTheServerIsStopping(t *testing.T) {
	causes := make(chan *LocalAbort, 4)
	events := NewEventManager()
	events.On(EVTAssocAborted, func(e *Event) {
		if e.Role == "SCP" {
			causes <- e.LocalAbort
		}
	})
	server, err := StartServer(context.Background(), SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1", Events: events,
	}, newGatedStore(0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	scu := NewSCU(SCUConfig{CallingAE: "SCANNER", CalledAE: "ARCHIVE", Address: server.Addr()})
	if err := scu.Associate(context.Background(), []PresentationContextItem{
		{ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ExplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatal(err)
	}
	defer scu.Association().transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned %v, want nil", err)
	}
	select {
	case local := <-causes:
		if local == nil || local.Cause != AbortCauseStopping {
			t.Fatalf("the abort was reported with %+v, want the cause %s", local, AbortCauseStopping)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no EVTAssocAborted reached the server's events")
	}
}
