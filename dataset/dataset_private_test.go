package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestPrivateBlockCreate tests creating a private block
func TestPrivateBlockCreate(t *testing.T) {
	ds := dataset.NewDataset()

	// Create a private block for GE
	pb, err := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")
	if err != nil {
		t.Fatalf("PrivateBlock() error = %v", err)
	}

	if pb == nil {
		t.Fatal("PrivateBlock() returned nil")
	}

	if pb.Group() != 0x0009 {
		t.Errorf("Group() = 0x%04X, want 0x0009", pb.Group())
	}

	if pb.Creator() != "GEMS_ACQU_01" {
		t.Errorf("Creator() = %q, want \"GEMS_ACQU_01\"", pb.Creator())
	}

	// Verify creator was registered
	if !ds.HasPrivateCreator(0x0009, "GEMS_ACQU_01") {
		t.Error("Private creator not registered in dataset")
	}
}

// TestPrivateBlockNonPrivateGroup tests error on non-private group
func TestPrivateBlockNonPrivateGroup(t *testing.T) {
	ds := dataset.NewDataset()

	// Try to create private block in even group (should fail)
	_, err := ds.PrivateBlock(0x0008, "TEST")
	if err == nil {
		t.Error("PrivateBlock() should return error for even group")
	}
}

// TestPrivateBlockEmptyCreator tests error on empty creator string
func TestPrivateBlockEmptyCreator(t *testing.T) {
	ds := dataset.NewDataset()

	_, err := ds.PrivateBlock(0x0009, "")
	if err == nil {
		t.Error("PrivateBlock() should return error for empty creator")
	}
}

// TestPrivateBlockReuseCreator tests reusing existing creator
func TestPrivateBlockReuseCreator(t *testing.T) {
	ds := dataset.NewDataset()

	// Create first block
	pb1, err := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")
	if err != nil {
		t.Fatalf("PrivateBlock() error = %v", err)
	}

	// Create second reference to same creator
	pb2, err := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")
	if err != nil {
		t.Fatalf("PrivateBlock() error = %v", err)
	}

	// Should have same block number
	if pb1.Block() != pb2.Block() {
		t.Errorf("Block numbers differ: %d vs %d", pb1.Block(), pb2.Block())
	}
}

// TestPrivateBlockMultipleCreators tests multiple creators in same group
func TestPrivateBlockMultipleCreators(t *testing.T) {
	ds := dataset.NewDataset()

	// Create multiple private blocks in same group
	pb1, _ := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")
	pb2, _ := ds.PrivateBlock(0x0009, "SIEMENS MR HEADER")
	pb3, _ := ds.PrivateBlock(0x0009, "PHILIPS MR IMAGING DD 001")

	// Should have different block numbers
	if pb1.Block() == pb2.Block() || pb2.Block() == pb3.Block() || pb1.Block() == pb3.Block() {
		t.Error("Different creators should have different block numbers")
	}

	// All should be in valid range (0x10-0xFF)
	// Note: block is uint8, so it's automatically <= 0xFF
	for _, block := range []uint8{pb1.Block(), pb2.Block(), pb3.Block()} {
		if block < 0x10 {
			t.Errorf("Block 0x%02X below minimum 0x10", block)
		}
	}
}

// TestPrivateBlockAddNew tests adding private elements
func TestPrivateBlockAddNew(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")

	// Add private element at offset 0x01
	err := pb.AddNew(0x01, dataelem.LO, []byte("Test Value"))
	if err != nil {
		t.Fatalf("AddNew() error = %v", err)
	}

	// Verify element exists
	if !pb.Contains(0x01) {
		t.Error("Private element not added")
	}
}

// TestPrivateBlockAddReservedOffset tests error on offset 0x00
func TestPrivateBlockAddReservedOffset(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")

	err := pb.AddNew(0x00, dataelem.LO, []byte("Test"))
	if err == nil {
		t.Error("AddNew() should return error for offset 0x00")
	}
}

