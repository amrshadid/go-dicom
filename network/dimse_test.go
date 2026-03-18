package network

import (
	"testing"
)

func TestBuildAndParseCEchoRQ(t *testing.T) {
	ds := BuildCEchoRQ(1)

	cmdField, msgID, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("ParseCommandDataset failed: %v", err)
	}

	if cmdField != CommandCEchoRQ {
		t.Errorf("expected command field 0x%04X, got 0x%04X", CommandCEchoRQ, cmdField)
	}
	if msgID != 1 {
		t.Errorf("expected message ID 1, got %d", msgID)
	}

	if HasDataSet(ds) {
		t.Error("C-ECHO-RQ should not have a dataset")
	}

	sopClass, err := GetAffectedSOPClassUID(ds)
	if err != nil {
		t.Fatalf("GetAffectedSOPClassUID failed: %v", err)
	}
	if sopClass != VerificationSOPClassUID {
		t.Errorf("expected SOP class %s, got %s", VerificationSOPClassUID, sopClass)
	}
}

func TestBuildAndParseCEchoRSP(t *testing.T) {
	ds := BuildCEchoRSP(42, StatusSuccess)

	cmdField, msgID, status, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("ParseCommandDataset failed: %v", err)
	}

	if cmdField != CommandCEchoRSP {
		t.Errorf("expected command field 0x%04X, got 0x%04X", CommandCEchoRSP, cmdField)
	}
	if msgID != 42 {
		t.Errorf("expected message ID 42, got %d", msgID)
	}
	if status != StatusSuccess {
		t.Errorf("expected status 0x%04X, got 0x%04X", StatusSuccess, status)
	}
}

func TestBuildCStoreRQ(t *testing.T) {
	ds := BuildCStoreRQ(5, CTImageStorageUID, "1.2.3.4.5", PriorityHigh)

	cmdField, msgID, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("ParseCommandDataset failed: %v", err)
	}

	if cmdField != CommandCStoreRQ {
		t.Errorf("expected command field 0x%04X, got 0x%04X", CommandCStoreRQ, cmdField)
	}
	if msgID != 5 {
		t.Errorf("expected message ID 5, got %d", msgID)
	}

	if !HasDataSet(ds) {
		t.Error("C-STORE-RQ should indicate dataset present")
	}

	sopClass, _ := GetAffectedSOPClassUID(ds)
	if sopClass != CTImageStorageUID {
		t.Errorf("expected SOP class %s, got %s", CTImageStorageUID, sopClass)
	}

	sopInstance, _ := GetAffectedSOPInstanceUID(ds)
	if sopInstance != "1.2.3.4.5" {
		t.Errorf("expected SOP instance '1.2.3.4.5', got '%s'", sopInstance)
	}
}

func TestBuildCFindRQ(t *testing.T) {
	ds := BuildCFindRQ(10, PatientRootQueryRetrieveFind, PriorityMedium)

	cmdField, msgID, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("ParseCommandDataset failed: %v", err)
	}

	if cmdField != CommandCFindRQ {
		t.Errorf("expected command field 0x%04X, got 0x%04X", CommandCFindRQ, cmdField)
	}
	if msgID != 10 {
		t.Errorf("expected message ID 10, got %d", msgID)
	}
}

func TestBuildCMoveRQ(t *testing.T) {
	ds := BuildCMoveRQ(20, PatientRootQueryRetrieveMove, "DEST_AE", PriorityLow)

	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("ParseCommandDataset failed: %v", err)
	}
	if cmdField != CommandCMoveRQ {
		t.Errorf("expected command field 0x%04X, got 0x%04X", CommandCMoveRQ, cmdField)
	}
}

func TestBuildCGetRQ(t *testing.T) {
	ds := BuildCGetRQ(30, PatientRootQueryRetrieveGet, PriorityMedium)

	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("ParseCommandDataset failed: %v", err)
	}
	if cmdField != CommandCGetRQ {
		t.Errorf("expected command field 0x%04X, got 0x%04X", CommandCGetRQ, cmdField)
	}
}

func TestCommandDatasetEncodeDecode(t *testing.T) {
	original := BuildCEchoRQ(99)

	encoded, err := EncodeCommandDataset(original)
	if err != nil {
		t.Fatalf("EncodeCommandDataset failed: %v", err)
	}

	decoded, err := DecodeCommandDataset(encoded)
	if err != nil {
		t.Fatalf("DecodeCommandDataset failed: %v", err)
	}

	cmdField, msgID, _, err := ParseCommandDataset(decoded)
	if err != nil {
		t.Fatalf("ParseCommandDataset failed: %v", err)
	}

	if cmdField != CommandCEchoRQ {
		t.Errorf("expected command field 0x%04X, got 0x%04X", CommandCEchoRQ, cmdField)
	}
	if msgID != 99 {
		t.Errorf("expected message ID 99, got %d", msgID)
	}
}

func TestIsPending(t *testing.T) {
	tests := []struct {
		status   uint16
		expected bool
	}{
		{StatusPending, true},
		{StatusPendingWarning, true},
		{StatusSuccess, false},
		{StatusCancel, false},
		{StatusUnableToProcess, false},
	}

	for _, tt := range tests {
		result := IsPending(tt.status)
		if result != tt.expected {
			t.Errorf("IsPending(0x%04X) = %v, want %v", tt.status, result, tt.expected)
		}
	}
}

func TestBuildCMoveRSP(t *testing.T) {
	ds := BuildCMoveRSP(15, PatientRootQueryRetrieveMove, StatusSuccess, 0, 5, 1, 2)

	cmdField, _, status, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("ParseCommandDataset failed: %v", err)
	}

	if cmdField != CommandCMoveRSP {
		t.Errorf("expected command field 0x%04X, got 0x%04X", CommandCMoveRSP, cmdField)
	}
	if status != StatusSuccess {
		t.Errorf("expected status 0x%04X, got 0x%04X", StatusSuccess, status)
	}
}

func TestBuildCFindRSP(t *testing.T) {
	ds := BuildCFindRSP(25, StudyRootQueryRetrieveFind, StatusPending, true)

	if !HasDataSet(ds) {
		t.Error("C-FIND-RSP with data should have dataset present")
	}

	ds2 := BuildCFindRSP(25, StudyRootQueryRetrieveFind, StatusSuccess, false)
	if HasDataSet(ds2) {
		t.Error("Final C-FIND-RSP should not have dataset")
	}
}
