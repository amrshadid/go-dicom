package network

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
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

// Logger provides structured logging for DICOM network operations.
type Logger struct {
	mu     sync.RWMutex
	level  LogLevel
	logger *log.Logger
}

// DefaultLogger is the package-level logger (silent by default).
var DefaultLogger = NewLogger(LogLevelSilent, os.Stderr)

// NewLogger creates a new Logger. If output is nil, os.Stderr is used.
func NewLogger(level LogLevel, output io.Writer) *Logger {
	if output == nil {
		output = os.Stderr
	}
	return &Logger{
		level:  level,
		logger: log.New(output, "", log.LstdFlags),
	}
}

// SetLevel changes the log level.
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput changes the log output destination.
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger.SetOutput(w)
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.level >= LogLevelError {
		l.logger.Printf("[ERROR] "+format, args...)
	}
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.level >= LogLevelWarn {
		l.logger.Printf("[WARN]  "+format, args...)
	}
}

// Info logs an informational message.
func (l *Logger) Info(format string, args ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.level >= LogLevelInfo {
		l.logger.Printf("[INFO]  "+format, args...)
	}
}

// Debug logs a debug message.
func (l *Logger) Debug(format string, args ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.level >= LogLevelDebug {
		l.logger.Printf("[DEBUG] "+format, args...)
	}
}

// SetDefaultLogLevel sets the log level on the package-level logger.
func SetDefaultLogLevel(level LogLevel) {
	DefaultLogger.SetLevel(level)
}

// DebugLogger enables debug-level logging to stderr.
// This is the equivalent of pynetdicom's debug_logger().
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
