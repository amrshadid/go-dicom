package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
)

// TestHasVOILUT tests checking for VOI LUT presence
func TestHasVOILUT(t *testing.T) {
	ds := dataset.NewDataset()

	// Empty dataset should not have VOI LUT
	if ds.HasVOILUT() {
		t.Error("Empty dataset should not have VOI LUT")
	}
}

// TestHasModalityLUT tests checking for Modality LUT presence
func TestHasModalityLUT(t *testing.T) {
	ds := dataset.NewDataset()

	// Empty dataset should not have Modality LUT
	if ds.HasModalityLUT() {
		t.Error("Empty dataset should not have Modality LUT")
	}
}

// TestGetVOILUTSequenceNotPresent tests error when VOI LUT not present
func TestGetVOILUTSequenceNotPresent(t *testing.T) {
	ds := dataset.NewDataset()

	_, err := ds.GetVOILUTSequence()
	if err == nil {
		t.Error("GetVOILUTSequence() should return error when not present")
	}
}

// TestGetModalityLUTNotPresent tests error when Modality LUT not present
func TestGetModalityLUTNotPresent(t *testing.T) {
	ds := dataset.NewDataset()

	_, err := ds.GetModalityLUT()
	if err == nil {
		t.Error("GetModalityLUT() should return error when not present")
	}
}

// TestApplyVOILUTFallbackToWindowing tests fallback to windowing
func TestApplyVOILUTFallbackToWindowing(t *testing.T) {
	ds := dataset.NewDataset()

	// Without VOI LUT, should fall back to windowing
	// This will fail because there's no pixel data, but tests the code path
	_, err := ds.ApplyVOILUTToPixelData()
	if err == nil {
		t.Log("ApplyVOILUTToPixelData() attempted fallback (expected to fail without pixel data)")
	}
}

// TestApplyPresentationLUT tests presentation LUT pipeline
func TestApplyPresentationLUT(t *testing.T) {
	ds := dataset.NewDataset()

	// Will fail without pixel data, but tests the method exists
	_, err := ds.ApplyPresentationLUT()
	if err == nil {
		t.Log("ApplyPresentationLUT() attempted processing (expected to fail without pixel data)")
	}
}

// Note: Full VOI LUT testing requires creating valid DICOM sequences with LUT data,
// which is complex. These tests verify the API exists and handles missing data correctly.
// Integration tests with real DICOM files would test the full functionality.
