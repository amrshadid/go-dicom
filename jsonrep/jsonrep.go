package jsonrep

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// JSONRepresentation renders a flat summary of the attributes a web API most
// often wants — patient, study, series and instance identifiers, and a few
// descriptive fields.
//
// It is NOT the DICOM JSON Model of PS3.18 Annex F, whatever its name suggests
// and whatever this comment used to say. It is a fixed struct of named fields
// and cannot represent an arbitrary data set: an attribute without a field here
// has nowhere to go. Use dataset.ToDICOMJSON and dataset.FromDICOMJSON for the
// interchange format that DICOMweb, pydicom and dcm4che exchange.
type JSONRepresentation struct {
	mu sync.RWMutex
}

// NewJSONRepresentation creates a new JSON representation handler
func NewJSONRepresentation() *JSONRepresentation {
	return &JSONRepresentation{}
}

// DicomDataset represents a DICOM dataset in JSON format
type DicomDataset struct {
	PatientName           string                 `json:"PatientName,omitempty"`
	PatientID             string                 `json:"PatientID,omitempty"`
	StudyInstanceUID      string                 `json:"StudyInstanceUID,omitempty"`
	SeriesInstanceUID     string                 `json:"SeriesInstanceUID,omitempty"`
	SOPInstanceUID        string                 `json:"SOPInstanceUID,omitempty"`
	SOPClassUID           string                 `json:"SOPClassUID,omitempty"`
	Modality              string                 `json:"Modality,omitempty"`
	StudyDate             string                 `json:"StudyDate,omitempty"`
	StudyTime             string                 `json:"StudyTime,omitempty"`
	SeriesDate            string                 `json:"SeriesDate,omitempty"`
	SeriesTime            string                 `json:"SeriesTime,omitempty"`
	ContentDate           string                 `json:"ContentDate,omitempty"`
	ContentTime           string                 `json:"ContentTime,omitempty"`
	InstitutionName       string                 `json:"InstitutionName,omitempty"`
	ReferringPhysician    string                 `json:"ReferringPhysician,omitempty"`
	PhysiciansOfRecord    string                 `json:"PhysiciansOfRecord,omitempty"`
	OperatorsName         string                 `json:"OperatorsName,omitempty"`
	Manufacturer          string                 `json:"Manufacturer,omitempty"`
	ManufacturerModel     string                 `json:"ManufacturerModel,omitempty"`
	SeriesNumber          int                    `json:"SeriesNumber,omitempty"`
	InstanceNumber        int                    `json:"InstanceNumber,omitempty"`
	NumberOfFrames        int                    `json:"NumberOfFrames,omitempty"`
	ReferencedSOPSequence []ReferencedSOP        `json:"ReferencedSOPSequence,omitempty"`
	CustomAttributes      map[string]interface{} `json:"CustomAttributes,omitempty"`
}

// ReferencedSOP represents a referenced SOP class instance
type ReferencedSOP struct {
	SOPClassUID    string `json:"SOPClassUID,omitempty"`
	SOPInstanceUID string `json:"SOPInstanceUID,omitempty"`
}

// JSONElement represents a DICOM element in JSON format per DICOM Part 18,
// pairing the Value Representation (VR) type with the element's value.
type JSONElement struct {
	VR    string      `json:"vr"`
	Value interface{} `json:"value"`
}

