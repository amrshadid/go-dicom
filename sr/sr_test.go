package sr_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/sr"
)

// ========== Basic Structured Report Tests ==========

// Test structured report creation
func TestNewStructuredReport(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	if srep.SOPInstanceUID != "1.2.3.4.5" {
		t.Errorf("expected SOPInstanceUID '1.2.3.4.5', got %s", srep.SOPInstanceUID)
	}

	if srep.ReportContent == nil {
		t.Fatal("expected non-nil ReportContent")
	}

	if srep.CreationTime.IsZero() {
		t.Error("expected non-zero CreationTime")
	}
}

// Test add finding
func TestAddFinding(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	finding := sr.Finding{
		ID:          "FIND001",
		Description: "Abnormality detected",
		Severity:    "MAJOR",
		Location:    "Left lung",
		Confidence:  0.95,
	}

	err := srep.AddFinding(finding)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	findings := srep.GetFindings()
	if len(findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(findings))
	}

	if findings[0].ID != "FIND001" {
		t.Error("finding ID mismatch")
	}
}

// Test finding validation - empty ID
func TestAddFindingEmptyID(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	finding := sr.Finding{
		Description: "Abnormality",
		Severity:    "MAJOR",
		Confidence:  0.95,
	}

	err := srep.AddFinding(finding)
	if err == nil {
		t.Error("expected error for empty finding ID")
	}
}

// Test finding validation - invalid confidence
func TestAddFindingInvalidConfidence(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	finding := sr.Finding{
		ID:          "FIND001",
		Description: "Abnormality",
		Severity:    "MAJOR",
		Confidence:  1.5,
	}

	err := srep.AddFinding(finding)
	if err == nil {
		t.Error("expected error for invalid confidence")
	}
}

// Test add conclusion
func TestAddConclusion(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	err := srep.AddConclusion("Recommend further investigation")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	conclusions := srep.GetConclusions()
	if len(conclusions) != 1 {
		t.Errorf("expected 1 conclusion, got %d", len(conclusions))
	}

	if conclusions[0] != "Recommend further investigation" {
		t.Error("conclusion mismatch")
	}
}

// Test add empty conclusion
func TestAddEmptyConclusion(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	err := srep.AddConclusion("")
	if err == nil {
		t.Error("expected error for empty conclusion")
	}
}

// Test add observation
func TestAddObservation(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	obs := sr.Observation{
		Code:  "TUMOR_SIZE",
		Value: "5.2",
		Unit:  "cm",
		Time:  time.Now(),
	}

	err := srep.AddObservation(obs)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	observations := srep.GetObservations()
	if len(observations) != 1 {
		t.Errorf("expected 1 observation, got %d", len(observations))
	}
}

// Test add reference
func TestAddReference(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	ref := sr.ReportReference{
		ReferencedSOPClassUID:    "1.2.840.10008.5.1.4.1.2",
		ReferencedSOPInstanceUID: "1.2.3.4.5.6",
		ReferenceType:            "IMAGE",
	}

	err := srep.AddReference(ref)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	references := srep.GetReferences()
	if len(references) != 1 {
		t.Errorf("expected 1 reference, got %d", len(references))
	}
}

// Test invalid reference
func TestAddInvalidReference(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	ref := sr.ReportReference{
		ReferencedSOPClassUID: "1.2.840.10008.5.1.4.1.2",
	}

	err := srep.AddReference(ref)
	if err == nil {
		t.Error("expected error for invalid reference")
	}
}

// Test SR manager creation
func TestNewStructuredReportManager(t *testing.T) {
	manager := sr.NewStructuredReportManager()

	if manager == nil {
		t.Fatal("expected non-nil manager")
	}

	reports := manager.ListReports()
	if len(reports) != 0 {
		t.Errorf("expected empty reports list, got %d", len(reports))
	}
}

// Test create template
func TestCreateTemplate(t *testing.T) {
	manager := sr.NewStructuredReportManager()

	template := sr.ReportTemplate{
		TemplateID:  "SR_TEMPLATE_001",
		Name:        "Oncology Report",
		Description: "Standard oncology report template",
		Sections: []sr.ReportSection{
			{
				ID:    "SEC001",
				Title: "Findings",
			},
		},
	}

	err := manager.CreateTemplate(template)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	retrieved, exists := manager.GetTemplate("SR_TEMPLATE_001")
	if !exists {
		t.Fatal("expected template to exist")
	}

	if retrieved.Name != "Oncology Report" {
		t.Error("template name mismatch")
	}
}

// Test duplicate template error
func TestCreateDuplicateTemplate(t *testing.T) {
	manager := sr.NewStructuredReportManager()

	template := sr.ReportTemplate{
		TemplateID: "SR_TEMPLATE_001",
		Name:       "Report",
	}

	_ = manager.CreateTemplate(template)

	err := manager.CreateTemplate(template)
	if err == nil {
		t.Error("expected error for duplicate template")
	}
}

