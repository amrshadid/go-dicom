package network

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/config"
)

// syncBuffer is a bytes.Buffer that may be written and read from different
// goroutines.
//
// A plain bytes.Buffer is not safe for that, and the SCP reports from the
// goroutine handling the association while the test inspects what it wrote — so
// using one directly is itself a data race, which -race duly reports and which
// says nothing about the code under test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// withConfigLogger points config.Logger at a buffer for the duration of a test
// and restores it afterwards, so tests do not leak logging configuration into
// each other.
func withConfigLogger(t *testing.T, level slog.Level) *syncBuffer {
	t.Helper()

	buf := &syncBuffer{}
	previous := config.Logger
	config.Logger = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level}))
	t.Cleanup(func() { config.Logger = previous })

	return buf
}

// withDefaultLoggerLevel restores DefaultLogger's level after a test, since it is
// package state shared with every other test in this file.
func withDefaultLoggerLevel(t *testing.T, level LogLevel) {
	t.Helper()

	DefaultLogger.mu.RLock()
	previous := DefaultLogger.level
	DefaultLogger.mu.RUnlock()

	DefaultLogger.SetLevel(level)
	t.Cleanup(func() { DefaultLogger.SetLevel(previous) })
}

// The whole package once reported through log.Printf, onto the standard logger,
// which a consumer cannot redirect or silence. DefaultLogger existed and was
// documented as silent by default, and 49 call sites ignored it — so an embedded
// SCP wrote association errors onto its host program's stderr and no API stopped
// it.
//
// A behavioral test cannot catch a reintroduction, because a single new
// log.Printf in a rarely taken branch would not be exercised. So this reads the
// package's own source and fails on any direct use of the log package.
func TestNoCallSiteBypassesTheConfigurableLogger(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	fset := token.NewFileSet()
	var offenders []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}

		// An import of "log" is the thing to catch: every way of writing to the
		// standard logger needs it, so this covers Printf, Print, Println, Fatal
		// and Panic without enumerating them.
		for _, imp := range file.Imports {
			if imp.Path.Value != `"log"` {
				continue
			}
			// A named import of something else is not the standard logger.
			if imp.Name != nil && imp.Name.Name != "log" {
				continue
			}
			offenders = append(offenders, fset.Position(imp.Pos()).String())
		}
	}

	if len(offenders) > 0 {
		t.Errorf("these files import \"log\" directly:\n  %s\n\n"+
			"Messages must go through DefaultLogger, which writes to config.Logger, "+
			"so that a program embedding this package can redirect or silence them. "+
			"Writing to the standard logger takes over the host program's stderr with "+
			"no way to turn it off.", strings.Join(offenders, "\n  "))
	}

	// Guard the guard: if the AST walk silently stopped matching, the test above
	// would pass on a package full of log.Printf. Confirm it saw real files.
	if len(entries) < 10 {
		t.Fatalf("only %d entries in the package directory; the scan is not looking "+
			"at what it should be", len(entries))
	}
}

func TestDefaultLoggerWritesThroughConfigLogger(t *testing.T) {
	buf := withConfigLogger(t, slog.LevelDebug)
	withDefaultLoggerLevel(t, LogLevelWarn)

	DefaultLogger.Error("accept error: %v", os.ErrClosed)

	got := buf.String()
	if !strings.Contains(got, "accept error") {
		t.Errorf("the message did not reach config.Logger; it wrote %q", got)
	}
	// The component attribute is what lets a consumer filter the network layer
	// out of a shared logger.
	if !strings.Contains(got, "component=network") {
		t.Errorf("the message carries no component attribute: %q", got)
	}
}

func TestConfigLoggerCanSilenceTheNetworkLayer(t *testing.T) {
	buf := withConfigLogger(t, slog.LevelError+1) // above every level we emit
	withDefaultLoggerLevel(t, LogLevelDebug)

	DefaultLogger.Error("an error")
	DefaultLogger.Warn("a warning")
	DefaultLogger.Info("some information")
	DefaultLogger.Debug("a detail")

	if got := buf.String(); got != "" {
		t.Errorf("config.Logger was set above every level this package emits, "+
			"and it still wrote: %q", got)
	}
}

