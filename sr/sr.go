package sr

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ========== Basic Structured Report Types ==========

// StructuredReport represents a DICOM Structured Report.
type StructuredReport struct {
	SOPInstanceUID    string
	SOPClassUID       string
	PatientName       string
	PatientID         string
	StudyInstanceUID  string
	SeriesInstanceUID string
	ReportContent     *ReportContent
	CreationTime      time.Time
	mu                sync.RWMutex
}

// ReportContent contains the structured report data.
type ReportContent struct {
	Findings     []Finding
	Conclusions  []string
	Observations []Observation
	References   []ReportReference
}

// Finding represents a finding in the report.
type Finding struct {
	ID          string
	Description string
	Severity    string
	Location    string
	Confidence  float64
}

// Observation represents an observation.
type Observation struct {
	Code  string
	Value string
	Unit  string
	Time  time.Time
}

// ReportReference represents a reference to other objects.
type ReportReference struct {
	ReferencedSOPClassUID    string
	ReferencedSOPInstanceUID string
	ReferenceType            string
}

// ========== Basic Report Templates ==========

// ReportTemplate defines a structured report template.
type ReportTemplate struct {
	TemplateID  string
	Name        string
	Description string
	Sections    []ReportSection
	CreatedTime time.Time
}

// ReportSection represents a section in a report template.
type ReportSection struct {
	ID     string
	Title  string
	Fields []TemplateField
}

// TemplateField represents a field in a report template.
type TemplateField struct {
	FieldID   string
	FieldName string
	FieldType string
	Required  bool
	Options   []string
}

// ========== Basic Structured Report Manager ==========

// StructuredReportManager manages structured reports.
type StructuredReportManager struct {
	reports   map[string]*StructuredReport
	templates map[string]*ReportTemplate
	mu        sync.RWMutex
}

// NewStructuredReportManager creates a new SR manager.
func NewStructuredReportManager() *StructuredReportManager {
	return &StructuredReportManager{
		reports:   make(map[string]*StructuredReport),
		templates: make(map[string]*ReportTemplate),
	}
}

// NewStructuredReport creates a new structured report.
func NewStructuredReport(sopInstanceUID, sopClassUID string) *StructuredReport {
	return &StructuredReport{
		SOPInstanceUID: sopInstanceUID,
		SOPClassUID:    sopClassUID,
		ReportContent:  &ReportContent{},
		CreationTime:   time.Now(),
	}
}

// AddFinding adds a finding to the report.
func (sr *StructuredReport) AddFinding(finding Finding) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if finding.ID == "" {
		return fmt.Errorf("finding ID cannot be empty")
	}

	if finding.Description == "" {
		return fmt.Errorf("finding description cannot be empty")
	}

	if finding.Confidence < 0 || finding.Confidence > 1.0 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}

	sr.ReportContent.Findings = append(sr.ReportContent.Findings, finding)
	return nil
}

// GetFindings returns all findings.
func (sr *StructuredReport) GetFindings() []Finding {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	result := make([]Finding, len(sr.ReportContent.Findings))
	copy(result, sr.ReportContent.Findings)
	return result
}

// AddConclusion adds a conclusion.
func (sr *StructuredReport) AddConclusion(conclusion string) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if conclusion == "" {
		return fmt.Errorf("conclusion cannot be empty")
	}

	sr.ReportContent.Conclusions = append(sr.ReportContent.Conclusions, conclusion)
	return nil
}

// GetConclusions returns all conclusions.
func (sr *StructuredReport) GetConclusions() []string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	result := make([]string, len(sr.ReportContent.Conclusions))
	copy(result, sr.ReportContent.Conclusions)
	return result
}

// AddObservation adds an observation.
func (sr *StructuredReport) AddObservation(obs Observation) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if obs.Code == "" {
		return fmt.Errorf("observation code cannot be empty")
	}

	sr.ReportContent.Observations = append(sr.ReportContent.Observations, obs)
	return nil
}

// GetObservations returns all observations.
func (sr *StructuredReport) GetObservations() []Observation {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	result := make([]Observation, len(sr.ReportContent.Observations))
	copy(result, sr.ReportContent.Observations)
	return result
}

