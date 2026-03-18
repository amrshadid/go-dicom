package config

import "fmt"

// ValidationMode represents how validation errors should be handled.
type ValidationMode int

const (
	// IGNORE - Do not validate, allow invalid values
	IGNORE ValidationMode = iota
	// WARN - Log warnings for invalid values but continue
	WARN
	// RAISE - Raise an error on invalid values
	RAISE
)

// String returns the string representation of a ValidationMode.
func (m ValidationMode) String() string {
	switch m {
	case IGNORE:
		return "IGNORE"
	case WARN:
		return "WARN"
	case RAISE:
		return "RAISE"
	default:
		return fmt.Sprintf("ValidationMode(%d)", m)
	}
}

// Description returns a human-readable description of the validation mode.
func (m ValidationMode) Description() string {
	switch m {
	case IGNORE:
		return "Ignore validation errors and allow invalid values"
	case WARN:
		return "Log warnings for validation errors but continue processing"
	case RAISE:
		return "Raise an error immediately on validation failure"
	default:
		return "Unknown validation mode"
	}
}

// IsValid returns true if the validation mode is a known value.
func (m ValidationMode) IsValid() bool {
	return m >= IGNORE && m <= RAISE
}

// ParseValidationMode parses a string into a ValidationMode.
func ParseValidationMode(s string) (ValidationMode, error) {
	switch s {
	case "IGNORE", "ignore":
		return IGNORE, nil
	case "WARN", "warn":
		return WARN, nil
	case "RAISE", "raise":
		return RAISE, nil
	default:
		return IGNORE, fmt.Errorf("invalid validation mode: %s", s)
	}
}

// BehaviorMode represents how to handle invalid operations.
// This is equivalent to pydicom's INVALID_KEYWORD_BEHAVIOR and INVALID_KEY_BEHAVIOR.
type BehaviorMode string

const (
	// BehaviorIgnore - Ignore invalid operations silently
	BehaviorIgnore BehaviorMode = "IGNORE"
	// BehaviorWarn - Log warnings for invalid operations but continue
	BehaviorWarn BehaviorMode = "WARN"
	// BehaviorRaise - Raise an error on invalid operations
	BehaviorRaise BehaviorMode = "RAISE"
)

// String returns the string representation of a BehaviorMode.
func (m BehaviorMode) String() string {
	return string(m)
}

// IsValid returns true if the behavior mode is a known value.
func (m BehaviorMode) IsValid() bool {
	switch m {
	case BehaviorIgnore, BehaviorWarn, BehaviorRaise:
		return true
	default:
		return false
	}
}

// ParseBehaviorMode parses a string into a BehaviorMode.
func ParseBehaviorMode(s string) (BehaviorMode, error) {
	switch s {
	case "IGNORE", "ignore":
		return BehaviorIgnore, nil
	case "WARN", "warn":
		return BehaviorWarn, nil
	case "RAISE", "raise":
		return BehaviorRaise, nil
	default:
		return BehaviorWarn, fmt.Errorf("invalid behavior mode: %s", s)
	}
}