func TestSetDefaultLogLevelSilencesTheNetworkLayerAlone(t *testing.T) {
	buf := withConfigLogger(t, slog.LevelDebug)
	withDefaultLoggerLevel(t, LogLevelSilent)

	DefaultLogger.Error("an error")
	DefaultLogger.Warn("a warning")

	if got := buf.String(); got != "" {
		t.Errorf("LogLevelSilent still wrote: %q", got)
	}

	// And the rest of the library keeps logging, which is the point of the level
	// being per-package rather than global.
	config.Logger.Warn("a message from elsewhere in the library")
	if !strings.Contains(buf.String(), "elsewhere") {
		t.Error("silencing the network layer also silenced config.Logger")
	}
}

func TestLevelsFilterInOrder(t *testing.T) {
	cases := []struct {
		level LogLevel
		want  []string
		omit  []string
	}{
		{LogLevelSilent, nil, []string{"an error", "a warning", "information", "a detail"}},
		{LogLevelError, []string{"an error"}, []string{"a warning", "information", "a detail"}},
		{LogLevelWarn, []string{"an error", "a warning"}, []string{"information", "a detail"}},
		{LogLevelInfo, []string{"an error", "a warning", "information"}, []string{"a detail"}},
		{LogLevelDebug, []string{"an error", "a warning", "information", "a detail"}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.level.String(), func(t *testing.T) {
			buf := withConfigLogger(t, slog.LevelDebug)
			withDefaultLoggerLevel(t, tc.level)

			DefaultLogger.Error("an error")
			DefaultLogger.Warn("a warning")
			DefaultLogger.Info("some information")
			DefaultLogger.Debug("a detail")

			got := buf.String()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("at %v, %q is missing from %q", tc.level, want, got)
				}
			}
			for _, omit := range tc.omit {
				if strings.Contains(got, omit) {
					t.Errorf("at %v, %q should have been filtered out of %q", tc.level, omit, got)
				}
			}
		})
	}
}

func TestSetOutputDivertsAwayFromConfigLogger(t *testing.T) {
	configBuf := withConfigLogger(t, slog.LevelDebug)

	own := &bytes.Buffer{}
	logger := NewLogger(LogLevelWarn, own)
	logger.Error("a diverted message")

	if !strings.Contains(own.String(), "a diverted message") {
		t.Errorf("the named output received %q", own.String())
	}
	if strings.Contains(configBuf.String(), "a diverted message") {
		t.Error("a logger with its own output still wrote to config.Logger")
	}

	// nil restores the default, so a caller can undo a diversion.
	logger.SetOutput(nil)
	logger.Error("a restored message")
	if !strings.Contains(configBuf.String(), "a restored message") {
		t.Errorf("SetOutput(nil) did not restore config.Logger; it holds %q",
			configBuf.String())
	}
}

// config.Logger is read at call time rather than captured when the Logger is
// built, so that a caller replacing it later has the change take effect. A logger
// constructed at package init — DefaultLogger is — would otherwise hold the
// logger that existed before main ran.
func TestConfigLoggerIsReadAtCallTime(t *testing.T) {
	withDefaultLoggerLevel(t, LogLevelWarn)
	withConfigLogger(t, slog.LevelDebug) // replaced below, before anything is logged

	later := &bytes.Buffer{}
	previous := config.Logger
	config.Logger = slog.New(slog.NewTextHandler(later, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { config.Logger = previous })

	DefaultLogger.Error("a message after the logger was replaced")

	if !strings.Contains(later.String(), "after the logger was replaced") {
		t.Errorf("the replacement logger received %q", later.String())
	}
}

// A nil config.Logger is reachable: it is an exported package variable, so a
// caller can assign nil to it. Logging must not panic in that case.
func TestNilConfigLoggerDoesNotPanic(t *testing.T) {
	withDefaultLoggerLevel(t, LogLevelDebug)

	previous := config.Logger
	config.Logger = nil
	t.Cleanup(func() { config.Logger = previous })

	DefaultLogger.Error("this must not panic")
	DefaultLogger.Warn("nor this")
}

func TestLoggerIsSafeForConcurrentUse(t *testing.T) {
	withConfigLogger(t, slog.LevelDebug)
	withDefaultLoggerLevel(t, LogLevelDebug)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			DefaultLogger.Warn("concurrent message %d", n)
		}(i)
		go func() {
			defer wg.Done()
			DefaultLogger.SetLevel(LogLevelDebug)
		}()
	}
	wg.Wait()
}