// AddReference adds a reference.
func (sr *StructuredReport) AddReference(ref ReportReference) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if ref.ReferencedSOPClassUID == "" || ref.ReferencedSOPInstanceUID == "" {
		return fmt.Errorf("reference must have SOPClassUID and SOPInstanceUID")
	}

	sr.ReportContent.References = append(sr.ReportContent.References, ref)
	return nil
}

// GetReferences returns all references.
func (sr *StructuredReport) GetReferences() []ReportReference {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	result := make([]ReportReference, len(sr.ReportContent.References))
	copy(result, sr.ReportContent.References)
	return result
}

// CreateTemplate creates a new report template.
func (m *StructuredReportManager) CreateTemplate(template ReportTemplate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if template.TemplateID == "" {
		return fmt.Errorf("template ID cannot be empty")
	}

	if template.Name == "" {
		return fmt.Errorf("template name cannot be empty")
	}

	if _, exists := m.templates[template.TemplateID]; exists {
		return fmt.Errorf("template with ID %s already exists", template.TemplateID)
	}

	m.templates[template.TemplateID] = &template
	return nil
}

// GetTemplate retrieves a template.
func (m *StructuredReportManager) GetTemplate(templateID string) (*ReportTemplate, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	template, exists := m.templates[templateID]
	return template, exists
}

// SaveReport saves a structured report.
func (m *StructuredReportManager) SaveReport(report *StructuredReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if report == nil {
		return fmt.Errorf("cannot save nil report")
	}

	if report.SOPInstanceUID == "" {
		return fmt.Errorf("report must have SOPInstanceUID")
	}

	m.reports[report.SOPInstanceUID] = report
	return nil
}

// GetReport retrieves a saved report.
func (m *StructuredReportManager) GetReport(sopInstanceUID string) (*StructuredReport, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	report, exists := m.reports[sopInstanceUID]
	return report, exists
}

// ListReports returns list of saved report UIDs.
func (m *StructuredReportManager) ListReports() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	uids := make([]string, 0, len(m.reports))
	for uid := range m.reports {
		uids = append(uids, uid)
	}
	return uids
}

// DeleteReport deletes a report.
func (m *StructuredReportManager) DeleteReport(sopInstanceUID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.reports[sopInstanceUID]; exists {
		delete(m.reports, sopInstanceUID)
		return true
	}
	return false
}

// ListTemplates returns list of template IDs.
func (m *StructuredReportManager) ListTemplates() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.templates))
	for id := range m.templates {
		ids = append(ids, id)
	}
	return ids
}

// ========== Basic Utilities ==========

// ValidateFinding validates a finding structure.
func ValidateFinding(f Finding) error {
	if f.ID == "" {
		return fmt.Errorf("finding ID required")
	}
	if f.Description == "" {
		return fmt.Errorf("finding description required")
	}
	if f.Severity != "CRITICAL" && f.Severity != "MAJOR" && f.Severity != "MINOR" && f.Severity != "INFO" {
		return fmt.Errorf("invalid severity: %s", f.Severity)
	}
	if f.Confidence < 0 || f.Confidence > 1.0 {
		return fmt.Errorf("confidence must be 0-1, got %f", f.Confidence)
	}
	return nil
}

// SummarizeFinding creates a summary of findings.
func SummarizeFinding(findings []Finding) map[string]int {
	summary := make(map[string]int)
	for _, f := range findings {
		summary[f.Severity]++
	}
	return summary
}

