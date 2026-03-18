package hooks

import (
	"fmt"
	"sync"

	"github.com/amrshadid/go-dicom/dataelem"
)

// RawDataElement represents raw DICOM element data before conversion.
type RawDataElement struct {
	Tag   string  // DICOM tag as "XXXX,XXXX"
	VR    *string // Value Representation
	Value []byte  // Raw binary value
}

// RawDataHook is a callback for raw data element processing.
type RawDataHook func(raw *RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error

// Hooks manages callback functions for DICOM parsing and writing.
type Hooks struct {
	rawElementVR     RawDataHook
	rawElementValue  RawDataHook
	rawElementKwargs map[string]interface{}
	mu               sync.RWMutex
}

// NewHooks creates a new hooks manager with default implementations.
func NewHooks() *Hooks {
	h := &Hooks{
		rawElementKwargs: make(map[string]interface{}),
	}
	h.rawElementVR = DefaultRawElementVR
	h.rawElementValue = DefaultRawElementValue
	return h
}

// RegisterCallback registers a callback for a specific hook.
func (h *Hooks) RegisterCallback(hookName string, fn RawDataHook) error {
	if fn == nil {
		return fmt.Errorf("callback function cannot be nil")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	switch hookName {
	case "raw_element_vr":
		h.rawElementVR = fn
		return nil
	case "raw_element_value":
		h.rawElementValue = fn
		return nil
	default:
		return fmt.Errorf("unknown hook: %s", hookName)
	}
}

// RegisterKwargs registers kwargs dictionary for hook callbacks.
func (h *Hooks) RegisterKwargs(hookName string, kwargs map[string]interface{}) error {
	if kwargs == nil {
		return fmt.Errorf("kwargs cannot be nil")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if hookName != "raw_element_kwargs" {
		return fmt.Errorf("unknown kwargs hook: %s", hookName)
	}
	h.rawElementKwargs = kwargs
	return nil
}

// UpdateKwargs updates specific kwargs values.
func (h *Hooks) UpdateKwargs(key string, value interface{}) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rawElementKwargs[key] = value
	return nil
}

// GetKwargs retrieves a copy of all registered kwargs.
func (h *Hooks) GetKwargs() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make(map[string]interface{})
	for k, v := range h.rawElementKwargs {
		result[k] = v
	}
	return result
}

// ExecuteRawElementVR executes the raw element VR hook.
func (h *Hooks) ExecuteRawElementVR(raw *RawDataElement) (map[string]interface{}, error) {
	if raw == nil {
		return nil, fmt.Errorf("raw data element cannot be nil")
	}
	h.mu.RLock()
	fn := h.rawElementVR
	kwargs := h.rawElementKwargs
	h.mu.RUnlock()
	if fn == nil {
		return nil, fmt.Errorf("raw_element_vr hook not registered")
	}
	data := make(map[string]interface{})
	err := fn(raw, data, kwargs)
	return data, err
}

// ExecuteRawElementValue executes the raw element value hook.
func (h *Hooks) ExecuteRawElementValue(raw *RawDataElement) (map[string]interface{}, error) {
	if raw == nil {
		return nil, fmt.Errorf("raw data element cannot be nil")
	}
	h.mu.RLock()
	fn := h.rawElementValue
	kwargs := h.rawElementKwargs
	h.mu.RUnlock()
	if fn == nil {
		return nil, fmt.Errorf("raw_element_value hook not registered")
	}
	data := make(map[string]interface{})
	err := fn(raw, data, kwargs)
	return data, err
}

// Reset resets all hooks to default implementations.
func (h *Hooks) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rawElementVR = DefaultRawElementVR
	h.rawElementValue = DefaultRawElementValue
	h.rawElementKwargs = make(map[string]interface{})
}

// DefaultRawElementVR is the default VR lookup implementation.
func DefaultRawElementVR(raw *RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
	if raw.VR != nil {
		data["VR"] = *raw.VR
		return nil
	}
	data["VR"] = "UN"
	return nil
}

// DefaultRawElementValue is the default value conversion implementation.
func DefaultRawElementValue(raw *RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
	if _, ok := data["VR"]; !ok {
		data["VR"] = "UN"
	}
	data["value"] = raw.Value
	return nil
}

// FixSeparatorHook fixes invalid multivalue separators in raw element data.
func FixSeparatorHook(invalidSeparator, validSeparator byte) RawDataHook {
	return func(raw *RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		// Get target VRs from kwargs
		targetVRs, ok := kwargs["target_VRs"].([]string)
		if !ok {
			targetVRs = []string{"DS", "IS"}
		}

		// Check if current VR is in target list
		if raw.VR != nil {
			for _, vr := range targetVRs {
				if *raw.VR == vr {
					// Replace invalid separator with valid one
					fixedValue := make([]byte, len(raw.Value))
					for i, b := range raw.Value {
						if b == invalidSeparator {
							fixedValue[i] = validSeparator
						} else {
							fixedValue[i] = b
						}
					}
					raw = &RawDataElement{
						Tag:   raw.Tag,
						VR:    raw.VR,
						Value: fixedValue,
					}
					break
				}
			}
		}

		// Call default value conversion
		return DefaultRawElementValue(raw, data, kwargs)
	}
}

