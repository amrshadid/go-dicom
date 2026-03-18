package network

import (
	"testing"
)

func TestNEventReportRQBuildAndParse(t *testing.T) {
	ds := BuildNEventReportRQ(1, "1.2.3", "1.2.3.4", 5, true)
	cmdField, msgID, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNEventReportRQ {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNEventReportRQ, cmdField)
	}
	if msgID != 1 {
		t.Errorf("expected msgID 1, got %d", msgID)
	}
	if !HasDataSet(ds) {
		t.Error("expected dataset present")
	}
}

func TestNEventReportRSPBuildAndParse(t *testing.T) {
	ds := BuildNEventReportRSP(42, "1.2.3", "1.2.3.4", 5, StatusSuccess)
	cmdField, _, status, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNEventReportRSP {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNEventReportRSP, cmdField)
	}
	if status != StatusSuccess {
		t.Errorf("expected status success, got 0x%04X", status)
	}
}

func TestNGetRQBuildAndParse(t *testing.T) {
	ds := BuildNGetRQ(10, "1.2.840.10008.5.1.1.16", "1.2.3.4")
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNGetRQ {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNGetRQ, cmdField)
	}
}

func TestNGetRSPBuildAndParse(t *testing.T) {
	ds := BuildNGetRSP(10, "1.2.3", "1.2.3.4", StatusSuccess, true)
	cmdField, _, status, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNGetRSP {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNGetRSP, cmdField)
	}
	if status != StatusSuccess {
		t.Errorf("expected success, got 0x%04X", status)
	}
}

func TestNSetRQBuildAndParse(t *testing.T) {
	ds := BuildNSetRQ(20, "1.2.3", "1.2.3.4")
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNSetRQ {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNSetRQ, cmdField)
	}
	if !HasDataSet(ds) {
		t.Error("N-SET-RQ should have dataset")
	}
}

func TestNSetRSPBuildAndParse(t *testing.T) {
	ds := BuildNSetRSP(20, "1.2.3", "1.2.3.4", StatusSuccess)
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNSetRSP {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNSetRSP, cmdField)
	}
}

func TestNActionRQBuildAndParse(t *testing.T) {
	ds := BuildNActionRQ(30, "1.2.3", "1.2.3.4", 1, false)
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNActionRQ {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNActionRQ, cmdField)
	}
}

func TestNActionRSPBuildAndParse(t *testing.T) {
	ds := BuildNActionRSP(30, "1.2.3", "1.2.3.4", 1, StatusSuccess)
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNActionRSP {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNActionRSP, cmdField)
	}
}

func TestNCreateRQBuildAndParse(t *testing.T) {
	ds := BuildNCreateRQ(40, "1.2.3", "1.2.3.4", true)
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNCreateRQ {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNCreateRQ, cmdField)
	}
}

func TestNCreateRSPBuildAndParse(t *testing.T) {
	ds := BuildNCreateRSP(40, "1.2.3", "1.2.3.4", StatusSuccess)
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNCreateRSP {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNCreateRSP, cmdField)
	}
}

func TestNDeleteRQBuildAndParse(t *testing.T) {
	ds := BuildNDeleteRQ(50, "1.2.3", "1.2.3.4")
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNDeleteRQ {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNDeleteRQ, cmdField)
	}
}

func TestNDeleteRSPBuildAndParse(t *testing.T) {
	ds := BuildNDeleteRSP(50, "1.2.3", "1.2.3.4", StatusSuccess)
	cmdField, _, _, err := ParseCommandDataset(ds)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if cmdField != CommandNDeleteRSP {
		t.Errorf("expected 0x%04X, got 0x%04X", CommandNDeleteRSP, cmdField)
	}
}

func TestAllNDIMSECommandConstants(t *testing.T) {
	// Verify all N-DIMSE command constants are defined and unique
	commands := map[uint16]string{
		CommandNEventReportRQ:  "N-EVENT-REPORT-RQ",
		CommandNEventReportRSP: "N-EVENT-REPORT-RSP",
		CommandNGetRQ:          "N-GET-RQ",
		CommandNGetRSP:         "N-GET-RSP",
		CommandNSetRQ:          "N-SET-RQ",
		CommandNSetRSP:         "N-SET-RSP",
		CommandNActionRQ:       "N-ACTION-RQ",
		CommandNActionRSP:      "N-ACTION-RSP",
		CommandNCreateRQ:       "N-CREATE-RQ",
		CommandNCreateRSP:      "N-CREATE-RSP",
		CommandNDeleteRQ:       "N-DELETE-RQ",
		CommandNDeleteRSP:      "N-DELETE-RSP",
	}

	if len(commands) != 12 {
		t.Errorf("expected 12 N-DIMSE commands, got %d", len(commands))
	}

	// Verify RQ commands have bit 15 clear, RSP commands have bit 15 set
	for cmd, name := range commands {
		isRSP := cmd&0x8000 != 0
		if isRSP && !contains(name, "RSP") {
			t.Errorf("%s (0x%04X) has RSP bit set but name doesn't contain RSP", name, cmd)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
