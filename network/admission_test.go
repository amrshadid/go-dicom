package network

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// An SCP accepted every association that reached its port, whatever the
// calling AE title and wherever it came from. SCPConfig.Admit decides who may
// associate, before anything is negotiated.

// admitServer starts an SCP with the given admission function and returns its
// address and the events it reported.
func admitServer(t *testing.T, cfg SCPConfig) (string, *recordedEvents) {
	t.Helper()
	events := &recordedEvents{}
	cfg.Events = NewEventManager()
	cfg.Events.OnAny(events.record)
	if cfg.AETitle == "" {
		cfg.AETitle = "ARCHIVE"
	}
	cfg.Port, cfg.BindAddress = 0, "127.0.0.1"
	server, err := StartServer(context.Background(), cfg, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return server.Addr(), events
}

// recordedEvents keeps what an EventManager was told.
type recordedEvents struct {
	mu     sync.Mutex
	events []Event
}

func (r *recordedEvents) record(e *Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, *e)
}

// of returns the recorded events of one type.
func (r *recordedEvents) of(eventType EventType) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Event
	for _, e := range r.events {
		if e.Type == eventType {
			out = append(out, e)
		}
	}
	return out
}

// await returns the recorded events of one type once there are n of them.
// The SCP reports an A-ASSOCIATE-RJ after writing it, so the peer can read the
// refusal before the event is emitted.
func (r *recordedEvents) await(t *testing.T, eventType EventType, n int) []Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := r.of(eventType)
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// associateAs associates with addr under the given AE titles and returns the
// rejection the SCP answered with, nil when it accepted. An accepted
// association is checked with a C-ECHO and released.
func associateAs(t *testing.T, addr, calling, called string) *AssociationError {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scu := NewSCU(SCUConfig{CallingAE: calling, CalledAE: called, Address: addr})
	err := scu.Associate(ctx, DefaultVerificationContexts())
	if err == nil {
		if err := scu.Echo(ctx); err != nil {
			t.Fatalf("%s: admitted, but the C-ECHO failed: %v", calling, err)
		}
		_ = scu.Release(ctx)
		return nil
	}
	var rejected *AssociationError
	if !errors.As(err, &rejected) || rejected.Code != "ASSOCIATION_REJECTED" {
		t.Fatalf("%s: want an A-ASSOCIATE-RJ, got %v", calling, err)
	}
	return rejected
}

// wantRejection fails unless got is the refusal want.
func wantRejection(t *testing.T, who string, got *AssociationError, want AssociationRejection) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: admitted, want refused with %s", who, want)
	}
	if have := (AssociationRejection{got.Result, got.Source, got.Reason}); have != want {
		t.Fatalf("%s: refused with %s, want %s", who, have, want)
	}
}

// TestAdmitByCallingAE: a function that lists its callers admits them and
// refuses anyone else with the reason it chose, as the peer reads it.
func TestAdmitByCallingAE(t *testing.T) {
	var seen sync.Map
	addr, events := admitServer(t, SCPConfig{Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
		seen.Store(req.CallingAE, req.CalledAE)
		if req.CallingAE == "SCANNER1" {
			return nil
		}
		return RejectCallingAE()
	}})

	if r := associateAs(t, addr, "SCANNER1", "ARCHIVE"); r != nil {
		t.Fatalf("a listed caller was refused: %v", r)
	}
	wantRejection(t, "STRANGER", associateAs(t, addr, "STRANGER", "ARCHIVE"), *RejectCallingAE())

	// The titles reach the function without their padding.
	if called, _ := seen.Load("STRANGER"); called != "ARCHIVE" {
		t.Errorf("the function was given called AE %q, want %q", called, "ARCHIVE")
	}
	rejected := events.await(t, EVTAssocRejected, 1)
	if len(rejected) != 1 {
		t.Fatalf("%d EVTAssocRejected events, want 1", len(rejected))
	}
	if want := "rejected-permanent, service-user: calling-AE-title-not-recognized"; rejected[0].Description != want {
		t.Errorf("EVTAssocRejected says %q, want %q", rejected[0].Description, want)
	}
	if rejected[0].CallingAE != "STRANGER" {
		t.Errorf("EVTAssocRejected names calling AE %q, want STRANGER", rejected[0].CallingAE)
	}
}

