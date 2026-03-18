// Package sr provides comprehensive support for DICOM Structured Reports.
//
// This package implements complete functionality for creating, validating, and managing
// DICOM Structured Reports (SR), including both basic reporting capabilities and
// advanced features for template-based SR construction and validation.
//
// # Core Concepts
//
// DICOM Structured Reports are hierarchical documents used to represent the results
// of medical observations and findings in a standardized, machine-readable format.
// They combine the semantic richness of coded concepts with the flexibility of
// structured templates.
//
// # Key Features
//
//   - **Basic SR Support**: Create and manage structured reports with findings,
//     observations, conclusions, and references
//   - **Advanced Coding**: Support for SNOMED-CT, DCM, LOINC, and other coding schemes
//   - **Template System**: Define and enforce structured report templates with constraints
//   - **Validation**: Comprehensive validation of SR documents against templates
//   - **Builders**: Fluent API for constructing reports and templates
//   - **Code Collections**: Pre-built collections for anatomy, findings, and modifiers
//   - **Context IDs**: Support for DICOM Context ID definitions
//   - **Thread-Safe**: All operations are safe for concurrent use
//
// # Basic Structured Report Types
//
// ## StructuredReport
//
// Represents a complete DICOM Structured Report containing patient information,
// clinical content, and metadata.
//
//	sr := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
//	sr.PatientName = "Doe^John"
//	sr.PatientID = "12345"
//
// ## Finding
//
// Represents a clinical finding within a report, including severity and confidence.
//
//	finding := sr.Finding{
//	    ID:          "FIND001",
//	    Description: "Abnormality detected",
//	    Severity:    "MAJOR",
//	    Location:    "Left lung",
//	    Confidence:  0.95,
//	}
//	sr.AddFinding(finding)
//
// ## Observation
//
// Represents a measured or observed value with code and units.
//
//	obs := sr.Observation{
//	    Code:  "TUMOR_SIZE",
//	    Value: "5.2",
//	    Unit:  "cm",
//	    Time:  time.Now(),
//	}
//	sr.AddObservation(obs)
//
// # Advanced Features
//
// ## SRCode
//
// Represents a coded concept using standard medical coding schemes.
// Supports SNOMED-CT, LOINC, DCM, and other schemes.
//
//	code := sr.NewSRCode("49953000", sr.CodeSchemes.SNOMED, "Abdomen")
//	codeWithVersion := sr.NewSRCodeWithVersion("49953000", sr.CodeSchemes.SNOMED, "Abdomen", "2023-01")
//
// ## SRTemplate
//
// Defines the structure and constraints for a specific type of structured report.
//
//	template := sr.NewSRTemplate("TEMPLATE_001", "Oncology Report", "DICOM")
//	template.AddContentNode(&sr.ContentNode{
//	    NodeID:      "findings",
//	    ConceptCode: code,
//	    NodeType:    "CONTAINER",
//	})
//
// ## SRValidator
//
// Validates SR documents against registered templates.
//
//	validator := sr.NewSRValidator()
//	validator.RegisterTemplate(template)
//	result := validator.ValidateSRDocument(report, "TEMPLATE_001")
//
// ## SRBuilder
//
// Provides fluent API for constructing structured reports.
//
//	report, _, err := sr.NewSRBuilder("1.2.3", "1.2.840.10008").
//	    AddPatientInfo("Doe^John", "12345").
//	    AddFinding("F1", "Finding", "MAJOR", "Abdomen", 0.9).
//	    AddConclusion("Further investigation recommended").
//	    Build()
//
// ## CodeCollection
//
// Manages collections of coded concepts for use in reports.
//
//	collection := sr.CreateAnatomyCodeCollection()
//	code, found := collection.GetCode("Abdomen")
//	if found {
//	    fmt.Printf("Abdomen code: %s\n", code.Value)
//	}
//
// # Creating a Basic Structured Report
//
// ## Step 1: Create the Report
//
//	report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
//	report.PatientName = "Smith^Jane"
//	report.PatientID = "P123456"
//
// ## Step 2: Add Clinical Content
//
//	report.AddFinding(sr.Finding{
//	    ID:          "F001",
//	    Description: "Nodule in right upper lobe",
//	    Severity:    "MAJOR",
//	    Location:    "Right lung",
//	    Confidence:  0.87,
//	})
//
//	report.AddObservation(sr.Observation{
//	    Code:  "NODULE_SIZE",
//	    Value: "12",
//	    Unit:  "mm",
//	    Time:  time.Now(),
//	})
//
//	report.AddConclusion("Recommend follow-up imaging")
//
// ## Step 3: Validate and Save
//
//	if err := sr.ValidateReport(report); err != nil {
//	    fmt.Printf("Validation error: %v\n", err)
//	}
//	manager := sr.NewStructuredReportManager()
//	manager.SaveReport(report)
//
// # Creating Advanced Structured Reports
//
// ## Using Templates
//
//	validator := sr.NewSRValidator()
//	template := sr.NewSRTemplate("TEMPLATE_001", "Lung Report", "DICOM")
//
//	constraint := &sr.Constraint{
//	    Name:         "requires_findings",
//	    Type:         "REQUIRED",
//	    ErrorMessage: "Report must contain at least one finding",
//	    Severity:     "ERROR",
//	}
//	template.AddConstraint(constraint)
//	validator.RegisterTemplate(template)
//
// ## Using the Builder with Validation
//
//	report, validationResult, err := sr.NewSRBuilder("1.2.3", "1.2.840.10008").
//	    WithValidator(validator).
//	    WithTemplate(template).
//	    AddPatientInfo("Doe^John", "12345").
//	    AddFinding("F1", "Finding", "MAJOR", "Abdomen", 0.9).
//	    Build()
//
//	if !validationResult.IsValid {
//	    for _, err := range validationResult.Errors {
//	        fmt.Printf("Error: %s\n", err.Message)
//	    }
//	}
//
// # Coding Schemes
//
// The package supports standard DICOM coding schemes:
//
//   - SNOMED-CT (SCT): Clinical Terms
//   - DCM: DICOM Content Mapping Resource
//   - LN: LOINC (Logical Observation Identifiers Names and Codes)
//   - UCUM: Unified Code for Units of Measure
//   - LOCAL: Local schemes
//   - CLSI: Clinical and Laboratory Standards Institute
//
// Access schemes via:
//
//	sr.CodeSchemes.SNOMED  // "SCT"
//	sr.CodeSchemes.DCM     // "DCM"
//	sr.CodeSchemes.LN      // "LN"
//	sr.CodeSchemes.UCUM    // "UCUM"
//
// # Pre-built Code Collections
//
// The package includes pre-built collections for common coding needs:
//
// ## Anatomy Codes
//
//	anatomy := sr.CreateAnatomyCodeCollection()
//	code, _ := anatomy.GetCode("Abdomen")   // SNOMED 89545001
//	code, _ := anatomy.GetCode("Liver")      // SNOMED 10200004
//	code, _ := anatomy.GetCode("Lung")       // SNOMED 39607008
//
// ## Finding Type Codes
//
//	findings := sr.CreateFindingTypeCodeCollection()
//	code, _ := findings.GetCode("Normal")     // SNOMED 17621005
//	code, _ := findings.GetCode("Abnormal")   // SNOMED 386585008
//
// ## Modifier Codes
//
//	modifiers := sr.CreateModifierCodeCollection()
//	code, _ := modifiers.GetCode("Left")      // SNOMED 7771000
//	code, _ := modifiers.GetCode("Bilateral") // SNOMED 51440002
//	code, _ := modifiers.GetCode("Acute")     // SNOMED 373610005
//
// # Context ID Registry
//
// Support for DICOM Context ID definitions:
//
//	registry := sr.NewContextIDRegistry()
//	codes := map[string]*sr.SRCode{
//	    "Yes": sr.NewSRCode("Y", "DCM", "Yes"),
//	    "No":  sr.NewSRCode("N", "DCM", "No"),
//	}
//	registry.RegisterCID(3850, codes)
//	code, found := registry.GetCIDCode(3850, "Yes")
//
// # Structured Report Manager
//
// Centralized management of reports and templates:
//
//	manager := sr.NewStructuredReportManager()
//
//	// Save reports
//	manager.SaveReport(report)
//
//	// Retrieve reports
//	report, exists := manager.GetReport("1.2.3.4.5")
//
//	// List all reports
//	allUIDs := manager.ListReports()
//
//	// Delete reports
//	deleted := manager.DeleteReport("1.2.3.4.5")
//
//	// Manage templates
//	manager.CreateTemplate(template)
//	retrieved, exists := manager.GetTemplate("TEMPLATE_001")
//	allTemplates := manager.ListTemplates()
//
// # Validation
//
// ## Basic Validation
//
//	err := sr.ValidateFinding(finding)
//	err := sr.ValidateReport(report)
//
// ## Advanced Validation with Templates
//
//	validator := sr.NewSRValidator()
//	result := validator.ValidateSRDocument(report, templateID)
//
//	if !result.IsValid {
//	    fmt.Printf("Validation took %v\n", result.Duration)
//	    for _, err := range result.Errors {
//	        fmt.Printf("Error [%s]: %s\n", err.Code, err.Message)
//	    }
//	    for _, warn := range result.Warnings {
//	        fmt.Printf("Warning [%s]: %s\n", warn.Code, warn.Message)
//	    }
//	}
//
// # Utilities
//
// ## Finding Summary
//
//	summary := sr.SummarizeFinding(findings)
//	fmt.Printf("Critical findings: %d\n", summary["CRITICAL"])
//	fmt.Printf("Major findings: %d\n", summary["MAJOR"])
//
// ## Filter Findings
//
//	critical := sr.FilterFindingsBySeverity(findings, "CRITICAL")
//
// ## Generate Summary
//
//	summary := sr.GenerateReportSummary(report)
//	fmt.Println(summary)
//
// # Thread Safety
//
// All types in this package are designed for thread-safe concurrent access:
//
//   - StructuredReport uses sync.RWMutex for content access
//   - StructuredReportManager uses sync.RWMutex for reports and templates
//   - SRTemplate uses sync.RWMutex for content and constraints
//   - SRValidator uses sync.RWMutex for template registry
//   - CodeCollection uses sync.RWMutex for code storage
//   - ContextIDRegistry uses sync.RWMutex for CID storage
//
// # Performance Considerations
//
//   - Report operations: O(1) for basic add/get operations
//   - Manager operations: O(1) for save/retrieve, O(n) for listing
//   - Validation: O(n) where n is the number of constraints
//   - Code lookups: O(1) in CodeCollection
//   - Template building: O(1) for adding nodes
//
// # Error Handling
//
// Functions return errors for validation failures and constraint violations:
//
//	// Adding invalid findings
//	err := report.AddFinding(finding) // Returns error if validation fails
//
//	// Creating duplicate templates
//	err := manager.CreateTemplate(template) // Returns error if template exists
//
//	// Registering invalid templates
//	err := validator.RegisterTemplate(nil) // Returns error if template is nil
//
// # DICOM Compliance
//
// The package implements DICOM standards for:
//
//   - Structured Report structure (SR Information Object Definition)
//   - Coded concept representation (SOP Classes, Value Representations)
//   - Finding severity levels (CRITICAL, MAJOR, MINOR, INFO)
//   - Standard coding schemes (SNOMED-CT, DCM, LOINC, UCUM)
//   - Template constraints and validation
//
// See: https://www.dicomstandard.org/
//
// # See Also
//
//   - uid package: UID management and validation
//   - dataset package: DICOM dataset structure
//   - tag package: DICOM tag definitions
//   - values package: Value encoding and conversion
package sr