// TestPrivateBlockGet tests retrieving private elements
func TestPrivateBlockGet(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")

	// Add element
	pb.AddNew(0x01, dataelem.LO, []byte("Test Value"))

	// Retrieve element
	elem, exists := pb.Get(0x01)
	if !exists {
		t.Fatal("Get() returned false for existing element")
	}

	if elem == nil {
		t.Fatal("Get() returned nil element")
	}

	value := elem.GetValue()
	if b, ok := value.([]byte); ok {
		if string(b) != "Test Value" {
			t.Errorf("Value = %q, want \"Test Value\"", string(b))
		}
	} else {
		t.Error("Value is not a byte slice")
	}
}

// TestPrivateBlockSetValue tests setting private element values
func TestPrivateBlockSetValue(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")

	// Set value (creates element if not exists)
	err := pb.SetValue(0x05, []byte("Initial"))
	if err != nil {
		t.Fatalf("SetValue() error = %v", err)
	}

	// Verify value
	value := pb.GetValue(0x05)
	if string(value) != "Initial" {
		t.Errorf("GetValue() = %q, want \"Initial\"", string(value))
	}

	// Update value
	err = pb.SetValue(0x05, []byte("Updated"))
	if err != nil {
		t.Fatalf("SetValue() error = %v", err)
	}

	// Verify updated value
	value = pb.GetValue(0x05)
	if string(value) != "Updated" {
		t.Errorf("GetValue() = %q, want \"Updated\"", string(value))
	}
}

// TestPrivateBlockRemove tests removing private elements
func TestPrivateBlockRemove(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")

	// Add element
	pb.AddNew(0x02, dataelem.LO, []byte("To Remove"))

	// Verify exists
	if !pb.Contains(0x02) {
		t.Fatal("Element not added")
	}

	// Remove element
	removed := pb.Remove(0x02)
	if !removed {
		t.Error("Remove() returned false")
	}

	// Verify removed
	if pb.Contains(0x02) {
		t.Error("Element still exists after removal")
	}
}

// TestPrivateBlockGetTag tests getting full tag from offset
func TestPrivateBlockGetTag(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")

	// Get tag for offset 0x10
	privateTag := pb.GetTag(0x10)

	// Verify group
	if privateTag.Group() != 0x0009 {
		t.Errorf("Tag group = 0x%04X, want 0x0009", privateTag.Group())
	}

	// Verify element incorporates block number
	// Element should be (block << 8) | offset
	expectedElement := (uint16(pb.Block()) << 8) | 0x0010
	if privateTag.Element() != expectedElement {
		t.Errorf("Tag element = 0x%04X, want 0x%04X", privateTag.Element(), expectedElement)
	}
}

// TestGetPrivateItem tests getting private element by creator
func TestGetPrivateItem(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0019, "SIEMENS MR HEADER")
	pb.AddNew(0x20, dataelem.LO, []byte("Siemens Data"))

	// Get element using creator
	elem, err := ds.GetPrivateItem(0x0019, 0x20, "SIEMENS MR HEADER")
	if err != nil {
		t.Fatalf("GetPrivateItem() error = %v", err)
	}

	if elem == nil {
		t.Fatal("GetPrivateItem() returned nil")
	}

	value := elem.GetValue()
	if b, ok := value.([]byte); ok {
		if string(b) != "Siemens Data" {
			t.Errorf("Value = %q, want \"Siemens Data\"", string(b))
		}
	}
}

// TestGetPrivateItemNonExistent tests error for non-existent creator
func TestGetPrivateItemNonExistent(t *testing.T) {
	ds := dataset.NewDataset()

	_, err := ds.GetPrivateItem(0x0019, 0x20, "NONEXISTENT")
	if err == nil {
		t.Error("GetPrivateItem() should return error for non-existent creator")
	}
}