// TestAdmitByAddress: the function is given the address the caller connects
// from, the same one the caller sees as its own.
func TestAdmitByAddress(t *testing.T) {
	allow := func(cidr string) func(context.Context, *AssociationRequest) *AssociationRejection {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		return func(_ context.Context, req *AssociationRequest) *AssociationRejection {
			tcp, ok := req.RemoteAddr.(*net.TCPAddr)
			if !ok || !network.Contains(tcp.IP) {
				return RejectCallingAE()
			}
			return nil
		}
	}

	loopback, _ := admitServer(t, SCPConfig{Admit: allow("127.0.0.0/8")})
	if r := associateAs(t, loopback, "ANY", "ARCHIVE"); r != nil {
		t.Fatalf("a caller from an allowed network was refused: %v", r)
	}
	elsewhere, _ := admitServer(t, SCPConfig{Admit: allow("10.0.0.0/8")})
	wantRejection(t, "a caller from 127.0.0.1", associateAs(t, elsewhere, "ANY", "ARCHIVE"), *RejectCallingAE())

	// The address is the caller's own end of the connection.
	got := make(chan *AssociationRequest, 1)
	addr, _ := admitServer(t, SCPConfig{Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
		got <- req
		return RejectCallingAE()
	}})
	transport, err := Dial(context.Background(), addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	if err := transport.WritePDU(context.Background(), associateRQ("ANY", "ARCHIVE")); err != nil {
		t.Fatal(err)
	}
	req := <-got
	if req.RemoteAddr.String() != transport.LocalAddr().String() {
		t.Errorf("the function was given remote address %s; the caller is at %s", req.RemoteAddr, transport.LocalAddr())
	}
	if req.LocalAddr.String() != addr {
		t.Errorf("the function was given local address %s; the SCP is at %s", req.LocalAddr, addr)
	}
	if req.VerifiedChains != nil {
		t.Errorf("a plain TCP caller has verified chains: %v", req.VerifiedChains)
	}
}

// associateRQ is an A-ASSOCIATE-RQ proposing verification.
func associateRQ(calling, called string) *AssociateRQ {
	return &AssociateRQ{
		ProtocolVersion:       ProtocolVersion,
		CallingAE:             calling,
		CalledAE:              called,
		ApplicationContextUID: DefaultApplicationContextUID,
		PresentationContexts:  DefaultVerificationContexts(),
		UserInformation: UserInformationItem{
			MaxPDULength:           DefaultMaxPDUSize,
			ImplementationClassUID: DefaultImplementationClassUID,
		},
	}
}

// TestEachRejectionArrivesAsSent: every result, source and reason PS3.8 Table
// 9-21 defines reaches the peer as the function chose it.
func TestEachRejectionArrivesAsSent(t *testing.T) {
	var choice atomic.Pointer[AssociationRejection]
	addr, events := admitServer(t, SCPConfig{Admit: func(context.Context, *AssociationRequest) *AssociationRejection {
		return choice.Load()
	}})

	var all []AssociationRejection
	for _, result := range []byte{RJResultRejectedPermanent, RJResultRejectedTransient} {
		for _, sr := range [][2]byte{
			{RJSourceServiceUser, RJReasonUserNoReason},
			{RJSourceServiceUser, RJReasonApplicationContextName},
			{RJSourceServiceUser, RJReasonCallingAETitleNotRecognized},
			{RJSourceServiceUser, RJReasonCalledAETitleNotRecognized},
			{RJSourceServiceProviderACSE, RJReasonACSENoReason},
			{RJSourceServiceProviderACSE, RJReasonProtocolVersionUnsupported},
			{RJSourceServiceProviderPresentation, RJReasonTemporaryCongestion},
			{RJSourceServiceProviderPresentation, RJReasonLocalLimitExceeded},
		} {
			all = append(all, AssociationRejection{result, sr[0], sr[1]})
		}
	}
	for _, want := range all {
		choice.Store(&want)
		wantRejection(t, want.String(), associateAs(t, addr, "ANY", "ARCHIVE"), want)
	}
	if got := len(events.await(t, EVTAssocRejected, len(all))); got != len(all) {
		t.Errorf("%d EVTAssocRejected events for %d refusals", got, len(all))
	}
}

