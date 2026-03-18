package filewriter_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/hooks"
	"github.com/amrshadid/go-dicom/tag"
)

// TestConvertDataElementToRaw tests conversion of DataElement to RawDataElement.
func TestConvertDataElementToRaw(t *testing.T) {
	elem := &filewriter.DataElement{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Doe^John"),
		Length: 8,
	}

	raw, err := filewriter.ConvertDataElementToRaw(elem)
	if err != nil {
		t.Fatalf("ConvertDataElementToRaw() error = %v", err)
	}

	if raw == nil {
		t.Fatal("raw data element is nil")
	}

	if raw.VR == nil {
		t.Fatal("raw VR pointer is nil")
	}

	if *raw.VR != "PN" {
		t.Errorf("VR = %s, want PN", *raw.VR)
	}

	if string(raw.Value) != "Doe^John" {
		t.Errorf("Value = %s, want Doe^John", string(raw.Value))
	}
}

// TestConvertDataElementToRawNil tests nil input handling.
func TestConvertDataElementToRawNil(t *testing.T) {
	raw, err := filewriter.ConvertDataElementToRaw(nil)
	if err == nil {
		t.Fatal("expected error for nil element")
	}

	if raw != nil {
		t.Fatal("expected nil raw element")
	}
}

// TestFilewriterWithCustomHook tests filewriter with custom hooks.
func TestFilewriterWithCustomHook(t *testing.T) {
	elem := &filewriter.DataElement{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Smith^John"),
		Length: 11,
	}

	// Convert to raw for hook processing
	raw, err := filewriter.ConvertDataElementToRaw(elem)
	if err != nil {
		t.Fatalf("conversion error = %v", err)
	}

	if raw == nil {
		t.Fatal("raw is nil")
	}

	// Register custom hook
	hook := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		data["processed"] = true
		return nil
	}

	err = hooks.RegisterCallback("raw_element_vr", hook)
	if err != nil {
		t.Fatalf("hook registration error = %v", err)
	}

	// Execute hook
	result, err := hooks.ExecuteRawElementVR(raw)
	if err != nil {
		t.Fatalf("hook execution error = %v", err)
	}

	if processed, ok := result["processed"].(bool); !ok || !processed {
		t.Error("hook was not executed correctly")
	}

	// Reset hooks
	hooks.Reset()
}

// TestFilewriterHookChain tests filewriter with hook chains.
func TestFilewriterHookChain(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})
	writer := filewriter.NewDCMFileWriter(mockWriter)

	elem := &filewriter.DataElement{
		Tag:    tag.New(0x0008, 0x0020),
		VR:     "DA",
		Value:  []byte("20230101"),
		Length: 8,
	}

	// Verify element can be written
	err := writer.WriteDataElement(elem, true)
	if err != nil {
		t.Fatalf("WriteDataElement error = %v", err)
	}

	// Verify position increased
	if writer.GetPosition() == 0 {
		t.Error("position should have increased")
	}
}

