package hooks_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/hooks"
)

func TestNewHooks(t *testing.T) {
	h := hooks.NewHooks()
	if h == nil {
		t.Fatal("NewHooks returned nil")
	}
}

func TestRegisterCallback(t *testing.T) {
	h := hooks.NewHooks()

	// Test registering valid callback
	customHook := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		data["VR"] = "CS"
		return nil
	}

	err := h.RegisterCallback("raw_element_vr", customHook)
	if err != nil {
		t.Fatalf("RegisterCallback failed: %v", err)
	}

	// Test registering nil callback
	err = h.RegisterCallback("raw_element_vr", nil)
	if err == nil {
		t.Error("RegisterCallback should reject nil callback")
	}

	// Test registering to unknown hook
	err = h.RegisterCallback("unknown_hook", customHook)
	if err == nil {
		t.Error("RegisterCallback should reject unknown hook")
	}
}

func TestRegisterKwargs(t *testing.T) {
	h := hooks.NewHooks()

	// Test registering kwargs
	kwargs := map[string]interface{}{
		"strict_mode": true,
		"encoding":    "utf-8",
	}

	err := h.RegisterKwargs("raw_element_kwargs", kwargs)
	if err != nil {
		t.Fatalf("RegisterKwargs failed: %v", err)
	}

	// Test registering nil kwargs
	err = h.RegisterKwargs("raw_element_kwargs", nil)
	if err == nil {
		t.Error("RegisterKwargs should reject nil kwargs")
	}

	// Test registering to unknown hook
	err = h.RegisterKwargs("unknown_kwargs", kwargs)
	if err == nil {
		t.Error("RegisterKwargs should reject unknown hook")
	}
}

func TestUpdateKwargs(t *testing.T) {
	h := hooks.NewHooks()

	// Update kwargs
	err := h.UpdateKwargs("key1", "value1")
	if err != nil {
		t.Fatalf("UpdateKwargs failed: %v", err)
	}

	// Verify update
	kwargs := h.GetKwargs()
	if kwargs["key1"] != "value1" {
		t.Error("UpdateKwargs did not set value correctly")
	}
}

func TestGetKwargs(t *testing.T) {
	h := hooks.NewHooks()

	// Register kwargs
	original := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}
	h.RegisterKwargs("raw_element_kwargs", original)

	// Get kwargs
	retrieved := h.GetKwargs()

	// Verify copy, not reference
	if len(retrieved) != len(original) {
		t.Error("GetKwargs returned wrong size")
	}

	// Modify retrieved and ensure original is unchanged
	retrieved["key1"] = "modified"
	// Verify that modifications to retrieved don't affect the internal state
	retrieved2 := h.GetKwargs()
	if retrieved2["key1"] != "value1" {
		t.Error("GetKwargs should return copy, not reference")
	}
}

func TestExecuteRawElementVR(t *testing.T) {
	h := hooks.NewHooks()

	// Create test raw element
	vrStr := "PN"
	raw := &hooks.RawDataElement{
		Tag:   "0010,0010",
		VR:    &vrStr,
		Value: []byte("Smith^John"),
	}

	// Execute hook
	data, err := h.ExecuteRawElementVR(raw)
	if err != nil {
		t.Fatalf("ExecuteRawElementVR failed: %v", err)
	}

	// Verify result
	if data["VR"] != "PN" {
		t.Errorf("Expected VR 'PN', got %v", data["VR"])
	}
}

func TestExecuteRawElementValue(t *testing.T) {
	h := hooks.NewHooks()

	// Create test raw element
	raw := &hooks.RawDataElement{
		Tag:   "0010,0010",
		VR:    nil,
		Value: []byte("Smith^John"),
	}

	// Execute hook
	data, err := h.ExecuteRawElementValue(raw)
	if err != nil {
		t.Fatalf("ExecuteRawElementValue failed: %v", err)
	}

	// Verify value is set
	if data["value"] == nil {
		t.Error("ExecuteRawElementValue did not set value")
	}
}

func TestDefaultRawElementVR(t *testing.T) {
	tests := []struct {
		name     string
		vr       *string
		expected string
	}{
		{
			name:     "With VR",
			vr:       strPtr("PN"),
			expected: "PN",
		},
		{
			name:     "Without VR",
			vr:       nil,
			expected: "UN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := &hooks.RawDataElement{
				Tag:   "0010,0010",
				VR:    tt.vr,
				Value: []byte("test"),
			}

			h := hooks.NewHooks()
			data, err := h.ExecuteRawElementVR(raw)
			if err != nil {
				return
			}

			// Verify VR is set
			if data["VR"] == nil {
				t.Error("ExecuteRawElementVR did not set VR")
			}
		})
	}
}

func TestDefaultRawElementValue(t *testing.T) {
	raw := &hooks.RawDataElement{
		Tag:   "0010,0010",
		VR:    strPtr("LO"),
		Value: []byte("TestValue"),
	}

	h := hooks.NewHooks()
	data, err := h.ExecuteRawElementValue(raw)
	if err != nil {
		t.Fatalf("ExecuteRawElementValue failed: %v", err)
	}

	if data["value"] == nil {
		t.Error("ExecuteRawElementValue did not set value")
	}
}

