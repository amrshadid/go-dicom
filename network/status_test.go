package network

import "testing"

func TestCategorizeStatus(t *testing.T) {
	tests := []struct {
		status   uint16
		expected StatusCategory
	}{
		{StatusSuccess, StatusCategorySuccess},
		{StatusCancel, StatusCategoryCancel},
		{StatusPending, StatusCategoryPending},
		{StatusPendingWarning, StatusCategoryPending},
		{StatusWarning, StatusCategoryWarning},
		{StatusStorageCoercionOfDataElements, StatusCategoryWarning},
		{StatusStorageElementsDiscarded, StatusCategoryWarning},
		{StatusUnableToProcess, StatusCategoryFailure},
		{StatusOutOfResources, StatusCategoryFailure},
		{StatusMoveDestUnknown, StatusCategoryFailure},
		{StatusClassNotSupported, StatusCategoryFailure},
	}

	for _, tt := range tests {
		result := CategorizeStatus(tt.status)
		if result != tt.expected {
			t.Errorf("CategorizeStatus(0x%04X) = %s, want %s", tt.status, result, tt.expected)
		}
	}
}

func TestStatusHelpers(t *testing.T) {
	if !IsSuccess(StatusSuccess) {
		t.Error("StatusSuccess should be success")
	}
	if IsSuccess(StatusUnableToProcess) {
		t.Error("StatusUnableToProcess should not be success")
	}
	if !IsFailure(StatusUnableToProcess) {
		t.Error("StatusUnableToProcess should be failure")
	}
	if !IsPending(StatusPending) {
		t.Error("StatusPending should be pending")
	}
	if !IsCancel(StatusCancel) {
		t.Error("StatusCancel should be cancel")
	}
	if !IsWarning(StatusWarning) {
		t.Error("StatusWarning should be warning")
	}
}

func TestStatusCategoryString(t *testing.T) {
	tests := []struct {
		cat      StatusCategory
		expected string
	}{
		{StatusCategorySuccess, "Success"},
		{StatusCategoryPending, "Pending"},
		{StatusCategoryCancel, "Cancel"},
		{StatusCategoryWarning, "Warning"},
		{StatusCategoryFailure, "Failure"},
		{StatusCategoryUnknown, "Unknown"},
	}

	for _, tt := range tests {
		if tt.cat.String() != tt.expected {
			t.Errorf("%d.String() = %q, want %q", tt.cat, tt.cat.String(), tt.expected)
		}
	}
}

func TestStatusNoSuchSOPInstanceNotOutOfResources(t *testing.T) {
	// PS3.7 Annex C: 0112H is No Such SOP Instance. "Refused: Out of Resources"
	// is a C-service status in the A7xxH range (StatusOutOfResources = 0xA700).
	if StatusNoSuchSOPInstance != 0x0112 {
		t.Fatalf("StatusNoSuchSOPInstance = 0x%04X, want 0x0112", StatusNoSuchSOPInstance)
	}
	if StatusRefusedOutOfResources != StatusNoSuchSOPInstance {
		t.Fatalf("StatusRefusedOutOfResources = 0x%04X, want StatusNoSuchSOPInstance (0x0112) for compatibility", StatusRefusedOutOfResources)
	}
	if StatusOutOfResources != 0xA700 {
		t.Fatalf("StatusOutOfResources = 0x%04X, want 0xA700", StatusOutOfResources)
	}
	if StatusResourceLimitation != 0x0213 {
		t.Fatalf("StatusResourceLimitation = 0x%04X, want 0x0213", StatusResourceLimitation)
	}
	if StatusNoSuchSOPInstance == StatusOutOfResources {
		t.Fatal("StatusNoSuchSOPInstance must not equal StatusOutOfResources")
	}
	if StatusNoSuchSOPInstance == StatusResourceLimitation {
		t.Fatal("StatusNoSuchSOPInstance must not equal StatusResourceLimitation")
	}
}

func TestFormatStatus(t *testing.T) {
	s := FormatStatus(StatusSuccess)
	if s != "0x0000 (Success)" {
		t.Errorf("FormatStatus(0x0000) = %q, want %q", s, "0x0000 (Success)")
	}

	s = FormatStatus(StatusPending)
	if s != "0xFF00 (Pending)" {
		t.Errorf("FormatStatus(0xFF00) = %q, want %q", s, "0xFF00 (Pending)")
	}
}
