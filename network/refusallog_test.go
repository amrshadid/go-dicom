package network

import (
	"context"
	"strings"
	"testing"
	"time"
)

// refusalLines returns the lines of a log that report an association being
// refused or rejected.
func refusalLines(log string) []string {
	var out []string
	for _, line := range strings.Split(log, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "refusing") || strings.Contains(lower, "rejected") ||
			strings.Contains(lower, "rejecting") {
			out = append(out, line)
		}
	}
	return out
}

// awaitLines waits until the log holds at least n refusal lines, and a little
// longer for any that should not be there, then returns them.
func awaitLines(log *lockedBuffer, n int) []string {
	deadline := time.Now().Add(5 * time.Second)
	for len(refusalLines(log.String())) < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	return refusalLines(log.String())
}

// TestRefusalIsLoggedOnceWithLoggingEventHandlers: a server that logs its
// events with LoggingEventHandlers, as a program wanting a log of its
// associations does, logged each refusal twice: once by the SCP where it
// decided, with the caller's address and the cause, and once more by the
// handler of EVTAssocRejected. The SCP's line is the one kept, since only it
// knows why, and it is there for a server with no event handlers too; the
// handler logs the rejections this side is sent. Every refusal an SCP sends
// is checked: by the admission function, for a called AE title that is not
// the SCP's, and over MaxAssociations.
func TestRefusalIsLoggedOnceWithLoggingEventHandlers(t *testing.T) {
	log := captureLog(t)
	events := NewEventManager()
	for eventType, handler := range LoggingEventHandlers(DefaultLogger) {
		events.On(eventType, handler)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1", Events: events, MaxAssociations: 1,
		Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
			if req.CallingAE == "STRANGER" {
				return RejectCallingAE()
			}
			return nil
		},
	}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	before := 0
	check := func(what string) {
		t.Helper()
		lines := awaitLines(log, before+1)
		if got := lines[before:]; len(got) != 1 {
			t.Errorf("%s was logged %d times, want once:\n%s", what, len(got), strings.Join(got, "\n"))
		}
		before = len(lines)
	}

	wantRejection(t, "STRANGER", associateAs(t, server.Addr(), "STRANGER", "ARCHIVE"), *RejectCallingAE())
	check("a refusal by the admission function")

	wantRejection(t, "WRONGTITLE", associateAs(t, server.Addr(), "SCANNER", "WRONGTITLE"), *RejectCalledAE())
	check("a refusal of a called AE title that is not the SCP's")
}

// TestCalledAERefusalIsLoggedOnce: an SCP with no admission function refuses
// a called AE title that is not its own. Nothing logged that refusal but
// LoggingEventHandlers, so a server without them refused callers silently;
// the SCP now logs it as it logs every refusal it decides, and the event
// handler does not log it again. At the default warning level the SCP's
// info line is not written, and the handler's line is still there: giving
// way unconditionally would have left a server logging its events with no
// word of a refusal.
func TestCalledAERefusalIsLoggedOnce(t *testing.T) {
	for _, c := range []struct {
		withHandlers bool
		level        LogLevel
	}{{false, LogLevelInfo}, {true, LogLevelInfo}, {true, LogLevelWarn}} {
		withHandlers := c.withHandlers
		log := captureLog(t)
		DefaultLogger.SetLevel(c.level)
		events := NewEventManager()
		if withHandlers {
			for eventType, handler := range LoggingEventHandlers(DefaultLogger) {
				events.On(eventType, handler)
			}
		}
		server, err := StartServer(context.Background(), SCPConfig{
			AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1", Events: events,
		}, &EchoHandler{})
		if err != nil {
			t.Fatal(err)
		}
		wantRejection(t, "WRONGTITLE", associateAs(t, server.Addr(), "SCANNER", "WRONGTITLE"), *RejectCalledAE())
		if got := awaitLines(log, 1); len(got) != 1 {
			t.Errorf("with LoggingEventHandlers %v at level %v, the refusal was logged %d times, want once:\n%s",
				withHandlers, c.level, len(got), strings.Join(got, "\n"))
		}
		server.Stop()
	}
}

// TestRejectionReceivedIsLogged: the requestor's side still logs a rejection
// it is sent, once, through LoggingEventHandlers on its own events: nothing
// else on that side reports it.
func TestRejectionReceivedIsLogged(t *testing.T) {
	addr, _ := admitServer(t, SCPConfig{Admit: func(context.Context, *AssociationRequest) *AssociationRejection {
		return RejectCallingAE()
	}})
	log := captureLog(t)
	events := NewEventManager()
	for eventType, handler := range LoggingEventHandlers(DefaultLogger) {
		events.On(eventType, handler)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scu := NewSCU(SCUConfig{CallingAE: "REQUESTOR", CalledAE: "ARCHIVE", Address: addr, Events: events})
	if err := scu.Associate(ctx, DefaultVerificationContexts()); err == nil {
		t.Fatal("a refused caller was admitted")
	}
	var mine []string
	for _, line := range awaitLines(log, 1) {
		if strings.Contains(line, "Association rejected") {
			mine = append(mine, line)
		}
	}
	if len(mine) != 1 {
		t.Errorf("the requestor logged the rejection it was sent %d times, want once:\n%s",
			len(mine), log.String())
	}
}