// Test save report
func TestSaveReport(t *testing.T) {
	manager := sr.NewStructuredReportManager()

	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
	srep.PatientName = "Doe^John"
	srep.PatientID = "12345"

	err := manager.SaveReport(srep)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	retrieved, exists := manager.GetReport("1.2.3.4.5")
	if !exists {
		t.Fatal("expected report to exist")
	}

	if retrieved.PatientName != "Doe^John" {
		t.Error("patient name mismatch")
	}
}

// Test save nil report
func TestSaveNilReport(t *testing.T) {
	manager := sr.NewStructuredReportManager()

	err := manager.SaveReport(nil)
	if err == nil {
		t.Error("expected error for nil report")
	}
}

// Test list reports
func TestListReports(t *testing.T) {
	manager := sr.NewStructuredReportManager()

	sr1 := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
	sr2 := sr.NewStructuredReport("1.2.3.4.6", "1.2.840.10008.5.1.4.1.1.66.4")

	manager.SaveReport(sr1)
	manager.SaveReport(sr2)

	reports := manager.ListReports()
	if len(reports) != 2 {
		t.Errorf("expected 2 reports, got %d", len(reports))
	}
}

// Test delete report
func TestDeleteReport(t *testing.T) {
	manager := sr.NewStructuredReportManager()

	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
	manager.SaveReport(srep)

	deleted := manager.DeleteReport("1.2.3.4.5")
	if !deleted {
		t.Error("expected report to be deleted")
	}

	_, exists := manager.GetReport("1.2.3.4.5")
	if exists {
		t.Error("expected report to be gone")
	}
}

// Test list templates
func TestListTemplates(t *testing.T) {
	manager := sr.NewStructuredReportManager()

	t1 := sr.ReportTemplate{TemplateID: "T1", Name: "Template1"}
	t2 := sr.ReportTemplate{TemplateID: "T2", Name: "Template2"}

	manager.CreateTemplate(t1)
	manager.CreateTemplate(t2)

	templates := manager.ListTemplates()
	if len(templates) != 2 {
		t.Errorf("expected 2 templates, got %d", len(templates))
	}
}

// Test validate finding
func TestValidateFinding(t *testing.T) {
	tests := []struct {
		name    string
		finding sr.Finding
		wantErr bool
	}{
		{
			name: "valid finding",
			finding: sr.Finding{
				ID:          "F1",
				Description: "Test",
				Severity:    "MAJOR",
				Confidence:  0.5,
			},
			wantErr: false,
		},
		{
			name: "invalid severity",
			finding: sr.Finding{
				ID:          "F1",
				Description: "Test",
				Severity:    "UNKNOWN",
				Confidence:  0.5,
			},
			wantErr: true,
		},
		{
			name: "invalid confidence",
			finding: sr.Finding{
				ID:          "F1",
				Description: "Test",
				Severity:    "MAJOR",
				Confidence:  1.5,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sr.ValidateFinding(tt.finding)
			if (err != nil) != tt.wantErr {
				t.Errorf("expected error: %v, got: %v", tt.wantErr, err)
			}
		})
	}
}

// Test summarize findings
func TestSummarizeFinding(t *testing.T) {
	findings := []sr.Finding{
		{ID: "F1", Severity: "CRITICAL"},
		{ID: "F2", Severity: "CRITICAL"},
		{ID: "F3", Severity: "MAJOR"},
		{ID: "F4", Severity: "MINOR"},
	}

	summary := sr.SummarizeFinding(findings)

	if summary["CRITICAL"] != 2 {
		t.Errorf("expected 2 CRITICAL, got %d", summary["CRITICAL"])
	}

	if summary["MAJOR"] != 1 {
		t.Errorf("expected 1 MAJOR, got %d", summary["MAJOR"])
	}

	if summary["MINOR"] != 1 {
		t.Errorf("expected 1 MINOR, got %d", summary["MINOR"])
	}
}

// Test filter findings by severity
func TestFilterFindingsBySeverity(t *testing.T) {
	findings := []sr.Finding{
		{ID: "F1", Severity: "CRITICAL"},
		{ID: "F2", Severity: "MAJOR"},
		{ID: "F3", Severity: "CRITICAL"},
	}

	critical := sr.FilterFindingsBySeverity(findings, "CRITICAL")

	if len(critical) != 2 {
		t.Errorf("expected 2 critical findings, got %d", len(critical))
	}

	for _, f := range critical {
		if f.Severity != "CRITICAL" {
			t.Error("expected only critical findings")
		}
	}
}

