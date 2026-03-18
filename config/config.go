package config

import (
	"context"
	"sync"
)

// Settings provides global configuration for DICOM operations.
//
// Settings follows the singleton pattern and is thread-safe.
// Use Get() to access the global settings instance.
type Settings struct {
	mu sync.RWMutex

	// Data type preferences
	UseDSDecimal       bool // Use Decimal for DS (decimal string) values instead of float64
	UseDSNumpy         bool // Use numpy arrays for multi-valued DS
	UseISNumpy         bool // Use numpy arrays for multi-valued IS (integer string)
	AllowDSFloat       bool // Allow float initialization of DSdecimal
	DatetimeConversion bool // Auto-convert DA/DT/TM to Go time.Time

	// Validation settings
	ReadingValidationMode ValidationMode // Validation level when reading files
	WritingValidationMode ValidationMode // Validation level when writing files

	// Behavior flags
	ShowFileMeta           bool // Include file meta in string output
	ReplaceUNWithKnownVR   bool // Replace UN VR with correct VR if known
	ConvertWrongLengthToUN bool // Convert invalid lengths to UN VR
	InferSQForUNVR         bool // Infer Sequence VR for UN VR

	// Dataset behavior modes
	InvalidKeywordBehavior BehaviorMode // How to handle invalid element keywords (default: WARN)
	InvalidKeyBehavior     BehaviorMode // How to handle invalid keys in 'contains' checks (default: WARN)

	// Advanced VR handling flags
	AssumeImplicitVRSwitch bool // Assume file switched to implicit VR on invalid explicit VR (default: true)
	UseNoneAsEmptyTextVR   bool // Use nil vs empty string for empty text VR values (default: false)
	ApplyJ2KCorrections    bool // Apply JPEG 2000 corrections from embedded metadata (default: true)

	// I/O settings
	BufferedReadSize int // Size for buffered reads (default 8192)
}

var (
	// defaultSettings is the global singleton instance
	defaultSettings = &Settings{
		ReadingValidationMode:  WARN,
		WritingValidationMode:  WARN,
		ShowFileMeta:           true,
		DatetimeConversion:     false,
		UseDSDecimal:           false,
		UseDSNumpy:             false,
		UseISNumpy:             false,
		AllowDSFloat:           false,
		ReplaceUNWithKnownVR:   false,
		ConvertWrongLengthToUN: false,
		InferSQForUNVR:         true,
		InvalidKeywordBehavior: BehaviorWarn,
		InvalidKeyBehavior:     BehaviorWarn,
		AssumeImplicitVRSwitch: true,
		UseNoneAsEmptyTextVR:   false,
		ApplyJ2KCorrections:    true,
		BufferedReadSize:       8192,
	}
)

// Get returns the global Settings instance.
func Get() *Settings {
	return defaultSettings
}

// Reset resets all settings to their default values.
func Reset() {
	defaultSettings.mu.Lock()
	defer defaultSettings.mu.Unlock()

	defaultSettings.ReadingValidationMode = WARN
	defaultSettings.WritingValidationMode = WARN
	defaultSettings.ShowFileMeta = true
	defaultSettings.DatetimeConversion = false
	defaultSettings.UseDSDecimal = false
	defaultSettings.UseDSNumpy = false
	defaultSettings.UseISNumpy = false
	defaultSettings.AllowDSFloat = false
	defaultSettings.ReplaceUNWithKnownVR = false
	defaultSettings.ConvertWrongLengthToUN = false
	defaultSettings.InferSQForUNVR = true
	defaultSettings.InvalidKeywordBehavior = BehaviorWarn
	defaultSettings.InvalidKeyBehavior = BehaviorWarn
	defaultSettings.AssumeImplicitVRSwitch = true
	defaultSettings.UseNoneAsEmptyTextVR = false
	defaultSettings.ApplyJ2KCorrections = true
	defaultSettings.BufferedReadSize = 8192
}

// SetReadingValidationMode sets the validation mode for reading files.
func (s *Settings) SetReadingValidationMode(mode ValidationMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ReadingValidationMode = mode
}

// GetReadingValidationMode gets the validation mode for reading files.
func (s *Settings) GetReadingValidationMode() ValidationMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ReadingValidationMode
}

// SetWritingValidationMode sets the validation mode for writing files.
func (s *Settings) SetWritingValidationMode(mode ValidationMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.WritingValidationMode = mode
}

// GetWritingValidationMode gets the validation mode for writing files.
func (s *Settings) GetWritingValidationMode() ValidationMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.WritingValidationMode
}

// SetUseDSDecimal sets whether to use Decimal for DS values.
func (s *Settings) SetUseDSDecimal(use bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UseDSDecimal = use
}

// GetUseDSDecimal gets whether to use Decimal for DS values.
func (s *Settings) GetUseDSDecimal() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.UseDSDecimal
}

// SetDatetimeConversion sets whether to auto-convert DA/DT/TM to time.Time.
func (s *Settings) SetDatetimeConversion(convert bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DatetimeConversion = convert
}

// GetDatetimeConversion gets whether to auto-convert DA/DT/TM to time.Time.
func (s *Settings) GetDatetimeConversion() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.DatetimeConversion
}

// SetShowFileMeta sets whether to include file meta in string output.
func (s *Settings) SetShowFileMeta(show bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ShowFileMeta = show
}

// GetShowFileMeta gets whether to include file meta in string output.
func (s *Settings) GetShowFileMeta() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ShowFileMeta
}

// SetBufferedReadSize sets the buffer size for buffered reads.
func (s *Settings) SetBufferedReadSize(size int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if size > 0 {
		s.BufferedReadSize = size
	}
}