// FilterFindingsBySeverity filters findings by severity.
func FilterFindingsBySeverity(findings []Finding, severity string) []Finding {
	var filtered []Finding
	for _, f := range findings {
		if f.Severity == severity {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// ValidateReport validates a structured report.
func ValidateReport(report *StructuredReport) error {
	if report == nil {
		return fmt.Errorf("report cannot be nil")
	}

	if report.SOPInstanceUID == "" {
		return fmt.Errorf("SOPInstanceUID required")
	}

	if report.SOPClassUID == "" {
		return fmt.Errorf("SOPClassUID required")
	}

	for _, f := range report.GetFindings() {
		if err := ValidateFinding(f); err != nil {
			return fmt.Errorf("invalid finding: %w", err)
		}
	}

	return nil
}

// GenerateReportSummary generates a text summary of the report.
func GenerateReportSummary(report *StructuredReport) string {
	summary := "STRUCTURED REPORT SUMMARY\n"
	summary += fmt.Sprintf("Patient: %s (ID: %s)\n", report.PatientName, report.PatientID)
	summary += fmt.Sprintf("Report ID: %s\n", report.SOPInstanceUID)
	summary += fmt.Sprintf("Created: %s\n", report.CreationTime.Format(time.RFC3339))

	findings := report.GetFindings()
	summary += fmt.Sprintf("\nFindings: %d\n", len(findings))
	for _, f := range findings {
		summary += fmt.Sprintf("  - %s (%s): %s [Confidence: %.1f%%]\n",
			f.ID, f.Severity, f.Description, f.Confidence*100)
	}

	conclusions := report.GetConclusions()
	summary += fmt.Sprintf("\nConclusions: %d\n", len(conclusions))
	for _, c := range conclusions {
		summary += fmt.Sprintf("  - %s\n", c)
	}

	return summary
}

// ========== Advanced SR Code System ==========

// SRCode represents a coded concept in a Structured Report.
type SRCode struct {
	Value              string
	SchemeDesignator   string
	Meaning            string
	SchemeVersion      string
	PrivateScheme      bool
}

// SRCodeSchemes defines standard coding scheme designators.
type SRCodeSchemes struct {
	SNOMED string
	DCM    string
	LN     string
	UCUM   string
	SRT    string
	LOCAL  string
	CLSI   string
	USCB   string
	ISO639 string
}

// CodeSchemes provides standard scheme designators.
var CodeSchemes = SRCodeSchemes{
	SNOMED: "SCT",
	DCM:    "DCM",
	LN:     "LN",
	UCUM:   "UCUM",
	SRT:    "SRT",
	LOCAL:  "LOCAL",
	CLSI:   "CLSI",
	USCB:   "USCB",
	ISO639: "ISO639",
}

// NewSRCode creates a new coded concept.
func NewSRCode(value, schemeDesignator, meaning string) *SRCode {
	return &SRCode{
		Value:            value,
		SchemeDesignator: schemeDesignator,
		Meaning:          meaning,
	}
}

// NewSRCodeWithVersion creates a new coded concept with scheme version.
func NewSRCodeWithVersion(value, schemeDesignator, meaning, schemeVersion string) *SRCode {
	return &SRCode{
		Value:            value,
		SchemeDesignator: schemeDesignator,
		Meaning:          meaning,
		SchemeVersion:    schemeVersion,
	}
}

// String returns string representation of the code.
func (sc *SRCode) String() string {
	if sc.SchemeVersion != "" {
		return fmt.Sprintf("Code(%s, %s, %s, version=%s)", sc.Value, sc.SchemeDesignator, sc.Meaning, sc.SchemeVersion)
	}
	return fmt.Sprintf("Code(%s, %s, %s)", sc.Value, sc.SchemeDesignator, sc.Meaning)
}

// Hash returns a hash value for the code.
func (sc *SRCode) Hash() string {
	return sc.SchemeDesignator + ":" + sc.Value
}

// Equals checks if two codes are equivalent.
func (sc *SRCode) Equals(other *SRCode) bool {
	if other == nil {
		return false
	}
	selfValue := sc.Value
	selfScheme := sc.SchemeDesignator
	if selfScheme == "SRT" {
		selfScheme = "SCT"
	}

	otherValue := other.Value
	otherScheme := other.SchemeDesignator
	if otherScheme == "SRT" {
		otherScheme = "SCT"
	}

	return selfValue == otherValue && selfScheme == otherScheme
}

// ========== Advanced SR Template System ==========

// SRTemplate represents a structured report template.
type SRTemplate struct {
	TemplateID      string
	MappingResource string
	Name            string
	Description     string
	TemplateType    string
	RootConceptCode *SRCode
	ContentSequence []*ContentNode
	Constraints     map[string]*Constraint
	SRDocumentType  string
	CreatedTime     time.Time
	ModifiedTime    time.Time
	IsDeprecated    bool
	BaseTemplates   []string
	mu              sync.RWMutex
}

// ContentNode represents a node in the template structure.
type ContentNode struct {
	NodeID           string
	ConceptCode      *SRCode
	NodeType         string
	RelationshipType string
	CardinalityMin   int
	CardinalityMax   int
	Children         []*ContentNode
	ValueType        string
	PossibleCodes    []*SRCode
	ValidRange       *NumericRange
	Properties       map[string]interface{}
}

// NumericRange defines valid numeric value ranges.
type NumericRange struct {
	Min             float64
	Max             float64
	Unit            *SRCode
	MeasurementUnit string
	HasMin          bool
	HasMax          bool
}

// Constraint represents validation constraints.
type Constraint struct {
	Name           string
	Type           string
	Expression     string
	ErrorMessage   string
	Severity       string
	AppliesToNodes []string
}

// NewSRTemplate creates a new SR template.
func NewSRTemplate(templateID, name, mappingResource string) *SRTemplate {
	return &SRTemplate{
		TemplateID:      templateID,
		Name:            name,
		MappingResource: mappingResource,
		ContentSequence: make([]*ContentNode, 0),
		Constraints:     make(map[string]*Constraint),
		CreatedTime:     time.Now(),
		ModifiedTime:    time.Now(),
	}
}

// AddContentNode adds a node to the template structure.
func (st *SRTemplate) AddContentNode(node *ContentNode) {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.ContentSequence = append(st.ContentSequence, node)
	st.ModifiedTime = time.Now()
}

// AddConstraint adds a validation constraint.
func (st *SRTemplate) AddConstraint(constraint *Constraint) error {
	if constraint == nil {
		return fmt.Errorf("constraint cannot be nil")
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	st.Constraints[constraint.Name] = constraint
	st.ModifiedTime = time.Now()
	return nil
}

// GetContentNode retrieves a node by ID.
func (st *SRTemplate) GetContentNode(nodeID string) *ContentNode {
	st.mu.RLock()
	defer st.mu.RUnlock()

	for _, node := range st.ContentSequence {
		if found := findNodeByID(node, nodeID); found != nil {
			return found
		}
	}
	return nil
}

// findNodeByID recursively searches for a node.
func findNodeByID(node *ContentNode, nodeID string) *ContentNode {
	if node.NodeID == nodeID {
		return node
	}

	for _, child := range node.Children {
		if found := findNodeByID(child, nodeID); found != nil {
			return found
		}
	}

	return nil
}

// ========== SR Validator ==========

// SRValidator validates Structured Reports against templates.
type SRValidator struct {
	templates map[string]*SRTemplate
	codeCache map[string]*SRCode
	mu        sync.RWMutex
}

// ValidationResult contains validation results.
type ValidationResult struct {
	IsValid  bool
	Errors   []ValidationError
	Warnings []ValidationWarning
	Duration time.Duration
}

// ValidationError represents a validation error.
type ValidationError struct {
	Code     string
	NodeID   string
	Message  string
	Severity string
}

// ValidationWarning represents a validation warning.
type ValidationWarning struct {
	Code    string
	NodeID  string
	Message string
}

// NewSRValidator creates a new SR validator.
func NewSRValidator() *SRValidator {
	return &SRValidator{
		templates: make(map[string]*SRTemplate),
		codeCache: make(map[string]*SRCode),
	}
}

// RegisterTemplate registers a template for validation.
func (srv *SRValidator) RegisterTemplate(template *SRTemplate) error {
	if template == nil {
		return fmt.Errorf("template cannot be nil")
	}

	if template.TemplateID == "" {
		return fmt.Errorf("template ID cannot be empty")
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	srv.templates[template.TemplateID] = template
	return nil
}

// GetTemplate retrieves a registered template.
func (srv *SRValidator) GetTemplate(templateID string) (*SRTemplate, bool) {
	srv.mu.RLock()
	defer srv.mu.RUnlock()

	template, exists := srv.templates[templateID]
	return template, exists
}

// ListTemplates returns all registered template IDs.
func (srv *SRValidator) ListTemplates() []string {
	srv.mu.RLock()
	defer srv.mu.RUnlock()

	ids := make([]string, 0, len(srv.templates))
	for id := range srv.templates {
		ids = append(ids, id)
	}

	sort.Strings(ids)
	return ids
}

// RemoveTemplate unregisters a template.
func (srv *SRValidator) RemoveTemplate(templateID string) bool {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	if _, exists := srv.templates[templateID]; !exists {
		return false
	}

	delete(srv.templates, templateID)
	return true
}

// ValidateSRDocument validates a SR document against a template.
func (srv *SRValidator) ValidateSRDocument(doc *StructuredReport, templateID string) *ValidationResult {
	startTime := time.Now()

	result := &ValidationResult{
		IsValid:  true,
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	template, exists := srv.GetTemplate(templateID)
	if !exists {
		result.IsValid = false
		result.Errors = append(result.Errors, ValidationError{
			Code:     "TEMPLATE_NOT_FOUND",
			Message:  fmt.Sprintf("Template %s not registered", templateID),
			Severity: "ERROR",
		})
		result.Duration = time.Since(startTime)
		return result
	}

	if doc.SOPInstanceUID == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, ValidationError{
			Code:     "MISSING_UID",
			Message:  "SOPInstanceUID is required",
			Severity: "ERROR",
		})
	}

	if doc.SOPClassUID == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, ValidationError{
			Code:     "MISSING_CLASS",
			Message:  "SOPClassUID is required",
			Severity: "ERROR",
		})
	}

	for constraintName, constraint := range template.Constraints {
		if !validateConstraint(doc, constraint) {
			if constraint.Severity == "ERROR" {
				result.IsValid = false
				result.Errors = append(result.Errors, ValidationError{
					Code:     "CONSTRAINT_VIOLATED",
					NodeID:   constraintName,
					Message:  constraint.ErrorMessage,
					Severity: "ERROR",
				})
			} else {
				result.Warnings = append(result.Warnings, ValidationWarning{
					Code:    "CONSTRAINT_WARNING",
					NodeID:  constraintName,
					Message: constraint.ErrorMessage,
				})
			}
		}
	}

	result.Duration = time.Since(startTime)
	return result
}

// validateConstraint validates a single constraint.
func validateConstraint(doc *StructuredReport, constraint *Constraint) bool {
	switch constraint.Type {
	case "REQUIRED":
		return doc.SOPInstanceUID != "" && doc.SOPClassUID != ""
	case "CARDINALITY":
		if len(doc.ReportContent.Findings) == 0 && constraint.Severity == "ERROR" {
			return false
		}
	default:
		return true
	}
	return true
}

// ========== SR Builder ==========

// SRBuilder provides fluent API for building Structured Reports.
type SRBuilder struct {
	report    *StructuredReport
	template  *SRTemplate
	validator *SRValidator
	mu        sync.RWMutex
}

// NewSRBuilder creates a new SR builder.
func NewSRBuilder(sopInstanceUID, sopClassUID string) *SRBuilder {
	return &SRBuilder{
		report: NewStructuredReport(sopInstanceUID, sopClassUID),
	}
}

// WithTemplate associates a template with the builder.
func (sb *SRBuilder) WithTemplate(template *SRTemplate) *SRBuilder {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.template = template
	return sb
}

// WithValidator associates a validator with the builder.
func (sb *SRBuilder) WithValidator(validator *SRValidator) *SRBuilder {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.validator = validator
	return sb
}

// AddPatientInfo adds patient information.
func (sb *SRBuilder) AddPatientInfo(name, id string) *SRBuilder {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.report.PatientName = name
	sb.report.PatientID = id
	return sb
}

// AddStudyInfo adds study information.
func (sb *SRBuilder) AddStudyInfo(studyUID, seriesUID string) *SRBuilder {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.report.StudyInstanceUID = studyUID
	sb.report.SeriesInstanceUID = seriesUID
	return sb
}

// AddFinding adds a finding to the report.
func (sb *SRBuilder) AddFinding(id, description, severity, location string, confidence float64) *SRBuilder {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	finding := Finding{
		ID:          id,
		Description: description,
		Severity:    severity,
		Location:    location,
		Confidence:  confidence,
	}

	sb.report.ReportContent.Findings = append(sb.report.ReportContent.Findings, finding)
	return sb
}

// AddObservation adds an observation to the report.
func (sb *SRBuilder) AddObservation(code, value, unit string) *SRBuilder {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	observation := Observation{
		Code:  code,
		Value: value,
		Unit:  unit,
		Time:  time.Now(),
	}

	sb.report.ReportContent.Observations = append(sb.report.ReportContent.Observations, observation)
	return sb
}

// AddConclusion adds a conclusion to the report.
func (sb *SRBuilder) AddConclusion(conclusion string) *SRBuilder {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.report.ReportContent.Conclusions = append(sb.report.ReportContent.Conclusions, conclusion)
	return sb
}

// Build returns the constructed SR and validates if validator is present.
func (sb *SRBuilder) Build() (*StructuredReport, *ValidationResult, error) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	if sb.report.SOPInstanceUID == "" {
		return nil, nil, fmt.Errorf("SOPInstanceUID is required")
	}

	if sb.report.SOPClassUID == "" {
		return nil, nil, fmt.Errorf("SOPClassUID is required")
	}

	var validationResult *ValidationResult
	if sb.validator != nil && sb.template != nil {
		validationResult = sb.validator.ValidateSRDocument(sb.report, sb.template.TemplateID)
		if !validationResult.IsValid {
			return sb.report, validationResult, fmt.Errorf("validation failed")
		}
	}

	return sb.report, validationResult, nil
}

// GetReport returns the constructed report from the builder.
func (sb *SRBuilder) GetReport() *StructuredReport {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.report
}

// GetTemplate returns the template associated with the builder.
func (sb *SRBuilder) GetTemplate() *SRTemplate {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.template
}

// GetValidator returns the validator associated with the builder.
func (sb *SRBuilder) GetValidator() *SRValidator {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.validator
}

// ========== Content Builder ==========

// ContentBuilder provides fluent API for building template content.
type ContentBuilder struct {
	template *SRTemplate
	mu       sync.RWMutex
}

// NewContentBuilder creates a new content builder.
func NewContentBuilder(template *SRTemplate) *ContentBuilder {
	return &ContentBuilder{
		template: template,
	}
}

// AddContainer adds a container node.
func (cb *ContentBuilder) AddContainer(nodeID string, conceptCode *SRCode, relationshipType string) *ContentNode {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	node := &ContentNode{
		NodeID:           nodeID,
		ConceptCode:      conceptCode,
		NodeType:         "CONTAINER",
		RelationshipType: relationshipType,
		Children:         make([]*ContentNode, 0),
		Properties:       make(map[string]interface{}),
	}

	cb.template.AddContentNode(node)
	return node
}

// AddValueNode adds a value node.
func (cb *ContentBuilder) AddValueNode(nodeID string, conceptCode *SRCode, valueType string) *ContentNode {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	node := &ContentNode{
		NodeID:        nodeID,
		ConceptCode:   conceptCode,
		NodeType:      "VALUE",
		ValueType:     valueType,
		Children:      make([]*ContentNode, 0),
		Properties:    make(map[string]interface{}),
		PossibleCodes: make([]*SRCode, 0),
	}

	cb.template.AddContentNode(node)
	return node
}

// AddConceptModifier adds a concept modifier.
func (cb *ContentBuilder) AddConceptModifier(parentNode *ContentNode, conceptCode *SRCode) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	modifier := &ContentNode{
		NodeID:           conceptCode.Value,
		ConceptCode:      conceptCode,
		NodeType:         "VALUE",
		RelationshipType: "HAS CONCEPT MOD",
		Children:         make([]*ContentNode, 0),
	}

	parentNode.Children = append(parentNode.Children, modifier)
}

