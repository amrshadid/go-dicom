package uid_test

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/uid"
)

// TestNewUID tests creating a new UID.
func TestNewUID(t *testing.T) {
	u := uid.New("1.2.840.10008.1.1")
	if u.String() != "1.2.840.10008.1.1" {
		t.Errorf("uid.New() returned %s, want 1.2.840.10008.1.1", u.String())
	}
}

// TestUIDIsEmpty tests the IsEmpty method.
func TestUIDIsEmpty(t *testing.T) {
	tests := []struct {
		u         uid.UID
		wantEmpty bool
	}{
		{uid.New(""), true},
		{uid.New("1.2.840.10008.1.1"), false},
	}

	for _, tt := range tests {
		if tt.u.IsEmpty() != tt.wantEmpty {
			t.Errorf("IsEmpty() = %v, want %v", tt.u.IsEmpty(), tt.wantEmpty)
		}
	}
}

// TestUIDIsValid tests UID format validation.
func TestUIDIsValid(t *testing.T) {
	tests := []struct {
		uid       string
		wantValid bool
	}{
		{"1.2.840.10008.1.1", true},
		{"1.2.840.10008.1.2", true},
		{"1.2.840.10008.5.1.4.1.1.2", true},
		{"", false},
		{"1", false},
		{"1.2..3", false},
		{"1.2.a.3", false},
		{"1.a.3", false},
		{".1.2.3", false},
		{"1.2.3.", false},
	}

	for _, tt := range tests {
		u := uid.New(tt.uid)
		if u.IsValid() != tt.wantValid {
			t.Errorf("IsValid(%s) = %v, want %v", tt.uid, u.IsValid(), tt.wantValid)
		}
	}
}

// TestUIDEquals tests UID equality.
func TestUIDEquals(t *testing.T) {
	u1 := uid.New("1.2.840.10008.1.1")
	u2 := uid.New("1.2.840.10008.1.1")
	u3 := uid.New("1.2.840.10008.1.2")

	if !u1.Equals(u2) {
		t.Error("Equals should return true for identical UIDs")
	}

	if u1.Equals(u3) {
		t.Error("Equals should return false for different UIDs")
	}
}

// TestUIDInfo tests retrieving UID information.
func TestUIDInfo(t *testing.T) {
	u := uid.New("1.2.840.10008.1.1")
	info := u.Info()

	if info == nil {
		t.Fatal("Info() returned nil for known UID")
	}

	if info.Name != "Verification SOP Class" {
		t.Errorf("Info.Name = %s, want 'Verification SOP Class'", info.Name)
	}

	if info.Type != "SOPClass" {
		t.Errorf("Info.Type = %s, want 'SOPClass'", info.Type)
	}

	if info.IsRetired {
		t.Error("Info.IsRetired should be false")
	}
}

// TestGetUIDInfo tests the GetUIDInfo function.
func TestGetUIDInfo(t *testing.T) {
	info := uid.GetUIDInfo("1.2.840.10008.1.2")

	if info == nil {
		t.Fatal("GetUIDInfo returned nil for known UID")
	}

	if info.UID != "1.2.840.10008.1.2" {
		t.Errorf("GetUIDInfo UID = %s, want 1.2.840.10008.1.2", info.UID)
	}

	if info.Name != "Implicit VR Little Endian" {
		t.Errorf("GetUIDInfo Name = %s, want 'Implicit VR Little Endian'", info.Name)
	}

	unknownInfo := uid.GetUIDInfo("1.2.3.4.5.6.7.8.9")
	if unknownInfo != nil {
		t.Error("GetUIDInfo should return nil for unknown UID")
	}
}