// GetBufferedReadSize gets the buffer size for buffered reads.
func (s *Settings) GetBufferedReadSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.BufferedReadSize
}

// SetReplaceUNWithKnownVR sets whether to replace UN with known VR.
func (s *Settings) SetReplaceUNWithKnownVR(replace bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ReplaceUNWithKnownVR = replace
}

// GetReplaceUNWithKnownVR gets whether to replace UN with known VR.
func (s *Settings) GetReplaceUNWithKnownVR() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ReplaceUNWithKnownVR
}

// SetInferSQForUNVR sets whether to infer SQ for UN VR.
func (s *Settings) SetInferSQForUNVR(infer bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InferSQForUNVR = infer
}

// GetInferSQForUNVR gets whether to infer SQ for UN VR.
func (s *Settings) GetInferSQForUNVR() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.InferSQForUNVR
}

// SetInvalidKeywordBehavior sets the behavior for invalid element keywords.
func (s *Settings) SetInvalidKeywordBehavior(mode BehaviorMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InvalidKeywordBehavior = mode
}

// GetInvalidKeywordBehavior gets the behavior for invalid element keywords.
func (s *Settings) GetInvalidKeywordBehavior() BehaviorMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.InvalidKeywordBehavior
}

// SetInvalidKeyBehavior sets the behavior for invalid keys in contains checks.
func (s *Settings) SetInvalidKeyBehavior(mode BehaviorMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InvalidKeyBehavior = mode
}

// GetInvalidKeyBehavior gets the behavior for invalid keys in contains checks.
func (s *Settings) GetInvalidKeyBehavior() BehaviorMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.InvalidKeyBehavior
}

// SetAssumeImplicitVRSwitch sets whether to assume implicit VR on invalid explicit VR.
func (s *Settings) SetAssumeImplicitVRSwitch(assume bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AssumeImplicitVRSwitch = assume
}

// GetAssumeImplicitVRSwitch gets whether to assume implicit VR on invalid explicit VR.
func (s *Settings) GetAssumeImplicitVRSwitch() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AssumeImplicitVRSwitch
}

// SetUseNoneAsEmptyTextVR sets whether to use nil vs empty string for empty text VR.
func (s *Settings) SetUseNoneAsEmptyTextVR(useNone bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UseNoneAsEmptyTextVR = useNone
}

// GetUseNoneAsEmptyTextVR gets whether to use nil vs empty string for empty text VR.
func (s *Settings) GetUseNoneAsEmptyTextVR() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.UseNoneAsEmptyTextVR
}

// SetApplyJ2KCorrections sets whether to apply JPEG 2000 corrections.
func (s *Settings) SetApplyJ2KCorrections(apply bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ApplyJ2KCorrections = apply
}

// GetApplyJ2KCorrections gets whether to apply JPEG 2000 corrections.
func (s *Settings) GetApplyJ2KCorrections() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ApplyJ2KCorrections
}

// ContextKey is used for storing settings in context.
type contextKey string

const (
	readingValidationModeKey contextKey = "reading_validation_mode"
	writingValidationModeKey contextKey = "writing_validation_mode"
)

// WithReadingValidationMode returns a context with a temporary reading validation mode.
func WithReadingValidationMode(ctx context.Context, mode ValidationMode) context.Context {
	return context.WithValue(ctx, readingValidationModeKey, mode)
}

// WithWritingValidationMode returns a context with a temporary writing validation mode.
func WithWritingValidationMode(ctx context.Context, mode ValidationMode) context.Context {
	return context.WithValue(ctx, writingValidationModeKey, mode)
}

// ReadingValidationModeFromContext returns the reading validation mode from context,
// or the global setting if not set in context.
func ReadingValidationModeFromContext(ctx context.Context) ValidationMode {
	if mode, ok := ctx.Value(readingValidationModeKey).(ValidationMode); ok {
		return mode
	}
	return Get().GetReadingValidationMode()
}

// WritingValidationModeFromContext returns the writing validation mode from context,
// or the global setting if not set in context.
func WritingValidationModeFromContext(ctx context.Context) ValidationMode {
	if mode, ok := ctx.Value(writingValidationModeKey).(ValidationMode); ok {
		return mode
	}
	return Get().GetWritingValidationMode()
}

// DisableValueValidation returns a context that temporarily disables both
// reading and writing validation.
// disable_value_validation() context manager.
//
// Example:
//
//	ctx := config.DisableValueValidation(context.Background())
//	// All operations with this context will have validation disabled
func DisableValueValidation(ctx context.Context) context.Context {
	ctx = WithReadingValidationMode(ctx, IGNORE)
	ctx = WithWritingValidationMode(ctx, IGNORE)
	return ctx
}

// StrictReading returns a context that temporarily enables strict validation
// for reading operations.
// context manager.
//
// Example:
//
//	ctx := config.StrictReading(context.Background())
//	// Reading operations with this context will raise errors on validation failures
func StrictReading(ctx context.Context) context.Context {
	return WithReadingValidationMode(ctx, RAISE)
}

// StrictWriting returns a context that temporarily enables strict validation
// for writing operations.
//
// Example:
//
//	ctx := config.StrictWriting(context.Background())
//	// Writing operations with this context will raise errors on validation failures
func StrictWriting(ctx context.Context) context.Context {
	return WithWritingValidationMode(ctx, RAISE)
}

// StrictValidation returns a context that temporarily enables strict validation
// for both reading and writing operations.
//
// Example:
//
//	ctx := config.StrictValidation(context.Background())
//	// All operations with this context will raise errors on validation failures
func StrictValidation(ctx context.Context) context.Context {
	ctx = WithReadingValidationMode(ctx, RAISE)
	ctx = WithWritingValidationMode(ctx, RAISE)
	return ctx
}
