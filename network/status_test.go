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