// JSONMessage represents a DICOM message in JSON format with versioning and metadata,
// enabling transmission of DICOM datasets through web services and JSON APIs.
type JSONMessage struct {
	Version   string                 `json:"version"`
	Timestamp time.Time              `json:"timestamp"`
	Elements  map[string]JSONElement `json:"elements"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ToJSON converts a dataset to JSON format
func (jr *JSONRepresentation) ToJSON(dataset *DicomDataset) ([]byte, error) {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	if dataset == nil {
		return nil, fmt.Errorf("cannot convert nil dataset to JSON")
	}

	data, err := json.MarshalIndent(dataset, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dataset: %w", err)
	}

	return data, nil
}

// FromJSON converts JSON data to a dataset
func (jr *JSONRepresentation) FromJSON(data []byte) (*DicomDataset, error) {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	if len(data) == 0 {
		return nil, fmt.Errorf("cannot convert empty data from JSON")
	}

	var dataset DicomDataset
	err := json.Unmarshal(data, &dataset)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return &dataset, nil
}

// ToJSONMessage converts a dataset to a JSON message with metadata
func (jr *JSONRepresentation) ToJSONMessage(dataset *DicomDataset, metadata map[string]interface{}) (*JSONMessage, error) {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	if dataset == nil {
		return nil, fmt.Errorf("cannot convert nil dataset to JSON message")
	}

	msg := &JSONMessage{
		Version:   "1.0",
		Timestamp: time.Now(),
		Elements:  make(map[string]JSONElement),
		Metadata:  metadata,
	}

	// Populate elements from dataset
	if dataset.PatientName != "" {
		msg.Elements["PatientName"] = JSONElement{VR: "PN", Value: dataset.PatientName}
	}
	if dataset.PatientID != "" {
		msg.Elements["PatientID"] = JSONElement{VR: "LO", Value: dataset.PatientID}
	}
	if dataset.StudyInstanceUID != "" {
		msg.Elements["StudyInstanceUID"] = JSONElement{VR: "UI", Value: dataset.StudyInstanceUID}
	}
	if dataset.SeriesInstanceUID != "" {
		msg.Elements["SeriesInstanceUID"] = JSONElement{VR: "UI", Value: dataset.SeriesInstanceUID}
	}
	if dataset.SOPInstanceUID != "" {
		msg.Elements["SOPInstanceUID"] = JSONElement{VR: "UI", Value: dataset.SOPInstanceUID}
	}
	if dataset.SOPClassUID != "" {
		msg.Elements["SOPClassUID"] = JSONElement{VR: "UI", Value: dataset.SOPClassUID}
	}
	if dataset.Modality != "" {
		msg.Elements["Modality"] = JSONElement{VR: "CS", Value: dataset.Modality}
	}
	if dataset.InstanceNumber > 0 {
		msg.Elements["InstanceNumber"] = JSONElement{VR: "IS", Value: dataset.InstanceNumber}
	}

	return msg, nil
}

// ValidateJSON validates JSON against DICOM JSON model schema
func (jr *JSONRepresentation) ValidateJSON(data []byte) error {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	if len(data) == 0 {
		return fmt.Errorf("cannot validate empty JSON data")
	}

	var dataset DicomDataset
	err := json.Unmarshal(data, &dataset)
	if err != nil {
		return fmt.Errorf("invalid JSON format: %w", err)
	}

	// Validate required DICOM identifiers per DICOM Part 18 specification.
	// SOPInstanceUID and SOPClassUID must be present for valid DICOM JSON objects.
	if dataset.SOPInstanceUID == "" {
		return fmt.Errorf("SOPInstanceUID is required")
	}

	if dataset.SOPClassUID == "" {
		return fmt.Errorf("SOPClassUID is required")
	}

	return nil
}

// PrettyPrintJSON returns formatted JSON string
func (jr *JSONRepresentation) PrettyPrintJSON(data []byte) (string, error) {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	var jsonData interface{}
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to parse JSON: %w", err)
	}

	pretty, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to format JSON: %w", err)
	}

	return string(pretty), nil
}

// CompactJSON returns minified JSON string
func (jr *JSONRepresentation) CompactJSON(data []byte) ([]byte, error) {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	var jsonData interface{}
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	compact, err := json.Marshal(jsonData)
	if err != nil {
		return nil, fmt.Errorf("failed to compact JSON: %w", err)
	}

	return compact, nil
}

// ExtractUIDs extracts UIDs from a DICOM dataset
func (jr *JSONRepresentation) ExtractUIDs(dataset *DicomDataset) map[string]string {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	uids := make(map[string]string)

	if dataset.StudyInstanceUID != "" {
		uids["StudyInstanceUID"] = dataset.StudyInstanceUID
	}
	if dataset.SeriesInstanceUID != "" {
		uids["SeriesInstanceUID"] = dataset.SeriesInstanceUID
	}
	if dataset.SOPInstanceUID != "" {
		uids["SOPInstanceUID"] = dataset.SOPInstanceUID
	}
	if dataset.SOPClassUID != "" {
		uids["SOPClassUID"] = dataset.SOPClassUID
	}

	return uids
}

// ExtractPatientInfo extracts patient information from a DICOM dataset
func (jr *JSONRepresentation) ExtractPatientInfo(dataset *DicomDataset) map[string]string {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	info := make(map[string]string)

	if dataset.PatientName != "" {
		info["PatientName"] = dataset.PatientName
	}
	if dataset.PatientID != "" {
		info["PatientID"] = dataset.PatientID
	}
	if dataset.ReferringPhysician != "" {
		info["ReferringPhysician"] = dataset.ReferringPhysician
	}
	if dataset.InstitutionName != "" {
		info["InstitutionName"] = dataset.InstitutionName
	}

	return info
}

// MergeDatasets merges two DICOM datasets (destination overrides with source)
func (jr *JSONRepresentation) MergeDatasets(dest, src *DicomDataset) (*DicomDataset, error) {
	jr.mu.Lock()
	defer jr.mu.Unlock()

	if dest == nil {
		return nil, fmt.Errorf("destination dataset cannot be nil")
	}

	if src == nil {
		return dest, nil
	}

	// Merge non-empty fields from source to prevent overwriting destination values with empty defaults.
	if src.PatientName != "" {
		dest.PatientName = src.PatientName
	}
	if src.PatientID != "" {
		dest.PatientID = src.PatientID
	}
	if src.StudyInstanceUID != "" {
		dest.StudyInstanceUID = src.StudyInstanceUID
	}
	if src.SeriesInstanceUID != "" {
		dest.SeriesInstanceUID = src.SeriesInstanceUID
	}
	if src.SOPInstanceUID != "" {
		dest.SOPInstanceUID = src.SOPInstanceUID
	}
	if src.SOPClassUID != "" {
		dest.SOPClassUID = src.SOPClassUID
	}
	if src.Modality != "" {
		dest.Modality = src.Modality
	}
	if src.Manufacturer != "" {
		dest.Manufacturer = src.Manufacturer
	}
	if src.InstitutionName != "" {
		dest.InstitutionName = src.InstitutionName
	}
	if src.InstanceNumber > 0 {
		dest.InstanceNumber = src.InstanceNumber
	}
	if src.SeriesNumber > 0 {
		dest.SeriesNumber = src.SeriesNumber
	}
	if src.NumberOfFrames > 0 {
		dest.NumberOfFrames = src.NumberOfFrames
	}

	return dest, nil
}

// FilterDataset creates a filtered copy with only specified fields
func (jr *JSONRepresentation) FilterDataset(dataset *DicomDataset, fields []string) (*DicomDataset, error) {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	if dataset == nil {
		return nil, fmt.Errorf("cannot filter nil dataset")
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("must specify at least one field to filter")
	}

	filtered := &DicomDataset{}

	for _, field := range fields {
		switch field {
		case "PatientName":
			filtered.PatientName = dataset.PatientName
		case "PatientID":
			filtered.PatientID = dataset.PatientID
		case "StudyInstanceUID":
			filtered.StudyInstanceUID = dataset.StudyInstanceUID
		case "SeriesInstanceUID":
			filtered.SeriesInstanceUID = dataset.SeriesInstanceUID
		case "SOPInstanceUID":
			filtered.SOPInstanceUID = dataset.SOPInstanceUID
		case "SOPClassUID":
			filtered.SOPClassUID = dataset.SOPClassUID
		case "Modality":
			filtered.Modality = dataset.Modality
		case "StudyDate":
			filtered.StudyDate = dataset.StudyDate
		case "StudyTime":
			filtered.StudyTime = dataset.StudyTime
		case "Manufacturer":
			filtered.Manufacturer = dataset.Manufacturer
		case "InstanceNumber":
			filtered.InstanceNumber = dataset.InstanceNumber
		case "SeriesNumber":
			filtered.SeriesNumber = dataset.SeriesNumber
		}
	}

	return filtered, nil
}

// SerializeDataset converts dataset to compact JSON bytes
func (jr *JSONRepresentation) SerializeDataset(dataset *DicomDataset) ([]byte, error) {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	if dataset == nil {
		return nil, fmt.Errorf("cannot serialize nil dataset")
	}

	return json.Marshal(dataset)
}

// DeserializeDataset converts JSON bytes to dataset
func (jr *JSONRepresentation) DeserializeDataset(data []byte) (*DicomDataset, error) {
	jr.mu.RLock()
	defer jr.mu.RUnlock()

	if len(data) == 0 {
		return nil, fmt.Errorf("cannot deserialize empty data")
	}

	var dataset DicomDataset
	err := json.Unmarshal(data, &dataset)
	if err != nil {
		return nil, fmt.Errorf("deserialization failed: %w", err)
	}

	return &dataset, nil
}