// TestRefusedCallerIsNotNegotiated: a refused caller gets an A-ASSOCIATE-RJ
// and nothing else: no A-ASSOCIATE-AC, no presentation context, and the
// connection closed.
func TestRefusedCallerIsNotNegotiated(t *testing.T) {
	addr, events := admitServer(t, SCPConfig{Admit: func(context.Context, *AssociationRequest) *AssociationRejection {
		return RejectCallingAE()
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transport, err := Dial(ctx, addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	if err := transport.WritePDU(ctx, associateRQ("STRANGER", "ARCHIVE")); err != nil {
		t.Fatal(err)
	}
	pdu, err := transport.ReadPDU(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rj, ok := pdu.(*AssociateRJ)
	if !ok {
		t.Fatalf("the SCP answered %T, want an A-ASSOCIATE-RJ", pdu)
	}
	if got := (AssociationRejection{rj.Result, rj.Source, rj.Reason}); got != *RejectCallingAE() {
		t.Fatalf("refused with %s", got)
	}
	// Closed by the SCP, and at once: the read ends because the connection
	// did, not because this test stopped waiting. Its own deadline used to
	// count as success, so an SCP that never closed passed.
	closeCtx, cancelClose := context.WithTimeout(ctx, 2*time.Second)
	defer cancelClose()
	next, err := transport.ReadPDU(closeCtx)
	if err == nil {
		t.Fatalf("after the A-ASSOCIATE-RJ the SCP sent %T; it should have closed the connection", next)
	}
	var timeout *TimeoutError
	if errors.As(err, &timeout) || errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, io.EOF) {
		t.Fatalf("2s after the A-ASSOCIATE-RJ the connection is still open: %v", err)
	}
	// The refusal is reported once it is written; by then an acceptance
	// would have been reported too.
	events.await(t, EVTAssocRejected, 1)
	if n := len(events.of(EVTAssocAccepted)) + len(events.of(EVTAssocEstablished)); n != 0 {
		t.Errorf("%d accepted or established events for a refused caller", n)
	}
}

// TestNoAdmitChangesNothing: without a function the SCP admits whoever asks
// for its own AE title and refuses any other title, as it did before.
func TestNoAdmitChangesNothing(t *testing.T) {
	addr, events := admitServer(t, SCPConfig{})
	if r := associateAs(t, addr, "ANYONE", "ARCHIVE"); r != nil {
		t.Fatalf("refused without a function: %v", r)
	}
	wantRejection(t, "another called AE", associateAs(t, addr, "ANYONE", "OTHER"), *RejectCalledAE())
	if rejected := events.await(t, EVTAssocRejected, 1); len(rejected) != 1 ||
		rejected[0].Description != "rejected-permanent, service-user: called-AE-title-not-recognized" {
		t.Errorf("EVTAssocRejected events: %+v", rejected)
	}
}

// TestAdmitDecidesTheCalledAE: with AcceptAnyCalledAE the SCP does not refuse
// a called AE title by itself, so one SCP can answer to several; the function
// refuses the titles it does not answer to.
func TestAdmitDecidesTheCalledAE(t *testing.T) {
	addr, _ := admitServer(t, SCPConfig{AcceptAnyCalledAE: true, Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
		switch req.CalledAE {
		case "TENANT_A", "TENANT_B":
			return nil
		}
		return RejectCalledAE()
	}})
	for _, called := range []string{"TENANT_A", "TENANT_B"} {
		if r := associateAs(t, addr, "SCANNER", called); r != nil {
			t.Fatalf("%s was refused: %v", called, r)
		}
	}
	// ARCHIVE is the SCP's AETitle, and the function does not list it.
	wantRejection(t, "ARCHIVE", associateAs(t, addr, "SCANNER", "ARCHIVE"), *RejectCalledAE())
}

// TestAdmitPanicRefuses: a panic in the function refuses that caller, and the
// server goes on serving the next.
func TestAdmitPanicRefuses(t *testing.T) {
	addr, _ := admitServer(t, SCPConfig{Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
		if req.CallingAE == "BOOM" {
			panic("the allow list is unreadable")
		}
		return nil
	}})
	wantRejection(t, "BOOM", associateAs(t, addr, "BOOM", "ARCHIVE"),
		AssociationRejection{RJResultRejectedPermanent, RJSourceServiceUser, RJReasonUserNoReason})
	if r := associateAs(t, addr, "NEXT", "ARCHIVE"); r != nil {
		t.Fatalf("after the panic the next caller was refused: %v", r)
	}
}

// TestAdmitThatBlocksIsBounded: a function that does not answer is bounded by
// ARTIM; the caller is refused as transient, and the function's context is
// canceled so it can stop.
func TestAdmitThatBlocksIsBounded(t *testing.T) {
	canceled := make(chan struct{})
	addr, _ := admitServer(t, SCPConfig{
		Network: NetworkConfig{ARTIMTimeout: 300 * time.Millisecond},
		Admit: func(ctx context.Context, _ *AssociationRequest) *AssociationRejection {
			<-ctx.Done()
			close(canceled)
			return nil
		},
	})
	start := time.Now()
	wantRejection(t, "a caller the function never answers", associateAs(t, addr, "ANY", "ARCHIVE"), *RejectBusy())
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the refusal took %v against an ARTIM of 300ms", took)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Error("the function's context was never canceled")
	}
}

// TestAdmitReservedValuesAreNotSent: a combination Table 9-21 reserves is
// replaced by a refusal the peer can read, not sent.
func TestAdmitReservedValuesAreNotSent(t *testing.T) {
	for _, reserved := range []AssociationRejection{
		{RJResultRejectedPermanent, RJSourceServiceUser, 4},
		{3, RJSourceServiceUser, RJReasonCallingAETitleNotRecognized},
		{RJResultRejectedTransient, RJSourceServiceProviderPresentation, 0},
		{RJResultRejectedPermanent, 4, 1},
	} {
		addr, _ := admitServer(t, SCPConfig{Admit: func(context.Context, *AssociationRequest) *AssociationRejection {
			return &reserved
		}})
		wantRejection(t, reserved.String(), associateAs(t, addr, "ANY", "ARCHIVE"),
			AssociationRejection{RJResultRejectedPermanent, RJSourceServiceUser, RJReasonUserNoReason})
	}
}

// TestAdmitSeesTheVerifiedChain: over mutual TLS the function is given the
// caller's certificate chain as verified, so it can admit by certificate.
func TestAdmitSeesTheVerifiedChain(t *testing.T) {
	pki := newTestPKI(t)
	serverTLS, clientTLS := mutualTLS(pki)
	addr, _ := admitServer(t, SCPConfig{TLS: serverTLS, Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
		if len(req.VerifiedChains) == 0 || req.VerifiedChains[0][0].Subject.CommonName != "client" {
			return RejectCallingAE()
		}
		return nil
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scu := NewSCU(SCUConfig{CallingAE: "ANY", CalledAE: "ARCHIVE", Address: addr, TLS: clientTLS})
	if err := scu.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatalf("a caller with the expected certificate was refused: %v", err)
	}
	_ = scu.Release(ctx)
}

// TestAssociationRejectionString: each defined combination has the standard's
// name, and an undefined one is shown by number.
func TestAssociationRejectionString(t *testing.T) {
	for r, want := range map[AssociationRejection]string{
		*RejectBusy(): "rejected-transient, service-provider (presentation): temporary-congestion",
		{RJResultRejectedPermanent, RJSourceServiceProviderACSE, RJReasonProtocolVersionUnsupported}: "rejected-permanent, service-provider (ACSE): protocol-version-not-supported",
		{9, 8, 7}: "result 9, source 8: reason 7",
	} {
		if got := r.String(); got != want {
			t.Errorf("%v: %q, want %q", [3]byte{r.Result, r.Source, r.Reason}, got, want)
		}
	}
	if !strings.Contains(RejectCalledAE().String(), "called-AE-title-not-recognized") {
		t.Error(RejectCalledAE().String())
	}
}
