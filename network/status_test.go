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

// TestStatusCodesMatchTheStandard covers #137. The values here are PS3.7 Annex
// C's, written as literals so a constant that drifts cannot pass by agreeing
// with itself. StatusRefusedOutOfResources was 0x0112, which is No Such SOP
// Instance: an SCP reporting exhaustion with it told the peer the instance did
// not exist, and a peer that retries on a resource failure would not retry.
func TestStatusCodesMatchTheStandard(t *testing.T) {
	for name, tc := range map[string]struct{ got, want uint16 }{
		"No Such SOP Instance":      {StatusNoSuchSOPInstance, 0x0112},
		"Refused: Out of Resources": {StatusOutOfResources, 0xA700},
		"Resource limitation":       {StatusResourceLimitation, 0x0213},
		"Processing failure":        {StatusProcessingFailure, 0x0110},
		"No Such SOP Class":         {StatusNoSuchSOPClass, 0x0118},
		"SOP Class not supported":   {StatusRefusedSOPClassNotSupported, 0x0122},
		"Missing attribute":         {StatusMissingAttribute, 0x0120},
		"Duplicate invocation":      {StatusDuplicateInvocation, 0x0210},
		"Unrecognized operation":    {StatusUnrecognizedOperation, 0x0211},
		"Mistyped argument":         {StatusMistypedArgument, 0x0212},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = 0x%04X, want 0x%04X", name, tc.got, tc.want)
		}
	}

	// The deprecated name keeps compiling, and keeps its value, so nobody's
	// build breaks over our mistake.
	if StatusRefusedOutOfResources != StatusNoSuchSOPInstance {
		t.Error("the deprecated alias no longer matches StatusNoSuchSOPInstance")
	}
	// And the three are distinct: conflating them is what the bug was.
	if StatusNoSuchSOPInstance == StatusOutOfResources ||
		StatusNoSuchSOPInstance == StatusResourceLimitation {
		t.Error("No Such SOP Instance must differ from both out-of-resources codes")
	}
}
