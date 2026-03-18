package hooks_test

import (
	"fmt"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/hooks"
)

// Test HookLevel String representation
func TestHookLevelString(t *testing.T) {
	tests := []struct {
		level    hooks.HookLevel
		expected string
	}{
		{hooks.PreValidation, "PreValidation"},
		{hooks.PostValidation, "PostValidation"},
		{hooks.PreCompression, "PreCompression"},
		{hooks.PostCompression, "PostCompression"},
		{hooks.PreSerialization, "PreSerialization"},
		{hooks.PostSerialization, "PostSerialization"},
	}

	for _, test := range tests {
		result := test.level.String()
		if result != test.expected {
			t.Errorf("HookLevel.String() for %d: expected %s, got %s", test.level, test.expected, result)
		}
	}
}

// Test NewHookChain
func TestNewHookChain(t *testing.T) {
	hc := hooks.NewHookChain()

	if hc == nil {
		t.Error("NewHookChain returned nil")
	}

	if hc.HookCount() != 0 {
		t.Errorf("Expected 0 hooks, got %d", hc.HookCount())
	}
}

// Test RegisterHook
func TestRegisterAdvancedHook(t *testing.T) {
	hc := hooks.NewHookChain()

	// Create a simple hook
	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	err := hc.RegisterHook(hooks.PreValidation, hook)
	if err != nil {
		t.Fatalf("RegisterHook failed: %v", err)
	}

	if hc.HookCount() != 1 {
		t.Errorf("Expected 1 hook, got %d", hc.HookCount())
	}

	if hc.HookCountAtLevel(hooks.PreValidation) != 1 {
		t.Errorf("Expected 1 hook at hooks.PreValidation, got %d", hc.HookCountAtLevel(hooks.PreValidation))
	}
}

// Test RegisterHook with nil function
func TestRegisterAdvancedHookNil(t *testing.T) {
	hc := hooks.NewHookChain()

	err := hc.RegisterHook(hooks.PreValidation, nil)
	if err == nil {
		t.Error("Expected error when registering nil hook")
	}
}

// Test ExecuteHooks
func TestExecuteAdvancedHooks(t *testing.T) {
	hc := hooks.NewHookChain()

	// Register a hook that returns the element unchanged
	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	hc.RegisterHook(hooks.PreValidation, hook)

	elem := &dataelem.DataElement{}
	result, err := hc.ExecuteHooks(elem, hooks.PreValidation)

	if err != nil {
		t.Fatalf("ExecuteHooks failed: %v", err)
	}

	if result == nil {
		t.Error("ExecuteHooks returned nil element")
	}
}

// Test ExecuteHooks with no registered hooks
func TestExecuteAdvancedHooksNoHooks(t *testing.T) {
	hc := hooks.NewHookChain()

	elem := &dataelem.DataElement{}
	result, err := hc.ExecuteHooks(elem, hooks.PreValidation)

	if err != nil {
		t.Fatalf("ExecuteHooks should not error when no hooks registered: %v", err)
	}

	if result != elem {
		t.Error("ExecuteHooks should return original element when no hooks registered")
	}
}

// Test ExecuteHooks with nil element
func TestExecuteAdvancedHooksNilElement(t *testing.T) {
	hc := hooks.NewHookChain()

	_, err := hc.ExecuteHooks(nil, hooks.PreValidation)
	if err == nil {
		t.Error("Expected error when executing hooks on nil element")
	}
}

// Test Hook chain execution
func TestAdvancedHookChainExecution(t *testing.T) {
	hc := hooks.NewHookChain()

	// Create hooks that track execution order
	executionOrder := []int{}

	hook1 := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		executionOrder = append(executionOrder, 1)
		return elem, nil
	}

	hook2 := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		executionOrder = append(executionOrder, 2)
		return elem, nil
	}

	hook3 := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		executionOrder = append(executionOrder, 3)
		return elem, nil
	}

	hc.RegisterHook(hooks.PreValidation, hook1)
	hc.RegisterHook(hooks.PreValidation, hook2)
	hc.RegisterHook(hooks.PreValidation, hook3)

	elem := &dataelem.DataElement{}
	_, err := hc.ExecuteHooks(elem, hooks.PreValidation)

	if err != nil {
		t.Fatalf("ExecuteHooks failed: %v", err)
	}

	if len(executionOrder) != 3 || executionOrder[0] != 1 || executionOrder[1] != 2 || executionOrder[2] != 3 {
		t.Errorf("Hooks not executed in order: %v", executionOrder)
	}
}