// TestGetPrivateValue tests getting private value by creator
func TestGetPrivateValue(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0019, "SIEMENS MR HEADER")
	pb.AddNew(0x30, dataelem.LO, []byte("Value Data"))

	value, err := ds.GetPrivateValue(0x0019, 0x30, "SIEMENS MR HEADER")
	if err != nil {
		t.Fatalf("GetPrivateValue() error = %v", err)
	}

	if string(value) != "Value Data" {
		t.Errorf("Value = %q, want \"Value Data\"", string(value))
	}
}

// TestGetAllPrivateCreators tests retrieving all private creators
func TestGetAllPrivateCreators(t *testing.T) {
	ds := dataset.NewDataset()

	// Add multiple creators in different groups
	ds.PrivateBlock(0x0009, "GEMS_ACQU_01")
	ds.PrivateBlock(0x0009, "GEMS_RELA_01") //nolint:misspell
	ds.PrivateBlock(0x0019, "SIEMENS MR HEADER")

	creators := ds.GetAllPrivateCreators()

	// Should have 2 groups
	if len(creators) != 2 {
		t.Errorf("GetAllPrivateCreators() returned %d groups, want 2", len(creators))
	}

	// Group 0x0009 should have 2 creators
	if len(creators[0x0009]) != 2 {
		t.Errorf("Group 0x0009 has %d creators, want 2", len(creators[0x0009]))
	}

	// Group 0x0019 should have 1 creator
	if len(creators[0x0019]) != 1 {
		t.Errorf("Group 0x0019 has %d creators, want 1", len(creators[0x0019]))
	}
}

// TestGetPrivateElements tests getting all elements for a creator
func TestGetPrivateElements(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0019, "SIEMENS MR HEADER")

	// Add multiple private elements
	pb.AddNew(0x10, dataelem.LO, []byte("Element 1"))
	pb.AddNew(0x20, dataelem.LO, []byte("Element 2"))
	pb.AddNew(0x30, dataelem.LO, []byte("Element 3"))

	elements := ds.GetPrivateElements(0x0019, "SIEMENS MR HEADER")

	if len(elements) != 3 {
		t.Errorf("GetPrivateElements() returned %d elements, want 3", len(elements))
	}
}

// TestGetPrivateElementsNonExistent tests getting elements for non-existent creator
func TestGetPrivateElementsNonExistent(t *testing.T) {
	ds := dataset.NewDataset()

	elements := ds.GetPrivateElements(0x0019, "NONEXISTENT")
	if elements != nil {
		t.Error("GetPrivateElements() should return nil for non-existent creator")
	}
}

// TestRemovePrivateBlock tests removing entire private block
func TestRemovePrivateBlock(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0019, "SIEMENS MR HEADER")

	// Add elements
	pb.AddNew(0x10, dataelem.LO, []byte("Element 1"))
	pb.AddNew(0x20, dataelem.LO, []byte("Element 2"))

	// Verify elements exist
	if !pb.Contains(0x10) || !pb.Contains(0x20) {
		t.Fatal("Elements not added")
	}

	// Remove entire block
	err := ds.RemovePrivateBlock(0x0019, "SIEMENS MR HEADER")
	if err != nil {
		t.Fatalf("RemovePrivateBlock() error = %v", err)
	}

	// Verify creator and elements removed
	if ds.HasPrivateCreator(0x0019, "SIEMENS MR HEADER") {
		t.Error("Private creator still exists after removal")
	}

	// Try to get private block (should create new one since old was removed)
	pb2, err := ds.PrivateBlock(0x0019, "SIEMENS MR HEADER")
	if err != nil {
		t.Fatalf("PrivateBlock() error = %v", err)
	}

	// Old elements should not exist
	if pb2.Contains(0x10) || pb2.Contains(0x20) {
		t.Error("Old elements still exist after block removal")
	}
}

