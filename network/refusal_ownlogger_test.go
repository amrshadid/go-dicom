package network

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/config"
)

// infoLogger is a slog.Logger writing what is at the info level and above
// to log.
func infoLogger(log *lockedBuffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// globalLog sends config.Logger to a buffer at the info level, and
// DefaultLogger through it at the info level, for the test.
func globalLog(t *testing.T) *lockedBuffer {
	t.Helper()
	log := &lockedBuffer{}
	previousConfig, previousDefault := config.Logger, DefaultLogger
	config.SetLogger(infoLogger(log))
	DefaultLogger = NewLogger(LogLevelInfo, nil)
	t.Cleanup(func() {
		config.Logger = previousConfig
		DefaultLogger = previousDefault
	})
	return log
}

// TestRefusalIsLoggedOnceWhereTheServerHasItsOwnLogger: whether
// LoggingEventHandlers leaves out a refusal is decided by whether the SCP's
// own line reached the same log. That was asked of DefaultLogger, which is
// the SCP's logger only when the server has none of its own; since
// SCPConfig.Logger the SCP's line goes to the server's logger. So a server
// with its own logger, logging its events to that same logger, had each
// refusal in it twice; and one logging its events to the global log, with
// DefaultLogger at the info level, had the refusal left out of the global
// log, where the SCP's line never went. Each case: every log that should
// have the refusal has it once.
func TestRefusalIsLoggedOnceWhereTheServerHasItsOwnLogger(t *testing.T) {
	for _, c := range []struct {
		name string
		// handlers is the logger LoggingEventHandlers is given; nil for none.
		handlers func(server *Server) *Logger
		// wantOwn and wantGlobal are the refusal lines wanted in the
		// server's own log and in the global one.
		wantOwn, wantGlobal int
		// separate is the lines wanted in a log of the handlers' own.
		separate *lockedBuffer
	}{
		{"handlers given the server's logger", func(s *Server) *Logger { return s.Logger }, 1, 0, nil},
		{"handlers given DefaultLogger", func(*Server) *Logger { return DefaultLogger }, 1, 1, nil},
		{"handlers given a logger of their own", nil, 1, 0, &lockedBuffer{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			global := globalLog(t)
			own := &lockedBuffer{}
			events := NewEventManager()
			server, err := StartServer(context.Background(), SCPConfig{
				AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1", Events: events, Logger: infoLogger(own),
			}, &EchoHandler{})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Stop()
			handlerLog := (*Logger)(nil)
			if c.separate != nil {
				handlerLog = NewLogger(LogLevelInfo, c.separate)
			} else {
				handlerLog = c.handlers(server)
			}
			for eventType, handler := range LoggingEventHandlers(handlerLog) {
				events.On(eventType, handler)
			}

			wantRejection(t, "WRONGTITLE", associateAs(t, server.Addr(), "SCANNER", "WRONGTITLE"), *RejectCalledAE())
			awaitLines(own, c.wantOwn)
			for _, l := range []struct {
				name string
				log  *lockedBuffer
				want int
			}{{"the server's own log", own, c.wantOwn}, {"the global log", global, c.wantGlobal},
				{"the handlers' own log", c.separate, 1}} {
				if l.log == nil {
					continue
				}
				if got := refusalLines(l.log.String()); len(got) != l.want {
					t.Errorf("%s has the refusal %d times, want %d:\n%s", l.name, len(got), l.want, strings.Join(got, "\n"))
				}
			}
		})
	}
}

// TestRefusalOfAStoppingServerIsLogged: a server that is stopping refuses
// an association asked for on a connection it accepted before the stop.
// The SCP logged nothing of that refusal, and LoggingEventHandlers left it
// out wherever the SCP's lines would have gone, so a server logging its
// events at the info level had no word of it. It is logged once, by the
// SCP, as every refusal it decides is.
func TestRefusalOfAStoppingServerIsLogged(t *testing.T) {
	for _, withHandlers := range []bool{false, true} {
		global := globalLog(t)
		handler := newGatedStore(time.Second)
		server, scu, _ := shutdownServer(t, handler)
		if withHandlers {
			for eventType, h := range LoggingEventHandlers(server.Logger) {
				server.Events.On(eventType, h)
			}
		}
		early, err := net.Dial("tcp", server.Addr())
		if err != nil {
			t.Fatal(err)
		}
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
		stored := make(chan error, 1)
		go func() { stored <- scu.Store(context.Background(), corpusInstance(t)) }()
		<-handler.started
		stopped := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stopped <- server.Shutdown(ctx)
		}()
		for deadline := time.Now().Add(5 * time.Second); !server.scp.draining() && time.Now().Before(deadline); {
			time.Sleep(5 * time.Millisecond)
		}
		raw, err := EncodePDU(associateRQ("SCANNER", "ARCHIVE"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := early.Write(raw); err != nil {
			t.Fatal(err)
		}
		rj := make([]byte, 10)
		_ = early.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := early.Read(rj); err != nil || rj[0] != 0x03 {
			t.Fatalf("the A-ASSOCIATE-RQ was answered % x, %v; want an A-ASSOCIATE-RJ", rj, err)
		}
		early.Close()
		<-stored
		_ = scu.Release(context.Background())
		<-stopped
		if got := awaitLines(global, 1); len(got) != 1 {
			t.Errorf("with LoggingEventHandlers %v, the refusal of a stopping server was logged %d times, want once:\n%s",
				withHandlers, len(got), strings.Join(got, "\n"))
		}
	}
}