// Test ClearHooks
func TestClearAdvancedHooks(t *testing.T) {
	hc := hooks.NewHookChain()

	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	hc.RegisterHook(hooks.PreValidation, hook)
	if hc.HookCountAtLevel(hooks.PreValidation) != 1 {
		t.Errorf("Expected 1 hook, got %d", hc.HookCountAtLevel(hooks.PreValidation))
	}

	hc.ClearHooks(hooks.PreValidation)
	if hc.HookCountAtLevel(hooks.PreValidation) != 0 {
		t.Errorf("Expected 0 hooks after clear, got %d", hc.HookCountAtLevel(hooks.PreValidation))
	}
}

// Test ClearAllHooks
func TestClearAllAdvancedHooks(t *testing.T) {
	hc := hooks.NewHookChain()

	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	hc.RegisterHook(hooks.PreValidation, hook)
	hc.RegisterHook(hooks.PostValidation, hook)
	hc.RegisterHook(hooks.PreCompression, hook)

	if hc.HookCount() != 3 {
		t.Errorf("Expected 3 hooks, got %d", hc.HookCount())
	}

	hc.ClearAllHooks()
	if hc.HookCount() != 0 {
		t.Errorf("Expected 0 hooks after clear all, got %d", hc.HookCount())
	}
}

// Test GetHookLevels
func TestGetAdvancedHookLevels(t *testing.T) {
	hc := hooks.NewHookChain()

	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	hc.RegisterHook(hooks.PreValidation, hook)
	hc.RegisterHook(hooks.PostValidation, hook)

	levels := hc.GetHookLevels()
	if len(levels) != 2 {
		t.Errorf("Expected 2 levels, got %d", len(levels))
	}
}

// Test ChainHooks
func TestChainAdvancedHooks(t *testing.T) {
	hc1 := hooks.NewHookChain()
	hc2 := hooks.NewHookChain()

	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	hc1.RegisterHook(hooks.PreValidation, hook)
	hc2.RegisterHook(hooks.PostValidation, hook)

	err := hc1.ChainHooks(hc2)
	if err != nil {
		t.Fatalf("ChainHooks failed: %v", err)
	}

	if hc1.HookCount() != 2 {
		t.Errorf("Expected 2 total hooks after chaining, got %d", hc1.HookCount())
	}
}

// Test Hook error handling
func TestAdvancedHookErrorHandling(t *testing.T) {
	hc := hooks.NewHookChain()

	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return nil, fmt.Errorf("test error")
	}

	hc.RegisterHook(hooks.PreValidation, hook)

	elem := &dataelem.DataElement{}
	_, err := hc.ExecuteHooks(elem, hooks.PreValidation)

	if err == nil {
		t.Error("Expected error from hook")
	}
}

// Test ValidatingHookFunc
func TestValidatingAdvancedHookFunc(t *testing.T) {
	hc := hooks.NewHookChain()

	validator := func(elem *dataelem.DataElement) error {
		if elem == nil {
			return fmt.Errorf("element is nil")
		}
		return nil
	}

	hook := hooks.ValidatingHookFunc(validator)
	hc.RegisterHook(hooks.PreValidation, hook)

	elem := &dataelem.DataElement{}
	result, err := hc.ExecuteHooks(elem, hooks.PreValidation)

	if err != nil {
		t.Fatalf("ValidatingHookFunc failed: %v", err)
	}

	if result != elem {
		t.Error("ValidatingHookFunc should return unchanged element")
	}
}

// Test TransformingHookFunc
func TestTransformingAdvancedHookFunc(t *testing.T) {
	hc := hooks.NewHookChain()

	transformer := func(elem *dataelem.DataElement) (*dataelem.DataElement, error) {
		return &dataelem.DataElement{}, nil
	}

	hook := hooks.TransformingHookFunc(transformer)
	hc.RegisterHook(hooks.PreValidation, hook)

	elem := &dataelem.DataElement{}
	result, err := hc.ExecuteHooks(elem, hooks.PreValidation)

	if err != nil {
		t.Fatalf("TransformingHookFunc failed: %v", err)
	}

	if result == nil {
		t.Error("TransformingHookFunc returned nil element")
	}
}

// Test HookRegistry
func TestAdvancedHookRegistry(t *testing.T) {
	registry := hooks.NewHookRegistry()

	chain := hooks.NewHookChain()
	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}
	chain.RegisterHook(hooks.PreValidation, hook)

	err := registry.RegisterChain("test_chain", chain)
	if err != nil {
		t.Fatalf("RegisterChain failed: %v", err)
	}

	retrieved, exists := registry.GetChain("test_chain")
	if !exists {
		t.Error("Chain not found in registry")
	}

	if retrieved.HookCount() != 1 {
		t.Errorf("Expected 1 hook in retrieved chain, got %d", retrieved.HookCount())
	}
}