// Test validate report
func TestValidateReport(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
	srep.PatientName = "Doe^John"

	err := sr.ValidateReport(srep)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// Test validate report - missing SOPInstanceUID
func TestValidateReportMissingSOPInstanceUID(t *testing.T) {
	srep := &sr.StructuredReport{
		SOPClassUID: "1.2.840.10008.5.1.4.1.1.66.4",
	}

	err := sr.ValidateReport(srep)
	if err == nil {
		t.Error("expected error for missing SOPInstanceUID")
	}
}

// Test generate report summary
func TestGenerateReportSummary(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
	srep.PatientName = "Doe^John"
	srep.PatientID = "12345"

	srep.AddFinding(sr.Finding{
		ID:          "F1",
		Description: "Abnormality",
		Severity:    "MAJOR",
		Confidence:  0.95,
	})

	srep.AddConclusion("Recommend treatment")

	summary := sr.GenerateReportSummary(srep)

	if len(summary) == 0 {
		t.Fatal("expected non-empty summary")
	}

	if !contains(summary, "Doe^John") {
		t.Error("expected patient name in summary")
	}

	if !contains(summary, "Abnormality") {
		t.Error("expected finding description in summary")
	}
}

// Test report with multiple findings and conclusions
func TestComplexReport(t *testing.T) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	for i := 1; i <= 3; i++ {
		finding := sr.Finding{
			ID:          fmt.Sprintf("%dF", i),
			Description: fmt.Sprintf("Finding %d", i),
			Severity:    "MAJOR",
			Confidence:  float64(i) * 0.1,
		}
		srep.AddFinding(finding)
	}

	findings := srep.GetFindings()
	if len(findings) != 3 {
		t.Errorf("expected 3 findings, got %d", len(findings))
	}

	for _, c := range []string{"Conclusion 1", "Conclusion 2"} {
		srep.AddConclusion(c)
	}

	conclusions := srep.GetConclusions()
	if len(conclusions) != 2 {
		t.Errorf("expected 2 conclusions, got %d", len(conclusions))
	}
}