// TestIsTransferSyntax tests the IsTransferSyntax function.
func TestIsTransferSyntax(t *testing.T) {
	tests := []struct {
		uid                string
		wantTransferSyntax bool
	}{
		{"1.2.840.10008.1.2", true},          // Implicit VR Little Endian
		{"1.2.840.10008.1.2.1", true},        // Explicit VR Little Endian
		{"1.2.840.10008.1.2.2", true},        // Explicit VR Big Endian
		{"1.2.840.10008.1.2.4.50", true},     // JPEG Baseline
		{"1.2.840.10008.1.1", false},         // Verification SOP Class
		{"1.2.840.10008.5.1.4.1.1.2", false}, // CR Image Storage
		{"1.2.3.4.5.6.7.8.9", false},         // Unknown UID
	}

	for _, tt := range tests {
		got := uid.IsTransferSyntax(tt.uid)
		if got != tt.wantTransferSyntax {
			t.Errorf("uid.IsTransferSyntax(%s) = %v, want %v", tt.uid, got, tt.wantTransferSyntax)
		}
	}
}

// TestIsSOPClass tests the IsSOPClass function.
func TestIsSOPClass(t *testing.T) {
	tests := []struct {
		uid          string
		wantSOPClass bool
	}{
		{"1.2.840.10008.1.1", true},           // Verification SOP Class
		{"1.2.840.10008.5.1.4.1.1.2.1", true}, // CT Image Storage
		{"1.2.840.10008.1.2", false},          // Transfer Syntax
		{"1.2.3.4.5.6.7.8.9", false},          // Unknown UID
	}

	for _, tt := range tests {
		got := uid.IsSOPClass(tt.uid)
		if got != tt.wantSOPClass {
			t.Errorf("uid.IsSOPClass(%s) = %v, want %v", tt.uid, got, tt.wantSOPClass)
		}
	}
}

// TestAllUIDs tests the AllUIDs function.
func TestAllUIDs(t *testing.T) {
	allUIDs := uid.AllUIDs()

	if len(allUIDs) == 0 {
		t.Fatal("AllUIDs returned empty slice")
	}

	// Check that they are sorted
	for i := 1; i < len(allUIDs); i++ {
		if allUIDs[i].String() < allUIDs[i-1].String() {
			t.Error("AllUIDs should be sorted")
			break
		}
	}

	// Check that known UIDs are present
	found := false
	for _, u := range allUIDs {
		if u.String() == "1.2.840.10008.1.2" {
			found = true
			break
		}
	}

	if !found {
		t.Error("AllUIDs should contain 1.2.840.10008.1.2")
	}
}

// TestAllUIDInfos tests the AllUIDInfos function.
func TestAllUIDInfos(t *testing.T) {
	allInfos := uid.AllUIDInfos()

	if len(allInfos) == 0 {
		t.Fatal("AllUIDInfos returned empty slice")
	}

	// Check that they are sorted
	for i := 1; i < len(allInfos); i++ {
		if allInfos[i].UID < allInfos[i-1].UID {
			t.Error("AllUIDInfos should be sorted")
			break
		}
	}
}

// TestGetByName tests the GetByName function.
func TestGetByName(t *testing.T) {
	tests := []struct {
		name      string
		wantUID   string
		wantFound bool
	}{
		{"Verification SOP Class", "1.2.840.10008.1.1", true},
		{"verification sop class", "1.2.840.10008.1.1", true},
		{"VERIFICATION SOP CLASS", "1.2.840.10008.1.1", true},
		{"Implicit VR Little Endian", "1.2.840.10008.1.2", true},
		{"Nonexistent UID Name", "", false},
	}

	for _, tt := range tests {
		got := uid.GetByName(tt.name)
		if tt.wantFound {
			if got == nil {
				t.Errorf("uid.GetByName(%s) returned nil, want UID", tt.name)
			} else if got.String() != tt.wantUID {
				t.Errorf("uid.GetByName(%s) = %s, want %s", tt.name, got.String(), tt.wantUID)
			}
		} else {
			if got != nil {
				t.Errorf("uid.GetByName(%s) = %v, want nil", tt.name, got)
			}
		}
	}
}

