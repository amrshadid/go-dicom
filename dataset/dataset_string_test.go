package dataset_test

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// TestFormatString tests formatted string output
func TestFormatString(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "John Doe")
	ds.SetStringByKeyword("PatientID", "12345")

	opts := dataset.DefaultStringFormatOptions()
	str := ds.FormatString(opts)

	if str == "" {
		t.Error("FormatString() returned empty string")
	}

	if !strings.Contains(str, "PatientName") {
		t.Error("FormatString() should contain PatientName")
	}
}

// TestPrettyString tests pretty string representation
func TestPrettyString(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Jane Smith")
	ds.SetStringByKeyword("Modality", "CT")

	str := ds.PrettyString()

	if str == "" {
		t.Error("PrettyString() returned empty string")
	}

	// Should be multi-line
	if !strings.Contains(str, "\n") {
		t.Error("PrettyString() should contain newlines")
	}
}

// TestCompactString tests compact string representation
func TestCompactString(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Test")

	str := ds.CompactString()

	if str == "" {
		t.Error("CompactString() returned empty string")
	}

	// Should be single line
	lines := strings.Split(str, "\n")
	if len(lines) > 1 {
		t.Error("CompactString() should be single line")
	}
}

// TestSummaryString tests summary representation
func TestSummaryString(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Summary Test")
	ds.SetStringByKeyword("StudyDescription", "Test Study")
	ds.SetStringByKeyword("Modality", "MR")

	str := ds.SummaryString()

	if !strings.Contains(str, "Summary Test") {
		t.Error("SummaryString() should contain patient name")
	}

	if !strings.Contains(str, "Elements:") {
		t.Error("SummaryString() should contain element count")
	}
}

// TestTreeString tests tree representation
func TestTreeString(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Tree Test")

	// Add a sequence
	seq := sequence.New()
	child := dataset.NewDataset()
	child.SetStringByKeyword("SeriesNumber", "1")
	seq.Append(child)
	ds.AddSequence(tag.New(0x0008, 0x1115), seq)

	str := ds.TreeString()

	if !strings.Contains(str, "├─") && !strings.Contains(str, "└─") {
		t.Error("TreeString() should contain tree characters")
	}
}

// TestDebugString tests debug representation
func TestDebugString(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Debug Test")

	str := ds.DebugString()

	if !strings.Contains(str, "Debug Info") {
		t.Error("DebugString() should contain 'Debug Info'")
	}

	if !strings.Contains(str, "Statistics") {
		t.Error("DebugString() should contain statistics")
	}
}

// TestStringWithSequences tests string representation with sequences
func TestStringWithSequences(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Seq Test")

	// Add sequence
	seq := sequence.New()
	for i := 0; i < 3; i++ {
		child := dataset.NewDataset()
		child.SetStringByKeyword("SeriesNumber", string(rune('1'+i)))
		seq.Append(child)
	}
	ds.AddSequence(tag.New(0x0008, 0x1115), seq)

	str := ds.PrettyString()

	if !strings.Contains(str, "Sequence") {
		t.Error("String should mention sequence")
	}
}

// TestStringFormatOptions tests string formatting options
func TestStringFormatOptions(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Options Test")

	// Test with different options
	opts := dataset.StringFormatOptions{
		ShowValues:      false,
		ShowHierarchy:   false,
		Compact:         false,
		ShowPrivateTags: false,
	}

	str := ds.FormatString(opts)
	if str == "" {
		t.Error("FormatString() returned empty string")
	}
}

// TestElementList tests element list formatting
func TestElementList(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "List Test")
	ds.SetStringByKeyword("PatientID", "00001")

	str := ds.ElementList()

	if str == "" {
		t.Error("ElementList() returned empty string")
	}
}

// TestStringPrivateTags tests string representation with private tags
func TestStringPrivateTags(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Private Test")

	// Add private tag
	pb, _ := ds.PrivateBlock(0x0009, "TEST_VENDOR")
	pb.AddNew(0x01, dataelem.LO, []byte("Private Data"))

	// With private tags
	opts := dataset.DefaultStringFormatOptions()
	opts.ShowPrivateTags = true
	str := ds.FormatString(opts)

	if !strings.Contains(str, "0009") {
		t.Error("String with ShowPrivateTags=true should show private tags")
	}

	// Without private tags
	opts.ShowPrivateTags = false
	str2 := ds.FormatString(opts)

	if strings.Contains(str2, "0009") {
		t.Error("String with ShowPrivateTags=false should not show private tags")
	}
}
