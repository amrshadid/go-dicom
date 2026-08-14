package network

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/amrshadid/go-dicom/config"
)

// LogLevel controls the verbosity of network logging.
type LogLevel int

const (
	LogLevelSilent LogLevel = iota // No logging
	LogLevelError                  // Errors only
	LogLevelWarn                   // Errors + warnings
	LogLevelInfo                   // Errors + warnings + info
	LogLevelDebug                  // Everything including PDU/DIMSE details
)

// String returns the level's name, so that a logging configuration reads as
// something other than an integer when printed.
func (l LogLevel) String() string {
	switch l {
	case LogLevelSilent:
		return "silent"
	case LogLevelError:
		return "error"
	case LogLevelWarn:
		return "warn"
	case LogLevelInfo:
		return "info"
	case LogLevelDebug:
		return "debug"
	default:
		return "unknown"
	}
}

// slogLevel maps a network log level onto the slog level a message is emitted at.
func (l LogLevel) slogLevel() slog.Level {
	switch l {
	case LogLevelError:
		return slog.LevelError
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelInfo:
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}

// Logger reports what the network layer is doing.
//
// Messages go to [config.Logger] unless an output is set explicitly, so a single
// config.SetLogger call controls everything this library emits — the network
// layer included. Two filters apply in order: this Logger's LogLevel, then the
// level of the slog handler underneath.
//
// This used to wrap a *log.Logger of its own, and the 49 places in this package
// that reported anything did not use it — they called log.Printf directly, onto
// the standard logger. A program embedding an SCP therefore got association
// errors on its stderr with no way to redirect or silence them: DefaultLogger
// was already silent, and setting its level changed nothing because nothing
// consulted it.
type Logger struct {
	mu    sync.RWMutex
	level LogLevel

	// own is non-nil only when the caller named an output. Otherwise messages go
	// to config.Logger, read at call time so that a later config.SetLogger is
	// picked up rather than captured here at construction.
	own *slog.Logger
}

// DefaultLogger is the package-level logger every message in this package goes
// through.
//
// It reports warnings and errors, matching config.Logger's own default, and
// writes through it — so config.SetLogger redirects or silences the network layer
// along with the rest of the library. SetDefaultLogLevel narrows or widens just
// this package.
var DefaultLogger = NewLogger(LogLevelWarn, nil)

// NewLogger returns a Logger emitting at level and above.
//
// A nil output means config.Logger, which is what the package-level DefaultLogger
// uses and what most callers want: one place configures the whole library. Pass a
// writer to send this logger's messages somewhere else instead.
func NewLogger(level LogLevel, output io.Writer) *Logger {
	l := &Logger{level: level}
	if output != nil {
		l.own = newTextLogger(output)
	}
	return l
}

// newTextLogger builds a logger writing to w at debug level, leaving the level
// filtering to Logger.level so that SetLevel works without rebuilding a handler.
func newTextLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// SetLevel changes the log level.
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput sends this logger's messages to w instead of config.Logger.
//
// Passing nil restores the default of writing through config.Logger.
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if w == nil {
		l.own = nil
		return
	}
	l.own = newTextLogger(w)
}

// log emits at the given level if this Logger admits it.
//
// The message keeps its printf shape rather than becoming slog key-value pairs:
// the call sites read as sentences, and rewriting 49 of them into attributes is
// how a transcription error gets introduced into the reporting of failures. The
// component attribute is there so a consumer can filter the network layer out.
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	l.mu.RLock()
	target := l.own
	enabled := l.level >= level
	l.mu.RUnlock()

	if !enabled {
		return
	}

	if target == nil {
		// Read config.Logger at call time, not at construction: a caller that
		// replaces it later expects the change to take effect.
		target = config.Logger
		if target == nil {
			return
		}
	}

	target.LogAttrs(context.Background(), level.slogLevel(),
		fmt.Sprintf(format, args...), slog.String("component", "network"))
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LogLevelError, format, args...)
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LogLevelWarn, format, args...)
}

// Info logs an informational message.
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LogLevelInfo, format, args...)
}

// Debug logs a debug message.
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LogLevelDebug, format, args...)
}

// SetDefaultLogLevel sets the log level on the package-level logger.
//
// Pass LogLevelSilent to stop the network layer reporting anything.
func SetDefaultLogLevel(level LogLevel) {
	DefaultLogger.SetLevel(level)
}

// DebugLogger enables debug-level logging to stderr.
// This is the equivalent of pynetdicom's debug_logger().
//
// It writes to stderr directly rather than through config.Logger, since
// config.Logger's own level would otherwise discard the debug messages this is
// being called to see.
func DebugLogger() {
	DefaultLogger.SetLevel(LogLevelDebug)
	DefaultLogger.SetOutput(os.Stderr)
}

// LoggingEventHandlers returns event handlers that log association lifecycle events.
// Attach these to an SCP's EventManager for operational visibility.
func LoggingEventHandlers(logger *Logger) map[EventType]EventHandler {
	return map[EventType]EventHandler{
		EVTConnOpen: func(e *Event) {
			logger.Info("Connection opened: %s", e.RemoteAddr)
		},
		EVTConnClose: func(e *Event) {
			logger.Info("Connection closed: %s", e.RemoteAddr)
		},
		EVTAssocRequested: func(e *Event) {
			logger.Info("Association requested: %s -> %s from %s", e.CallingAE, e.CalledAE, e.RemoteAddr)
		},
		EVTAssocAccepted: func(e *Event) {
			logger.Info("Association accepted: %s -> %s", e.CallingAE, e.CalledAE)
		},
		EVTAssocRejected: func(e *Event) {
			logger.Warn("Association rejected: %s -> %s: %s", e.CallingAE, e.CalledAE, e.Description)
		},
		EVTAssocReleased: func(e *Event) {
			logger.Info("Association released: %s", e.CallingAE)
		},
		EVTAssocAborted: func(e *Event) {
			logger.Warn("Association aborted: %s: %s", e.CallingAE, e.Description)
		},
		EVTDIMSERecv: func(e *Event) {
			logger.Debug("DIMSE received: command=0x%04X from %s", e.CommandType, e.CallingAE)
		},
		EVTDIMSESent: func(e *Event) {
			logger.Debug("DIMSE sent: command=0x%04X to %s", e.CommandType, e.CalledAE)
		},
		EVTPDURecv: func(e *Event) {
			logger.Debug("PDU received: type=%s from %s", PDUTypeString(e.PDUType), e.RemoteAddr)
		},
		EVTPDUSent: func(e *Event) {
			logger.Debug("PDU sent: type=%s to %s", PDUTypeString(e.PDUType), e.RemoteAddr)
		},
	}
}

// FormatStatus returns a human-readable string for a DIMSE status code.
func FormatStatus(status uint16) string {
	category := CategorizeStatus(status)
	return fmt.Sprintf("0x%04X (%s)", status, category)
}