// RetryAlternateVRHook retries conversion with alternate VRs if primary fails.
func RetryAlternateVRHook(primaryVRs []string, alternateVRs []string) RawDataHook {
	return func(raw *RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
		// Try primary conversion first
		err := DefaultRawElementValue(raw, data, kwargs)
		if err == nil {
			return nil
		}

		// Check if current VR is in primary list
		if raw.VR == nil {
			return err
		}

		// Try alternate VRs
		for _, altVR := range alternateVRs {
			altVRStr := altVR
			altRaw := &RawDataElement{
				Tag:   raw.Tag,
				VR:    &altVRStr,
				Value: raw.Value,
			}

			// Try conversion with alternate VR
			altData := make(map[string]interface{})
			altData["VR"] = altVR
			altErr := DefaultRawElementValue(altRaw, altData, kwargs)
			if altErr == nil {
				// Success - update data with alternate VR
				data["VR"] = altVR
				data["value"] = altData["value"]
				return nil
			}
		}

		// All failed, return original error
		return err
	}
}

// GlobalHooks is the global hooks instance.
var GlobalHooks = NewHooks()

// RegisterCallback registers a callback in the global hooks instance.
func RegisterCallback(hookName string, fn RawDataHook) error {
	return GlobalHooks.RegisterCallback(hookName, fn)
}

// RegisterKwargs registers kwargs in the global hooks instance.
func RegisterKwargs(hookName string, kwargs map[string]interface{}) error {
	return GlobalHooks.RegisterKwargs(hookName, kwargs)
}

// UpdateKwargs updates kwargs in the global hooks instance.
func UpdateKwargs(key string, value interface{}) error {
	return GlobalHooks.UpdateKwargs(key, value)
}

// ExecuteRawElementVR executes the VR hook on global instance.
func ExecuteRawElementVR(raw *RawDataElement) (map[string]interface{}, error) {
	return GlobalHooks.ExecuteRawElementVR(raw)
}

// ExecuteRawElementValue executes the value hook on global instance.
func ExecuteRawElementValue(raw *RawDataElement) (map[string]interface{}, error) {
	return GlobalHooks.ExecuteRawElementValue(raw)
}

// Reset resets global hooks to defaults.
func Reset() {
	GlobalHooks.Reset()
}

// HookLevel represents the processing stage at which a hook executes.
type HookLevel int

const (
	PreValidation     HookLevel = iota // Before element validation
	PostValidation                     // After element validation
	PreCompression                     // Before data compression
	PostCompression                    // After data compression
	PreSerialization                   // Before dataset serialization
	PostSerialization                  // After dataset serialization
)

// String returns the string representation of a HookLevel.
func (hl HookLevel) String() string {
	switch hl {
	case PreValidation:
		return "PreValidation"
	case PostValidation:
		return "PostValidation"
	case PreCompression:
		return "PreCompression"
	case PostCompression:
		return "PostCompression"
	case PreSerialization:
		return "PreSerialization"
	case PostSerialization:
		return "PostSerialization"
	default:
		return "Unknown"
	}
}

// AdvancedHookFunc is a callback function that can modify data elements.
// Returns the (possibly modified) element and an error.
type AdvancedHookFunc func(elem *dataelem.DataElement, level HookLevel) (*dataelem.DataElement, error)

// HookChain manages a collection of hooks organized by processing level.
type HookChain struct {
	hooks map[HookLevel][]AdvancedHookFunc
	mu    sync.RWMutex
}

// NewHookChain creates a new empty hook chain.
func NewHookChain() *HookChain {
	return &HookChain{
		hooks: make(map[HookLevel][]AdvancedHookFunc),
	}
}