// Test concurrent operations
func TestConcurrentOperations(t *testing.T) {
	manager := sr.NewStructuredReportManager()

	done := make(chan bool, 20)

	for i := 0; i < 10; i++ {
		go func() {
			_ = manager.ListReports()
			_ = manager.ListTemplates()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		go func(id int) {
			srep := sr.NewStructuredReport(fmt.Sprintf("%d1.2.3", id), "1.2.840.10008.5.1.4.1.1.66.4")
			_ = manager.SaveReport(srep)
			done <- true
		}(i)
	}

	for i := 0; i < 20; i++ {
		<-done
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Benchmark structured report creation
func BenchmarkNewStructuredReport(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
	}
}

// Benchmark add finding
func BenchmarkAddFinding(b *testing.B) {
	srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")

	finding := sr.Finding{
		ID:          "F1",
		Description: "Test",
		Severity:    "MAJOR",
		Confidence:  0.95,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = srep.AddFinding(finding)
	}
}

// Benchmark manager operations
func BenchmarkManagerSaveReport(b *testing.B) {
	manager := sr.NewStructuredReportManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		srep := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
		_ = manager.SaveReport(srep)
	}
}

// ========== Advanced SR Tests ==========

func TestNewSRCode(t *testing.T) {
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")

	if code.Value != "49953000" {
		t.Errorf("Expected value '49953000', got '%s'", code.Value)
	}
	if code.SchemeDesignator != "SCT" {
		t.Errorf("Expected scheme 'SCT', got '%s'", code.SchemeDesignator)
	}
	if code.Meaning != "Abdomen" {
		t.Errorf("Expected meaning 'Abdomen', got '%s'", code.Meaning)
	}
}

func TestNewSRCodeWithVersion(t *testing.T) {
	code := sr.NewSRCodeWithVersion("49953000", sr.CodeSchemes.SNOMED, "Abdomen", "2023-01")

	if code.SchemeVersion != "2023-01" {
		t.Errorf("Expected version '2023-01', got '%s'", code.SchemeVersion)
	}
}

func TestSRCodeString(t *testing.T) {
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")
	str := code.String()

	if str == "" {
		t.Error("String representation should not be empty")
	}
}

func TestSRCodeHash(t *testing.T) {
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")
	hash := code.Hash()

	expected := "SCT:49953000"
	if hash != expected {
		t.Errorf("Expected hash '%s', got '%s'", expected, hash)
	}
}

func TestSRCodeEquals(t *testing.T) {
	code1 := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")
	code2 := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")
	code3 := sr.NewSRCode("12738006", sr.CodeSchemes.SNOMED, "Brain")

	if !code1.Equals(code2) {
		t.Error("Codes with same value and scheme should be equal")
	}

	if code1.Equals(code3) {
		t.Error("Codes with different values should not be equal")
	}
}

func TestSRCodeEqualsNil(t *testing.T) {
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")

	if code.Equals(nil) {
		t.Error("Code should not equal nil")
	}
}

func TestSRCodeSRTMapping(t *testing.T) {
	code1 := sr.NewSRCode("12345", "SRT", "Test")
	code2 := sr.NewSRCode("12345", "SCT", "Test")

	if !code1.Equals(code2) {
		t.Error("SRT codes should map to SCT for equality")
	}
}

func TestCodeSchemes(t *testing.T) {
	if sr.CodeSchemes.SNOMED != "SCT" {
		t.Errorf("Expected SNOMED scheme 'SCT', got '%s'", sr.CodeSchemes.SNOMED)
	}
	if sr.CodeSchemes.DCM != "DCM" {
		t.Errorf("Expected DCM scheme 'DCM', got '%s'", sr.CodeSchemes.DCM)
	}
	if sr.CodeSchemes.LOCAL != "LOCAL" {
		t.Errorf("Expected LOCAL scheme 'LOCAL', got '%s'", sr.CodeSchemes.LOCAL)
	}
}

func TestNewSRTemplate(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test Template", "DICOM")

	if template.TemplateID != "1234" {
		t.Errorf("Expected ID '1234', got '%s'", template.TemplateID)
	}
	if template.Name != "Test Template" {
		t.Errorf("Expected name 'Test Template', got '%s'", template.Name)
	}
	if template.MappingResource != "DICOM" {
		t.Errorf("Expected mapping 'DICOM', got '%s'", template.MappingResource)
	}
	if len(template.ContentSequence) != 0 {
		t.Error("New template should have empty content sequence")
	}
}

func TestAddContentNode(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")
	node := &sr.ContentNode{
		NodeID:      "node1",
		ConceptCode: code,
		NodeType:    "CONTAINER",
	}

	template.AddContentNode(node)

	if len(template.ContentSequence) != 1 {
		t.Errorf("Expected 1 node, got %d", len(template.ContentSequence))
	}
}

func TestAddConstraint(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	constraint := &sr.Constraint{
		Name:         "required_finding",
		Type:         "REQUIRED",
		ErrorMessage: "At least one finding is required",
		Severity:     "ERROR",
	}

	err := template.AddConstraint(constraint)
	if err != nil {
		t.Fatalf("Failed to add constraint: %v", err)
	}

	if len(template.Constraints) != 1 {
		t.Errorf("Expected 1 constraint, got %d", len(template.Constraints))
	}
}

func TestAddConstraintNil(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")

	err := template.AddConstraint(nil)
	if err == nil {
		t.Error("Expected error for nil constraint")
	}
}

func TestGetContentNode(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")
	node := &sr.ContentNode{
		NodeID:      "node1",
		ConceptCode: code,
		NodeType:    "CONTAINER",
		Children:    make([]*sr.ContentNode, 0),
	}

	template.AddContentNode(node)

	found := template.GetContentNode("node1")
	if found == nil {
		t.Fatal("Expected to find node")
	}

	if found.NodeID != "node1" {
		t.Errorf("Expected node ID 'node1', got '%s'", found.NodeID)
	}
}

func TestGetContentNodeNotFound(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")

	found := template.GetContentNode("nonexistent")
	if found != nil {
		t.Error("Expected nil for nonexistent node")
	}
}

func TestNewSRValidator(t *testing.T) {
	validator := sr.NewSRValidator()

	if validator == nil {
		t.Error("Expected validator, got nil")
	}
}

func TestRegisterTemplate(t *testing.T) {
	validator := sr.NewSRValidator()
	template := sr.NewSRTemplate("1234", "Test", "DICOM")

	err := validator.RegisterTemplate(template)
	if err != nil {
		t.Fatalf("Failed to register template: %v", err)
	}

	registered, exists := validator.GetTemplate("1234")
	if !exists {
		t.Error("Template should be registered")
	}

	if registered.Name != "Test" {
		t.Errorf("Expected name 'Test', got '%s'", registered.Name)
	}
}

func TestRegisterTemplateNil(t *testing.T) {
	validator := sr.NewSRValidator()

	err := validator.RegisterTemplate(nil)
	if err == nil {
		t.Error("Expected error for nil template")
	}
}

func TestRegisterTemplateEmptyID(t *testing.T) {
	validator := sr.NewSRValidator()
	template := sr.NewSRTemplate("", "Test", "DICOM")

	err := validator.RegisterTemplate(template)
	if err == nil {
		t.Error("Expected error for empty template ID")
	}
}

func TestGetTemplate(t *testing.T) {
	validator := sr.NewSRValidator()
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	validator.RegisterTemplate(template)

	found, exists := validator.GetTemplate("1234")
	if !exists {
		t.Error("Template should exist")
	}

	if found.TemplateID != "1234" {
		t.Errorf("Expected ID '1234', got '%s'", found.TemplateID)
	}
}

func TestGetTemplateNotFound(t *testing.T) {
	validator := sr.NewSRValidator()

	_, exists := validator.GetTemplate("nonexistent")
	if exists {
		t.Error("Nonexistent template should not exist")
	}
}

func TestListAdvancedTemplates(t *testing.T) {
	validator := sr.NewSRValidator()

	validator.RegisterTemplate(sr.NewSRTemplate("1234", "Test1", "DICOM"))
	validator.RegisterTemplate(sr.NewSRTemplate("5678", "Test2", "DICOM"))
	validator.RegisterTemplate(sr.NewSRTemplate("9012", "Test3", "DICOM"))

	templates := validator.ListTemplates()
	if len(templates) != 3 {
		t.Errorf("Expected 3 templates, got %d", len(templates))
	}
}

func TestRemoveTemplate(t *testing.T) {
	validator := sr.NewSRValidator()
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	validator.RegisterTemplate(template)

	success := validator.RemoveTemplate("1234")
	if !success {
		t.Error("Expected successful removal")
	}

	_, exists := validator.GetTemplate("1234")
	if exists {
		t.Error("Template should not exist after removal")
	}
}

func TestRemoveTemplateNotFound(t *testing.T) {
	validator := sr.NewSRValidator()

	success := validator.RemoveTemplate("nonexistent")
	if success {
		t.Error("Expected removal to fail for nonexistent template")
	}
}

func TestValidateSRDocumentNoTemplate(t *testing.T) {
	validator := sr.NewSRValidator()
	doc := sr.NewStructuredReport("1.2.3", "1.2.840.10008")

	result := validator.ValidateSRDocument(doc, "nonexistent")

	if result.IsValid {
		t.Error("Validation should fail for nonexistent template")
	}

	if len(result.Errors) == 0 {
		t.Error("Expected validation errors")
	}
}

func TestValidateSRDocumentMissingUID(t *testing.T) {
	validator := sr.NewSRValidator()
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	validator.RegisterTemplate(template)

	doc := &sr.StructuredReport{
		SOPClassUID: "1.2.840.10008",
	}

	result := validator.ValidateSRDocument(doc, "1234")

	if result.IsValid {
		t.Error("Validation should fail for missing SOPInstanceUID")
	}
}

func TestValidateSRDocumentValid(t *testing.T) {
	validator := sr.NewSRValidator()
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	validator.RegisterTemplate(template)

	doc := sr.NewStructuredReport("1.2.3", "1.2.840.10008")

	result := validator.ValidateSRDocument(doc, "1234")

	if !result.IsValid {
		t.Error("Validation should pass for valid document")
	}

	if result.Duration < 0 {
		t.Error("Duration should not be negative")
	}
}

func TestNewSRBuilder(t *testing.T) {
	builder := sr.NewSRBuilder("1.2.3", "1.2.840.10008")

	if builder == nil {
		t.Error("Expected builder, got nil")
	}

	report := builder.GetReport()
	if report.SOPInstanceUID != "1.2.3" {
		t.Errorf("Expected UID '1.2.3', got '%s'", report.SOPInstanceUID)
	}
}

func TestBuilderWithTemplate(t *testing.T) {
	builder := sr.NewSRBuilder("1.2.3", "1.2.840.10008")
	template := sr.NewSRTemplate("1234", "Test", "DICOM")

	result := builder.WithTemplate(template)

	if result != builder {
		t.Error("WithTemplate should return builder for chaining")
	}

	tmpl := builder.GetTemplate()
	if tmpl.TemplateID != "1234" {
		t.Error("Template should be set")
	}
}

func TestBuilderWithValidator(t *testing.T) {
	builder := sr.NewSRBuilder("1.2.3", "1.2.840.10008")
	validator := sr.NewSRValidator()

	result := builder.WithValidator(validator)

	if result != builder {
		t.Error("WithValidator should return builder for chaining")
	}

	val := builder.GetValidator()
	if val == nil {
		t.Error("Validator should be set")
	}
}

func TestBuilderAddPatientInfo(t *testing.T) {
	builder := sr.NewSRBuilder("1.2.3", "1.2.840.10008")

	builder.AddPatientInfo("Doe^John", "12345")

	report := builder.GetReport()
	if report.PatientName != "Doe^John" {
		t.Errorf("Expected name 'Doe^John', got '%s'", report.PatientName)
	}

	if report.PatientID != "12345" {
		t.Errorf("Expected ID '12345', got '%s'", report.PatientID)
	}
}

func TestBuilderAddStudyInfo(t *testing.T) {
	builder := sr.NewSRBuilder("1.2.3", "1.2.840.10008")

	builder.AddStudyInfo("1.2.3.4", "1.2.3.4.5")

	report := builder.GetReport()
	if report.StudyInstanceUID != "1.2.3.4" {
		t.Errorf("Expected study UID '1.2.3.4', got '%s'", report.StudyInstanceUID)
	}

	if report.SeriesInstanceUID != "1.2.3.4.5" {
		t.Errorf("Expected series UID '1.2.3.4.5', got '%s'", report.SeriesInstanceUID)
	}
}

func TestBuilderAddFinding(t *testing.T) {
	builder := sr.NewSRBuilder("1.2.3", "1.2.840.10008")

	builder.AddFinding("F1", "Abnormality detected", "MAJOR", "Abdomen", 0.95)

	if len(builder.GetReport().ReportContent.Findings) != 1 {
		t.Errorf("Expected 1 finding, got %d", len(builder.GetReport().ReportContent.Findings))
	}

	finding := builder.GetReport().ReportContent.Findings[0]
	if finding.Confidence != 0.95 {
		t.Errorf("Expected confidence 0.95, got %f", finding.Confidence)
	}
}

func TestBuilderAddObservation(t *testing.T) {
	builder := sr.NewSRBuilder("1.2.3", "1.2.840.10008")

	builder.AddObservation("Heart Rate", "72", "bpm")

	if len(builder.GetReport().ReportContent.Observations) != 1 {
		t.Errorf("Expected 1 observation, got %d", len(builder.GetReport().ReportContent.Observations))
	}

	obs := builder.GetReport().ReportContent.Observations[0]
	if obs.Value != "72" {
		t.Errorf("Expected value '72', got '%s'", obs.Value)
	}
}

func TestBuilderAddConclusion(t *testing.T) {
	builder := sr.NewSRBuilder("1.2.3", "1.2.840.10008")

	builder.AddConclusion("No significant abnormality")

	if len(builder.GetReport().ReportContent.Conclusions) != 1 {
		t.Errorf("Expected 1 conclusion, got %d", len(builder.GetReport().ReportContent.Conclusions))
	}

	if builder.GetReport().ReportContent.Conclusions[0] != "No significant abnormality" {
		t.Error("Conclusion not added correctly")
	}
}

func TestBuilderBuild(t *testing.T) {
	builder := sr.NewSRBuilder("1.2.3", "1.2.840.10008")
	builder.AddPatientInfo("Doe^John", "12345")

	report, _, err := builder.Build()

	if err != nil {
		t.Fatalf("Build should not fail: %v", err)
	}

	if report.SOPInstanceUID != "1.2.3" {
		t.Error("Report not built correctly")
	}
}

func TestBuilderBuildMissingUID(t *testing.T) {
	// Create a builder with class UID but no instance UID
	builder := sr.NewSRBuilder("", "1.2.840.10008")

	_, _, err := builder.Build()

	if err == nil {
		t.Error("Build should fail for missing SOPInstanceUID")
	}
}

func TestBuilderBuildMissingClass(t *testing.T) {
	// Create a builder with instance UID but no class UID
	builder := sr.NewSRBuilder("1.2.3", "")

	_, _, err := builder.Build()

	if err == nil {
		t.Error("Build should fail for missing SOPClassUID")
	}
}

func TestBuilderChaining(t *testing.T) {
	validator := sr.NewSRValidator()
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	validator.RegisterTemplate(template)

	report, _, err := sr.NewSRBuilder("1.2.3", "1.2.840.10008").
		WithValidator(validator).
		WithTemplate(template).
		AddPatientInfo("Doe^John", "12345").
		AddFinding("F1", "Finding", "MAJOR", "Abdomen", 0.9).
		Build()

	if err != nil {
		t.Fatalf("Chained build failed: %v", err)
	}

	if len(report.ReportContent.Findings) != 1 {
		t.Error("Findings not added in chain")
	}
}

func TestNewContentBuilder(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	builder := sr.NewContentBuilder(template)

	if builder == nil {
		t.Error("Expected builder, got nil")
	}
}

func TestAddContainer(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	builder := sr.NewContentBuilder(template)
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")

	node := builder.AddContainer("node1", code, "CONTAINS")

	if node.NodeType != "CONTAINER" {
		t.Errorf("Expected CONTAINER, got %s", node.NodeType)
	}

	if len(template.ContentSequence) != 1 {
		t.Error("Node should be added to template")
	}
}

func TestAddValueNode(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	builder := sr.NewContentBuilder(template)
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Finding")

	node := builder.AddValueNode("node1", code, "TEXT")

	if node.NodeType != "VALUE" {
		t.Errorf("Expected VALUE, got %s", node.NodeType)
	}

	if node.ValueType != "TEXT" {
		t.Errorf("Expected TEXT value type, got %s", node.ValueType)
	}
}

func TestAddConceptModifier(t *testing.T) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")
	parentNode := &sr.ContentNode{
		NodeID:      "parent",
		ConceptCode: code,
		Children:    make([]*sr.ContentNode, 0),
	}

	modCode := sr.NewSRCode("7771000", sr.CodeSchemes.SNOMED, "Left")
	builder := sr.NewContentBuilder(template)
	builder.AddConceptModifier(parentNode, modCode)

	if len(parentNode.Children) != 1 {
		t.Error("Modifier should be added as child")
	}

	if parentNode.Children[0].RelationshipType != "HAS CONCEPT MOD" {
		t.Error("Modifier relationship type incorrect")
	}
}

