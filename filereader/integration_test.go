package filereader_test

import (
	"fmt"
	"testing"

	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/hooks"
	"github.com/amrshadid/go-dicom/tag"
)

// TestConvertDataElementValueToRaw tests conversion of DataElementValue to RawDataElement.
func TestConvertDataElementValueToRaw(t *testing.T) {
	elem := &filereader.DataElementValue{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Doe^John"),
		Length: 8,
	}

	raw, err := filereader.ConvertRawDataElement(elem, "utf-8")
	if err != nil {
		t.Fatalf("ConvertRawDataElement() error = %v", err)
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

// TestConvertDataElementValueToRawNil tests nil input handling.
func TestConvertDataElementValueToRawNil(t *testing.T) {
	raw, err := filereader.ConvertRawDataElement(nil, "utf-8")
	if err == nil {
		t.Fatal("expected error for nil element")
	}

	if raw != nil {
		t.Fatal("expected nil raw element")
	}
}

// TestFilereaderWithCustomHook tests filereader with custom hooks.
func TestFilereaderWithCustomHook(t *testing.T) {
	elem := &filereader.DataElementValue{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Smith^John"),
		Length: 11,
	}

	// Convert to raw for hook processing
	raw, err := filereader.ConvertRawDataElement(elem, "utf-8")
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

// TestFilereaderWithFixSeparatorHook tests filereader with separator fix hook.
func TestFilereaderWithFixSeparatorHook(t *testing.T) {
	elem := &filereader.DataElementValue{
		Tag:    tag.New(0x0028, 0x0009),
		VR:     "AT",
		Value:  []byte("0010:0010\\0010:0020"),
		Length: 19,
	}

	// Convert to raw
	raw, err := filereader.ConvertRawDataElement(elem, "utf-8")
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

// TestFilereaderMultipleDataElementsWithHooks tests multiple elements with hooks.
func TestFilereaderMultipleDataElementsWithHooks(t *testing.T) {
	elements := []*filereader.DataElementValue{
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
		raw, err := filereader.ConvertRawDataElement(elem, "utf-8")
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

// TestFilereaderHookErrorHandling tests error handling with hooks.
func TestFilereaderHookErrorHandling(t *testing.T) {
	elem := &filereader.DataElementValue{
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
	raw, err := filereader.ConvertRawDataElement(elem, "utf-8")
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

// TestFilereaderConcurrentDataElementConversion tests concurrent element conversions.
func TestFilereaderConcurrentDataElementConversion(t *testing.T) {
	numGoroutines := 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			elem := &filereader.DataElementValue{
				Tag:    tag.New(uint16(0x0010), uint16(0x0010+index)),
				VR:     "PN",
				Value:  []byte(fmt.Sprintf("Patient%d", index)),
				Length: uint32(len(fmt.Sprintf("Patient%d", index))),
			}

			raw, err := filereader.ConvertRawDataElement(elem, "utf-8")
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

// TestFilereaderTagConversionInRaw tests tag conversion to string in raw element.
func TestFilereaderTagConversionInRaw(t *testing.T) {
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
		elem := &filereader.DataElementValue{
			Tag:    tag.New(tt.group, tt.element),
			VR:     "UI",
			Value:  []byte("1.2.3"),
			Length: 5,
		}

		raw, err := filereader.ConvertRawDataElement(elem, "utf-8")
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

// TestFilereaderRawElementVRWithHooks tests VR lookup with hooks during reading.
func TestFilereaderRawElementVRWithHooks(t *testing.T) {
	elem := &filereader.DataElementValue{
		Tag:    tag.New(0x0008, 0x0008),
		VR:     "CS",
		Value:  []byte("ORIGINAL\\PRIMARY"),
		Length: 16,
	}

	raw, err := filereader.ConvertRawDataElement(elem, "utf-8")
	if err != nil {
		t.Fatalf("conversion error = %v", err)
	}

	// Register VR hook
	hook := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		if raw.VR != nil {
			data["VR"] = *raw.VR
		} else {
			data["VR"] = "UN"
		}
		return nil
	}

	err = hooks.RegisterCallback("raw_element_vr", hook)
	if err != nil {
		t.Fatalf("hook registration error = %v", err)
	}

	result, err := hooks.ExecuteRawElementVR(raw)
	if err != nil {
		t.Fatalf("hook execution error = %v", err)
	}

	if vrValue, ok := result["VR"]; !ok || vrValue != "CS" {
		t.Errorf("VR mismatch: got %v, want CS", vrValue)
	}

	hooks.Reset()
}

// TestFilereaderDataElementWithMultipleHooks tests element processing with multiple hooks.
func TestFilereaderDataElementWithMultipleHooks(t *testing.T) {
	elem := &filereader.DataElementValue{
		Tag:    tag.New(0x0010, 0x0030),
		VR:     "DA",
		Value:  []byte("19700101"),
		Length: 8,
	}

	raw, err := filereader.ConvertRawDataElement(elem, "utf-8")
	if err != nil {
		t.Fatalf("conversion error = %v", err)
	}

	// Register first hook for VR
	hook1Called := false
	hook1 := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		hook1Called = true
		data["hook1"] = true
		return nil
	}

	// Register second hook for value
	hook2Called := false
	hook2 := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		hook2Called = true
		data["hook2"] = true
		return nil
	}

	err = hooks.RegisterCallback("raw_element_vr", hook1)
	if err != nil {
		t.Fatalf("hook1 registration error = %v", err)
	}

	result1, err := hooks.ExecuteRawElementVR(raw)
	if err != nil {
		t.Fatalf("hook1 execution error = %v", err)
	}

	if !hook1Called || !result1["hook1"].(bool) {
		t.Error("hook1 not executed correctly")
	}

	hooks.Reset()

	err = hooks.RegisterCallback("raw_element_value", hook2)
	if err != nil {
		t.Fatalf("hook2 registration error = %v", err)
	}

	result2, err := hooks.ExecuteRawElementValue(raw)
	if err != nil {
		t.Fatalf("hook2 execution error = %v", err)
	}

	if !hook2Called || !result2["hook2"].(bool) {
		t.Error("hook2 not executed correctly")
	}

	hooks.Reset()
}

// TestFilereaderEncodingParameter tests encoding parameter in conversion.
func TestFilereaderEncodingParameter(t *testing.T) {
	tests := []struct {
		encoding string
		valid    bool
	}{
		{"utf-8", true},
		{"ISO_IR 100", true},
		{"ISO_IR 110", true},
		{"", true}, // Empty encoding is allowed
	}

	elem := &filereader.DataElementValue{
		Tag:    tag.New(0x0010, 0x0010),
		VR:     "PN",
		Value:  []byte("Test"),
		Length: 4,
	}

	for _, tt := range tests {
		raw, err := filereader.ConvertRawDataElement(elem, tt.encoding)
		if err != nil {
			t.Errorf("ConvertRawDataElement with encoding %s error = %v", tt.encoding, err)
		}

		if raw == nil {
			t.Errorf("raw element is nil for encoding %s", tt.encoding)
		}
	}
}
