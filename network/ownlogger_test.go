package network

import (
	"context"
	"log/slog"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/config"
)

// ownLog is a logger of one server or client, for these tests: everything,
// at every level, kept to be read.
func ownLog() (*slog.Logger, *logBuffer) {
	buf := &logBuffer{}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// noGlobalLog points DefaultLogger and config.Logger at a buffer the test
// fails on if anything is written to it, for the length of the test.
func noGlobalLog(t *testing.T) {
	t.Helper()
	global := &logBuffer{}
	previous, previousConfig := DefaultLogger, config.Logger
	DefaultLogger = NewLogger(LogLevelDebug, global)
	config.Logger = slog.New(slog.NewTextHandler(global, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() {
		DefaultLogger, config.Logger = previous, previousConfig
		if s := global.String(); s != "" {
			t.Errorf("a server or client given its own logger wrote to the global one:\n%s", s)
		}
	})
}

// eventually waits for log to hold every one of want.
func eventually(t *testing.T, name string, log *logBuffer, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		missing := ""
		for _, w := range want {
			if !strings.Contains(log.String(), w) {
				missing = w
				break
			}
		}
		if missing == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s's log has no %q:\n%s", name, missing, log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestEachServerAndClientLogsToItsOwnLogger: two servers in one process, one
// plain and one TLS, and a client, each given its own Logger, log only to
// it, and nothing reaches DefaultLogger or config.Logger: a refused caller, a
// PDU of an unknown type, a handler panic, a peer timed out, a failed TLS
// handshake, the start-up line, a panic in an event handler, and the
// client's own negotiation. Every line went through the global DefaultLogger,
// so two servers could not be told apart in it (#472).
func TestEachServerAndClientLogsToItsOwnLogger(t *testing.T) {
	noGlobalLog(t)
	ctx := context.Background()
	pki := newTestPKI(t)
	serverTLS, _ := mutualTLS(pki)

	logA, bufA := ownLog()
	logB, bufB := ownLog()
	logC, bufC := ownLog()
	eventsA := NewEventManager()
	eventsA.On(EVTAssocReleased, func(*Event) { panic("an observer of ARCHIVE_A fails") })
	a, err := StartServer(ctx, SCPConfig{
		AETitle: "ARCHIVE_A", Port: 0, BindAddress: "127.0.0.1", Logger: logA, Events: eventsA,
		Network: NetworkConfig{DIMSETimeout: 300 * time.Millisecond},
		Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
			if req.CallingAE == "NOBODY_A" {
				return RejectCallingAE()
			}
			return nil
		},
	}, &faultyHandler{where: "C-ECHO"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := StartServer(ctx, SCPConfig{
		AETitle: "ARCHIVE_B", Port: 0, BindAddress: "127.0.0.1", Logger: logB, TLS: serverTLS,
		Network: NetworkConfig{DIMSETimeout: 300 * time.Millisecond},
	}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	// Stopped before the global logger is checked.
	defer b.Stop()
	defer a.Stop()

	clientEvents := NewEventManager()
	clientEvents.On(EVTAssocEstablished, func(*Event) { panic("an observer of the client fails") })
	client := func(calling string, events *EventManager, l *slog.Logger) *SCU {
		return NewSCU(SCUConfig{CallingAE: calling, CalledAE: "ARCHIVE_A", Address: a.Addr(), Events: events, Logger: l,
			ExtendedNegotiation: &ExtendedNegotiation{AsyncOperations: &AsynchronousOperationsWindow{}}})
	}

	// ARCHIVE_A: a refused caller, a handler panic, a timed-out peer, a PDU
	// of an unknown type, a panic in an event handler.
	if err := client("NOBODY_A", nil, logC).Associate(ctx, DefaultVerificationContexts()); err == nil {
		t.Error("NOBODY_A was admitted")
	}
	faulty := client("FAULTY", clientEvents, logC)
	if err := faulty.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatal(err)
	}
	_ = faulty.Echo(ctx)
	idle := client("IDLE_A", nil, logC)
	if err := idle.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatal(err)
	}
	odd := client("ODD_A", nil, logC)
	if err := odd.Associate(ctx, append(DefaultVerificationContexts(),
		PresentationContextItem{ID: 99, AbstractSyntax: "1.2.826.0.1.3680043.2.1125.472", TransferSyntaxes: DefaultTransferSyntaxes()})); err != nil {
		t.Fatal(err)
	}
	if _, err := odd.Association().transport.conn.Write([]byte{0x99, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	released := client("RELEASED_A", nil, logC)
	if err := released.Associate(ctx, DefaultVerificationContexts()); err != nil {
		t.Fatal(err)
	}
	_ = released.Release(ctx)

	// ARCHIVE_B: plain TCP to its TLS port.
	conn, err := net.Dial("tcp", b.Addr())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := EncodePDU(associateRQ("PLAIN_B", "ARCHIVE_B"))
	_, _ = conn.Write(raw)
	defer conn.Close()

	eventually(t, "ARCHIVE_A", bufA, "Server ARCHIVE_A started", `refusing \"NOBODY_A`, `panic serving the association with \"FAULTY`,
		"aborting the association with IDLE_A", "unknown PDU type: 0x99", "panic in a handler of EVT_RELEASED", "refused presentation context 99")
	eventually(t, "ARCHIVE_B", bufB, "Server ARCHIVE_B started", "failed to read association request", "first record does not look like a TLS handshake")
	eventually(t, "the client", bufC, "asynchronous operations: an unlimited window", "panic in a handler of EVT_ESTABLISHED")
	// Unreachable, its connection would be closed by its finalizer, and IDLE_A
	// would not be timed out.
	runtime.KeepAlive(idle)

	for name, c := range map[string]struct {
		log *logBuffer
		not []string
	}{
		"ARCHIVE_A":  {bufA, []string{"ARCHIVE_B", "TLS", "asynchronous operations"}},
		"ARCHIVE_B":  {bufB, []string{"ARCHIVE_A", "NOBODY_A", "FAULTY", "IDLE_A", "asynchronous operations"}},
		"the client": {bufC, []string{"ARCHIVE_B", "refusing", "panic serving", "aborting the association"}},
	} {
		for _, other := range c.not {
			if strings.Contains(c.log.String(), other) {
				t.Errorf("%s's log has %q, which is another's:\n%s", name, other, c.log.String())
			}
		}
	}
}

// TestNoLoggerKeepsTheGlobalOne: a server and a client given no Logger log
// through DefaultLogger, as before.
func TestNoLoggerKeepsTheGlobalOne(t *testing.T) {
	global := logTo(t)
	server, err := StartServer(context.Background(), SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1", Network: NetworkConfig{DIMSETimeout: 200 * time.Millisecond},
	}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	scu := NewSCU(SCUConfig{CallingAE: "IDLE", CalledAE: "ARCHIVE", Address: server.Addr(),
		ExtendedNegotiation: &ExtendedNegotiation{AsyncOperations: &AsynchronousOperationsWindow{}}})
	if err := scu.Associate(context.Background(), DefaultVerificationContexts()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "DefaultLogger", global, "aborting the association with IDLE", "asynchronous operations: an unlimited window")
	runtime.KeepAlive(scu)
}