func TestNewContextIDRegistry(t *testing.T) {
	registry := sr.NewContextIDRegistry()

	if registry == nil {
		t.Error("Expected registry, got nil")
	}
}

func TestRegisterCID(t *testing.T) {
	registry := sr.NewContextIDRegistry()
	codes := map[string]*sr.SRCode{
		"Normal":   sr.NewSRCode("N", "DCM", "Normal"),
		"Abnormal": sr.NewSRCode("A", "DCM", "Abnormal"),
	}

	err := registry.RegisterCID(1234, codes)

	if err != nil {
		t.Fatalf("Failed to register CID: %v", err)
	}
}

func TestRegisterCIDInvalid(t *testing.T) {
	registry := sr.NewContextIDRegistry()

	err := registry.RegisterCID(-1, make(map[string]*sr.SRCode))

	if err == nil {
		t.Error("Expected error for negative CID")
	}
}

func TestGetCIDCodes(t *testing.T) {
	registry := sr.NewContextIDRegistry()
	codes := map[string]*sr.SRCode{
		"Normal": sr.NewSRCode("N", "DCM", "Normal"),
	}

	registry.RegisterCID(1234, codes)

	retrieved := registry.GetCIDCodes(1234)

	if len(retrieved) != 1 {
		t.Errorf("Expected 1 code, got %d", len(retrieved))
	}

	if code, exists := retrieved["Normal"]; !exists || code.Value != "N" {
		t.Error("Code not retrieved correctly")
	}
}