// TestGetAllPrivateTags tests getting all private tags
func TestGetAllPrivateTags(t *testing.T) {
	ds := dataset.NewDataset()

	// Add some standard tags
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe")))

	// Add private tags
	pb1, _ := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")
	pb1.AddNew(0x10, dataelem.LO, []byte("Private 1"))
	pb1.AddNew(0x20, dataelem.LO, []byte("Private 2"))

	pb2, _ := ds.PrivateBlock(0x0019, "SIEMENS MR HEADER")
	pb2.AddNew(0x15, dataelem.LO, []byte("Private 3"))

	privateTags := ds.GetAllPrivateTags()

	// Should have: 2 creators + 3 data elements = 5 private tags
	if len(privateTags) != 5 {
		t.Errorf("GetAllPrivateTags() returned %d tags, want 5", len(privateTags))
	}

	// Verify all are private
	for _, privateTag := range privateTags {
		if !privateTag.IsPrivate() {
			t.Errorf("Tag %s is not private", privateTag.String())
		}
	}
}

// TestPrivateBlockString tests string representation
func TestPrivateBlockString(t *testing.T) {
	ds := dataset.NewDataset()
	pb, _ := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")

	str := pb.String()
	if str == "" {
		t.Error("String() returned empty string")
	}

	// Should contain group, creator, and block info
	if !contains(str, "0009") || !contains(str, "GEMS_ACQU_01") {
		t.Errorf("String() = %q, should contain group and creator", str)
	}
}

// TestPrivateBlockFullScenario tests realistic private tag usage
func TestPrivateBlockFullScenario(t *testing.T) {
	ds := dataset.NewDataset()

	// Simulate GE scanner adding private data
	geBlock, err := ds.PrivateBlock(0x0009, "GEMS_ACQU_01")
	if err != nil {
		t.Fatalf("Failed to create GE private block: %v", err)
	}

	geBlock.AddNew(0x01, dataelem.LO, []byte("AcquisitionDate"))
	geBlock.AddNew(0x02, dataelem.LO, []byte("AcquisitionTime"))
	geBlock.AddNew(0x10, dataelem.LO, []byte("ScanOptions"))

	// Simulate Siemens scanner adding private data
	siemensBlock, err := ds.PrivateBlock(0x0019, "SIEMENS MR HEADER")
	if err != nil {
		t.Fatalf("Failed to create Siemens private block: %v", err)
	}

	siemensBlock.AddNew(0x10, dataelem.LO, []byte("MagneticFieldStrength"))
	siemensBlock.AddNew(0x20, dataelem.LO, []byte("FlipAngle"))

	// Verify all private creators
	allCreators := ds.GetAllPrivateCreators()
	if len(allCreators) != 2 {
		t.Errorf("Expected 2 groups with private creators, got %d", len(allCreators))
	}

	// Get specific private value
	scanOptions, err := ds.GetPrivateValue(0x0009, 0x10, "GEMS_ACQU_01")
	if err != nil {
		t.Fatalf("Failed to get private value: %v", err)
	}
	if string(scanOptions) != "ScanOptions" {
		t.Errorf("Got %q, want \"ScanOptions\"", string(scanOptions))
	}

	// Get all Siemens private elements
	siemensElems := ds.GetPrivateElements(0x0019, "SIEMENS MR HEADER")
	if len(siemensElems) != 2 {
		t.Errorf("Expected 2 Siemens elements, got %d", len(siemensElems))
	}

	// Remove GE block
	err = ds.RemovePrivateBlock(0x0009, "GEMS_ACQU_01")
	if err != nil {
		t.Fatalf("Failed to remove GE block: %v", err)
	}

	// Verify Siemens data still exists
	if !ds.HasPrivateCreator(0x0019, "SIEMENS MR HEADER") {
		t.Error("Siemens creator should still exist")
	}

	// Verify GE data removed
	if ds.HasPrivateCreator(0x0009, "GEMS_ACQU_01") {
		t.Error("GE creator should be removed")
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && hasSubstring(s, substr))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