// ========== Context ID Registry ==========

// ContextIDRegistry stores Context ID definitions.
type ContextIDRegistry struct {
	cids map[int]map[string]*SRCode
	mu   sync.RWMutex
}

// NewContextIDRegistry creates a new CID registry.
func NewContextIDRegistry() *ContextIDRegistry {
	return &ContextIDRegistry{
		cids: make(map[int]map[string]*SRCode),
	}
}

// RegisterCID registers codes for a Context ID.
func (cir *ContextIDRegistry) RegisterCID(cidNumber int, codes map[string]*SRCode) error {
	if cidNumber <= 0 {
		return fmt.Errorf("CID number must be positive")
	}

	cir.mu.Lock()
	defer cir.mu.Unlock()

	cir.cids[cidNumber] = codes
	return nil
}

// GetCIDCodes retrieves all codes for a Context ID.
func (cir *ContextIDRegistry) GetCIDCodes(cidNumber int) map[string]*SRCode {
	cir.mu.RLock()
	defer cir.mu.RUnlock()

	codes, exists := cir.cids[cidNumber]
	if !exists {
		return make(map[string]*SRCode)
	}

	codeCopy := make(map[string]*SRCode)
	for k, v := range codes {
		codeCopy[k] = v
	}

	return codeCopy
}