func TestGetCIDCodesNotFound(t *testing.T) {
	registry := sr.NewContextIDRegistry()

	retrieved := registry.GetCIDCodes(9999)

	if len(retrieved) != 0 {
		t.Errorf("Expected 0 codes, got %d", len(retrieved))
	}
}

func TestGetCIDCode(t *testing.T) {
	registry := sr.NewContextIDRegistry()
	codes := map[string]*sr.SRCode{
		"Normal": sr.NewSRCode("N", "DCM", "Normal"),
	}

	registry.RegisterCID(1234, codes)

	code, found := registry.GetCIDCode(1234, "Normal")

	if !found {
		t.Error("Code should be found")
	}

	if code.Value != "N" {
		t.Errorf("Expected 'N', got '%s'", code.Value)
	}
}

func TestListCIDs(t *testing.T) {
	registry := sr.NewContextIDRegistry()

	registry.RegisterCID(1234, make(map[string]*sr.SRCode))
	registry.RegisterCID(5678, make(map[string]*sr.SRCode))

	cids := registry.ListCIDs()

	if len(cids) != 2 {
		t.Errorf("Expected 2 CIDs, got %d", len(cids))
	}
}

func TestNewCodeCollection(t *testing.T) {
	collection := sr.NewCodeCollection("TestCollection")

	if collection.Size() != 0 {
		t.Error("New collection should be empty")
	}
}

