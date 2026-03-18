# SR

DICOM Structured Report creation, validation, and management with template-based construction, standardized medical coding (SNOMED-CT, LOINC, DCM), fluent builders, and file I/O.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/sr"

// Create a report
report := sr.NewStructuredReport("1.2.3.4.5", "1.2.840.10008.5.1.4.1.1.66.4")
report.PatientName = "Smith^Jane"
report.AddFinding(sr.Finding{
    ID: "F001", Description: "Nodule in right upper lobe",
    Severity: "MAJOR", Location: "Right lung", Confidence: 0.87,
})
report.AddObservation(sr.Observation{Code: "NODULE_SIZE", Value: "12", Unit: "mm"})
report.AddConclusion("Recommend follow-up imaging")

// Fluent builder
report, result, err := sr.NewSRBuilder("1.2.3", "1.2.840.10008").
    AddPatientInfo("Doe^John", "12345").
    AddFinding("F1", "Abnormality", "MAJOR", "Abdomen", 0.95).
    Build()

// File I/O
sr.WriteSRFile("output.dcm", report)
report, _ := sr.ReadSRFile("document.dcm")

// Dataset conversion
ds, _ := report.ToDataset()
report, _ := sr.FromDataset(ds)
```

## API Reference

```go
// Report
func NewStructuredReport(sopInstanceUID, sopClassUID string) *StructuredReport
func (r *StructuredReport) AddFinding(f Finding) error
func (r *StructuredReport) AddObservation(o Observation) error
func (r *StructuredReport) AddConclusion(text string) error
func (r *StructuredReport) ToDataset() (*dataset.Dataset, error)
func FromDataset(ds *dataset.Dataset) (*StructuredReport, error)

// Builder
func NewSRBuilder(sopInstanceUID, sopClassUID string) *SRBuilder
func (b *SRBuilder) AddPatientInfo(name, id string) *SRBuilder
func (b *SRBuilder) AddFinding(id, desc, severity, location string, confidence float64) *SRBuilder
func (b *SRBuilder) WithValidator(v *SRValidator) *SRBuilder
func (b *SRBuilder) Build() (*StructuredReport, *ValidationResult, error)

// Coding
func NewSRCode(value, schemeDesignator, meaning string) *SRCode
func CreateAnatomyCodeCollection() *CodeCollection
func CreateFindingTypeCodeCollection() *CodeCollection

// Validation and templates
func NewSRValidator() *SRValidator
func NewSRTemplate(id, name, mappingResource string) *SRTemplate

// File I/O
func ReadSRFile(path string) (*StructuredReport, error)
func WriteSRFile(path string, report *StructuredReport) error

// Manager
func NewStructuredReportManager() *StructuredReportManager
func ValidateReport(report *StructuredReport) error
func GenerateReportSummary(report *StructuredReport) string
```

## References

- [DICOM PS3.3 Section A](https://dicom.nema.org/medical/dicom/current/output/html/part03.html) - Structured Report IOD definitions
- [SNOMED-CT](https://www.snomed.org/) / [LOINC](https://loinc.org/) - Medical coding standards
