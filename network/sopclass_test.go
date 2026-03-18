package network

import (
	"testing"
)

func TestAllStorageSOPClassUIDs(t *testing.T) {
	uids := AllStorageSOPClassUIDs()
	if len(uids) < 80 {
		t.Errorf("expected 80+ storage SOP classes, got %d", len(uids))
	}

	// Check for duplicates
	seen := make(map[string]bool)
	for _, uid := range uids {
		if seen[uid] {
			t.Errorf("duplicate SOP Class UID: %s", uid)
		}
		seen[uid] = true
	}

	// Verify key SOP classes are present
	expected := []string{
		CTImageStorageSOP,
		MRImageStorageSOP,
		UltrasoundImageStorageSOP,
		SecondaryCaptureImageStorageSOP,
		RTImageStorageUID,
		PositronEmissionTomographyImageUID,
		EncapsulatedPDFStorageUID,
		TwelveLeadECGWaveformStorageUID,
		BasicTextSRStorageUID,
		SegmentationStorageUID,
		BreastTomosynthesisImageStorageUID,
	}

	for _, uid := range expected {
		if !seen[uid] {
			t.Errorf("missing expected SOP Class: %s", uid)
		}
	}
}

func TestAllQueryRetrieveSOPClassUIDs(t *testing.T) {
	uids := AllQueryRetrieveSOPClassUIDs()
	if len(uids) != 6 {
		t.Errorf("expected 6 Q/R SOP classes, got %d", len(uids))
	}
}

func TestAllTransferSyntaxUIDs(t *testing.T) {
	uids := AllTransferSyntaxUIDs()
	if len(uids) < 10 {
		t.Errorf("expected 10+ transfer syntaxes, got %d", len(uids))
	}

	// Verify key transfer syntaxes
	found := make(map[string]bool)
	for _, uid := range uids {
		found[uid] = true
	}

	required := []string{
		ImplicitVRLittleEndianUID,
		ExplicitVRLittleEndianUID,
		JPEGBaselineUID,
		JPEG2000LosslessUID,
		RLELosslessUID,
	}
	for _, uid := range required {
		if !found[uid] {
			t.Errorf("missing required transfer syntax: %s", uid)
		}
	}
}

func TestIsStorageSOPClass(t *testing.T) {
	if !IsStorageSOPClass(CTImageStorageSOP) {
		t.Error("CT should be a storage SOP class")
	}
	if !IsStorageSOPClass(EncapsulatedPDFStorageUID) {
		t.Error("Encapsulated PDF should be a storage SOP class")
	}
	if IsStorageSOPClass(VerificationSOPClassUID) {
		t.Error("Verification should not be a storage SOP class")
	}
	if IsStorageSOPClass(PatientRootQueryRetrieveFind) {
		t.Error("Q/R Find should not be a storage SOP class")
	}
}

func TestIsQueryRetrieveSOPClass(t *testing.T) {
	if !IsQueryRetrieveSOPClass(PatientRootQueryRetrieveFind) {
		t.Error("Patient Root Q/R Find should be a Q/R SOP class")
	}
	if !IsQueryRetrieveSOPClass(StudyRootQueryRetrieveGet) {
		t.Error("Study Root Q/R Get should be a Q/R SOP class")
	}
	if IsQueryRetrieveSOPClass(CTImageStorageSOP) {
		t.Error("CT Storage should not be a Q/R SOP class")
	}
}

func TestAllWorklistSOPClassUIDs(t *testing.T) {
	uids := AllWorklistSOPClassUIDs()
	if len(uids) != 1 {
		t.Errorf("expected 1 worklist SOP class, got %d", len(uids))
	}
	if uids[0] != ModalityWorklistInformationModelFindUID {
		t.Errorf("expected MWL Find UID, got %s", uids[0])
	}
}

func TestUncompressedTransferSyntaxUIDs(t *testing.T) {
	uids := UncompressedTransferSyntaxUIDs()
	if len(uids) != 3 {
		t.Errorf("expected 3 uncompressed transfer syntaxes, got %d", len(uids))
	}
}
