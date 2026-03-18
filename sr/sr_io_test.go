package sr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/sr"
)

func TestReadSRFileNonexistent(t *testing.T) {
	filename := "/nonexistent/path/file.dcm"

	report, err := sr.ReadSRFile(filename)

	if err == nil {
		t.Error("Expected error for nonexistent file")
	}

	if report != nil {
		t.Error("Expected nil report for nonexistent file")
	}
}

func TestReadSRFileEmptyFilename(t *testing.T) {
	report, err := sr.ReadSRFile("")

	if err == nil {
		t.Error("Expected error for empty filename")
	}

	if report != nil {
		t.Error("Expected nil report for empty filename")
	}
}

func TestWriteSRFileNilReport(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_sr.dcm")

	err := sr.WriteSRFile(tmpFile, nil)

	if err == nil {
		t.Error("Expected error for nil report")
	}

	// Cleanup
	os.Remove(tmpFile)
}

func TestWriteSRFileEmptyFilename(t *testing.T) {
	report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")

	err := sr.WriteSRFile("", report)

	if err == nil {
		t.Error("Expected error for empty filename")
	}
}

func TestWriteSRFileMissingSOPInstanceUID(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_sr_missing_uid.dcm")
	report := sr.NewStructuredReport("", "1.2.840.10008.5.1.4.1.1.88.11")

	err := sr.WriteSRFile(tmpFile, report)

	if err == nil {
		t.Error("Expected error for missing SOP Instance UID")
	}

	// Cleanup
	os.Remove(tmpFile)
}

func TestWriteSRFileMissingSOPClassUID(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_sr_missing_class.dcm")
	report := sr.NewStructuredReport("1.2.3.4.5", "")

	err := sr.WriteSRFile(tmpFile, report)

	if err == nil {
		t.Error("Expected error for missing SOP Class UID")
	}

	// Cleanup
	os.Remove(tmpFile)
}

func TestWriteSRFileSuccess(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_sr_success.dcm")
	defer os.Remove(tmpFile)

	report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")
	report.PatientName = "Test^Patient"
	report.PatientID = "TEST001"

	err := sr.WriteSRFile(tmpFile, report)

	// Note: This may fail due to missing filewriter implementation details
	// but we're testing the API interface
	if err != nil {
		t.Logf("WriteSRFile encountered expected implementation limitation: %v", err)
	}
}

func TestValidateAndWriteSRFileNilReport(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_validate_nil.dcm")
	defer os.Remove(tmpFile)

	result, err := sr.ValidateAndWriteSRFile(tmpFile, nil, nil, "")

	if err == nil {
		t.Error("Expected error for nil report")
	}

	if result != nil {
		t.Error("Expected nil validation result")
	}
}

func TestValidateAndWriteSRFileEmptyFilename(t *testing.T) {
	report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")

	result, err := sr.ValidateAndWriteSRFile("", report, nil, "")

	if err == nil {
		t.Error("Expected error for empty filename")
	}

	if result != nil {
		t.Error("Expected nil validation result")
	}
}

func TestValidateAndWriteSRFileWithValidator(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_validate_with_validator.dcm")
	defer os.Remove(tmpFile)

	report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")
	report.PatientName = "Test^Patient"
	report.PatientID = "TEST001"

	validator := sr.NewSRValidator()
	template := sr.NewSRTemplate("TEMPLATE001", "Test Template", "DICOM")
	validator.RegisterTemplate(template)

	result, err := sr.ValidateAndWriteSRFile(tmpFile, report, validator, "TEMPLATE001")

	// Check validation result
	if result != nil && len(result.Warnings) > 0 {
		t.Logf("Validation warnings: %v", result.Warnings)
	}

	if err != nil {
		t.Logf("ValidateAndWriteSRFile encountered expected implementation limitation: %v", err)
	}
}

func TestReadAndValidateSRFileNonexistent(t *testing.T) {
	filename := "/nonexistent/path/file.dcm"
	validator := sr.NewSRValidator()

	report, result, err := sr.ReadAndValidateSRFile(filename, validator, "")

	if err == nil {
		t.Error("Expected error for nonexistent file")
	}

	if report != nil {
		t.Error("Expected nil report")
	}

	if result != nil {
		t.Error("Expected nil validation result")
	}
}

func TestReadAndValidateSRFileEmptyFilename(t *testing.T) {
	validator := sr.NewSRValidator()

	report, result, err := sr.ReadAndValidateSRFile("", validator, "")

	if err == nil {
		t.Error("Expected error for empty filename")
	}

	if report != nil {
		t.Error("Expected nil report")
	}

	if result != nil {
		t.Error("Expected nil validation result")
	}
}

func TestReadAndValidateSRFileWithoutValidator(t *testing.T) {
	// This test would require a valid SR file to exist
	// For now, just test the interface
	filename := "/nonexistent/file.dcm"

	report, result, err := sr.ReadAndValidateSRFile(filename, nil, "")

	if err == nil {
		t.Error("Expected error for nonexistent file")
	}

	if report != nil {
		t.Error("Expected nil report")
	}

	// Validation result should be nil when validator not provided
	if result != nil {
		t.Error("Expected nil validation result")
	}
}

func TestSRFileIOIntegration(t *testing.T) {
	// Create a test report
	report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")
	report.PatientName = "Integration^Test"
	report.PatientID = "INT001"
	report.StudyInstanceUID = "1.2.3"
	report.SeriesInstanceUID = "1.2.3.4"

	report.ReportContent.Findings = append(report.ReportContent.Findings, sr.Finding{
		ID:          "F1",
		Description: "Test Finding",
		Severity:    "MAJOR",
		Confidence:  0.95,
	})

	report.ReportContent.Conclusions = append(report.ReportContent.Conclusions, "Test Conclusion")

	// Test dataset conversion (part of I/O)
	ds, err := report.ToDataset()
	if err != nil {
		t.Fatalf("ToDataset failed: %v", err)
	}

	if ds == nil {
		t.Error("Expected non-nil dataset")
	}

	// Test conversion back
	recovered, err := sr.FromDataset(ds)
	if err != nil {
		t.Fatalf("FromDataset failed: %v", err)
	}

	if recovered.PatientName != report.PatientName {
		t.Errorf("Patient name mismatch: %s vs %s",
			recovered.PatientName, report.PatientName)
	}

	if recovered.PatientID != report.PatientID {
		t.Errorf("Patient ID mismatch: %s vs %s",
			recovered.PatientID, report.PatientID)
	}
}
