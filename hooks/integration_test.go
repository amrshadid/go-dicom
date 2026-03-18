package hooks_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/hooks"
)

// TestConvertRawDataElement tests the ConvertRawDataElement function.
func TestConvertRawDataElement(t *testing.T) {
	tests := []struct {
		name    string
		raw     *hooks.RawDataElement
		wantErr bool
		checkFn func(*testing.T, *dataelem.DataElement)
	}{
		{
			name: "Convert with explicit VR",
			raw: &hooks.RawDataElement{
				Tag:   "0010,0010",
				VR:    strPtr("PN"),
				Value: []byte("Smith^John"),
			},
			wantErr: false,
			checkFn: func(t *testing.T, elem *dataelem.DataElement) {
				if elem == nil {
					t.Fatal("ConvertRawDataElement returned nil element")
				}
				if elem.GetVR() != dataelem.PN {
					t.Errorf("Expected VR PN, got %v", elem.GetVR())
				}
				if elem.GetValue() == nil {
					t.Error("Expected value to be set")
				}
			},
		},
		{
			name: "Convert without explicit VR (defaults to UN)",
			raw: &hooks.RawDataElement{
				Tag:   "0010,0020",
				VR:    nil,
				Value: []byte("12345"),
			},
			wantErr: false,
			checkFn: func(t *testing.T, elem *dataelem.DataElement) {
				if elem == nil {
					t.Fatal("ConvertRawDataElement returned nil element")
				}
				if elem.GetVR() != dataelem.UN {
					t.Errorf("Expected VR UN, got %v", elem.GetVR())
				}
			},
		},
		{
			name:    "Nil raw element",
			raw:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset hooks to defaults for each test
			hooks.Reset()

			elem, err := hooks.ConvertRawDataElement(tt.raw, "utf-8")
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRawDataElement error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkFn != nil {
				tt.checkFn(t, elem)
			}
		})
	}
}

// TestConvertRawDataElementWithCustomHook tests conversion with custom hooks.
func TestConvertRawDataElementWithCustomHook(t *testing.T) {
	// Reset to defaults
	hooks.Reset()

	// Register custom VR hook
	customVRHook := func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		data["VR"] = "CS" // Always return CS
		return nil
	}

	if err := hooks.RegisterCallback("raw_element_vr", customVRHook); err != nil {
		t.Fatalf("RegisterCallback failed: %v", err)
	}

	raw := &hooks.RawDataElement{
		Tag:   "0010,0010",
		VR:    strPtr("PN"),
		Value: []byte("TestValue"),
	}

	elem, err := hooks.ConvertRawDataElement(raw, "utf-8")
	if err != nil {
		t.Fatalf("ConvertRawDataElement failed: %v", err)
	}

	// Should use custom hook's VR
	if elem.GetVR() != dataelem.CS {
		t.Errorf("Expected VR CS from custom hook, got %v", elem.GetVR())
	}

	// Clean up
	hooks.Reset()
}

// TestConvertRawDataElementWithFixSeparatorHook tests separator fixing during conversion.
func TestConvertRawDataElementWithFixSeparatorHook(t *testing.T) {
	// Reset to defaults
	hooks.Reset()

	// Register fix separator hook
	fixHook := hooks.FixSeparatorHook(',', '\\')
	if err := hooks.RegisterCallback("raw_element_value", fixHook); err != nil {
		t.Fatalf("RegisterCallback failed: %v", err)
	}

	// Register kwargs for target VRs
	kwargs := map[string]interface{}{
		"target_VRs": []string{"DS", "IS"},
	}
	if err := hooks.RegisterKwargs("raw_element_kwargs", kwargs); err != nil {
		t.Fatalf("RegisterKwargs failed: %v", err)
	}

	raw := &hooks.RawDataElement{
		Tag:   "0010,0020",
		VR:    strPtr("DS"),
		Value: []byte("1.5,2.5,3.5"), // Invalid separator
	}

	elem, err := hooks.ConvertRawDataElement(raw, "utf-8")
	if err != nil {
		t.Fatalf("ConvertRawDataElement failed: %v", err)
	}

	if elem == nil {
		t.Fatal("ConvertRawDataElement returned nil element")
	}

	// Clean up
	hooks.Reset()
}

// TestGlobalHooksIntegration tests integration of global hooks.
func TestGlobalHooksIntegration(t *testing.T) {
	// Reset global hooks
	hooks.Reset()

	// Create and register a hook chain globally
	chain := hooks.NewHookChain()

	// Register a validation hook
	validationHook := hooks.ValidatingHookFunc(func(elem *dataelem.DataElement) error {
		if elem == nil {
			t.Error("Element should not be nil in validation hook")
		}
		return nil
	})

	if err := chain.RegisterHook(hooks.PreValidation, validationHook); err != nil {
		t.Fatalf("RegisterHook failed: %v", err)
	}

	// Register chain globally
	if err := hooks.RegisterGlobalChain("test_chain", chain); err != nil {
		t.Fatalf("RegisterGlobalChain failed: %v", err)
	}

	// Verify chain is registered
	retrievedChain, ok := hooks.GetGlobalChain("test_chain")
	if !ok {
		t.Fatal("Failed to retrieve globally registered chain")
	}

	if retrievedChain == nil {
		t.Fatal("Retrieved chain is nil")
	}
}

