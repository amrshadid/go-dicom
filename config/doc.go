// Package config provides centralized, thread-safe global configuration management
// for DICOM operations.
//
// # Overview
//
// The config package acts as the control center for the entire DICOM library,
// allowing users to:
//   - Configure validation behavior during read/write operations
//   - Control logging verbosity and debug modes
//   - Register custom pixel data handlers (plugins)
//   - Override settings per-operation using Go contexts
//   - Manage VR handling edge cases
//   - Control data type conversions
//
// # Singleton Pattern
//
// The config package uses a singleton pattern with a global Settings instance
// accessed via Get(). This ensures consistent behavior across the entire library:
//
//	settings := config.Get()
//	settings.SetReadingValidationMode(config.RAISE)
//	settings.SetWritingValidationMode(config.WARN)
//
// # Thread Safety
//
// All operations on Settings are thread-safe using sync.RWMutex. This allows
// safe concurrent reads and writes from multiple goroutines:
//
//	// Safe concurrent reads
//	mode := settings.GetReadingValidationMode()
//
//	// Safe concurrent writes
//	settings.SetReadingValidationMode(config.IGNORE)
//
// # Context-Based Overrides
//
// For per-operation configuration without modifying global state, use context:
//
//	ctx := context.Background()
//	ctx = config.WithReadingValidationMode(ctx, config.RAISE)
//	mode := config.ReadingValidationModeFromContext(ctx)  // Returns RAISE
//	globalMode := config.Get().GetReadingValidationMode() // Returns original
//
// This pattern is useful for:
//   - Strict validation for critical operations
//   - Lenient validation for data recovery
//   - Testing different configurations
//   - Per-request settings in APIs
//
// # Validation Modes
//
// The config package defines three validation modes:
//
//   - IGNORE: Silently accept invalid data
//   - WARN: Log warnings for invalid data but continue
//   - RAISE: Return errors for invalid data
//
// These modes are applied during:
//   - File reading (ReadingValidationMode)
//   - File writing (WritingValidationMode)
//   - Element validation (DICOM dictionary conformance)
//   - VR value validation (type constraints)
//
// Example usage:
//
//	config.Get().SetReadingValidationMode(config.WARN)
//	config.Get().SetWritingValidationMode(config.RAISE)
//
//	mode := config.Get().GetReadingValidationMode() // Returns WARN
//	desc := mode.Description() // "Log warnings but continue"
//
// # Behavior Modes
//
// Behavior modes control how the library reacts to invalid operations:
//
//   - BehaviorIgnore: Silently ignore invalid operations
//   - BehaviorWarn: Log warnings for invalid operations
//   - BehaviorRaise: Raise errors for invalid operations (not all operations support this)
//
// Used for:
//   - InvalidKeywordBehavior: How to handle invalid element keywords
//   - InvalidKeyBehavior: How to handle invalid keys in contains checks
//
// Example:
//
//	config.Get().SetInvalidKeywordBehavior(config.BehaviorIgnore)
//	behavior := config.Get().GetInvalidKeywordBehavior() // Returns BehaviorIgnore
//
// # Data Type Settings
//
// Configure how DICOM data types are converted to Go types:
//
//   - UseDSDecimal: Use decimal.Decimal for DS (Decimal String) instead of float64
//   - UseDSNumpy: Use numpy-style arrays for multi-valued DS (Python compatibility)
//   - UseISNumpy: Use numpy-style arrays for multi-valued IS
//   - AllowDSFloat: Allow float initialization of decimal numbers
//   - DatetimeConversion: Auto-convert DA/DT/TM to Go time.Time
//
// Example:
//
//	config.Get().SetUseDSDecimal(true)      // Preserve decimal precision
//	config.Get().SetDatetimeConversion(true) // Get Go time.Time objects
//
// Default behavior preserves floating-point precision and leaves dates as strings.
//
// # VR Handling Settings
//
// Configure edge case handling for Value Representations:
//
//   - ReplaceUNWithKnownVR: Replace UN VR with correct VR if known
//   - ConvertWrongLengthToUN: Convert invalid lengths to UN VR
//   - InferSQForUNVR: Infer Sequence VR for UN VR (default: true)
//   - AssumeImplicitVRSwitch: Assume file switched to implicit VR on invalid explicit VR (default: true)
//   - UseNoneAsEmptyTextVR: Use nil vs empty string for empty text VR values (default: false)
//   - ApplyJ2KCorrections: Apply JPEG 2000 corrections from embedded metadata (default: true)
//
// These settings handle non-standard files that violate DICOM specifications.
//
// Example:
//
//	config.Get().SetReplaceUNWithKnownVR(true)
//	config.Get().SetApplyJ2KCorrections(true)
//
// # I/O Settings
//
// Configure input/output behavior:
//
//   - BufferedReadSize: Size for buffered reads (default: 8192 bytes)
//   - ShowFileMeta: Include DICOM file meta in string output (default: true)
//
// Example:
//
//	config.Get().SetBufferedReadSize(16384)  // Larger buffer for network I/O
//	config.Get().SetShowFileMeta(false)       // Compact output
//
// # Pixel Data Handler Registry
//
// Register custom pixel data handlers for compression algorithms:
//
//	type PixelDataHandler interface {
//	    Name() string
//	    SupportsTransferSyntax(uid string) bool
//	    IsAvailable() bool
//	    GetPixelData(ds interface{}) ([]byte, error)
//	    NeedsRGBConversion(ds interface{}) bool
//	}
//
// Register and use handlers:
//
//	handler := &MyJPEGHandler{}
//	config.RegisterPixelDataHandler(handler)
//
//	// Later, retrieve handlers
//	handlers := config.GetPixelDataHandlers()
//	handler := config.FindPixelDataHandler("1.2.840.10008.1.2.5") // RLE
//
// Handlers are searched in registration order, allowing priority-based selection.
//
// # Logging Configuration
//
// Control library logging and debugging:
//
//	// Enable debug output to stderr
//	config.SetDebug(true, os.Stderr)
//
//	// Use custom logger
//	myLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
//	config.SetLogger(myLogger)
//
//	// Check debug status
//	if config.IsDebugging() {
//	    // Perform debug operations
//	}
//
// The library uses Go 1.21+ structured logging (slog).
//
// # Reset Functionality
//
// Reset all settings to defaults:
//
//	config.Reset()
//
// This is useful in tests to ensure clean state between test cases.
//
// # Configuration Presets
//
// The config package provides context functions for common scenarios:
//
//	// Strict validation for reading
//	ctx := config.StrictReading(ctx)
//
//	// Strict validation for writing
//	ctx = config.StrictWriting(ctx)
//
//	// Strict validation for both
//	ctx = config.StrictValidation(ctx)
//
//	// Disable validation entirely
//	ctx = config.DisableValueValidation(ctx)
//
// # Integration Points
//
// The config package integrates with:
//   - FileReader: Uses validation modes and logger
//   - FileWriter: Uses validation modes and logger
//   - Dataset: Uses behavior modes for invalid operations
//   - DataElement: Uses VR handling settings
//   - Compress: Pixel data handlers
//   - CharSet: Logger for encoding issues
//
// # Default Configuration
//
// The following shows the default configuration:
//
//	ReadingValidationMode  = WARN
//	WritingValidationMode  = WARN
//	InvalidKeywordBehavior = BehaviorWarn
//	InvalidKeyBehavior     = BehaviorWarn
//	InferSQForUNVR         = true
//	AssumeImplicitVRSwitch = true
//	UseNoneAsEmptyTextVR   = false
//	ApplyJ2KCorrections    = true
//	BufferedReadSize       = 8192
//	ShowFileMeta           = true
//
// All data type conversions are disabled by default (preserve original types).
//
// # Common Configuration Patterns
//
// ## Strict Reading (Fail on Errors)
//
//	config.Get().SetReadingValidationMode(config.RAISE)
//
// ## Lenient Reading (Ignore Errors)
//
//	config.Get().SetReadingValidationMode(config.IGNORE)
//
// ## Per-Operation Strict Mode
//
//	ctx := config.WithReadingValidationMode(context.Background(), config.RAISE)
//	// Use ctx for file reading
//
// ## Enable Debugging
//
//	config.SetDebug(true, os.Stderr)
//	if config.IsDebugging() {
//	    log.Println("Debug mode enabled")
//	}
//
// ## Register Custom Handler
//
//	type CustomHandler struct{}
//	func (h *CustomHandler) Name() string { return "Custom" }
//	func (h *CustomHandler) SupportsTransferSyntax(uid string) bool { return true }
//	func (h *CustomHandler) IsAvailable() bool { return true }
//	func (h *CustomHandler) GetPixelData(ds interface{}) ([]byte, error) { /* ... */ }
//	func (h *CustomHandler) NeedsRGBConversion(ds interface{}) bool { return false }
//
//	config.RegisterPixelDataHandler(&CustomHandler{})
//
// # Thread Safety Guarantees
//
// All operations provide the following guarantees:
//   - Getter methods are read operations (multiple concurrent callers safe)
//   - Setter methods are atomic write operations (serialized with readers)
//   - Handler registration is copy-safe (internal slice copying)
//   - Context values are immutable (no sharing of references)
//   - Logger is slog.Logger (concurrent-safe by design)
//
// # Performance Considerations
//
// The config package is designed for minimal overhead:
//   - Read operations use RWMutex (fast concurrent reads)
//   - Write operations are rare (typically at startup)
//   - Context lookups are O(1) map operations
//   - Handler lookups are O(n) but typically n < 5
//   - Logging is asynchronous (slog design)
//
// # Relationship with PyDICOM
//
// The config package provides functionality similar to pydicom's settings:
//
//	pydicom Setting                 Go Config Equivalent
//	settings.datetime_conversion    SetDatetimeConversion()
//	settings.show_file_meta         SetShowFileMeta()
//	settings.writing_validation_mode SetWritingValidationMode()
//	settings.reading_validation_mode SetReadingValidationMode()
//	settings.assume_implicit_VR_switch SetAssumeImplicitVRSwitch()
//
// Go's config package adds additional settings not in pydicom:
//   - Pixel data handler registration
//   - Debug mode with custom output
//   - Per-operation context overrides
//   - Behavior mode control
//   - Type conversion preferences
//
// # Advanced Topics
//
// ## Multiple Handlers for Same Syntax
//
// If multiple handlers support the same transfer syntax, the first registered
// handler (in registration order) will be used:
//
//	config.RegisterPixelDataHandler(handler1) // Will be checked first
//	config.RegisterPixelDataHandler(handler2)
//
// ## Context Value Ordering
//
// Context values are checked in order:
//  1. Operation-specific context value (if provided)
//  2. Global config setting
//
// This allows override patterns:
//
//	globalCtx := config.StrictValidation(context.Background())
//	operationCtx := config.WithReadingValidationMode(globalCtx, config.IGNORE)
//	// operationCtx will use IGNORE for reading, RAISE for writing
//
// # Compatibility Notes
//
// The config package is compatible with:
//   - Go 1.19+ (for context support)
//   - Go 1.21+ (for slog logging)
//   - All DICOM standards
//   - Custom decompression plugins
//   - Multi-goroutine applications
package config