// RegisterHook registers a new hook at the specified level.
// Multiple hooks can be registered at the same level; they execute in order.
func (hc *HookChain) RegisterHook(level HookLevel, fn AdvancedHookFunc) error {
	if fn == nil {
		return fmt.Errorf("hook function cannot be nil")
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	if _, exists := hc.hooks[level]; !exists {
		hc.hooks[level] = make([]AdvancedHookFunc, 0)
	}

	hc.hooks[level] = append(hc.hooks[level], fn)
	return nil
}

// ExecuteHooks executes all hooks at the specified level in order.
// If any hook returns an error, execution stops and the error is returned.
// The data element may be modified by hooks.
func (hc *HookChain) ExecuteHooks(elem *dataelem.DataElement, level HookLevel) (*dataelem.DataElement, error) {
	if elem == nil {
		return nil, fmt.Errorf("element cannot be nil")
	}

	hc.mu.RLock()
	hooks, exists := hc.hooks[level]
	hc.mu.RUnlock()

	if !exists || len(hooks) == 0 {
		return elem, nil
	}

	// Execute hooks in order, passing the element through the chain
	current := elem
	for i, hook := range hooks {
		result, err := hook(current, level)
		if err != nil {
			return nil, fmt.Errorf("hook %d at level %s failed: %w", i, level.String(), err)
		}
		if result == nil {
			return nil, fmt.Errorf("hook %d at level %s returned nil element", i, level.String())
		}
		current = result
	}

	return current, nil
}

// ClearHooks removes all hooks at the specified level.
func (hc *HookChain) ClearHooks(level HookLevel) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	delete(hc.hooks, level)
}

// ClearAllHooks removes all hooks from all levels.
func (hc *HookChain) ClearAllHooks() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.hooks = make(map[HookLevel][]AdvancedHookFunc)
}

// HookCount returns the total number of hooks registered.
func (hc *HookChain) HookCount() int {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	count := 0
	for _, hooks := range hc.hooks {
		count += len(hooks)
	}
	return count
}

// HookCountAtLevel returns the number of hooks at a specific level.
func (hc *HookChain) HookCountAtLevel(level HookLevel) int {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if hooks, exists := hc.hooks[level]; exists {
		return len(hooks)
	}
	return 0
}

// ChainHooks combines multiple hook chains into a single chain.
// Hooks from other chains are appended in order.
func (hc *HookChain) ChainHooks(others ...*HookChain) error {
	if len(others) == 0 {
		return nil
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	for _, other := range others {
		if other == nil {
			continue
		}

		other.mu.RLock()
		for level, hooks := range other.hooks {
			if _, exists := hc.hooks[level]; !exists {
				hc.hooks[level] = make([]AdvancedHookFunc, 0)
			}
			hc.hooks[level] = append(hc.hooks[level], hooks...)
		}
		other.mu.RUnlock()
	}

	return nil
}

// GetHookLevels returns all levels that have hooks registered.
func (hc *HookChain) GetHookLevels() []HookLevel {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	levels := make([]HookLevel, 0, len(hc.hooks))
	for level := range hc.hooks {
		levels = append(levels, level)
	}
	return levels
}

// ValidatingHookFunc creates a hook that validates an element without modification.
func ValidatingHookFunc(validator func(*dataelem.DataElement) error) AdvancedHookFunc {
	return func(elem *dataelem.DataElement, level HookLevel) (*dataelem.DataElement, error) {
		if err := validator(elem); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
		return elem, nil
	}
}

// TransformingHookFunc creates a hook that transforms an element.
func TransformingHookFunc(transformer func(*dataelem.DataElement) (*dataelem.DataElement, error)) AdvancedHookFunc {
	return func(elem *dataelem.DataElement, level HookLevel) (*dataelem.DataElement, error) {
		return transformer(elem)
	}
}

// FilteringHookFunc creates a hook that filters elements based on a predicate.
// Returns the element unchanged if the predicate returns true, nil if false.
func FilteringHookFunc(predicate func(*dataelem.DataElement) bool) AdvancedHookFunc {
	return func(elem *dataelem.DataElement, level HookLevel) (*dataelem.DataElement, error) {
		if predicate(elem) {
			return elem, nil
		}
		return nil, fmt.Errorf("element filtered out by predicate")
	}
}

// HookRegistry maintains a registry of named hook chains for reuse.
type HookRegistry struct {
	chains map[string]*HookChain
	mu     sync.RWMutex
}

// NewHookRegistry creates a new empty hook registry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		chains: make(map[string]*HookChain),
	}
}

// RegisterChain stores a hook chain with a given name.
func (hr *HookRegistry) RegisterChain(name string, chain *HookChain) error {
	if name == "" {
		return fmt.Errorf("chain name cannot be empty")
	}
	if chain == nil {
		return fmt.Errorf("chain cannot be nil")
	}

	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.chains[name] = chain
	return nil
}

// GetChain retrieves a hook chain by name.
func (hr *HookRegistry) GetChain(name string) (*HookChain, bool) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	chain, exists := hr.chains[name]
	return chain, exists
}

// RemoveChain removes a hook chain from the registry.
func (hr *HookRegistry) RemoveChain(name string) bool {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	if _, exists := hr.chains[name]; exists {
		delete(hr.chains, name)
		return true
	}
	return false
}

// ListChains returns all registered chain names.
func (hr *HookRegistry) ListChains() []string {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	names := make([]string, 0, len(hr.chains))
	for name := range hr.chains {
		names = append(names, name)
	}
	return names
}

