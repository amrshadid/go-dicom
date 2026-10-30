package sr

import (
	"fmt"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// ToDataset converts a StructuredReport to an SR document.
//
// The data set carries the modules every SR IOD requires (PS3.3 A.35):
// Patient, General Study, SR Document Series, General Equipment, SR Document
// General, SOP Common and SR Document Content, with the report itself as the
// content tree. Type 2 attributes the report has no value for are written
// empty, as the standard says to. It used to hold none of these but the
// UIDs, a (0002,0000) element that belongs to the file meta alone, and the
// count of findings where a code belongs, so dcmtk refused it (#216).
func (sr *StructuredReport) ToDataset() (*dataset.Dataset, error) {
	if sr == nil {
		return nil, fmt.Errorf("structured report is nil")
	}
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	created := sr.CreationTime
	if created.IsZero() {
		created = time.Now()
	}
	date, clock := created.Format("20060102"), created.Format("150405")

	ds := dataset.NewDataset()
	if !isASCII(sr.PatientName) || !reportIsASCII(sr.ReportContent) {
		addString(ds, tag.New(0x0008, 0x0005), dataelem.CS, "ISO_IR 192")
	}

	// SOP Common
	addString(ds, tag.New(0x0008, 0x0016), dataelem.UI, sr.SOPClassUID)
	addString(ds, tag.New(0x0008, 0x0018), dataelem.UI, sr.SOPInstanceUID)
	addString(ds, tag.New(0x0008, 0x0012), dataelem.DA, date)
	addString(ds, tag.New(0x0008, 0x0013), dataelem.TM, clock)

	// General Study
	addRequired(ds, tag.New(0x0020, 0x000D), dataelem.UI, sr.StudyInstanceUID)
	addString(ds, tag.New(0x0008, 0x0020), dataelem.DA, date)
	addString(ds, tag.New(0x0008, 0x0030), dataelem.TM, clock)
	addEmpty(ds, tag.New(0x0008, 0x0050), dataelem.SH) // Accession Number
	addEmpty(ds, tag.New(0x0008, 0x0090), dataelem.PN) // Referring Physician's Name
	addEmpty(ds, tag.New(0x0020, 0x0010), dataelem.SH) // Study ID

	// Patient
	addRequired(ds, tag.New(0x0010, 0x0010), dataelem.PN, sr.PatientName)
	addRequired(ds, tag.New(0x0010, 0x0020), dataelem.LO, sr.PatientID)
	addEmpty(ds, tag.New(0x0010, 0x0030), dataelem.DA) // Patient's Birth Date
	addEmpty(ds, tag.New(0x0010, 0x0040), dataelem.CS) // Patient's Sex

	// SR Document Series
	addString(ds, tag.New(0x0008, 0x0060), dataelem.CS, "SR")
	addRequired(ds, tag.New(0x0020, 0x000E), dataelem.UI, sr.SeriesInstanceUID)
	addString(ds, tag.New(0x0020, 0x0011), dataelem.IS, "1")
	addEmpty(ds, tag.New(0x0008, 0x1111), dataelem.SQ) // Referenced Performed Procedure Step Sequence

	// General Equipment
	addEmpty(ds, tag.New(0x0008, 0x0070), dataelem.LO) // Manufacturer

	// SR Document General
	addString(ds, tag.New(0x0020, 0x0013), dataelem.IS, "1")
	addString(ds, tag.New(0x0040, 0xA491), dataelem.CS, "COMPLETE")
	addString(ds, tag.New(0x0040, 0xA493), dataelem.CS, "UNVERIFIED")
	addString(ds, tag.New(0x0008, 0x0023), dataelem.DA, date)
	addString(ds, tag.New(0x0008, 0x0033), dataelem.TM, clock)
	addEmpty(ds, tag.New(0x0040, 0xA372), dataelem.SQ) // Performed Procedure Code Sequence

	// SR Document Content
	if err := WriteContentTree(ds, sr.contentTree()); err != nil {
		return nil, fmt.Errorf("writing the content tree: %w", err)
	}
	return ds, nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func reportIsASCII(content *ReportContent) bool {
	if content == nil {
		return true
	}
	var text []string
	for _, f := range content.Findings {
		text = append(text, f.ID, f.Description, f.Severity, f.Location)
	}
	for _, o := range content.Observations {
		text = append(text, o.Code, o.Value, o.Unit)
	}
	text = append(text, content.Conclusions...)
	for _, t := range text {
		if !isASCII(t) {
			return false
		}
	}
	return true
}

// FromDataset converts a DICOM Dataset to a StructuredReport.
func FromDataset(ds *dataset.Dataset) (*StructuredReport, error) {
	if ds == nil {
		return nil, fmt.Errorf("dataset is nil")
	}

	report := &StructuredReport{
		CreationTime: time.Now(),
		ReportContent: &ReportContent{
			Findings:     make([]Finding, 0),
			Observations: make([]Observation, 0),
			Conclusions:  make([]string, 0),
			References:   make([]ReportReference, 0),
		},
	}

	// Extract SOP Instance UID (0008,0018)
	if elem, ok := ds.Get(tag.New(0x0008, 0x0018)); ok {
		if value, err := extractStringValue(elem); err == nil {
			report.SOPInstanceUID = value
		}
	}

	// Extract SOP Class UID (0008,0016)
	if elem, ok := ds.Get(tag.New(0x0008, 0x0016)); ok {
		if value, err := extractStringValue(elem); err == nil {
			report.SOPClassUID = value
		}
	}

	// Extract Patient's Name (0010,0010)
	if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
		if value, err := extractStringValue(elem); err == nil {
			report.PatientName = value
		}
	}

	// Extract Patient ID (0010,0020)
	if elem, ok := ds.Get(tag.New(0x0010, 0x0020)); ok {
		if value, err := extractStringValue(elem); err == nil {
			report.PatientID = value
		}
	}

	// Extract Study Instance UID (0020,000D)
	if elem, ok := ds.Get(tag.New(0x0020, 0x000D)); ok {
		if value, err := extractStringValue(elem); err == nil {
			report.StudyInstanceUID = value
		}
	}

	// Extract Series Instance UID (0020,000E)
	if elem, ok := ds.Get(tag.New(0x0020, 0x000E)); ok {
		if value, err := extractStringValue(elem); err == nil {
			report.SeriesInstanceUID = value
		}
	}

	// Extract Instance Creation Time (0008,0012 and 0008,0013)
	creationTime := extractCreationTime(ds)
	if !creationTime.IsZero() {
		report.CreationTime = creationTime
	}

	// An SR document says what it says in its content tree, whether this
	// package wrote it or something else did (#216).
	if root, err := ReadContentTree(ds); err == nil {
		report.ReportContent = readReportContent(root)
		return report, nil
	}

	// A data set with no tree: what the package's earlier ToDataset wrote.
	if elem, ok := ds.Get(tag.New(0x0008, 0x1030)); ok {
		if value, err := extractStringValue(elem); err == nil && value != "" {
			report.ReportContent.Findings = append(report.ReportContent.Findings, Finding{
				ID:          "1",
				Description: value,
				Severity:    "MAJOR",
				Confidence:  0.95,
			})
		}
	}

	// Extract conclusions if present
	if elem, ok := ds.Get(tag.New(0x0040, 0xA160)); ok {
		if value, err := extractStringValue(elem); err == nil && value != "" {
			report.ReportContent.Conclusions = append(report.ReportContent.Conclusions, value)
		}
	}

	return report, nil
}

// extractCreationTime extracts the creation time from dataset study date/time fields.
func extractCreationTime(ds *dataset.Dataset) time.Time {
	var dateStr, timeStr string

	// Try Instance Creation Date (0008,0012)
	if elem, ok := ds.Get(tag.New(0x0008, 0x0012)); ok {
		if value, err := extractStringValue(elem); err == nil {
			dateStr = value
		}
	}

	// Try Instance Creation Time (0008,0013)
	if elem, ok := ds.Get(tag.New(0x0008, 0x0013)); ok {
		if value, err := extractStringValue(elem); err == nil {
			timeStr = value
		}
	}

	// Fall back to Study Date (0008,0020) if not found
	if dateStr == "" {
		if elem, ok := ds.Get(tag.New(0x0008, 0x0020)); ok {
			if value, err := extractStringValue(elem); err == nil {
				dateStr = value
			}
		}
	}

	// Try to parse date and time
	if dateStr != "" {
		if timeStr != "" {
			// Combine date and time
			datetime := dateStr + timeStr
			if t, err := time.Parse("20060102150405", datetime[:14]); err == nil {
				return t
			}
		} else {
			// Just date
			if t, err := time.Parse("20060102", dateStr); err == nil {
				return t
			}
		}
	}

	return time.Time{} // Return zero time if not found
}

// extractStringValue extracts a string value from a DataElement.
func extractStringValue(elem *dataelem.DataElement) (string, error) {
	if elem == nil {
		return "", fmt.Errorf("element is nil")
	}

	// Get the value from the element
	values := elem.GetValue()
	if values == nil {
		return "", fmt.Errorf("element value is nil")
	}

	// Handle different value types
	switch v := values.(type) {
	case []byte:
		// What a data set read from a file holds. Without this case nothing
		// was extracted from one, and ReadSRFile returned an empty report.
		return strings.TrimRight(string(v), " \x00"), nil
	case string:
		return v, nil
	case []interface{}:
		if len(v) > 0 {
			if str, ok := v[0].(string); ok {
				return str, nil
			}
		}
	case []string:
		if len(v) > 0 {
			return v[0], nil
		}
	}

	return "", fmt.Errorf("unable to extract string value from element")
}