// GetCIDCode retrieves a specific code from a Context ID.
func (cir *ContextIDRegistry) GetCIDCode(cidNumber int, codeValue string) (*SRCode, bool) {
	cir.mu.RLock()
	defer cir.mu.RUnlock()

	codes, exists := cir.cids[cidNumber]
	if !exists {
		return nil, false
	}

	code, found := codes[codeValue]
	return code, found
}

// ListCIDs returns all registered CID numbers.
func (cir *ContextIDRegistry) ListCIDs() []int {
	cir.mu.RLock()
	defer cir.mu.RUnlock()

	cidNumbers := make([]int, 0, len(cir.cids))
	for cid := range cir.cids {
		cidNumbers = append(cidNumbers, cid)
	}

	sort.Ints(cidNumbers)
	return cidNumbers
}

// ========== Code Collection ==========

// CodeCollection represents a collection of codes.
type CodeCollection struct {
	name  string
	codes map[string]*SRCode
	mu    sync.RWMutex
}

// NewCodeCollection creates a new code collection.
func NewCodeCollection(name string) *CodeCollection {
	return &CodeCollection{
		name:  name,
		codes: make(map[string]*SRCode),
	}
}

// AddCode adds a code to the collection.
func (cc *CodeCollection) AddCode(keyword string, code *SRCode) error {
	if keyword == "" {
		return fmt.Errorf("keyword cannot be empty")
	}

	if code == nil {
		return fmt.Errorf("code cannot be nil")
	}

	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.codes[keyword] = code
	return nil
}