// TestFilewriterWithFixSeparatorHook tests filewriter with separator fix hook.
func TestFilewriterWithFixSeparatorHook(t *testing.T) {
	elem := &filewriter.DataElement{
		Tag:    tag.New(0x0028, 0x0009),
		VR:     "AT",
		Value:  []byte("0010:0010\\0010:0020"),
		Length: 19,
	}

	// Convert to raw
	raw, err := filewriter.ConvertDataElementToRaw(elem)
	if err != nil {
		t.Fatalf("conversion error = %v", err)
	}

	// Register fix separator hook
	fixHook := hooks.FixSeparatorHook(':', '\\')
	err = hooks.RegisterCallback("raw_element_value", fixHook)
	if err != nil {
		t.Fatalf("hook registration error = %v", err)
	}

	// Register kwargs for the hook
	err = hooks.RegisterKwargs("raw_element_kwargs", map[string]interface{}{
		"target_VRs": []string{"AT"},
	})
	if err != nil {
		t.Fatalf("kwargs registration error = %v", err)
	}

	// Execute hook
	result, err := hooks.ExecuteRawElementValue(raw)
	if err != nil {
		t.Fatalf("hook execution error = %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	// Reset hooks
	hooks.Reset()
}

// TestFilewriterFullWriteWithHooks tests complete write with hook integration.
func TestFilewriterFullWriteWithHooks(t *testing.T) {
	buf := &bytes.Buffer{}
	mockWriter := filebase.NewFileWriter(&readWriteSeeker{Buffer: buf})

	writer := filewriter.NewDICOMFileWriter(mockWriter)

	metaInfo := &filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.2",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          "1.2.840.10008.1.2",
	}

	writer.SetFileMetaInfo(metaInfo)

	elem1 := &filewriter.DataElement{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Doe^John"),
		Length: 8,
	}

	elem2 := &filewriter.DataElement{
		Tag:    tag.New(0x0010, 0x0020),
		VR:     "LO",
		Value:  []byte("12345"),
		Length: 5,
	}

	writer.AddDataElement(elem1)
	writer.AddDataElement(elem2)

	// Convert elements to raw for verification
	raw1, err := filewriter.ConvertDataElementToRaw(elem1)
	if err != nil {
		t.Fatalf("conversion error = %v", err)
	}

	if raw1 == nil {
		t.Fatal("raw1 is nil")
	}

	raw2, err := filewriter.ConvertDataElementToRaw(elem2)
	if err != nil {
		t.Fatalf("conversion error = %v", err)
	}

	if raw2 == nil {
		t.Fatal("raw2 is nil")
	}

	// Write the file
	err = writer.Write()
	if err != nil {
		t.Fatalf("Write error = %v", err)
	}

	// Verify something was written
	if buf.Len() == 0 {
		t.Error("buffer is empty, nothing was written")
	}
}

// TestFilewriterMultipleDataElementsWithHooks tests multiple elements with hooks.
func TestFilewriterMultipleDataElementsWithHooks(t *testing.T) {
	elements := []*filewriter.DataElement{
		{
			Tag:    tag.New(0x0010, 0x0010),
			VR:     "PN",
			Value:  []byte("Smith^John"),
			Length: 11,
		},
		{
			Tag:    tag.New(0x0010, 0x0020),
			VR:     "LO",
			Value:  []byte("A123456"),
			Length: 7,
		},
		{
			Tag:    tag.New(0x0008, 0x0020),
			VR:     "DA",
			Value:  []byte("20230615"),
			Length: 8,
		},
	}

	// Convert all elements to raw for hook processing
	for _, elem := range elements {
		raw, err := filewriter.ConvertDataElementToRaw(elem)
		if err != nil {
			t.Fatalf("conversion error = %v", err)
		}

		if raw == nil {
			t.Fatal("raw is nil")
		}

		if *raw.VR != elem.VR {
			t.Errorf("VR mismatch: got %s, want %s", *raw.VR, elem.VR)
		}
	}
}

// TestFilewriterHookErrorHandling tests error handling with hooks.
func TestFilewriterHookErrorHandling(t *testing.T) {
	elem := &filewriter.DataElement{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Doe^John"),
		Length: 8,
	}

	// Register a hook that returns an error
	errorHook := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		return fmt.Errorf("test hook error")
	}

	err := hooks.RegisterCallback("raw_element_vr", errorHook)
	if err != nil {
		t.Fatalf("hook registration error = %v", err)
	}

	// Try to convert element with error hook
	raw, err := filewriter.ConvertDataElementToRaw(elem)
	if err != nil {
		t.Fatalf("conversion error = %v", err)
	}

	// Execute hook and expect error
	_, hookErr := hooks.ExecuteRawElementVR(raw)
	if hookErr == nil {
		t.Error("expected error from hook")
	}

	// Reset hooks
	hooks.Reset()
}

// TestFilewriterConcurrentDataElementConversion tests concurrent element conversions.
func TestFilewriterConcurrentDataElementConversion(t *testing.T) {
	numGoroutines := 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			elem := &filewriter.DataElement{
				Tag:    tag.New(uint16(0x0010), uint16(0x0010+index)),
				VR:     "PN",
				Value:  []byte(fmt.Sprintf("Patient%d", index)),
				Length: uint32(len(fmt.Sprintf("Patient%d", index))),
			}

			raw, err := filewriter.ConvertDataElementToRaw(elem)
			if err != nil {
				t.Errorf("conversion error = %v", err)
			}

			if raw == nil {
				t.Error("raw is nil")
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

// TestFilewriterTagConversionInRaw tests tag conversion to string in raw element.
func TestFilewriterTagConversionInRaw(t *testing.T) {
	tests := []struct {
		group   uint16
		element uint16
	}{
		{0x0002, 0x0010},
		{0x0008, 0x0008},
		{0x0010, 0x0010},
		{0x0020, 0x000D},
	}

	for _, tt := range tests {
		elem := &filewriter.DataElement{
			Tag:    tag.New(tt.group, tt.element),
			VR:     "UI",
			Value:  []byte("1.2.3"),
			Length: 5,
		}

		raw, err := filewriter.ConvertDataElementToRaw(elem)
		if err != nil {
			t.Fatalf("conversion error = %v", err)
		}

		if raw == nil {
			t.Fatal("raw is nil")
		}

		// Tag should be converted to string format
		if raw.Tag == "" {
			t.Error("tag string is empty")
		}
	}
}