func TestReset(t *testing.T) {
	h := hooks.NewHooks()

	// Register custom hook
	customHook := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		data["VR"] = "CS"
		return nil
	}
	h.RegisterCallback("raw_element_vr", customHook)

	// Register kwargs
	h.RegisterKwargs("raw_element_kwargs", map[string]interface{}{"key": "value"})

	// Reset
	h.Reset()

	// Verify reset - should use default again
	raw := &hooks.RawDataElement{Tag: "0010,0010", VR: nil, Value: []byte("test")}
	data, _ := h.ExecuteRawElementVR(raw)

	if data["VR"] != "UN" {
		t.Error("Reset did not restore default VR hook")
	}

	kwargs := h.GetKwargs()
	if len(kwargs) != 0 {
		t.Error("Reset did not clear kwargs")
	}
}

func TestFixSeparatorHook(t *testing.T) {
	// Create fix separator hook
	fixHook := hooks.FixSeparatorHook(',', '\\')

	tests := []struct {
		name      string
		vr        *string
		value     []byte
		targetVRs []string
		shouldFix bool
	}{
		{
			name:      "Fix DS VR with comma",
			vr:        strPtr("DS"),
			value:     []byte("1.5,2.5,3.5"),
			targetVRs: []string{"DS", "IS"},
			shouldFix: true,
		},
		{
			name:      "Skip non-target VR",
			vr:        strPtr("LO"),
			value:     []byte("1.5,2.5,3.5"),
			targetVRs: []string{"DS", "IS"},
			shouldFix: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := &hooks.RawDataElement{
				Tag:   "0010,0010",
				VR:    tt.vr,
				Value: tt.value,
			}

			data := make(map[string]interface{})
			kwargs := map[string]interface{}{"target_VRs": tt.targetVRs}

			err := fixHook(raw, data, kwargs)
			if err != nil {
				t.Fatalf("FixSeparatorHook failed: %v", err)
			}

			// Verify value conversion happened
			if data["value"] == nil {
				t.Error("FixSeparatorHook did not set value")
			}
		})
	}
}

func TestRetryAlternateVRHook(t *testing.T) {
	// Create retry hook
	retryHook := hooks.RetryAlternateVRHook(
		[]string{"DS"},
		[]string{"US", "SS"},
	)

	raw := &hooks.RawDataElement{
		Tag:   "0010,0010",
		VR:    strPtr("DS"),
		Value: []byte("123"),
	}

	data := make(map[string]interface{})
	kwargs := make(map[string]interface{})

	err := retryHook(raw, data, kwargs)
	if err != nil {
		t.Fatalf("RetryAlternateVRHook failed: %v", err)
	}

	// Verify value was set
	if data["value"] == nil {
		t.Error("RetryAlternateVRHook did not set value")
	}
}

func TestGlobalHooks(t *testing.T) {
	// Reset global hooks
	hooks.Reset()

	// Register custom hook globally
	customHook := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		data["VR"] = "CS"
		return nil
	}

	err := hooks.RegisterCallback("raw_element_vr", customHook)
	if err != nil {
		t.Fatalf("Global RegisterCallback failed: %v", err)
	}

	// Register kwargs globally
	kwargs := map[string]interface{}{"test_key": "test_value"}
	err = hooks.RegisterKwargs("raw_element_kwargs", kwargs)
	if err != nil {
		t.Fatalf("Global RegisterKwargs failed: %v", err)
	}

	// Update kwargs globally
	err = hooks.UpdateKwargs("new_key", "new_value")
	if err != nil {
		t.Fatalf("Global UpdateKwargs failed: %v", err)
	}

	// Execute hooks globally
	raw := &hooks.RawDataElement{Tag: "0010,0010", VR: nil, Value: []byte("test")}
	data, err := hooks.ExecuteRawElementVR(raw)
	if err != nil {
		t.Fatalf("Global ExecuteRawElementVR failed: %v", err)
	}

	if data["VR"] != "CS" {
		t.Errorf("Expected CS from custom hook, got %v", data["VR"])
	}

	// Clean up
	hooks.Reset()
}

func TestConcurrentHookRegistration(t *testing.T) {
	h := hooks.NewHooks()
	done := make(chan bool)

	// Register multiple hooks concurrently
	for i := 0; i < 10; i++ {
		go func() {
			hook := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
				data["VR"] = "UN"
				return nil
			}
			_ = h.RegisterCallback("raw_element_vr", hook)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Test passed if no panic occurred
}

func TestConcurrentHookExecution(t *testing.T) {
	h := hooks.NewHooks()
	done := make(chan bool)

	raw := &hooks.RawDataElement{
		Tag:   "0010,0010",
		VR:    strPtr("PN"),
		Value: []byte("test"),
	}

	// Execute hooks concurrently
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = h.ExecuteRawElementVR(raw)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Test passed if no panic occurred
}

// Helper function
func strPtr(s string) *string {
	return &s
}
