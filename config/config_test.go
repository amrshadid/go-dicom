package config_test

import (
	"sync"
	"testing"

	"github.com/amrshadid/go-dicom/config"
)

func TestValidationModes(t *testing.T) {
	tests := []struct {
		mode config.ValidationMode
		want string
	}{
		{config.IGNORE, "IGNORE"},
		{config.WARN, "WARN"},
		{config.RAISE, "RAISE"},
	}

	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("ValidationMode.String() = %v, want %v", got, tt.want)
		}

		if !tt.mode.IsValid() {
			t.Errorf("ValidationMode %v should be valid", tt.mode)
		}
	}
}

func TestParseValidationMode(t *testing.T) {
	tests := []struct {
		input   string
		want    config.ValidationMode
		wantErr bool
	}{
		{"IGNORE", config.IGNORE, false},
		{"ignore", config.IGNORE, false},
		{"WARN", config.WARN, false},
		{"warn", config.WARN, false},
		{"RAISE", config.RAISE, false},
		{"raise", config.RAISE, false},
		{"invalid", config.IGNORE, true},
	}

	for _, tt := range tests {
		got, err := config.ParseValidationMode(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseValidationMode(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseValidationMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDefaultSettings(t *testing.T) {
	settings := config.Get()

	if settings == nil {
		t.Fatal("Get() returned nil")
	}

	// Check default values
	if settings.GetReadingValidationMode() != config.WARN {
		t.Errorf("Default ReadingValidationMode = %v, want WARN", settings.GetReadingValidationMode())
	}

	if settings.GetWritingValidationMode() != config.WARN {
		t.Errorf("Default WritingValidationMode = %v, want WARN", settings.GetWritingValidationMode())
	}

	if !settings.GetShowFileMeta() {
		t.Error("Default ShowFileMeta = false, want true")
	}

	if settings.GetDatetimeConversion() {
		t.Error("Default DatetimeConversion = true, want false")
	}

	if settings.GetUseDSDecimal() {
		t.Error("Default UseDSDecimal = true, want false")
	}

	if !settings.GetAssumeImplicitVRSwitch() {
		t.Error("Default AssumeImplicitVRSwitch = false, want true")
	}

	if settings.GetBufferedReadSize() != 8192 {
		t.Errorf("Default BufferedReadSize = %d, want 8192", settings.GetBufferedReadSize())
	}
}

func TestSetReadingValidationMode(t *testing.T) {
	settings := config.Get()

	// Save original
	original := settings.GetReadingValidationMode()
	defer settings.SetReadingValidationMode(original)

	settings.SetReadingValidationMode(config.RAISE)
	if got := settings.GetReadingValidationMode(); got != config.RAISE {
		t.Errorf("After SetReadingValidationMode(RAISE), got %v, want RAISE", got)
	}

	settings.SetReadingValidationMode(config.IGNORE)
	if got := settings.GetReadingValidationMode(); got != config.IGNORE {
		t.Errorf("After SetReadingValidationMode(IGNORE), got %v, want IGNORE", got)
	}
}

func TestSetWritingValidationMode(t *testing.T) {
	settings := config.Get()

	// Save original
	original := settings.GetWritingValidationMode()
	defer settings.SetWritingValidationMode(original)

	settings.SetWritingValidationMode(config.RAISE)
	if got := settings.GetWritingValidationMode(); got != config.RAISE {
		t.Errorf("After SetWritingValidationMode(RAISE), got %v, want RAISE", got)
	}

	settings.SetWritingValidationMode(config.IGNORE)
	if got := settings.GetWritingValidationMode(); got != config.IGNORE {
		t.Errorf("After SetWritingValidationMode(IGNORE), got %v, want IGNORE", got)
	}
}

func TestBehaviorModes(t *testing.T) {
	tests := []struct {
		mode config.BehaviorMode
		want string
	}{
		{config.BehaviorIgnore, "IGNORE"},
		{config.BehaviorWarn, "WARN"},
		{config.BehaviorRaise, "RAISE"},
	}

	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("BehaviorMode.String() = %v, want %v", got, tt.want)
		}

		if !tt.mode.IsValid() {
			t.Errorf("BehaviorMode %v should be valid", tt.mode)
		}
	}
}

func TestParseBehaviorMode(t *testing.T) {
	tests := []struct {
		input   string
		want    config.BehaviorMode
		wantErr bool
	}{
		{"IGNORE", config.BehaviorIgnore, false},
		{"ignore", config.BehaviorIgnore, false},
		{"WARN", config.BehaviorWarn, false},
		{"warn", config.BehaviorWarn, false},
		{"RAISE", config.BehaviorRaise, false},
		{"raise", config.BehaviorRaise, false},
		{"invalid", config.BehaviorWarn, true},
	}

	for _, tt := range tests {
		got, err := config.ParseBehaviorMode(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseBehaviorMode(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseBehaviorMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestReset(t *testing.T) {
	settings := config.Get()

	// Modify settings
	settings.SetReadingValidationMode(config.IGNORE)
	settings.SetWritingValidationMode(config.RAISE)
	settings.SetShowFileMeta(false)
	settings.SetDatetimeConversion(true)
	settings.SetBufferedReadSize(16384)

	// Reset to defaults
	config.Reset()

	// Verify all defaults are restored
	if settings.GetReadingValidationMode() != config.WARN {
		t.Errorf("After Reset, ReadingValidationMode = %v, want WARN", settings.GetReadingValidationMode())
	}

	if settings.GetWritingValidationMode() != config.WARN {
		t.Errorf("After Reset, WritingValidationMode = %v, want WARN", settings.GetWritingValidationMode())
	}

	if !settings.GetShowFileMeta() {
		t.Error("After Reset, ShowFileMeta = false, want true")
	}

	if settings.GetDatetimeConversion() {
		t.Error("After Reset, DatetimeConversion = true, want false")
	}

	if settings.GetBufferedReadSize() != 8192 {
		t.Errorf("After Reset, BufferedReadSize = %d, want 8192", settings.GetBufferedReadSize())
	}
}

func TestThreadSafety(t *testing.T) {
	settings := config.Get()

	// Save original values
	origReading := settings.GetReadingValidationMode()
	origWriting := settings.GetWritingValidationMode()
	defer func() {
		settings.SetReadingValidationMode(origReading)
		settings.SetWritingValidationMode(origWriting)
	}()

	var wg sync.WaitGroup
	iterations := 100

	// Test concurrent reads and writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Alternate between modes
				mode := config.ValidationMode(j % 3)
				settings.SetReadingValidationMode(mode)
				_ = settings.GetReadingValidationMode()
			}
		}(i)
	}

	wg.Wait()
}

func TestSetInvalidKeywordBehavior(t *testing.T) {
	settings := config.Get()

	// Save original
	original := settings.GetInvalidKeywordBehavior()
	defer settings.SetInvalidKeywordBehavior(original)

	settings.SetInvalidKeywordBehavior(config.BehaviorRaise)
	if got := settings.GetInvalidKeywordBehavior(); got != config.BehaviorRaise {
		t.Errorf("After SetInvalidKeywordBehavior(RAISE), got %v, want RAISE", got)
	}

	settings.SetInvalidKeywordBehavior(config.BehaviorIgnore)
	if got := settings.GetInvalidKeywordBehavior(); got != config.BehaviorIgnore {
		t.Errorf("After SetInvalidKeywordBehavior(IGNORE), got %v, want IGNORE", got)
	}
}

func TestSetInvalidKeyBehavior(t *testing.T) {
	settings := config.Get()

	// Save original
	original := settings.GetInvalidKeyBehavior()
	defer settings.SetInvalidKeyBehavior(original)

	settings.SetInvalidKeyBehavior(config.BehaviorRaise)
	if got := settings.GetInvalidKeyBehavior(); got != config.BehaviorRaise {
		t.Errorf("After SetInvalidKeyBehavior(RAISE), got %v, want RAISE", got)
	}

	settings.SetInvalidKeyBehavior(config.BehaviorIgnore)
	if got := settings.GetInvalidKeyBehavior(); got != config.BehaviorIgnore {
		t.Errorf("After SetInvalidKeyBehavior(IGNORE), got %v, want IGNORE", got)
	}
}

func TestBooleanFlags(t *testing.T) {
	settings := config.Get()

	// Save originals
	origShowFileMeta := settings.GetShowFileMeta()
	origDatetime := settings.GetDatetimeConversion()
	origDSDecimal := settings.GetUseDSDecimal()
	origImplicitVR := settings.GetAssumeImplicitVRSwitch()

	defer func() {
		settings.SetShowFileMeta(origShowFileMeta)
		settings.SetDatetimeConversion(origDatetime)
		settings.SetUseDSDecimal(origDSDecimal)
		settings.SetAssumeImplicitVRSwitch(origImplicitVR)
	}()

	// Test ShowFileMeta
	settings.SetShowFileMeta(false)
	if settings.GetShowFileMeta() {
		t.Error("ShowFileMeta should be false")
	}
	settings.SetShowFileMeta(true)
	if !settings.GetShowFileMeta() {
		t.Error("ShowFileMeta should be true")
	}

	// Test DatetimeConversion
	settings.SetDatetimeConversion(true)
	if !settings.GetDatetimeConversion() {
		t.Error("DatetimeConversion should be true")
	}
	settings.SetDatetimeConversion(false)
	if settings.GetDatetimeConversion() {
		t.Error("DatetimeConversion should be false")
	}

	// Test UseDSDecimal
	settings.SetUseDSDecimal(true)
	if !settings.GetUseDSDecimal() {
		t.Error("UseDSDecimal should be true")
	}
	settings.SetUseDSDecimal(false)
	if settings.GetUseDSDecimal() {
		t.Error("UseDSDecimal should be false")
	}

	// Test AssumeImplicitVRSwitch
	settings.SetAssumeImplicitVRSwitch(false)
	if settings.GetAssumeImplicitVRSwitch() {
		t.Error("AssumeImplicitVRSwitch should be false")
	}
	settings.SetAssumeImplicitVRSwitch(true)
	if !settings.GetAssumeImplicitVRSwitch() {
		t.Error("AssumeImplicitVRSwitch should be true")
	}
}

func TestBufferedReadSize(t *testing.T) {
	settings := config.Get()

	// Save original
	original := settings.GetBufferedReadSize()
	defer settings.SetBufferedReadSize(original)

	// Test setting valid size
	settings.SetBufferedReadSize(16384)
	if got := settings.GetBufferedReadSize(); got != 16384 {
		t.Errorf("BufferedReadSize = %d, want 16384", got)
	}

	// Test setting another valid size
	settings.SetBufferedReadSize(4096)
	if got := settings.GetBufferedReadSize(); got != 4096 {
		t.Errorf("BufferedReadSize = %d, want 4096", got)
	}

	// Test setting invalid size (0 or negative) - should not change
	settings.SetBufferedReadSize(4096) // Set to known value
	settings.SetBufferedReadSize(0)
	if got := settings.GetBufferedReadSize(); got != 4096 {
		t.Errorf("BufferedReadSize after setting 0 = %d, want 4096 (unchanged)", got)
	}

	settings.SetBufferedReadSize(-100)
	if got := settings.GetBufferedReadSize(); got != 4096 {
		t.Errorf("BufferedReadSize after setting -100 = %d, want 4096 (unchanged)", got)
	}
}

func TestGetReturnsGlobalInstance(t *testing.T) {
	settings1 := config.Get()
	settings2 := config.Get()

	if settings1 != settings2 {
		t.Error("Get() should return the same global instance")
	}

	// Verify changes through one reference affect the other
	original := settings1.GetReadingValidationMode()
	defer settings1.SetReadingValidationMode(original)

	settings1.SetReadingValidationMode(config.RAISE)
	if settings2.GetReadingValidationMode() != config.RAISE {
		t.Error("Changes through one reference should affect the other")
	}
}

func TestValidationModeDescription(t *testing.T) {
	tests := []struct {
		mode config.ValidationMode
		want string
	}{
		{config.IGNORE, "Ignore validation errors and allow invalid values"},
		{config.WARN, "Log warnings for validation errors but continue processing"},
		{config.RAISE, "Raise an error immediately on validation failure"},
	}

	for _, tt := range tests {
		if got := tt.mode.Description(); got != tt.want {
			t.Errorf("ValidationMode(%v).Description() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestInvalidValidationMode(t *testing.T) {
	invalidMode := config.ValidationMode(99)

	if invalidMode.IsValid() {
		t.Error("ValidationMode(99) should not be valid")
	}

	if got := invalidMode.String(); got != "ValidationMode(99)" {
		t.Errorf("Invalid mode string = %q, want \"ValidationMode(99)\"", got)
	}

	if got := invalidMode.Description(); got != "Unknown validation mode" {
		t.Errorf("Invalid mode description = %q, want \"Unknown validation mode\"", got)
	}
}

func TestInvalidBehaviorMode(t *testing.T) {
	invalidMode := config.BehaviorMode("INVALID")

	if invalidMode.IsValid() {
		t.Error("BehaviorMode(\"INVALID\") should not be valid")
	}
}
