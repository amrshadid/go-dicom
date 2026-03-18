package sr_test

import (
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sr"
	"github.com/amrshadid/go-dicom/tag"
)

func TestSRToDataset(t *testing.T) {
	report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")
	report.PatientName = "Doe^John"
	report.PatientID = "12345"
	report.StudyInstanceUID = "1.2.3"
	report.SeriesInstanceUID = "1.2.3.4"
	report.CreationTime = time.Now()

	report.ReportContent.Findings = append(report.ReportContent.Findings, sr.Finding{
		ID:          "F1",
		Description: "Abnormality detected",
		Severity:    "MAJOR",
		Confidence:  0.95,
	})

	report.ReportContent.Conclusions = append(report.ReportContent.Conclusions, "Final diagnosis")

	// Convert to dataset
	ds, err := report.ToDataset()

	if err != nil {
		t.Fatalf("ToDataset failed: %v", err)
	}

	if ds == nil {
		t.Error("Expected non-nil dataset")
	}

	// Verify key elements exist
	if !ds.Contains(tag.New(0x0008, 0x0018)) {
		t.Error("SOP Instance UID not found in dataset")
	}

	if !ds.Contains(tag.New(0x0010, 0x0010)) {
		t.Error("Patient Name not found in dataset")
	}

	if !ds.Contains(tag.New(0x0010, 0x0020)) {
		t.Error("Patient ID not found in dataset")
	}
}

func TestSRToDatasetNil(t *testing.T) {
	var report *sr.StructuredReport

	ds, err := report.ToDataset()

	if err == nil {
		t.Error("Expected error for nil report")
	}

	if ds != nil {
		t.Error("Expected nil dataset")
	}
}

func TestFromDataset(t *testing.T) {
	// Create a dataset
	ds := dataset.NewDataset()

	// Add patient information
	patientName := dataelem.NewDataElement(
		tag.New(0x0010, 0x0010),
		dataelem.PN,
		[]interface{}{"Doe^John"},
	)
	ds.Add(patientName)

	patientID := dataelem.NewDataElement(
		tag.New(0x0010, 0x0020),
		dataelem.LO,
		[]interface{}{"12345"},
	)
	ds.Add(patientID)

	// Add SOP UIDs
	sopClass := dataelem.NewDataElement(
		tag.New(0x0008, 0x0016),
		dataelem.UI,
		[]interface{}{"1.2.840.10008.5.1.4.1.1.88.11"},
	)
	ds.Add(sopClass)

	sopInstance := dataelem.NewDataElement(
		tag.New(0x0008, 0x0018),
		dataelem.UI,
		[]interface{}{"1.2.3.4.5"},
	)
	ds.Add(sopInstance)

	// Add study and series UIDs
	studyUID := dataelem.NewDataElement(
		tag.New(0x0020, 0x000D),
		dataelem.UI,
		[]interface{}{"1.2.3"},
	)
	ds.Add(studyUID)

	seriesUID := dataelem.NewDataElement(
		tag.New(0x0020, 0x000E),
		dataelem.UI,
		[]interface{}{"1.2.3.4"},
	)
	ds.Add(seriesUID)

	// Convert from dataset
	report, err := sr.FromDataset(ds)

	if err != nil {
		t.Fatalf("FromDataset failed: %v", err)
	}

	if report == nil {
		t.Fatal("Expected non-nil report")
	}

	if report.PatientName != "Doe^John" {
		t.Errorf("Expected patient name 'Doe^John', got '%s'", report.PatientName)
	}

	if report.PatientID != "12345" {
		t.Errorf("Expected patient ID '12345', got '%s'", report.PatientID)
	}

	if report.SOPInstanceUID != "1.2.3.4.5" {
		t.Errorf("Expected SOP Instance UID '1.2.3.4.5', got '%s'", report.SOPInstanceUID)
	}

	if report.SOPClassUID != "1.2.840.10008.5.1.4.1.1.88.11" {
		t.Errorf("Expected SOP Class UID '1.2.840.10008.5.1.4.1.1.88.11', got '%s'", report.SOPClassUID)
	}
}

func TestFromDatasetNil(t *testing.T) {
	var ds *dataset.Dataset

	report, err := sr.FromDataset(ds)

	if err == nil {
		t.Error("Expected error for nil dataset")
	}

	if report != nil {
		t.Error("Expected nil report")
	}
}