// The proof that the fix does what the issue was about: a running SCP hitting a
// real error path must not write to the host program's stderr.
//
// os.Stderr is swapped for a pipe here rather than trusting that the call sites
// were all converted, because the failure this guards against is a message
// arriving on the embedder's terminal — and that is what stderr is.
func TestAnSCPDoesNotWriteToTheProcessStderr(t *testing.T) {
	logged := withConfigLogger(t, slog.LevelDebug)
	withDefaultLoggerLevel(t, LogLevelDebug)

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	realStderr := os.Stderr
	os.Stderr = write
	t.Cleanup(func() { os.Stderr = realStderr })

	// Drain the pipe concurrently so that a writer cannot block on a full buffer.
	captured := &syncBuffer{}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(captured, read)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	scp := NewSCP(SCPConfig{AETitle: "TEST_SCP", BindAddress: "127.0.0.1"})
	scp.SetHandler(&EchoHandler{})

	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			transport, acceptErr := ln.Accept(ctx)
			if acceptErr != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				scp.handleConnection(ctx, transport)
			}()
		}
	}()

	// Connect and hang up without sending an A-ASSOCIATE-RQ. The SCP reports
	// "failed to read association request", which was one of the log.Printf sites.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = conn.Close()

	// Then send something that is not an association request, for the second path.
	conn2, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_, _ = conn2.Write([]byte{0x07, 0x00, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00})
	_ = conn2.Close()

	// Give the handler goroutines time to run and report.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && logged.Len() == 0 {
		time.Sleep(20 * time.Millisecond)
	}

	_ = ln.Close()
	cancel()
	wg.Wait()

	_ = write.Close()
	<-drained
	_ = read.Close()

	if got := captured.String(); got != "" {
		t.Errorf("the SCP wrote to the process stderr:\n%s\n\n"+
			"Everything it reports must go through config.Logger so that an embedding "+
			"program can redirect or silence it.", got)
	}

	// And confirm the messages were not simply lost: the point is redirection, not
	// suppression.
	if logged.Len() == 0 {
		t.Error("nothing reached config.Logger either; the error paths reported nothing " +
			"at all, so this test is not proving anything")
	} else {
		t.Logf("config.Logger received:\n%s", logged.String())
	}
}

// A bare TCP connect-and-close is not a fault, and must not be logged as one.
//
// This is what every health check, load-balancer probe and port scan does: open a
// connection, see that it opened, close it. The SCP read it as a failed association
// request and logged an error per probe, so a server behind a load balancer produced
// a steady stream of errors describing itself working correctly.
//
// It was found by watching a demo recording's server log rather than by a test —
// the readiness probe in front of the demo was `nc -z`, and the log the recording
// was meant to show quiet had an error in it from the probe itself.
func TestABareConnectIsNotLoggedAsAnError(t *testing.T) {
	logged := withConfigLogger(t, slog.LevelDebug)
	withDefaultLoggerLevel(t, LogLevelDebug)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	scp := NewSCP(SCPConfig{AETitle: "TEST_SCP", BindAddress: "127.0.0.1"})
	scp.SetHandler(&EchoHandler{})

	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			transport, acceptErr := ln.Accept(ctx)
			if acceptErr != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				scp.handleConnection(ctx, transport)
			}()
		}
	}()

	// Exactly what `nc -z` does.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = conn.Close()

	// Wait for the handler to have reported something, so that an empty log means
	// "nothing was logged" rather than "the test read it too early".
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && logged.Len() == 0 {
		time.Sleep(20 * time.Millisecond)
	}

	_ = ln.Close()
	cancel()
	wg.Wait()

	got := logged.String()
	if got == "" {
		t.Fatal("the probe was not reported at any level, so this test cannot tell " +
			"a demotion to debug from the code path never having run")
	}
	for _, level := range []string{"level=ERROR", "level=WARN"} {
		if strings.Contains(got, level) {
			t.Errorf("a bare connect-and-close was logged at %s:\n%s\n\n"+
				"A probe that opens and closes a connection is how health checks work. "+
				"It reaches the PDU read as EOF and belongs at debug.",
				strings.TrimPrefix(level, "level="), got)
		}
	}
	if !strings.Contains(got, "level=DEBUG") {
		t.Errorf("expected the probe at debug level, got:\n%s", got)
	}
}
