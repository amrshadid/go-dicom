package config

import (
	"context"
	"io"
	"log/slog"
	"os"
)

var (
	// Logger is the global logger for DICOM operations.
	// By default, it logs warnings and errors to stderr.
	Logger *slog.Logger

	// Debugging indicates whether debug logging is currently enabled.
	Debugging bool = false
)

func init() {
	// Initialize with WARNING level by default
	Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

// SetDebug enables or disables debug logging for DICOM operations.
//
// When debug is enabled:
//   - Log level is set to DEBUG
//   - File location and element details are logged during reading/writing
//   - The Debugging flag is set to true
//
// When debug is disabled:
//   - Log level is set to WARN
//   - Only warnings and errors are logged
//   - The Debugging flag is set to false
//
// Parameters:
//   - enabled: true to enable debug logging, false to disable
//   - output: optional io.Writer for log output (defaults to os.Stderr if nil)
//
// Example:
//
//	// Enable debug logging to stderr
//	config.SetDebug(true, nil)
//
//	// Enable debug logging to a file
//	file, _ := os.Create("dicom.log")
//	config.SetDebug(true, file)
//
//	// Disable debug logging
//	config.SetDebug(false, nil)
func SetDebug(enabled bool, output io.Writer) {
	Debugging = enabled

	if output == nil {
		output = os.Stderr
	}

	var level slog.Level
	if enabled {
		level = slog.LevelDebug
	} else {
		level = slog.LevelWarn
	}

	Logger = slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{
		Level: level,
	}))
}

// SetLogger allows setting a custom logger for DICOM operations.
// This provides full control over log formatting, output, and level handling.
//
// Parameters:
//   - logger: the custom slog.Logger to use
//
// Example:
//
//	// Use JSON logging
//	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
//	    Level: slog.LevelInfo,
//	})
//	config.SetLogger(slog.New(jsonHandler))
//
//	// Use custom handler with additional context
//	customLogger := slog.Default().With("module", "dicom")
//	config.SetLogger(customLogger)
func SetLogger(logger *slog.Logger) {
	if logger != nil {
		Logger = logger
	}
}

// loggerKey is the context key WithLogger stores a logger under.
type loggerKey struct{}

// WithLogger returns a context that carries logger, for the operations
// given that context to log to instead of Logger. It is how one file read
// logs to a logger of its own while another, at the same time, logs to a
// different one: the logger travels with the work, the way the reading
// validation mode does (WithReadingValidationMode), and no global is
// changed. A nil logger leaves ctx as it is.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFromContext returns the logger WithLogger put in ctx, or Logger
// when there is none. It is looked up at each line, so a context without
// one follows SetLogger as it always did.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
			return logger
		}
	}
	return Logger
}

// GetLogger returns the current global logger.
// This can be useful for checking the current logger configuration.
func GetLogger() *slog.Logger {
	return Logger
}

// IsDebugging returns true if debug logging is currently enabled.
func IsDebugging() bool {
	return Debugging
}
