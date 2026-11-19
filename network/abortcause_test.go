package network

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
)

// abortLog keeps the abort events one side reports.
type abortLog struct {
	mu     sync.Mutex
	events []*Event
}

func (l *abortLog) manager() *EventManager {
	em := NewEventManager()
	keep := func(e *Event) {
		if _, ok := e.PDU.(*AbortPDU); !ok {
			return
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		l.events = append(l.events, e)
	}
	em.On(EVTAssocAborted, keep)
	em.On(EVTPDUSent, keep)
	em.On(EVTPDURecv, keep)
	return em
}

// wait returns the abort events once there are n of them.
func (l *abortLog) wait(t *testing.T, n int) []*Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		l.mu.Lock()
		got := append([]*Event(nil), l.events...)
		l.mu.Unlock()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d abort events, want %d", len(got), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sentAbort checks the two events of an A-ABORT this side sent, EVTPDUSent
// and EVTAssocAborted, and returns their LocalAbort.
func sentAbort(t *testing.T, events []*Event) *LocalAbort {
	t.Helper()
	var sent, aborted *Event
	for _, e := range events {
		switch e.Type {
		case EVTPDUSent:
			sent = e
		case EVTAssocAborted:
			aborted = e
		}
	}
	if sent == nil || aborted == nil {
		t.Fatalf("events %v: want EVTPDUSent and EVTAssocAborted of the A-ABORT", events)
	}
	if sent.LocalAbort != aborted.LocalAbort || aborted.LocalAbort == nil {
		t.Fatalf("EVTPDUSent carries %+v and EVTAssocAborted %+v; want the same cause on both", sent.LocalAbort, aborted.LocalAbort)
	}
	return aborted.LocalAbort
}

// causeServer starts a server reporting to log, with the given configuration.
func causeServer(t *testing.T, cfg SCPConfig, handler Handler, log *abortLog) *Server {
	t.Helper()
	cfg.AETitle, cfg.Port, cfg.BindAddress, cfg.Events = "ARCHIVE", 0, "127.0.0.1", log.manager()
	server, err := StartServer(context.Background(), cfg, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return server
}

// associated returns an SCU associated with server for Verification.
func associated(t *testing.T, server *Server, events *EventManager) *SCU {
	t.Helper()
	scu := NewSCU(SCUConfig{CallingAE: "SCANNER", CalledAE: "ARCHIVE", Address: server.Addr(), Events: events})
	if err := scu.Associate(context.Background(), DefaultVerificationContexts()); err != nil {
		t.Fatal(err)
	}
	return scu
}

// TestAbortCauses: each kind of abort this side sends is reported with its
// own cause and the error behind it, and the PDU on the wire is what PS3.8
// Table 9-26 says, unchanged. A timeout, a protocol violation and a handler
// panic were all reported as the same EVTAssocAborted with nothing but the
// PDU, and two of them byte for byte the same (#470).
func TestAbortCauses(t *testing.T) {
	logTo(t) // the aborts are logged; not onto the test's output

	t.Run("timeout", func(t *testing.T) {
		log := &abortLog{}
		server := causeServer(t, SCPConfig{Network: NetworkConfig{DIMSETimeout: 200 * time.Millisecond}}, &EchoHandler{}, log)
		associated(t, server, nil)
		events := log.wait(t, 2)
		local := sentAbort(t, events)
		var timeout *TimeoutError
		if local.Cause != AbortCauseTimeout || !errors.As(local.Err, &timeout) {
			t.Errorf("cause %v, error %v; want timeout and a *TimeoutError", local.Cause, local.Err)
		}
		if got := events[0].PDUBytes(); !bytes.Equal(got, []byte{7, 0, 0, 0, 0, 4, 0, 0, 2, 0}) {
			t.Errorf("A-ABORT on the wire % x, want service-provider, reason-not-specified", got)
		}
	})

	t.Run("protocol violation", func(t *testing.T) {
		log := &abortLog{}
		server := causeServer(t, SCPConfig{}, &EchoHandler{}, log)
		scu := associated(t, server, nil)
		// A second A-ASSOCIATE-RQ on an established association (AA-8).
		if err := scu.Association().transport.WritePDU(context.Background(), associateRQ("SCANNER", "ARCHIVE")); err != nil {
			t.Fatal(err)
		}
		events := log.wait(t, 2)
		local := sentAbort(t, events)
		if local.Cause != AbortCauseProtocolViolation || local.Err == nil || !strings.Contains(local.Err.Error(), "AssociateRQ") {
			t.Errorf("cause %v, error %v; want protocol-violation naming the PDU", local.Cause, local.Err)
		}
		if got := events[0].PDUBytes(); !bytes.Equal(got, []byte{7, 0, 0, 0, 0, 4, 0, 0, 2, 2}) {
			t.Errorf("A-ABORT on the wire % x, want service-provider, unexpected-PDU", got)
		}
	})

	t.Run("unknown command", func(t *testing.T) {
		log := &abortLog{}
		server := causeServer(t, SCPConfig{}, &EchoHandler{}, log)
		scu := associated(t, server, nil)
		ds := dataset.NewDataset()
		addUIElement(ds, tagAffectedSOPClassUID, VerificationSOPClassUID)
		addUSElement(ds, tagCommandField, 0x0002)
		addUSElement(ds, tagMessageID, 1)
		addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
		cmd, err := EncodeCommandDataset(ds)
		if err != nil {
			t.Fatal(err)
		}
		if err := scu.Association().SendPData(context.Background(), 1, cmd, true); err != nil {
			t.Fatal(err)
		}
		events := log.wait(t, 2)
		local := sentAbort(t, events)
		if local.Cause != AbortCauseProtocolViolation || local.Err == nil || !strings.Contains(local.Err.Error(), "0x0002") {
			t.Errorf("cause %v, error %v; want protocol-violation naming the command", local.Cause, local.Err)
		}
	})

	t.Run("wrong first PDU", func(t *testing.T) {
		log := &abortLog{}
		server := causeServer(t, SCPConfig{}, &EchoHandler{}, log)
		conn, err := net.Dial("tcp", server.Addr())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		raw, _ := EncodePDU(&ReleaseRQ{})
		if _, err := conn.Write(raw); err != nil {
			t.Fatal(err)
		}
		events := log.wait(t, 2)
		local := sentAbort(t, events)
		if local.Cause != AbortCauseProtocolViolation || local.Err == nil {
			t.Errorf("cause %v, error %v; want protocol-violation", local.Cause, local.Err)
		}
	})

	t.Run("handler panic", func(t *testing.T) {
		log := &abortLog{}
		server := causeServer(t, SCPConfig{}, &faultyHandler{where: "C-ECHO"}, log)
		scu := NewSCU(SCUConfig{CallingAE: "FAULTY", CalledAE: "ARCHIVE", Address: server.Addr()})
		if err := scu.Associate(context.Background(), DefaultVerificationContexts()); err != nil {
			t.Fatal(err)
		}
		_ = scu.Echo(context.Background())
		events := log.wait(t, 2)
		local := sentAbort(t, events)
		if local.Cause != AbortCauseHandlerPanic || local.Err == nil || !strings.Contains(local.Err.Error(), "C-ECHO") {
			t.Errorf("cause %v, error %v; want handler-panic with the panic's value", local.Cause, local.Err)
		}
	})

	t.Run("server stopping", func(t *testing.T) {
		log := &abortLog{}
		started := make(chan struct{})
		server := causeServer(t, SCPConfig{}, &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
			close(started)
			_, err := io.Copy(io.Discard, data)
			return nil, err
		}}, log)
		scu := NewSCU(SCUConfig{CallingAE: "SCANNER", CalledAE: "ARCHIVE", Address: server.Addr()})
		if err := scu.Associate(context.Background(), []PresentationContextItem{
			{ID: ctxExplicit, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ExplicitVRLittleEndianUID}},
		}); err != nil {
			t.Fatal(err)
		}
		// The data set is started and never finished, then the server stops.
		cmd, _ := EncodeCommandDataset(BuildCStoreRQ(1, CTImageStorageUID, "1.2.3", PriorityMedium))
		_ = scu.Association().SendPData(context.Background(), ctxExplicit, cmd, true)
		_ = sendPDUs(context.Background(), scu, [][]PDVItem{{{PresentationContextID: ctxExplicit, Data: make([]byte, 64)}}})
		<-started
		server.Stop()
		events := log.wait(t, 2)
		local := sentAbort(t, events)
		if local.Cause != AbortCauseStopping || !errors.Is(local.Err, context.Canceled) {
			t.Errorf("cause %v, error %v; want stopping and context.Canceled", local.Cause, local.Err)
		}
	})

	t.Run("requested", func(t *testing.T) {
		log := &abortLog{}
		server := causeServer(t, SCPConfig{}, &EchoHandler{}, &abortLog{})
		scu := associated(t, server, log.manager())
		if err := scu.Abort(context.Background()); err != nil {
			t.Fatal(err)
		}
		events := log.wait(t, 2)
		local := sentAbort(t, events)
		if local.Cause != AbortCauseRequested || local.Err != nil {
			t.Errorf("cause %v, error %v; want requested and no error", local.Cause, local.Err)
		}
	})

	t.Run("sent by the peer", func(t *testing.T) {
		log := &abortLog{}
		server := causeServer(t, SCPConfig{}, &EchoHandler{}, log)
		scu := associated(t, server, nil)
		_ = scu.Abort(context.Background())
		for _, e := range log.wait(t, 2) {
			if e.LocalAbort != nil {
				t.Errorf("%s of an A-ABORT the peer sent carries %+v; want nil", EventTypeString(e.Type), e.LocalAbort)
			}
		}
	})
}

// TestAbortCauseNames: each cause has the name its String gives it.
func TestAbortCauseNames(t *testing.T) {
	for cause, want := range map[AbortCause]string{
		AbortCauseUnknown: "unknown", AbortCauseTimeout: "timeout",
		AbortCauseProtocolViolation: "protocol-violation", AbortCauseHandlerPanic: "handler-panic",
		AbortCauseStopping: "stopping", AbortCauseRequested: "requested",
	} {
		if got := cause.String(); got != want {
			t.Errorf("AbortCause(%d).String() = %q, want %q", cause, got, want)
		}
	}
}