// TestGetByType tests the GetByType function.
func TestGetByType(t *testing.T) {
	transferSyntaxes := uid.GetByType("TransferSyntax")
	if len(transferSyntaxes) == 0 {
		t.Fatal("uid.GetByType('TransferSyntax') returned empty slice")
	}

	// All should be transfer syntaxes
	for _, u := range transferSyntaxes {
		if !uid.IsTransferSyntax(u.String()) {
			t.Errorf("uid.GetByType('TransferSyntax') returned non-transfer-syntax UID: %s", u.String())
		}
	}

	sopClasses := uid.GetByType("SOPClass")
	if len(sopClasses) == 0 {
		t.Fatal("uid.GetByType('SOPClass') returned empty slice")
	}

	// All should be SOP classes
	for _, u := range sopClasses {
		if !uid.IsSOPClass(u.String()) {
			t.Errorf("uid.GetByType('SOPClass') returned non-SOP-class UID: %s", u.String())
		}
	}
}

// TestLittleEndianTransferSyntaxes tests the LittleEndianTransferSyntaxes function.
func TestLittleEndianTransferSyntaxes(t *testing.T) {
	syntaxes := uid.LittleEndianTransferSyntaxes()

	if len(syntaxes) != 2 {
		t.Errorf("uid.LittleEndianTransferSyntaxes() returned %d syntaxes, want 2", len(syntaxes))
	}

	expected := []string{"1.2.840.10008.1.2", "1.2.840.10008.1.2.1"}
	for i, s := range syntaxes {
		if i < len(expected) && s.String() != expected[i] {
			t.Errorf("uid.LittleEndianTransferSyntaxes()[%d] = %s, want %s", i, s.String(), expected[i])
		}
	}
}

// TestBigEndianTransferSyntax tests the BigEndianTransferSyntax function.
func TestBigEndianTransferSyntax(t *testing.T) {
	u := uid.BigEndianTransferSyntax()
	if u.String() != "1.2.840.10008.1.2.2" {
		t.Errorf("uid.BigEndianTransferSyntax() = %s, want 1.2.840.10008.1.2.2", u.String())
	}
}

// TestImplicitVRLittleEndian tests the ImplicitVRLittleEndian function.
func TestImplicitVRLittleEndian(t *testing.T) {
	u := uid.ImplicitVRLittleEndian()
	if u.String() != "1.2.840.10008.1.2" {
		t.Errorf("uid.ImplicitVRLittleEndian() = %s, want 1.2.840.10008.1.2", u.String())
	}
}

// TestExplicitVRLittleEndian tests the ExplicitVRLittleEndian function.
func TestExplicitVRLittleEndian(t *testing.T) {
	u := uid.ExplicitVRLittleEndian()
	if u.String() != "1.2.840.10008.1.2.1" {
		t.Errorf("uid.ExplicitVRLittleEndian() = %s, want 1.2.840.10008.1.2.1", u.String())
	}
}

// TestCompressedTransferSyntaxes tests the CompressedTransferSyntaxes function.
func TestCompressedTransferSyntaxes(t *testing.T) {
	syntaxes := uid.CompressedTransferSyntaxes()

	if len(syntaxes) == 0 {
		t.Fatal("CompressedTransferSyntaxes returned empty slice")
	}

	// All should be recognized as compressed
	for _, u := range syntaxes {
		if !uid.IsCompressed(u) {
			t.Errorf("uid.IsCompressed(%s) = false, want true", u.String())
		}
	}
}

// TestIsCompressed tests the IsCompressed function.
func TestIsCompressed(t *testing.T) {
	tests := []struct {
		uid            string
		wantCompressed bool
	}{
		{"1.2.840.10008.1.2", false},     // Implicit VR Little Endian
		{"1.2.840.10008.1.2.1", false},   // Explicit VR Little Endian
		{"1.2.840.10008.1.2.4.50", true}, // JPEG Baseline
		{"1.2.840.10008.1.2.4.70", true}, // JPEG Lossless
		{"1.2.840.10008.1.2.5", true},    // RLE Lossless
	}

	for _, tt := range tests {
		u := uid.New(tt.uid)
		got := uid.IsCompressed(u)
		if got != tt.wantCompressed {
			t.Errorf("uid.IsCompressed(%s) = %v, want %v", tt.uid, got, tt.wantCompressed)
		}
	}
}

