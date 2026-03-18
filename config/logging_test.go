package config_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/config"
)

// TestSetDebug tests the SetDebug function.
func TestSetDebug(t *testing.T) {
	// Test enabling debug
	t.Run("EnableDebug", func(t *testing.T) {
		var buf bytes.Buffer
		config.SetDebug(true, &buf)

		if !config.Debugging {
			t.Error("Debugging should be true after SetDebug(true)")
		}

		// Log a debug message
		config.Logger.Debug("test debug message")

		output := buf.String()
		if !strings.Contains(output, "test debug message") {
			t.Error("Debug message should be logged when debugging is enabled")
		}
	})

	// Test disabling debug
	t.Run("DisableDebug", func(t *testing.T) {
		var buf bytes.Buffer
		config.SetDebug(false, &buf)

		if config.Debugging {
			t.Error("Debugging should be false after SetDebug(false)")
		}

		// Log a debug message (should not appear)
		config.Logger.Debug("test debug message")

		output := buf.String()
		if strings.Contains(output, "test debug message") {
			t.Error("Debug message should not be logged when debugging is disabled")
		}

		// Log a warning (should appear)
		config.Logger.Warn("test warning message")

		output = buf.String()
		if !strings.Contains(output, "test warning message") {
			t.Error("Warning message should be logged when debugging is disabled")
		}
	})

	// Test nil output (should default to stderr)
	t.Run("NilOutput", func(t *testing.T) {
		config.SetDebug(false, nil)

		if config.Debugging {
			t.Error("Debugging should be false")
		}

		if config.Logger == nil {
			t.Error("Logger should not be nil after SetDebug")
		}
	})
}

// TestSetLogger tests the SetLogger function.
func TestSetLogger(t *testing.T) {
	// Create a custom logger
	var buf bytes.Buffer
	customLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	config.SetLogger(customLogger)

	if config.Logger != customLogger {
		t.Error("Logger should be the custom logger after SetLogger")
	}

	// Log a message
	config.Logger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Error("Custom logger should log messages")
	}

	// Verify it's JSON format
	if !strings.Contains(output, `"msg"`) {
		t.Error("Custom JSON handler should produce JSON output")
	}

	// Test nil logger (should be ignored)
	oldLogger := config.Logger
	config.SetLogger(nil)

	if config.Logger != oldLogger {
		t.Error("SetLogger(nil) should not change the logger")
	}
}

// TestGetLogger tests the GetLogger function.
func TestGetLogger(t *testing.T) {
	logger := config.GetLogger()

	if logger == nil {
		t.Error("GetLogger should return a non-nil logger")
	}

	if logger != config.Logger {
		t.Error("GetLogger should return the global Logger")
	}
}

// TestIsDebugging tests the IsDebugging function.
func TestIsDebugging(t *testing.T) {
	config.SetDebug(true, nil)
	if !config.IsDebugging() {
		t.Error("IsDebugging should return true when debugging is enabled")
	}

	config.SetDebug(false, nil)
	if config.IsDebugging() {
		t.Error("IsDebugging should return false when debugging is disabled")
	}
}

// TestLoggerLevels tests that logger respects different log levels.
func TestLoggerLevels(t *testing.T) {
	// Test DEBUG level
	t.Run("DebugLevel", func(t *testing.T) {
		var buf bytes.Buffer
		config.SetDebug(true, &buf)

		config.Logger.Debug("debug msg")
		config.Logger.Info("info msg")
		config.Logger.Warn("warn msg")
		config.Logger.Error("error msg")

		output := buf.String()
		if !strings.Contains(output, "debug msg") {
			t.Error("Debug level should show debug messages")
		}
		if !strings.Contains(output, "info msg") {
			t.Error("Debug level should show info messages")
		}
		if !strings.Contains(output, "warn msg") {
			t.Error("Debug level should show warn messages")
		}
		if !strings.Contains(output, "error msg") {
			t.Error("Debug level should show error messages")
		}
	})

	// Test WARN level
	t.Run("WarnLevel", func(t *testing.T) {
		var buf bytes.Buffer
		config.SetDebug(false, &buf)

		config.Logger.Debug("debug msg")
		config.Logger.Info("info msg")
		config.Logger.Warn("warn msg")
		config.Logger.Error("error msg")

		output := buf.String()
		if strings.Contains(output, "debug msg") {
			t.Error("Warn level should not show debug messages")
		}
		if strings.Contains(output, "info msg") {
			t.Error("Warn level should not show info messages")
		}
		if !strings.Contains(output, "warn msg") {
			t.Error("Warn level should show warn messages")
		}
		if !strings.Contains(output, "error msg") {
			t.Error("Warn level should show error messages")
		}
	})
}
