package jsonrep_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/jsonrep"
)

// Test JSON representation creation
func TestNewJSONRepresentation(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()
	if jr == nil {
		t.Fatal("expected non-nil JSON representation")
	}
}

// Test dataset to JSON conversion
func TestDatasetToJSON(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{
		PatientName:      "Doe^John",
		PatientID:        "12345",
		StudyInstanceUID: "1.2.3.4.5",
		SOPInstanceUID:   "1.2.3.4.5.6",
		SOPClassUID:      "1.2.840.10008.5.1.4.1.2",
	}

	jsonData, err := jr.ToJSON(dataset)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(jsonData) == 0 {
		t.Fatal("expected non-empty JSON data")
	}

	// Verify it's valid JSON
	var result map[string]interface{}
	err = json.Unmarshal(jsonData, &result)
	if err != nil {
		t.Fatalf("generated invalid JSON: %v", err)
	}
}

// Test nil dataset to JSON error
func TestToJSONNilDataset(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.ToJSON(nil)
	if err == nil {
		t.Error("expected error for nil dataset")
	}
}

// Test JSON to dataset conversion
func TestJSONToDataset(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	jsonData := []byte(`{
		"PatientName": "Doe^John",
		"PatientID": "12345",
		"StudyInstanceUID": "1.2.3.4.5",
		"SOPInstanceUID": "1.2.3.4.5.6",
		"SOPClassUID": "1.2.840.10008.5.1.4.1.2"
	}`)

	dataset, err := jr.FromJSON(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dataset.PatientName != "Doe^John" {
		t.Errorf("expected PatientName 'Doe^John', got %s", dataset.PatientName)
	}

	if dataset.PatientID != "12345" {
		t.Errorf("expected PatientID '12345', got %s", dataset.PatientID)
	}
}

// Test empty JSON error
func TestFromJSONEmpty(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.FromJSON([]byte{})
	if err == nil {
		t.Error("expected error for empty JSON")
	}
}

// Test invalid JSON error
func TestFromJSONInvalid(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.FromJSON([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// Test JSON message creation
func TestToJSONMessage(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{
		PatientName:    "Doe^John",
		PatientID:      "12345",
		SOPInstanceUID: "1.2.3.4.5.6",
		SOPClassUID:    "1.2.840.10008.5.1.4.1.2",
		Modality:       "CT",
	}

	metadata := map[string]interface{}{
		"source":    "PACS",
		"timestamp": time.Now(),
	}

	msg, err := jr.ToJSONMessage(dataset, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Version != "1.0" {
		t.Errorf("expected version 1.0, got %s", msg.Version)
	}

	if len(msg.Elements) == 0 {
		t.Error("expected non-empty elements")
	}

	if msg.Elements["PatientName"].Value != "Doe^John" {
		t.Error("expected PatientName in elements")
	}
}

// Test nil dataset to JSON message error
func TestToJSONMessageNilDataset(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.ToJSONMessage(nil, nil)
	if err == nil {
		t.Error("expected error for nil dataset")
	}
}

// Test JSON validation - valid
func TestValidateJSONValid(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	jsonData := []byte(`{
		"PatientName": "Doe^John",
		"PatientID": "12345",
		"SOPInstanceUID": "1.2.3.4.5.6",
		"SOPClassUID": "1.2.840.10008.5.1.4.1.2"
	}`)

	err := jr.ValidateJSON(jsonData)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// Test JSON validation - missing SOPInstanceUID
func TestValidateJSONMissingSOPInstanceUID(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	jsonData := []byte(`{
		"PatientName": "Doe^John",
		"SOPClassUID": "1.2.840.10008.5.1.4.1.2"
	}`)

	err := jr.ValidateJSON(jsonData)
	if err == nil {
		t.Error("expected error for missing SOPInstanceUID")
	}
}

// Test JSON validation - missing SOPClassUID
func TestValidateJSONMissingSOPClassUID(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	jsonData := []byte(`{
		"PatientName": "Doe^John",
		"SOPInstanceUID": "1.2.3.4.5.6"
	}`)

	err := jr.ValidateJSON(jsonData)
	if err == nil {
		t.Error("expected error for missing SOPClassUID")
	}
}

// Test empty JSON validation
func TestValidateJSONEmpty(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	err := jr.ValidateJSON([]byte{})
	if err == nil {
		t.Error("expected error for empty JSON")
	}
}

// Test pretty print JSON
func TestPrettyPrintJSON(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	jsonData := []byte(`{"PatientName":"Doe^John","PatientID":"12345"}`)

	pretty, err := jr.PrettyPrintJSON(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pretty) == 0 {
		t.Error("expected non-empty pretty JSON")
	}

	// Pretty printed JSON should have newlines
	if len(pretty) <= len(string(jsonData)) {
		t.Error("expected pretty JSON to be longer than compact")
	}
}

// Test pretty print invalid JSON
func TestPrettyPrintInvalidJSON(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.PrettyPrintJSON([]byte("invalid"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// Test compact JSON
func TestCompactJSON(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	jsonData := []byte(`{
		"PatientName": "Doe^John",
		"PatientID": "12345"
	}`)

	compact, err := jr.CompactJSON(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(compact) == 0 {
		t.Error("expected non-empty compact JSON")
	}

	// Compact JSON should have no newlines or extra spaces
	if string(compact) != `{"PatientID":"12345","PatientName":"Doe^John"}` &&
		string(compact) != `{"PatientName":"Doe^John","PatientID":"12345"}` {
		t.Errorf("unexpected compact JSON: %s", string(compact))
	}
}

// Test extract UIDs
func TestExtractUIDs(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{
		StudyInstanceUID:  "1.2.3.4",
		SeriesInstanceUID: "1.2.3.4.5",
		SOPInstanceUID:    "1.2.3.4.5.6",
		SOPClassUID:       "1.2.840.10008.5.1.4.1.2",
	}

	uids := jr.ExtractUIDs(dataset)

	if uids["StudyInstanceUID"] != "1.2.3.4" {
		t.Error("expected StudyInstanceUID to be extracted")
	}

	if uids["SeriesInstanceUID"] != "1.2.3.4.5" {
		t.Error("expected SeriesInstanceUID to be extracted")
	}

	if uids["SOPInstanceUID"] != "1.2.3.4.5.6" {
		t.Error("expected SOPInstanceUID to be extracted")
	}

	if uids["SOPClassUID"] != "1.2.840.10008.5.1.4.1.2" {
		t.Error("expected SOPClassUID to be extracted")
	}
}

// Test extract UIDs with empty dataset
func TestExtractUIDsEmpty(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{}

	uids := jr.ExtractUIDs(dataset)

	if len(uids) != 0 {
		t.Error("expected empty UID map for empty dataset")
	}
}

// Test extract patient info
func TestExtractPatientInfo(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{
		PatientName:        "Doe^John",
		PatientID:          "12345",
		ReferringPhysician: "Smith^Jane",
		InstitutionName:    "Hospital XYZ",
	}

	info := jr.ExtractPatientInfo(dataset)

	if info["PatientName"] != "Doe^John" {
		t.Error("expected PatientName to be extracted")
	}

	if info["PatientID"] != "12345" {
		t.Error("expected PatientID to be extracted")
	}

	if info["ReferringPhysician"] != "Smith^Jane" {
		t.Error("expected ReferringPhysician to be extracted")
	}

	if info["InstitutionName"] != "Hospital XYZ" {
		t.Error("expected InstitutionName to be extracted")
	}
}

// Test merge datasets
func TestMergeDatasets(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dest := &jsonrep.DicomDataset{
		PatientID:    "12345",
		PatientName:  "Doe^John",
		SeriesNumber: 1,
	}

	src := &jsonrep.DicomDataset{
		PatientName:    "Doe^Jane",
		Modality:       "CT",
		InstanceNumber: 5,
	}

	merged, err := jr.MergeDatasets(dest, src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// src overrides dest
	if merged.PatientName != "Doe^Jane" {
		t.Errorf("expected PatientName to be overridden, got %s", merged.PatientName)
	}

	// dest fields preserved when src doesn't have them
	if merged.PatientID != "12345" {
		t.Errorf("expected PatientID to be preserved, got %s", merged.PatientID)
	}

	// src new fields added
	if merged.Modality != "CT" {
		t.Errorf("expected Modality to be added, got %s", merged.Modality)
	}
}

// Test merge with nil destination
func TestMergeDatasetsNilDest(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.MergeDatasets(nil, &jsonrep.DicomDataset{})
	if err == nil {
		t.Error("expected error for nil destination")
	}
}

// Test merge with nil source
func TestMergeDatasetsNilSrc(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dest := &jsonrep.DicomDataset{PatientID: "12345"}

	merged, err := jr.MergeDatasets(dest, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if merged.PatientID != "12345" {
		t.Error("expected destination to be unchanged")
	}
}

// Test filter dataset
func TestFilterDataset(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{
		PatientName:      "Doe^John",
		PatientID:        "12345",
		StudyInstanceUID: "1.2.3.4",
		SeriesNumber:     1,
		Modality:         "CT",
		Manufacturer:     "GE",
	}

	fields := []string{"PatientName", "PatientID", "Modality"}

	filtered, err := jr.FilterDataset(dataset, fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if filtered.PatientName != "Doe^John" {
		t.Error("expected PatientName in filtered dataset")
	}

	if filtered.PatientID != "12345" {
		t.Error("expected PatientID in filtered dataset")
	}

	if filtered.Modality != "CT" {
		t.Error("expected Modality in filtered dataset")
	}

	if filtered.SeriesNumber != 0 {
		t.Error("expected SeriesNumber not in filtered dataset")
	}

	if filtered.Manufacturer != "" {
		t.Error("expected Manufacturer not in filtered dataset")
	}
}

// Test filter with nil dataset
func TestFilterDatasetNil(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.FilterDataset(nil, []string{"PatientID"})
	if err == nil {
		t.Error("expected error for nil dataset")
	}
}

// Test filter with no fields
func TestFilterDatasetNoFields(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.FilterDataset(&jsonrep.DicomDataset{}, []string{})
	if err == nil {
		t.Error("expected error for empty field list")
	}
}

// Test serialize dataset
func TestSerializeDataset(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{
		PatientName:    "Doe^John",
		PatientID:      "12345",
		SOPInstanceUID: "1.2.3.4.5.6",
		SOPClassUID:    "1.2.840.10008.5.1.4.1.2",
	}

	data, err := jr.SerializeDataset(dataset)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty serialized data")
	}
}

// Test serialize nil dataset
func TestSerializeNilDataset(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.SerializeDataset(nil)
	if err == nil {
		t.Error("expected error for nil dataset")
	}
}

// Test deserialize dataset
func TestDeserializeDataset(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	data := []byte(`{
		"PatientName": "Doe^John",
		"PatientID": "12345",
		"SOPInstanceUID": "1.2.3.4.5.6",
		"SOPClassUID": "1.2.840.10008.5.1.4.1.2"
	}`)

	dataset, err := jr.DeserializeDataset(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dataset.PatientName != "Doe^John" {
		t.Error("expected PatientName to be deserialized")
	}

	if dataset.PatientID != "12345" {
		t.Error("expected PatientID to be deserialized")
	}
}

// Test deserialize empty data
func TestDeserializeEmpty(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	_, err := jr.DeserializeDataset([]byte{})
	if err == nil {
		t.Error("expected error for empty data")
	}
}

// Test roundtrip conversion
func TestRoundtripConversion(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	original := &jsonrep.DicomDataset{
		PatientName:       "Doe^John",
		PatientID:         "12345",
		StudyInstanceUID:  "1.2.3.4",
		SeriesInstanceUID: "1.2.3.4.5",
		SOPInstanceUID:    "1.2.3.4.5.6",
		SOPClassUID:       "1.2.840.10008.5.1.4.1.2",
		Modality:          "CT",
		InstanceNumber:    5,
	}

	// Convert to JSON
	jsonData, err := jr.ToJSON(original)
	if err != nil {
		t.Fatalf("to JSON failed: %v", err)
	}

	// Convert back from JSON
	recovered, err := jr.FromJSON(jsonData)
	if err != nil {
		t.Fatalf("from JSON failed: %v", err)
	}

	// Verify all fields match
	if recovered.PatientName != original.PatientName {
		t.Error("PatientName mismatch")
	}

	if recovered.PatientID != original.PatientID {
		t.Error("PatientID mismatch")
	}

	if recovered.SOPInstanceUID != original.SOPInstanceUID {
		t.Error("SOPInstanceUID mismatch")
	}

	if recovered.Modality != original.Modality {
		t.Error("Modality mismatch")
	}

	if recovered.InstanceNumber != original.InstanceNumber {
		t.Error("InstanceNumber mismatch")
	}
}

// Test concurrent operations
func TestConcurrentOperations(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{
		PatientName:    "Doe^John",
		PatientID:      "12345",
		SOPInstanceUID: "1.2.3.4.5.6",
		SOPClassUID:    "1.2.840.10008.5.1.4.1.2",
	}

	done := make(chan bool, 20)

	// Launch concurrent readers
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = jr.ToJSON(dataset)
			_ = jr.ValidateJSON([]byte(`{"SOPInstanceUID":"1.2.3","SOPClassUID":"1.2"}`))
			done <- true
		}()
	}

	// Launch concurrent writers
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = jr.ToJSON(dataset)
			_, _ = jr.FilterDataset(dataset, []string{"PatientID"})
			done <- true
		}()
	}

	// Wait for completion
	for i := 0; i < 20; i++ {
		<-done
	}
}

// Test JSON element VR types
func TestJSONElementVRTypes(t *testing.T) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{
		PatientName:    "Doe^John",
		PatientID:      "12345",
		SOPInstanceUID: "1.2.3.4.5.6",
		SOPClassUID:    "1.2.840.10008.5.1.4.1.2",
		Modality:       "CT",
		InstanceNumber: 5,
	}

	msg, err := jr.ToJSONMessage(dataset, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check VR types
	if msg.Elements["PatientName"].VR != "PN" {
		t.Errorf("expected PN for PatientName, got %s", msg.Elements["PatientName"].VR)
	}

	if msg.Elements["PatientID"].VR != "LO" {
		t.Errorf("expected LO for PatientID, got %s", msg.Elements["PatientID"].VR)
	}

	if msg.Elements["Modality"].VR != "CS" {
		t.Errorf("expected CS for Modality, got %s", msg.Elements["Modality"].VR)
	}
}

// Benchmark JSON serialization
func BenchmarkToJSON(b *testing.B) {
	jr := jsonrep.NewJSONRepresentation()

	dataset := &jsonrep.DicomDataset{
		PatientName:       "Doe^John",
		PatientID:         "12345",
		StudyInstanceUID:  "1.2.3.4",
		SeriesInstanceUID: "1.2.3.4.5",
		SOPInstanceUID:    "1.2.3.4.5.6",
		SOPClassUID:       "1.2.840.10008.5.1.4.1.2",
		Modality:          "CT",
		InstanceNumber:    5,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = jr.ToJSON(dataset)
	}
}

// Benchmark JSON deserialization
func BenchmarkFromJSON(b *testing.B) {
	jr := jsonrep.NewJSONRepresentation()

	jsonData := []byte(`{
		"PatientName":"Doe^John",
		"PatientID":"12345",
		"StudyInstanceUID":"1.2.3.4",
		"SeriesInstanceUID":"1.2.3.4.5",
		"SOPInstanceUID":"1.2.3.4.5.6",
		"SOPClassUID":"1.2.840.10008.5.1.4.1.2",
		"Modality":"CT",
		"InstanceNumber":5
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = jr.FromJSON(jsonData)
	}
}

// Benchmark validation
func BenchmarkValidateJSON(b *testing.B) {
	jr := jsonrep.NewJSONRepresentation()

	jsonData := []byte(`{
		"SOPInstanceUID":"1.2.3.4.5.6",
		"SOPClassUID":"1.2.840.10008.5.1.4.1.2"
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = jr.ValidateJSON(jsonData)
	}
}