// GetCode retrieves a code by keyword.
func (cc *CodeCollection) GetCode(keyword string) (*SRCode, bool) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	code, exists := cc.codes[keyword]
	return code, exists
}

// Contains checks if a code is in the collection.
func (cc *CodeCollection) Contains(code *SRCode) bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	if code == nil {
		return false
	}

	for _, c := range cc.codes {
		if c.Equals(code) {
			return true
		}
	}

	return false
}

// List returns all keywords in the collection.
func (cc *CodeCollection) List(filters ...string) []string {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	keywords := make([]string, 0, len(cc.codes))
	for keyword := range cc.codes {
		keywords = append(keywords, keyword)
	}

	if len(filters) > 0 {
		filtered := make([]string, 0)
		for _, kw := range keywords {
			for _, filter := range filters {
				if containsKeyword(kw, filter) {
					filtered = append(filtered, kw)
					break
				}
			}
		}
		keywords = filtered
	}

	sort.Strings(keywords)
	return keywords
}

// Size returns the number of codes in the collection.
func (cc *CodeCollection) Size() int {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	return len(cc.codes)
}

// ========== Helper Functions ==========

// containsKeyword helper function for case-insensitive substring match.
func containsKeyword(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	sLower := toLower(s)
	substrLower := toLower(substr)
	return len(sLower) >= len(substrLower) && isSubstring(sLower, substrLower)
}

