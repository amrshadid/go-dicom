package sr

import (
	"fmt"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// ToDataset converts a StructuredReport to a DICOM Dataset.
func (sr *StructuredReport) ToDataset() (*dataset.Dataset, error) {
	if sr == nil {
		return nil, fmt.Errorf("structured report is nil")
	}

	ds := dataset.NewDataset()

	// Add file meta information tags
	if err := addFileMetaElements(ds); err != nil {
		return nil, fmt.Errorf("failed to add file meta elements: %w", err)
	}

	// Add patient module elements
	if err := addPatientElements(ds, sr); err != nil {
		return nil, fmt.Errorf("failed to add patient elements: %w", err)
	}

	// Add study module elements
	if err := addStudyElements(ds, sr); err != nil {
		return nil, fmt.Errorf("failed to add study elements: %w", err)
	}

	// Add series module elements
	if err := addSeriesElements(ds, sr); err != nil {
		return nil, fmt.Errorf("failed to add series elements: %w", err)
	}

	// Add SOP common module elements
	if err := addSOPCommonElements(ds, sr); err != nil {
		return nil, fmt.Errorf("failed to add SOP common elements: %w", err)
	}

	// Add SR document content elements
	if err := addSRDocumentElements(ds, sr); err != nil {
		return nil, fmt.Errorf("failed to add SR document elements: %w", err)
	}

	return ds, nil
}

// addFileMetaElements adds file meta information.
func addFileMetaElements(ds *dataset.Dataset) error {
	groupLengthElem := dataelem.NewDataElement(
		tag.New(0x0002, 0x0000),
		dataelem.UL,
		[]interface{}{0},
	)
	if err := ds.Add(groupLengthElem); err != nil {
		return err
	}
	return nil
}

// addPatientElements adds patient module elements.
func addPatientElements(ds *dataset.Dataset, sr *StructuredReport) error {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// Patient's Name (0010,0010)
	if sr.PatientName != "" {
		nameElem := dataelem.NewDataElement(
			tag.New(0x0010, 0x0010),
			dataelem.PN,
			[]interface{}{sr.PatientName},
		)
		if err := ds.Add(nameElem); err != nil {
			return err
		}
	}

	// Patient ID (0010,0020)
	if sr.PatientID != "" {
		idElem := dataelem.NewDataElement(
			tag.New(0x0010, 0x0020),
			dataelem.LO,
			[]interface{}{sr.PatientID},
		)
		if err := ds.Add(idElem); err != nil {
			return err
		}
	}

	return nil
}

// addStudyElements adds study module elements.
func addStudyElements(ds *dataset.Dataset, sr *StructuredReport) error {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// Study Instance UID (0020,000D)
	if sr.StudyInstanceUID != "" {
		studyUID := dataelem.NewDataElement(
			tag.New(0x0020, 0x000D),
			dataelem.UI,
			[]interface{}{sr.StudyInstanceUID},
		)
		if err := ds.Add(studyUID); err != nil {
			return err
		}
	}

	// Study Date (0008,0020) - Use creation time
	if !sr.CreationTime.IsZero() {
		studyDate := sr.CreationTime.Format("20060102")
		dateElem := dataelem.NewDataElement(
			tag.New(0x0008, 0x0020),
			dataelem.DA,
			[]interface{}{studyDate},
		)
		if err := ds.Add(dateElem); err != nil {
			return err
		}

		// Study Time (0008,0030)
		studyTime := sr.CreationTime.Format("150405.000000")
		timeElem := dataelem.NewDataElement(
			tag.New(0x0008, 0x0030),
			dataelem.TM,
			[]interface{}{studyTime},
		)
		if err := ds.Add(timeElem); err != nil {
			return err
		}
	}

	return nil
}

// addSeriesElements adds series module elements.
func addSeriesElements(ds *dataset.Dataset, sr *StructuredReport) error {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// Series Instance UID (0020,000E)
	if sr.SeriesInstanceUID != "" {
		seriesUID := dataelem.NewDataElement(
			tag.New(0x0020, 0x000E),
			dataelem.UI,
			[]interface{}{sr.SeriesInstanceUID},
		)
		if err := ds.Add(seriesUID); err != nil {
			return err
		}
	}

	// Series Number (0020,0011)
	seriesNumElem := dataelem.NewDataElement(
		tag.New(0x0020, 0x0011),
		dataelem.IS,
		[]interface{}{"1"},
	)
	if err := ds.Add(seriesNumElem); err != nil {
		return err
	}

	return nil
}

// addSOPCommonElements adds SOP common module elements.
func addSOPCommonElements(ds *dataset.Dataset, sr *StructuredReport) error {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// SOP Class UID (0008,0016)
	if sr.SOPClassUID != "" {
		sopClassElem := dataelem.NewDataElement(
			tag.New(0x0008, 0x0016),
			dataelem.UI,
			[]interface{}{sr.SOPClassUID},
		)
		if err := ds.Add(sopClassElem); err != nil {
			return err
		}
	}

	// SOP Instance UID (0008,0018)
	if sr.SOPInstanceUID != "" {
		sopInstanceElem := dataelem.NewDataElement(
			tag.New(0x0008, 0x0018),
			dataelem.UI,
			[]interface{}{sr.SOPInstanceUID},
		)
		if err := ds.Add(sopInstanceElem); err != nil {
			return err
		}
	}

	// Instance Creation Date (0008,0012)
	if !sr.CreationTime.IsZero() {
		creationDate := sr.CreationTime.Format("20060102")
		dateElem := dataelem.NewDataElement(
			tag.New(0x0008, 0x0012),
			dataelem.DA,
			[]interface{}{creationDate},
		)
		if err := ds.Add(dateElem); err != nil {
			return err
		}

		// Instance Creation Time (0008,0013)
		creationTime := sr.CreationTime.Format("150405.000000")
		timeElem := dataelem.NewDataElement(
			tag.New(0x0008, 0x0013),
			dataelem.TM,
			[]interface{}{creationTime},
		)
		if err := ds.Add(timeElem); err != nil {
			return err
		}
	}

	return nil
}

// addSRDocumentElements adds SR-specific document elements.
func addSRDocumentElements(ds *dataset.Dataset, sr *StructuredReport) error {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	if sr.ReportContent == nil {
		return nil
	}

	// Add findings count (0040,A043)
	findingCount := len(sr.ReportContent.Findings)
	if findingCount > 0 {
		findingsElem := dataelem.NewDataElement(
			tag.New(0x0040, 0xA043),
			dataelem.IS,
			[]interface{}{fmt.Sprintf("%d", findingCount)},
		)
		if err := ds.Add(findingsElem); err != nil {
			return err
		}

		// Add first finding details
		if len(sr.ReportContent.Findings) > 0 {
			finding := sr.ReportContent.Findings[0]
			if finding.Description != "" {
				descElem := dataelem.NewDataElement(
					tag.New(0x0008, 0x1030),
					dataelem.LO,
					[]interface{}{finding.Description},
				)
				if err := ds.Add(descElem); err != nil {
					return err
				}
			}
		}
	}

	// Add conclusions
	if len(sr.ReportContent.Conclusions) > 0 {
		conclusion := sr.ReportContent.Conclusions[0]
		if conclusion != "" {
			concElem := dataelem.NewDataElement(
				tag.New(0x0040, 0xA160),
				dataelem.UT,
				[]interface{}{conclusion},
			)
			if err := ds.Add(concElem); err != nil {
				return err
			}
		}
	}

	return nil
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

	// Extract findings description if present
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