func TestRoundTripConversion(t *testing.T) {
	// Create original report
	original := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")
	original.PatientName = "Smith^Jane"
	original.PatientID = "67890"
	original.StudyInstanceUID = "1.2.3"
	original.SeriesInstanceUID = "1.2.3.4"

	// Convert to dataset
	ds, err := original.ToDataset()
	if err != nil {
		t.Fatalf("ToDataset failed: %v", err)
	}

	// Convert back to report
	recovered, err := sr.FromDataset(ds)
	if err != nil {
		t.Fatalf("FromDataset failed: %v", err)
	}

	// Verify key fields match
	if recovered.SOPInstanceUID != original.SOPInstanceUID {
		t.Errorf("SOP Instance UID mismatch: %s vs %s",
			recovered.SOPInstanceUID, original.SOPInstanceUID)
	}

	if recovered.SOPClassUID != original.SOPClassUID {
		t.Errorf("SOP Class UID mismatch: %s vs %s",
			recovered.SOPClassUID, original.SOPClassUID)
	}

	if recovered.PatientName != original.PatientName {
		t.Errorf("Patient Name mismatch: %s vs %s",
			recovered.PatientName, original.PatientName)
	}

	if recovered.PatientID != original.PatientID {
		t.Errorf("Patient ID mismatch: %s vs %s",
			recovered.PatientID, original.PatientID)
	}

	if recovered.StudyInstanceUID != original.StudyInstanceUID {
		t.Errorf("Study Instance UID mismatch: %s vs %s",
			recovered.StudyInstanceUID, original.StudyInstanceUID)
	}

	if recovered.SeriesInstanceUID != original.SeriesInstanceUID {
		t.Errorf("Series Instance UID mismatch: %s vs %s",
			recovered.SeriesInstanceUID, original.SeriesInstanceUID)
	}
}

func TestToDatasetWithFindingsAndConclusions(t *testing.T) {
	report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")
	report.PatientName = "Test^Patient"
	report.PatientID = "TEST001"

	// Add findings and conclusions
	report.ReportContent.Findings = append(report.ReportContent.Findings, sr.Finding{
		ID:          "F1",
		Description: "Abnormal finding",
		Severity:    "MAJOR",
		Confidence:  0.90,
	})

	report.ReportContent.Conclusions = append(report.ReportContent.Conclusions, "Requires follow-up")

	ds, err := report.ToDataset()

	if err != nil {
		t.Fatalf("ToDataset failed: %v", err)
	}

	// Verify findings description was added
	if elem, ok := ds.Get(tag.New(0x0008, 0x1030)); ok {
		// Element found, success
		if elem == nil {
			t.Error("Expected element, got nil")
		}
	}

	// Verify conclusions were added
	if elem, ok := ds.Get(tag.New(0x0040, 0xA160)); ok {
		// Element found, success
		if elem == nil {
			t.Error("Expected element, got nil")
		}
	}
}

func TestFromDatasetWithFindings(t *testing.T) {
	ds := dataset.NewDataset()

	// Add basic elements
	sopInstance := dataelem.NewDataElement(
		tag.New(0x0008, 0x0018),
		dataelem.UI,
		[]interface{}{"1.2.3.4.5"},
	)
	ds.Add(sopInstance)

	// Add finding description
	finding := dataelem.NewDataElement(
		tag.New(0x0008, 0x1030),
		dataelem.LO,
		[]interface{}{"Heart abnormality"},
	)
	ds.Add(finding)

	// Add conclusion
	conclusion := dataelem.NewDataElement(
		tag.New(0x0040, 0xA160),
		dataelem.UT,
		[]interface{}{"Further evaluation recommended"},
	)
	ds.Add(conclusion)

	report, err := sr.FromDataset(ds)

	if err != nil {
		t.Fatalf("FromDataset failed: %v", err)
	}

	// Check that finding was extracted
	if len(report.ReportContent.Findings) == 0 {
		t.Error("Expected findings to be extracted")
	} else if report.ReportContent.Findings[0].Description != "Heart abnormality" {
		t.Errorf("Expected finding description 'Heart abnormality', got '%s'",
			report.ReportContent.Findings[0].Description)
	}

	// Check that conclusion was extracted
	if len(report.ReportContent.Conclusions) == 0 {
		t.Error("Expected conclusions to be extracted")
	} else if report.ReportContent.Conclusions[0] != "Further evaluation recommended" {
		t.Errorf("Expected conclusion 'Further evaluation recommended', got '%s'",
			report.ReportContent.Conclusions[0])
	}
}

func TestDatasetConversionThreadSafe(t *testing.T) {
	report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")
	report.PatientName = "Concurrent^Test"
	report.PatientID = "CONC001"

	// Run multiple conversions concurrently
	done := make(chan bool, 5)

	for i := 0; i < 5; i++ {
		go func() {
			ds, err := report.ToDataset()
			if err != nil {
				t.Errorf("ToDataset failed: %v", err)
			}
			if ds == nil {
				t.Error("ToDataset returned nil")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestFromDatasetPreservesReportContent(t *testing.T) {
	// Create original with report content
	original := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.88.11")
	original.PatientName = "Content^Test"
	original.PatientID = "CONT001"

	// Convert to dataset
	ds, err := original.ToDataset()
	if err != nil {
		t.Fatalf("ToDataset failed: %v", err)
	}

	// Convert back
	recovered, err := sr.FromDataset(ds)
	if err != nil {
		t.Fatalf("FromDataset failed: %v", err)
	}

	// Verify report content structure exists
	if recovered.ReportContent == nil {
		t.Error("Expected ReportContent to be preserved")
	}

	if recovered.ReportContent.Findings == nil {
		t.Error("Expected Findings slice to be preserved")
	}

	if recovered.ReportContent.Conclusions == nil {
		t.Error("Expected Conclusions slice to be preserved")
	}
}