// toLower returns lowercase version of string.
func toLower(s string) string {
	result := make([]rune, len([]rune(s)))
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			result[i] = r + 32
		} else {
			result[i] = r
		}
	}
	return string(result)
}

// isSubstring checks if substr is in s.
func isSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ========== Built-in Code Collections ==========

// CreateAnatomyCodeCollection creates a collection of anatomy codes.
func CreateAnatomyCodeCollection() *CodeCollection {
	collection := NewCodeCollection("Anatomy")

	anatomyCodes := map[string]*SRCode{
		"Abdomen":  NewSRCode("89545001", CodeSchemes.SNOMED, "Abdomen"),
		"Appendix": NewSRCode("66754008", CodeSchemes.SNOMED, "Appendix"),
		"Brain":    NewSRCode("12738006", CodeSchemes.SNOMED, "Brain"),
		"Chest":    NewSRCode("51185008", CodeSchemes.SNOMED, "Chest"),
		"Colon":    NewSRCode("71854001", CodeSchemes.SNOMED, "Colon"),
		"Heart":    NewSRCode("80891009", CodeSchemes.SNOMED, "Heart"),
		"Kidney":   NewSRCode("64033007", CodeSchemes.SNOMED, "Kidney"),
		"Liver":    NewSRCode("10200004", CodeSchemes.SNOMED, "Liver"),
		"Lung":     NewSRCode("39607008", CodeSchemes.SNOMED, "Lung"),
		"Pancreas": NewSRCode("15776009", CodeSchemes.SNOMED, "Pancreas"),
		"Pelvis":   NewSRCode("25117009", CodeSchemes.SNOMED, "Pelvis"),
		"Prostate": NewSRCode("41216001", CodeSchemes.SNOMED, "Prostate"),
		"Spine":    NewSRCode("8923008", CodeSchemes.SNOMED, "Spine"),
		"Stomach":  NewSRCode("69695003", CodeSchemes.SNOMED, "Stomach"),
		"Thyroid":  NewSRCode("69748006", CodeSchemes.SNOMED, "Thyroid"),
	}

	for keyword, code := range anatomyCodes {
		_ = collection.AddCode(keyword, code)
	}

	return collection
}