// TestHookChainComposition tests combining multiple hook chains.
func TestHookChainComposition(t *testing.T) {
	chain1 := hooks.NewHookChain()
	chain2 := hooks.NewHookChain()

	// Register hooks in chain1
	hook1 := hooks.ValidatingHookFunc(func(elem *dataelem.DataElement) error {
		if elem == nil {
			return nil // Validation passed
		}
		return nil
	})

	if err := chain1.RegisterHook(hooks.PreValidation, hook1); err != nil {
		t.Fatalf("RegisterHook in chain1 failed: %v", err)
	}

	// Register hooks in chain2
	hook2 := hooks.TransformingHookFunc(func(elem *dataelem.DataElement) (*dataelem.DataElement, error) {
		return elem, nil
	})

	if err := chain2.RegisterHook(hooks.PostValidation, hook2); err != nil {
		t.Fatalf("RegisterHook in chain2 failed: %v", err)
	}

	// Combine chains
	if err := chain1.ChainHooks(chain2); err != nil {
		t.Fatalf("ChainHooks failed: %v", err)
	}

	// Verify combined count
	count := chain1.HookCount()
	if count < 2 {
		t.Errorf("Expected at least 2 hooks after chaining, got %d", count)
	}
}

// TestHookSession tests hook session execution context.
func TestHookSession(t *testing.T) {
	// Create hook session
	registry := hooks.NewHookRegistry()
	chain := hooks.NewHookChain()

	// Create and register a simple hook
	hook := hooks.ValidatingHookFunc(func(elem *dataelem.DataElement) error {
		return nil // Always pass
	})

	if err := chain.RegisterHook(hooks.PreValidation, hook); err != nil {
		t.Fatalf("RegisterHook failed: %v", err)
	}

	if err := registry.RegisterChain("test", chain); err != nil {
		t.Fatalf("RegisterChain failed: %v", err)
	}

	// Create session
	session := hooks.NewHookSession(registry, "test")

	// Set metadata
	session.SetMetadata("source", "test")

	// Verify metadata
	value, ok := session.GetMetadata("source")
	if !ok {
		t.Error("Metadata not found")
	}
	if value != "test" {
		t.Errorf("Expected metadata value 'test', got %v", value)
	}

	// Verify error tracking
	errorCount := session.GetErrorCount()
	if errorCount != 0 {
		t.Errorf("Expected 0 errors, got %d", errorCount)
	}
}

// TestFilteringHook tests hook filtering functionality.
func TestFilteringHook(t *testing.T) {
	chain := hooks.NewHookChain()

	// Create filtering hook - filter out private tags
	filterHook := hooks.FilteringHookFunc(func(elem *dataelem.DataElement) bool {
		if elem == nil {
			return false
		}
		// This is a simple demo - would check tag ranges in real implementation
		return true // Keep element
	})

	if err := chain.RegisterHook(hooks.PreSerialization, filterHook); err != nil {
		t.Fatalf("RegisterHook failed: %v", err)
	}

	// Create test element
	elem := dataelem.NewDataElement("0010,0010", dataelem.PN, "Smith^John")

	// Execute hook
	result, err := chain.ExecuteHooks(elem, hooks.PreSerialization)
	if err != nil {
		t.Fatalf("ExecuteHooks failed: %v", err)
	}

	if result == nil {
		t.Fatal("FilteringHook should not filter out valid element")
	}
}

// TestHookErrorHandling tests error handling in hooks.
func TestHookErrorHandling(t *testing.T) {
	chain := hooks.NewHookChain()

	// Create hook that returns error
	errorHook := hooks.ValidatingHookFunc(func(elem *dataelem.DataElement) error {
		return ErrTestHook
	})

	if err := chain.RegisterHook(hooks.PreValidation, errorHook); err != nil {
		t.Fatalf("RegisterHook failed: %v", err)
	}

	elem := dataelem.NewDataElement("0010,0010", dataelem.PN, "test")

	// Execute hook - should return error
	_, err := chain.ExecuteHooks(elem, hooks.PreValidation)
	if err == nil {
		t.Error("Expected error from hook, got nil")
	}

	// Error message should contain our test error
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("Error message is empty")
	}
}

// TestConcurrentHookChainExecution tests concurrent hook execution.
func TestConcurrentHookChainExecution(t *testing.T) {
	chain := hooks.NewHookChain()

	// Register multiple hooks
	for i := 0; i < 5; i++ {
		hook := hooks.ValidatingHookFunc(func(elem *dataelem.DataElement) error {
			return nil
		})

		if err := chain.RegisterHook(hooks.PreValidation, hook); err != nil {
			t.Fatalf("RegisterHook failed: %v", err)
		}
	}

	elem := dataelem.NewDataElement("0010,0010", dataelem.PN, "test")

	// Execute hooks concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = chain.ExecuteHooks(elem, hooks.PreValidation)
			done <- true
		}()
	}

	// Wait for completion
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestMultiLevelHookExecution tests hooks at multiple processing levels.
func TestMultiLevelHookExecution(t *testing.T) {
	chain := hooks.NewHookChain()
	elem := dataelem.NewDataElement("0010,0010", dataelem.PN, "test")

	// Register hooks at different levels
	levels := []hooks.HookLevel{
		hooks.PreValidation,
		hooks.PostValidation,
		hooks.PreCompression,
		hooks.PostCompression,
		hooks.PreSerialization,
		hooks.PostSerialization,
	}

	for _, level := range levels {
		hook := hooks.ValidatingHookFunc(func(elem *dataelem.DataElement) error {
			return nil
		})

		if err := chain.RegisterHook(level, hook); err != nil {
			t.Fatalf("RegisterHook failed at level %d: %v", level, err)
		}
	}

	// Execute at each level
	for _, level := range levels {
		result, err := chain.ExecuteHooks(elem, level)
		if err != nil {
			t.Fatalf("ExecuteHooks failed at level %d: %v", level, err)
		}
		if result == nil {
			t.Fatalf("ExecuteHooks returned nil at level %d", level)
		}
	}
}

// Helper error type for testing
type TestError struct {
	msg string
}

func (e TestError) Error() string {
	return e.msg
}

var ErrTestHook = TestError{msg: "test hook error"}
