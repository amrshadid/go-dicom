package waveforms_test

import (
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/waveforms"
)

// TestDataElemIntegration tests integration between waveforms and dataelem modules.
func TestDataElemIntegration(t *testing.T) {
	// Create a sample waveform group
	wf := &waveforms.WaveformGroup{
		WaveformOriginality:      "ORIGINAL",
		NumberOfWaveformChannels: 2,
		NumberOfSamples:          100,
		SamplingFrequency:        500.0,
		MultiplexGroupLabel:      "ECG",
		ChannelLabel:             []string{"Lead I", "Lead II"},
		ChannelSource:            []string{"Electrode", "Electrode"},
		ChannelUnits:             []string{"mV", "mV"},
		WaveformData: [][]int16{
			generateSampleData(100, 100), // Channel 1
			generateSampleData(100, -50), // Channel 2
		},
	}

	// Convert to sequence element
	seqElem, err := dataelem.WaveformGroupsToSequence([]*waveforms.WaveformGroup{wf}, binary.LittleEndian)
	if err != nil {
		t.Fatalf("Failed to convert waveform to sequence: %v", err)
	}

	if seqElem == nil {
		t.Fatal("Sequence element is nil")
	}

	if seqElem.GetVR() != dataelem.SQ {
		t.Errorf("Expected VR=SQ, got %v", seqElem.GetVR())
	}

	// Convert back to waveform groups
	groups, err := seqElem.WaveformSequenceToGroups(binary.LittleEndian)
	if err != nil {
		t.Fatalf("Failed to convert sequence to waveforms: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("Expected 1 waveform group, got %d", len(groups))
	}

	// Verify data integrity
	recovered := groups[0]
	if recovered.WaveformOriginality != wf.WaveformOriginality {
		t.Errorf("Originality mismatch: got %s, want %s", recovered.WaveformOriginality, wf.WaveformOriginality)
	}

	if recovered.NumberOfWaveformChannels != wf.NumberOfWaveformChannels {
		t.Errorf("Channel count mismatch: got %d, want %d", recovered.NumberOfWaveformChannels, wf.NumberOfWaveformChannels)
	}

	if recovered.NumberOfSamples != wf.NumberOfSamples {
		t.Errorf("Sample count mismatch: got %d, want %d", recovered.NumberOfSamples, wf.NumberOfSamples)
	}

	if recovered.SamplingFrequency != wf.SamplingFrequency {
		t.Errorf("Sampling frequency mismatch: got %.1f, want %.1f", recovered.SamplingFrequency, wf.SamplingFrequency)
	}

	// Verify waveform data
	if len(recovered.WaveformData) != len(wf.WaveformData) {
		t.Fatalf("Channel data count mismatch: got %d, want %d", len(recovered.WaveformData), len(wf.WaveformData))
	}

	for ch := 0; ch < len(recovered.WaveformData); ch++ {
		if len(recovered.WaveformData[ch]) != len(wf.WaveformData[ch]) {
			t.Errorf("Channel %d sample count mismatch: got %d, want %d",
				ch, len(recovered.WaveformData[ch]), len(wf.WaveformData[ch]))
		}
	}
}

// TestDatasetIntegration tests integration between waveforms and dataset modules.
func TestDatasetIntegration(t *testing.T) {
	// Create a dataset
	ds := dataset.NewDataset()

	// Create sample waveforms
	wf1 := &waveforms.WaveformGroup{
		WaveformOriginality:      "ORIGINAL",
		NumberOfWaveformChannels: 2,
		NumberOfSamples:          500,
		SamplingFrequency:        250.0,
		MultiplexGroupLabel:      "ECG Group 1",
		WaveformData: [][]int16{
			generateSampleData(500, 100),
			generateSampleData(500, -80),
		},
	}

	wf2 := &waveforms.WaveformGroup{
		WaveformOriginality:      "DERIVED",
		NumberOfWaveformChannels: 1,
		NumberOfSamples:          1000,
		SamplingFrequency:        1000.0,
		MultiplexGroupLabel:      "Processed ECG",
		WaveformData: [][]int16{
			generateSampleData(1000, 50),
		},
	}

	// Test HasWaveforms before adding
	if ds.HasWaveforms() {
		t.Error("Dataset should not have waveforms initially")
	}

	// Add waveforms to dataset
	err := ds.AddWaveforms([]*waveforms.WaveformGroup{wf1, wf2}, binary.LittleEndian)
	if err != nil {
		t.Fatalf("Failed to add waveforms to dataset: %v", err)
	}

	// Test HasWaveforms after adding
	if !ds.HasWaveforms() {
		t.Error("Dataset should have waveforms after adding")
	}

	// Test GetWaveformCount
	count := ds.GetWaveformCount(binary.LittleEndian)
	if count != 2 {
		t.Errorf("Expected 2 waveform groups, got %d", count)
	}

	// Retrieve waveforms
	groups, err := ds.GetWaveforms(binary.LittleEndian)
	if err != nil {
		t.Fatalf("Failed to get waveforms from dataset: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("Expected 2 waveform groups, got %d", len(groups))
	}

	// Verify first group
	if groups[0].NumberOfWaveformChannels != 2 {
		t.Errorf("Group 0: expected 2 channels, got %d", groups[0].NumberOfWaveformChannels)
	}

	if groups[0].SamplingFrequency != 250.0 {
		t.Errorf("Group 0: expected 250.0 Hz, got %.1f", groups[0].SamplingFrequency)
	}

	// Verify second group
	if groups[1].NumberOfWaveformChannels != 1 {
		t.Errorf("Group 1: expected 1 channel, got %d", groups[1].NumberOfWaveformChannels)
	}

	if groups[1].SamplingFrequency != 1000.0 {
		t.Errorf("Group 1: expected 1000.0 Hz, got %.1f", groups[1].SamplingFrequency)
	}

	// Test validation
	err = ds.ValidateWaveforms(binary.LittleEndian)
	if err != nil {
		t.Errorf("Waveform validation failed: %v", err)
	}

	// Test summary
	summary := ds.GetWaveformSummary(binary.LittleEndian)
	if summary == "" {
		t.Error("Waveform summary is empty")
	}
	t.Logf("Waveform summary:\n%s", summary)

	// Test removal
	if !ds.RemoveWaveforms() {
		t.Error("Failed to remove waveforms")
	}

	if ds.HasWaveforms() {
		t.Error("Dataset should not have waveforms after removal")
	}
}

// TestMultipleWaveformGroups tests handling of multiple waveform groups.
func TestMultipleWaveformGroups(t *testing.T) {
	ds := dataset.NewDataset()

	// Create 5 different waveform groups
	groups := make([]*waveforms.WaveformGroup, 5)
	for i := 0; i < 5; i++ {
		groups[i] = &waveforms.WaveformGroup{
			WaveformOriginality:      "ORIGINAL",
			NumberOfWaveformChannels: i + 1, // 1, 2, 3, 4, 5 channels
			NumberOfSamples:          (i + 1) * 100,
			SamplingFrequency:        float64((i + 1) * 100),
			MultiplexGroupLabel:      "",
			WaveformData:             make([][]int16, i+1),
		}

		// Generate data for each channel
		for ch := 0; ch < i+1; ch++ {
			groups[i].WaveformData[ch] = generateSampleData((i+1)*100, int16((ch+1)*10))
		}
	}

	// Add all groups
	err := ds.AddWaveforms(groups, binary.LittleEndian)
	if err != nil {
		t.Fatalf("Failed to add waveforms: %v", err)
	}

	// Verify count
	count := ds.GetWaveformCount(binary.LittleEndian)
	if count != 5 {
		t.Errorf("Expected 5 waveform groups, got %d", count)
	}

	// Retrieve and verify each group
	retrieved, err := ds.GetWaveforms(binary.LittleEndian)
	if err != nil {
		t.Fatalf("Failed to retrieve waveforms: %v", err)
	}

	for i, wf := range retrieved {
		expectedChannels := i + 1
		if wf.NumberOfWaveformChannels != expectedChannels {
			t.Errorf("Group %d: expected %d channels, got %d",
				i, expectedChannels, wf.NumberOfWaveformChannels)
		}

		expectedSamples := (i + 1) * 100
		if wf.NumberOfSamples != expectedSamples {
			t.Errorf("Group %d: expected %d samples, got %d",
				i, expectedSamples, wf.NumberOfSamples)
		}
	}
}

// TestWaveformDataIntegrity tests that waveform data is preserved correctly.
func TestWaveformDataIntegrity(t *testing.T) {
	ds := dataset.NewDataset()

	// Create waveform with specific data pattern
	testData := [][]int16{
		{0, 100, 200, 300, 400, 500, 400, 300, 200, 100},
		{500, 400, 300, 200, 100, 0, -100, -200, -300, -400},
		{-500, -400, -300, -200, -100, 0, 100, 200, 300, 400},
	}

	wf := &waveforms.WaveformGroup{
		WaveformOriginality:      "ORIGINAL",
		NumberOfWaveformChannels: 3,
		NumberOfSamples:          10,
		SamplingFrequency:        100.0,
		WaveformData:             testData,
	}

	// Add to dataset
	err := ds.AddWaveforms([]*waveforms.WaveformGroup{wf}, binary.LittleEndian)
	if err != nil {
		t.Fatalf("Failed to add waveform: %v", err)
	}

	// Retrieve
	groups, err := ds.GetWaveforms(binary.LittleEndian)
	if err != nil {
		t.Fatalf("Failed to retrieve waveforms: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("Expected 1 group, got %d", len(groups))
	}

	// Verify data sample by sample
	retrieved := groups[0]
	for ch := 0; ch < 3; ch++ {
		for sample := 0; sample < 10; sample++ {
			original := testData[ch][sample]
			got := retrieved.WaveformData[ch][sample]
			if got != original {
				t.Errorf("Data mismatch at channel %d sample %d: got %d, want %d",
					ch, sample, got, original)
			}
		}
	}
}

// TestEmptyWaveforms tests handling of empty waveform cases.
func TestEmptyWaveforms(t *testing.T) {
	ds := dataset.NewDataset()

	// Test HasWaveforms on empty dataset
	if ds.HasWaveforms() {
		t.Error("Empty dataset should not have waveforms")
	}

	// Test GetWaveformCount on empty dataset
	count := ds.GetWaveformCount(binary.LittleEndian)
	if count != 0 {
		t.Errorf("Expected 0 waveforms, got %d", count)
	}

	// Test GetWaveforms on empty dataset
	groups, err := ds.GetWaveforms(binary.LittleEndian)
	if err != nil {
		t.Errorf("GetWaveforms should not error on empty dataset: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("Expected 0 groups, got %d", len(groups))
	}

	// Test RemoveWaveforms on empty dataset
	if ds.RemoveWaveforms() {
		t.Error("RemoveWaveforms should return false for empty dataset")
	}
}

// TestInvalidWaveforms tests error handling for invalid waveform data.
func TestInvalidWaveforms(t *testing.T) {
	ds := dataset.NewDataset()

	// Test with nil waveform group
	invalidGroups := []*waveforms.WaveformGroup{nil}
	err := ds.AddWaveforms(invalidGroups, binary.LittleEndian)
	if err == nil {
		t.Error("Adding nil waveform should error")
	}

	// Test with invalid channel count
	invalidWf := &waveforms.WaveformGroup{
		NumberOfWaveformChannels: 0, // Invalid
		NumberOfSamples:          100,
		SamplingFrequency:        500.0,
		WaveformData:             [][]int16{},
	}
	err = ds.AddWaveforms([]*waveforms.WaveformGroup{invalidWf}, binary.LittleEndian)
	if err == nil {
		t.Error("Adding waveform with 0 channels should error")
	}

	// Test with mismatched data
	mismatchedWf := &waveforms.WaveformGroup{
		NumberOfWaveformChannels: 2,
		NumberOfSamples:          100,
		SamplingFrequency:        500.0,
		WaveformData: [][]int16{
			generateSampleData(100, 100), // Only 1 channel instead of 2
		},
	}
	err = ds.AddWaveforms([]*waveforms.WaveformGroup{mismatchedWf}, binary.LittleEndian)
	if err == nil {
		t.Error("Adding waveform with mismatched channel count should error")
	}
}

// Helper function to generate sample waveform data
func generateSampleData(samples int, baseline int16) []int16 {
	data := make([]int16, samples)
	for i := 0; i < samples; i++ {
		// Simple pattern with variation
		amplitude := int16(float64(baseline) * 0.3)
		data[i] = baseline + int16(float64(amplitude)*0.5)
	}
	return data
}