// ClearRegistry removes all chains from the registry.
func (hr *HookRegistry) ClearRegistry() {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	hr.chains = make(map[string]*HookChain)
}

// ChainRegistry combines another registry into this one.
func (hr *HookRegistry) ChainRegistry(other *HookRegistry) error {
	if other == nil {
		return nil
	}

	other.mu.RLock()
	defer other.mu.RUnlock()

	hr.mu.Lock()
	defer hr.mu.Unlock()

	for name, chain := range other.chains {
		if _, exists := hr.chains[name]; !exists {
			hr.chains[name] = chain
		}
	}

	return nil
}

// ExecuteChain retrieves a chain by name and executes it.
func (hr *HookRegistry) ExecuteChain(name string, elem *dataelem.DataElement, level HookLevel) (*dataelem.DataElement, error) {
	chain, exists := hr.GetChain(name)
	if !exists {
		return nil, fmt.Errorf("hook chain not found: %s", name)
	}

	return chain.ExecuteHooks(elem, level)
}

// HookSession represents a context for hook execution with pre-configured state.
type HookSession struct {
	registry     *HookRegistry
	chainName    string
	metadata     map[string]interface{}
	errorCount   int
	warningCount int
	mu           sync.RWMutex
}

// NewHookSession creates a new hook session with a registry and chain name.
func NewHookSession(registry *HookRegistry, chainName string) *HookSession {
	return &HookSession{
		registry:     registry,
		chainName:    chainName,
		metadata:     make(map[string]interface{}),
		errorCount:   0,
		warningCount: 0,
	}
}

// Execute runs the configured chain on an element.
func (hs *HookSession) Execute(elem *dataelem.DataElement, level HookLevel) (*dataelem.DataElement, error) {
	result, err := hs.registry.ExecuteChain(hs.chainName, elem, level)
	if err != nil {
		hs.mu.Lock()
		hs.errorCount++
		hs.mu.Unlock()
		return nil, err
	}
	return result, nil
}

// SetMetadata sets a metadata value in the session.
func (hs *HookSession) SetMetadata(key string, value interface{}) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	hs.metadata[key] = value
}

// GetMetadata retrieves a metadata value from the session.
func (hs *HookSession) GetMetadata(key string) (interface{}, bool) {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	val, exists := hs.metadata[key]
	return val, exists
}

// GetErrorCount returns the number of errors that occurred in this session.
func (hs *HookSession) GetErrorCount() int {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	return hs.errorCount
}

// GetWarningCount returns the number of warnings that occurred in this session.
func (hs *HookSession) GetWarningCount() int {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	return hs.warningCount
}

// Reset clears all session state.
func (hs *HookSession) Reset() {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	hs.metadata = make(map[string]interface{})
	hs.errorCount = 0
	hs.warningCount = 0
}

// Global hook registry for application-wide hook management.
var globalRegistry *HookRegistry

// ConvertRawDataElement converts a RawDataElement to a DataElement using hooks.
// This is the main integration point for raw element conversion, similar to
// pydicom's convert_raw_data_element function.
func ConvertRawDataElement(raw *RawDataElement, encoding string) (*dataelem.DataElement, error) {
	if raw == nil {
		return nil, fmt.Errorf("raw data element cannot be nil")
	}

	// Execute VR lookup hook
	vrData, err := ExecuteRawElementVR(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to execute VR hook: %w", err)
	}

	vrValue, ok := vrData["VR"]
	if !ok {
		return nil, fmt.Errorf("VR hook did not set VR value")
	}

	vr, ok := vrValue.(string)
	if !ok {
		return nil, fmt.Errorf("VR value is not a string: %T", vrValue)
	}

	// Execute value conversion hook
	valueData, err := ExecuteRawElementValue(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to execute value hook: %w", err)
	}

	value, ok := valueData["value"]
	if !ok {
		return nil, fmt.Errorf("value hook did not set value")
	}

	// Create DataElement from converted data
	elem := dataelem.NewDataElement(raw.Tag, dataelem.VR(vr), value)
	return elem, nil
}

func init() {
	globalRegistry = NewHookRegistry()
}

// RegisterGlobalChain registers a hook chain globally.
func RegisterGlobalChain(name string, chain *HookChain) error {
	return globalRegistry.RegisterChain(name, chain)
}

// GetGlobalChain retrieves a globally registered hook chain.
func GetGlobalChain(name string) (*HookChain, bool) {
	return globalRegistry.GetChain(name)
}

// ExecuteGlobalChain executes a globally registered chain.
func ExecuteGlobalChain(name string, elem *dataelem.DataElement, level HookLevel) (*dataelem.DataElement, error) {
	return globalRegistry.ExecuteChain(name, elem, level)
}

// ListGlobalChains returns all globally registered chain names.
func ListGlobalChains() []string {
	return globalRegistry.ListChains()
}