func TestAddCode(t *testing.T) {
	collection := sr.NewCodeCollection("Test")
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")

	err := collection.AddCode("Abdomen", code)

	if err != nil {
		t.Fatalf("Failed to add code: %v", err)
	}

	if collection.Size() != 1 {
		t.Errorf("Expected size 1, got %d", collection.Size())
	}
}

func TestAddCodeEmptyKeyword(t *testing.T) {
	collection := sr.NewCodeCollection("Test")
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")

	err := collection.AddCode("", code)

	if err == nil {
		t.Error("Expected error for empty keyword")
	}
}

func TestAddCodeNil(t *testing.T) {
	collection := sr.NewCodeCollection("Test")

	err := collection.AddCode("Abdomen", nil)

	if err == nil {
		t.Error("Expected error for nil code")
	}
}

func TestGetCode(t *testing.T) {
	collection := sr.NewCodeCollection("Test")
	originalCode := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")

	collection.AddCode("Abdomen", originalCode)

	retrieved, found := collection.GetCode("Abdomen")

	if !found {
		t.Error("Code should be found")
	}

	if retrieved.Value != "49953000" {
		t.Error("Retrieved code does not match")
	}
}

func TestContains(t *testing.T) {
	collection := sr.NewCodeCollection("Test")
	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")

	collection.AddCode("Abdomen", code)

	if !collection.Contains(code) {
		t.Error("Collection should contain the code")
	}
}

func TestContainsNil(t *testing.T) {
	collection := sr.NewCodeCollection("Test")

	if collection.Contains(nil) {
		t.Error("Collection should not contain nil")
	}
}