// TestIsLossless tests the IsLossless function.
func TestIsLossless(t *testing.T) {
	tests := []struct {
		uid          string
		wantLossless bool
	}{
		{"1.2.840.10008.1.2", true},       // Implicit VR Little Endian
		{"1.2.840.10008.1.2.1", true},     // Explicit VR Little Endian
		{"1.2.840.10008.1.2.2", true},     // Explicit VR Big Endian
		{"1.2.840.10008.1.2.4.50", false}, // JPEG Baseline (lossy)
		{"1.2.840.10008.1.2.4.51", false}, // JPEG Extended (lossy)
		{"1.2.840.10008.1.2.4.70", true},  // JPEG Lossless
		{"1.2.840.10008.1.2.5", true},     // RLE Lossless
	}

	for _, tt := range tests {
		u := uid.New(tt.uid)
		got := uid.IsLossless(u)
		if got != tt.wantLossless {
			t.Errorf("uid.IsLossless(%s) = %v, want %v", tt.uid, got, tt.wantLossless)
		}
	}
}

// TestSupportsMultipleFrames tests the SupportsMultipleFrames function.
func TestSupportsMultipleFrames(t *testing.T) {
	u := uid.New("1.2.840.10008.1.2")
	if !uid.SupportsMultipleFrames(u) {
		t.Error("SupportsMultipleFrames should return true for transfer syntax")
	}

	u2 := uid.New("1.2.3.4.5")
	if uid.SupportsMultipleFrames(u2) {
		t.Error("SupportsMultipleFrames should return false for unknown UID")
	}
}

// TestVerificationSOPClass tests the VerificationSOPClass function.
func TestVerificationSOPClass(t *testing.T) {
	u := uid.VerificationSOPClass()
	if u.String() != "1.2.840.10008.1.1" {
		t.Errorf("uid.VerificationSOPClass() = %s, want 1.2.840.10008.1.1", u.String())
	}
}

// TestCTImageStorage tests the CTImageStorage function.
func TestCTImageStorage(t *testing.T) {
	u := uid.CTImageStorage()
	if u.String() != "1.2.840.10008.5.1.4.1.1.2.1" {
		t.Errorf("uid.CTImageStorage() = %s, want 1.2.840.10008.5.1.4.1.1.2.1", u.String())
	}
}

// TestMRImageStorage tests the MRImageStorage function.
func TestMRImageStorage(t *testing.T) {
	u := uid.MRImageStorage()
	if u.String() != "1.2.840.10008.5.1.4.1.1.4" {
		t.Errorf("uid.MRImageStorage() = %s, want 1.2.840.10008.5.1.4.1.1.4", u.String())
	}
}

// TestUIDWithSpaces tests UID parsing with spaces.
func TestUIDWithSpaces(t *testing.T) {
	u := uid.New(" 1.2.840.10008.1.1 ")
	if strings.TrimSpace(u.String()) != "1.2.840.10008.1.1" {
		t.Errorf("UID with spaces not handled properly: %s", u.String())
	}
}

// TestMultipleUIDOperations tests using multiple UIDs together.
func TestMultipleUIDOperations(t *testing.T) {
	u1 := uid.New("1.2.840.10008.1.2")
	u2 := uid.New("1.2.840.10008.1.2.1")
	u3 := uid.New("1.2.840.10008.1.2")

	if !uid.IsTransferSyntax(u1.String()) {
		t.Error("IsTransferSyntax failed for Implicit VR")
	}

	if !uid.IsTransferSyntax(u2.String()) {
		t.Error("IsTransferSyntax failed for Explicit VR")
	}

	if !u1.Equals(u3) {
		t.Error("UIDs should be equal")
	}

	if u1.Equals(u2) {
		t.Error("UIDs should not be equal")
	}
}