// Test HookRegistry ListChains
func TestAdvancedHookRegistryListChains(t *testing.T) {
	registry := hooks.NewHookRegistry()

	chain1 := hooks.NewHookChain()
	chain2 := hooks.NewHookChain()

	registry.RegisterChain("chain1", chain1)
	registry.RegisterChain("chain2", chain2)

	chains := registry.ListChains()
	if len(chains) != 2 {
		t.Errorf("Expected 2 chains, got %d", len(chains))
	}
}

// Test HookRegistry RemoveChain
func TestAdvancedHookRegistryRemoveChain(t *testing.T) {
	registry := hooks.NewHookRegistry()

	chain := hooks.NewHookChain()
	registry.RegisterChain("test_chain", chain)

	success := registry.RemoveChain("test_chain")
	if !success {
		t.Error("RemoveChain should return true")
	}

	_, exists := registry.GetChain("test_chain")
	if exists {
		t.Error("Chain should not exist after removal")
	}
}

// Test HookSession
func TestAdvancedHookSession(t *testing.T) {
	registry := hooks.NewHookRegistry()
	chain := hooks.NewHookChain()

	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	chain.RegisterHook(hooks.PreValidation, hook)
	registry.RegisterChain("test_chain", chain)

	session := hooks.NewHookSession(registry, "test_chain")

	elem := &dataelem.DataElement{}
	result, err := session.Execute(elem, hooks.PreValidation)

	if err != nil {
		t.Fatalf("Session Execute failed: %v", err)
	}

	if result != elem {
		t.Error("Session should return the element")
	}
}

// Test HookSession Metadata
func TestAdvancedHookSessionMetadata(t *testing.T) {
	registry := hooks.NewHookRegistry()
	chain := hooks.NewHookChain()
	registry.RegisterChain("test", chain)

	session := hooks.NewHookSession(registry, "test")

	session.SetMetadata("key1", "value1")
	session.SetMetadata("key2", 42)

	val1, exists1 := session.GetMetadata("key1")
	if !exists1 || val1 != "value1" {
		t.Error("Metadata retrieval failed for key1")
	}

	val2, exists2 := session.GetMetadata("key2")
	if !exists2 || val2 != 42 {
		t.Error("Metadata retrieval failed for key2")
	}
}

// Test HookSession Reset
func TestAdvancedHookSessionReset(t *testing.T) {
	registry := hooks.NewHookRegistry()
	chain := hooks.NewHookChain()
	registry.RegisterChain("test", chain)

	session := hooks.NewHookSession(registry, "test")
	session.SetMetadata("key", "value")

	session.Reset()

	_, exists := session.GetMetadata("key")
	if exists {
		t.Error("Metadata should be cleared after reset")
	}
}

// Test GlobalRegistry
func TestAdvancedGlobalRegistry(t *testing.T) {
	chain := hooks.NewHookChain()
	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}
	chain.RegisterHook(hooks.PreValidation, hook)

	err := hooks.RegisterGlobalChain("global_test_adv", chain)
	if err != nil {
		t.Fatalf("RegisterGlobalChain failed: %v", err)
	}

	retrieved, exists := hooks.GetGlobalChain("global_test_adv")
	if !exists {
		t.Error("Global chain not found")
	}

	if retrieved.HookCount() != 1 {
		t.Errorf("Expected 1 hook, got %d", retrieved.HookCount())
	}
}

// Test Concurrent Hook Execution
func TestAdvancedConcurrentHookExecution(t *testing.T) {
	hc := hooks.NewHookChain()

	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	hc.RegisterHook(hooks.PreValidation, hook)

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			elem := &dataelem.DataElement{}
			_, _ = hc.ExecuteHooks(elem, hooks.PreValidation)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// Benchmark tests

func BenchmarkNewAdvancedHookChain(b *testing.B) {
	for i := 0; i < b.N; i++ {
		hooks.NewHookChain()
	}
}

func BenchmarkRegisterAdvancedHook(b *testing.B) {
	hc := hooks.NewHookChain()
	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hc.RegisterHook(hooks.PreValidation, hook)
	}
}

func BenchmarkExecuteAdvancedHooks(b *testing.B) {
	hc := hooks.NewHookChain()
	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}
	hc.RegisterHook(hooks.PreValidation, hook)

	elem := &dataelem.DataElement{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hc.ExecuteHooks(elem, hooks.PreValidation)
	}
}

func BenchmarkAdvancedHookChain(b *testing.B) {
	hc := hooks.NewHookChain()
	hook := func(elem *dataelem.DataElement, level hooks.HookLevel) (*dataelem.DataElement, error) {
		return elem, nil
	}

	// Register multiple hooks
	for i := 0; i < 10; i++ {
		hc.RegisterHook(hooks.PreValidation, hook)
	}

	elem := &dataelem.DataElement{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hc.ExecuteHooks(elem, hooks.PreValidation)
	}
}