func TestList(t *testing.T) {
	collection := sr.NewCodeCollection("Test")

	collection.AddCode("Abdomen", sr.NewSRCode("1", "SCT", "Abdomen"))
	collection.AddCode("Brain", sr.NewSRCode("2", "SCT", "Brain"))

	keywords := collection.List()

	if len(keywords) != 2 {
		t.Errorf("Expected 2 keywords, got %d", len(keywords))
	}
}

func TestListWithFilters(t *testing.T) {
	collection := sr.NewCodeCollection("Test")

	collection.AddCode("Abdomen", sr.NewSRCode("1", "SCT", "Abdomen"))
	collection.AddCode("Brain", sr.NewSRCode("2", "SCT", "Brain"))
	collection.AddCode("Abdominal", sr.NewSRCode("3", "SCT", "Abdominal"))

	keywords := collection.List("abd")

	if len(keywords) != 2 {
		t.Errorf("Expected 2 filtered keywords, got %d: %v", len(keywords), keywords)
	}
}

func TestCreateAnatomyCodeCollection(t *testing.T) {
	collection := sr.CreateAnatomyCodeCollection()

	if collection.Size() == 0 {
		t.Error("Anatomy collection should not be empty")
	}

	code, found := collection.GetCode("Abdomen")
	if !found {
		t.Error("Abdomen code should exist")
	}

	if code.Value != "89545001" {
		t.Errorf("Expected code '89545001', got '%s'", code.Value)
	}
}

func TestCreateFindingTypeCodeCollection(t *testing.T) {
	collection := sr.CreateFindingTypeCodeCollection()

	if collection.Size() == 0 {
		t.Error("Finding type collection should not be empty")
	}

	code, found := collection.GetCode("Normal")
	if !found {
		t.Error("Normal code should exist")
	}

	if code.Value != "17621005" {
		t.Errorf("Expected code '17621005', got '%s'", code.Value)
	}
}

func TestCreateModifierCodeCollection(t *testing.T) {
	collection := sr.CreateModifierCodeCollection()

	if collection.Size() == 0 {
		t.Error("Modifier collection should not be empty")
	}

	code, found := collection.GetCode("Left")
	if !found {
		t.Error("Left modifier should exist")
	}

	if code.Value != "7771000" {
		t.Errorf("Expected code '7771000', got '%s'", code.Value)
	}
}

func TestConcurrentCodeCollectionAdd(t *testing.T) {
	collection := sr.NewCodeCollection("Test")

	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(index int) {
			code := sr.NewSRCode(fmt.Sprintf("%d", index), "SCT", fmt.Sprintf("Code%d", index))
			err := collection.AddCode(fmt.Sprintf("Code%d", index), code)
			done <- err
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent add failed: %v", err)
		}
	}

	if collection.Size() != 10 {
		t.Errorf("Expected size 10, got %d", collection.Size())
	}
}

func TestConcurrentTemplateValidation(t *testing.T) {
	validator := sr.NewSRValidator()
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	validator.RegisterTemplate(template)

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_, exists := validator.GetTemplate("1234")
			done <- exists
		}()
	}

	for i := 0; i < 10; i++ {
		if !<-done {
			t.Error("Concurrent template retrieval failed")
		}
	}
}

// Benchmark tests
func BenchmarkNewSRCode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sr.NewSRCode("49953000", "SCT", "Abdomen")
	}
}

func BenchmarkSRCodeHash(b *testing.B) {
	code := sr.NewSRCode("49953000", "SCT", "Abdomen")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		code.Hash()
	}
}

func BenchmarkSRCodeEquals(b *testing.B) {
	code1 := sr.NewSRCode("49953000", "SCT", "Abdomen")
	code2 := sr.NewSRCode("49953000", "SCT", "Abdomen")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		code1.Equals(code2)
	}
}

func BenchmarkTemplateAddNode(b *testing.B) {
	template := sr.NewSRTemplate("1234", "Test", "DICOM")
	code := sr.NewSRCode("49953000", "SCT", "Abdomen")
	node := &sr.ContentNode{
		NodeID:      "node",
		ConceptCode: code,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		template.AddContentNode(node)
	}
}

func BenchmarkValidatorRegisterTemplate(b *testing.B) {
	validator := sr.NewSRValidator()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		template := sr.NewSRTemplate(fmt.Sprintf("tmpl%d", i), "Test", "DICOM")
		validator.RegisterTemplate(template)
	}
}

func BenchmarkSRBuilderBuild(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sr.NewSRBuilder("1.2.3", "1.2.840.10008").
			AddPatientInfo("Doe^John", "12345").
			AddFinding("F1", "Finding", "MAJOR", "Abdomen", 0.9).
			Build()
	}
}

func BenchmarkCodeCollectionAdd(b *testing.B) {
	collection := sr.NewCodeCollection("Test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		code := sr.NewSRCode(fmt.Sprintf("%d", i), "SCT", fmt.Sprintf("Code%d", i))
		collection.AddCode(fmt.Sprintf("Code%d", i), code)
	}
}
