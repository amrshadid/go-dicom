package charset

import (
	"context"
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/errors"
)

// ConvertEncodings converts DICOM Specific Character Set values to Go encoding names.
func ConvertEncodings(values []string) ([]string, error) {
	return ConvertEncodingsWithContext(context.Background(), values)
}

// ConvertEncodingsWithContext converts DICOM Specific Character Set values to Go encoding names with context support.
func ConvertEncodingsWithContext(ctx context.Context, values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{DefaultEncoding}, nil
	}

	if len(values) == 1 && strings.TrimSpace(values[0]) == "" {
		return []string{DefaultEncoding}, nil
	}

	validationMode := config.ReadingValidationModeFromContext(ctx)
	encodings := make([]string, 0, len(values))

	for _, val := range values {
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}

		encoding, err := convertSingleEncoding(ctx, val, validationMode)
		if err != nil {
			return nil, err
		}

		encodings = append(encodings, encoding)
	}

	if len(encodings) == 0 {
		return []string{DefaultEncoding}, nil
	}

	encodings, err := validateStandAloneEncodings(ctx, values, encodings, validationMode)
	if err != nil {
		return nil, err
	}

	return encodings, nil
}

// convertSingleEncoding converts a single DICOM encoding value to a Go encoding name.
func convertSingleEncoding(ctx context.Context, dicomName string, validationMode config.ValidationMode) (string, error) {
	if encoding := DicomToGoEncoding(dicomName); encoding != "" {
		return encoding, nil
	}

	correctedName, encoding := attemptMisspellingCorrection(dicomName)
	if encoding != "" {
		msg := fmt.Sprintf("Incorrect value for Specific Character Set '%s' - assuming '%s'",
			dicomName, correctedName)
		logWarning(msg)
		return encoding, nil
	}

	if isValidGoEncoding(dicomName) {
		return dicomName, nil
	}

	return handleUnknownEncoding(ctx, dicomName, validationMode)
}

// attemptMisspellingCorrection attempts to correct common DICOM encoding name misspellings.
func attemptMisspellingCorrection(dicomName string) (string, string) {
	if strings.Contains(dicomName, "ISO ") && strings.Contains(dicomName, " IR ") {
		corrected := strings.Replace(dicomName, "ISO ", "ISO_", 1)
		corrected = strings.Replace(corrected, " IR ", "_IR ", 1)
		if encoding := DicomToGoEncoding(corrected); encoding != "" {
			return corrected, encoding
		}
	}

	if strings.Contains(dicomName, "2022") {
		corrected := dicomName
		corrected = strings.ReplaceAll(corrected, "_2022_", " 2022 ")
		corrected = strings.ReplaceAll(corrected, ".2022.", " 2022 ")
		corrected = strings.ReplaceAll(corrected, "_IR_", " IR ")
		corrected = strings.ReplaceAll(corrected, ".IR.", " IR ")
		if encoding := DicomToGoEncoding(corrected); encoding != "" {
			return corrected, encoding
		}
	}

	return "", ""
}

// handleUnknownEncoding handles unknown encoding values based on validation mode.
func handleUnknownEncoding(ctx context.Context, dicomName string, validationMode config.ValidationMode) (string, error) {
	switch validationMode {
	case config.RAISE:
		return "", errors.NewDicomUnicodeDecodeError(
			dicomName,
			nil,
			fmt.Sprintf("Unknown encoding '%s'", dicomName),
		)

	case config.WARN:
		msg := fmt.Sprintf("Unknown encoding '%s' - using default encoding instead", dicomName)
		logWarning(msg)
		return DefaultEncoding, nil

	case config.IGNORE:
		return DefaultEncoding, nil

	default:
		return DefaultEncoding, nil
	}
}

// validateStandAloneEncodings validates that stand-alone encodings are not used with code extensions.
func validateStandAloneEncodings(ctx context.Context, origValues []string, encodings []string, validationMode config.ValidationMode) ([]string, error) {
	if len(encodings) <= 1 {
		return encodings, nil
	}

	if IsStandAloneEncoding(origValues[0]) {
		msg := fmt.Sprintf("Stand-alone encoding '%s' cannot be used with code extensions - "+
			"removing additional encodings %v", origValues[0], origValues[1:])

		switch validationMode {
		case config.RAISE:
			return nil, errors.NewDicomUnicodeDecodeError(
				origValues[0],
				nil,
				"Stand-alone encoding cannot be used with code extensions",
			)
		case config.WARN:
			logWarning(msg)
			return encodings[:1], nil
		case config.IGNORE:
			return encodings[:1], nil
		}
	}

	for i := 1; i < len(origValues); i++ {
		if IsStandAloneEncoding(origValues[i]) {
			msg := fmt.Sprintf("Stand-alone encoding '%s' found in code extensions (position %d) - "+
				"removing it", origValues[i], i+1)

			switch validationMode {
			case config.RAISE:
				return nil, errors.NewDicomUnicodeDecodeError(
					origValues[i],
					nil,
					"Stand-alone encoding cannot be used in code extensions",
				)
			case config.WARN:
				logWarning(msg)
				result := make([]string, 0, len(encodings)-1)
				result = append(result, encodings[:i]...)
				result = append(result, encodings[i+1:]...)
				return result, nil
			case config.IGNORE:
				result := make([]string, 0, len(encodings)-1)
				result = append(result, encodings[:i]...)
				result = append(result, encodings[i+1:]...)
				return result, nil
			}
		}
	}

	return encodings, nil
}

// isValidGoEncoding checks if a string is a valid Go/IANA encoding name.
func isValidGoEncoding(name string) bool {
	if name == "" {
		return false
	}

	info := GetEncodingInfoByGoName(name)
	if info != nil {
		return true
	}

	decoder := getDecoder(name)
	if decoder != nil {
		return true
	}

	encoder := getEncoder(name)
	return encoder != nil
}

// logWarning logs a warning message using the global logger.
func logWarning(msg string) {
	config.Logger.Warn(msg, "module", "charset")
}