// CreateFindingTypeCodeCollection creates a collection of finding type codes.
func CreateFindingTypeCodeCollection() *CodeCollection {
	collection := NewCodeCollection("FindingTypes")

	findingCodes := map[string]*SRCode{
		"Abnormal":    NewSRCode("386585008", CodeSchemes.SNOMED, "Abnormal"),
		"Normal":      NewSRCode("17621005", CodeSchemes.SNOMED, "Normal"),
		"Uncertain":   NewSRCode("unknown", CodeSchemes.DCM, "Uncertain"),
		"Significant": NewSRCode("386587000", CodeSchemes.SNOMED, "Significant"),
	}

	for keyword, code := range findingCodes {
		_ = collection.AddCode(keyword, code)
	}

	return collection
}

// CreateModifierCodeCollection creates a collection of modifier codes.
func CreateModifierCodeCollection() *CodeCollection {
	collection := NewCodeCollection("Modifiers")

	modifierCodes := map[string]*SRCode{
		"Bilateral": NewSRCode("51440002", CodeSchemes.SNOMED, "Bilateral"),
		"Left":      NewSRCode("7771000", CodeSchemes.SNOMED, "Left"),
		"Right":     NewSRCode("24028007", CodeSchemes.SNOMED, "Right"),
		"Acute":     NewSRCode("373610005", CodeSchemes.SNOMED, "Acute"),
		"Chronic":   NewSRCode("90734009", CodeSchemes.SNOMED, "Chronic"),
		"Mild":      NewSRCode("255604002", CodeSchemes.SNOMED, "Mild"),
		"Moderate":  NewSRCode("6736007", CodeSchemes.SNOMED, "Moderate"),
		"Severe":    NewSRCode("24484000", CodeSchemes.SNOMED, "Severe"),
	}

	for keyword, code := range modifierCodes {
		_ = collection.AddCode(keyword, code)
	}

	return collection
}
