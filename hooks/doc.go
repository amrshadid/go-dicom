// Package hooks provides DICOM parsing and processing hook support.
//
// This package implements both basic raw data hooks and advanced hook chains
// for controlling DICOM element processing at multiple stages. Hooks enable
// customization of VR handling, value conversion, validation, transformation,
// and filtering during DICOM parsing and writing.
//
// # Core Concepts
//
// ## Basic Hooks
//
// Basic hooks handle raw DICOM element data before conversion, including VR
// lookup and value conversion from binary to typed representations.
//
// ## Raw Data Element
//
// Represents an unparsed DICOM element with tag, optional VR, and raw bytes.
// Raw data hooks receive these elements for conversion.
//
// ## Hook Functions
//
// RawDataHook is a callback that processes raw elements with access to kwargs
// dictionary for configuration. Returns result data map and error.
//
// ## Advanced Hooks
//
// Advanced hooks process typed DataElement objects at specific processing levels,
// enabling validation, transformation, and filtering throughout the parsing pipeline.
//
// ## Hook Levels
//
// Hooks execute at 6 processing stages:
//   - PreValidation: Before element validation
//   - PostValidation: After element validation
//   - PreCompression: Before data compression
//   - PostCompression: After data compression
//   - PreSerialization: Before dataset serialization
//   - PostSerialization: After dataset serialization
//
// ## Hook Chains
//
// HookChain manages multiple AdvancedHookFunc at each level. Multiple hooks
// at the same level execute in registration order, with output feeding to next.
//
// ## Hook Registry
//
// HookRegistry maintains named hook chains for reuse across application.
// Enables pre-configured processing pipelines.
//
// # Basic Usage
//
// ## Using Global Hooks
//
//	raw := &hooks.RawDataElement{
//	    Tag:   "0010,0010",
//	    VR:    strPtr("PN"),
//	    Value: []byte("Doe^John"),
//	}
//
//	// Register custom callback
//	hooks.RegisterCallback("raw_element_value", customHook)
//
//	// Execute hook
//	result, err := hooks.ExecuteRawElementValue(raw)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// ## Using Hook Chains
//
//	chain := hooks.NewHookChain()
//
//	// Register hook at specific level
//	err := chain.RegisterHook(hooks.PostValidation, myHook)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Execute hooks on element
//	result, err := chain.ExecuteHooks(elem, hooks.PostValidation)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Advanced Usage
//
// ## Hook Registries
//
// Create reusable hook configurations:
//
//	registry := hooks.NewHookRegistry()
//
//	// Create and register chain
//	chain := hooks.NewHookChain()
//	chain.RegisterHook(hooks.PreValidation, validateHook)
//	chain.RegisterHook(hooks.PostValidation, transformHook)
//
//	registry.RegisterChain("medical_imaging", chain)
//
//	// Use in application
//	result, err := registry.ExecuteChain("medical_imaging", elem, hooks.PreValidation)
//
// ## Hook Sessions
//
// Create execution context with metadata and error tracking:
//
//	session := hooks.NewHookSession(registry, "medical_imaging")
//
//	// Track processing state
//	session.SetMetadata("source", "PACS")
//	session.SetMetadata("timestamp", time.Now())
//
//	// Execute with error tracking
//	result, err := session.Execute(elem, hooks.PreValidation)
//	if err != nil {
//	    fmt.Printf("Errors: %d\n", session.GetErrorCount())
//	}
//
// ## Combining Hook Chains
//
// Merge multiple chains into single pipeline:
//
//	chain1 := hooks.NewHookChain()
//	chain1.RegisterHook(hooks.PreValidation, hook1)
//
//	chain2 := hooks.NewHookChain()
//	chain2.RegisterHook(hooks.PostValidation, hook2)
//
//	// Combine chains
//	chain1.ChainHooks(chain2)
//
// # Hook Functions
//
// ## Validating Hooks
//
// Hooks that validate without modification:
//
//	validator := hooks.ValidatingHookFunc(func(elem *dataelem.DataElement) error {
//	    if elem == nil {
//	        return fmt.Errorf("element cannot be nil")
//	    }
//	    return nil
//	})
//
// ## Transforming Hooks
//
// Hooks that modify elements:
//
//	transformer := hooks.TransformingHookFunc(func(elem *dataelem.DataElement) (*dataelem.DataElement, error) {
//	    // Modify element
//	    return elem, nil
//	})
//
// ## Filtering Hooks
//
// Hooks that accept/reject elements:
//
//	filter := hooks.FilteringHookFunc(func(elem *dataelem.DataElement) bool {
//	    // Return true to keep, false to filter out
//	    return elem.Tag != "0010,1010"  // Filter out age
//	})
//
// # Raw Data Hooks
//
// ## VR Lookup Hook
//
// Determine Value Representation from raw element:
//
//	hooks.RegisterCallback("raw_element_vr", func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
//	    if raw.VR != nil {
//	        data["VR"] = *raw.VR
//	    } else {
//	        data["VR"] = "UN"  // Unknown
//	    }
//	    return nil
//	})
//
// ## Value Conversion Hook
//
// Convert binary data to typed value:
//
//	hooks.RegisterCallback("raw_element_value", func(raw *hooks.RawDataElement, data map[string]interface{}, kwargs map[string]interface{}) error {
//	    // Convert based on VR type
//	    data["value"] = string(raw.Value)
//	    return nil
//	})
//
// ## Separator Fixing Hook
//
// Fix multivalue separator issues:
//
//	hook := hooks.FixSeparatorHook('\\', '^')
//	hooks.RegisterCallback("raw_element_value", hook)
//
// ## Alternate VR Hook
//
// Retry conversion with alternate VR if primary fails:
//
//	hook := hooks.RetryAlternateVRHook(
//	    []string{"OB"},
//	    []string{"OW", "UN"},
//	)
//	hooks.RegisterCallback("raw_element_value", hook)
//
// # Thread Safety
//
// All hook types are thread-safe through internal sync.RWMutex:
//   - Multiple goroutines can read concurrently
//   - Write operations are mutually exclusive
//   - Safe for concurrent hook registration and execution
//
// Example:
//
//	chain := hooks.NewHookChain()
//
//	// Register hook from one goroutine
//	go func() {
//	    chain.RegisterHook(hooks.PreValidation, hook1)
//	}()
//
//	// Execute from multiple goroutines
//	for i := 0; i < 10; i++ {
//	    go func() {
//	        result, _ := chain.ExecuteHooks(elem, hooks.PreValidation)
//	    }()
//	}
//
// # Performance Characteristics
//
//   - **RegisterHook**: O(1) - Append to hook slice
//   - **ExecuteHooks**: O(h*n) where h is hook count, n is hook complexity
//   - **RegisterChain**: O(1) - Map insertion
//   - **GetChain**: O(1) - Map lookup
//   - **RemoveChain**: O(1) - Map deletion
//   - **ChainHooks**: O(h) where h is total hooks in source chains
//   - **ClearHooks**: O(1) - Delete map entry
//   - **HookCount**: O(l) where l is number of levels
//
// # Error Handling
//
// Hooks return errors for:
//   - Nil elements (ExecuteHooks, all AdvancedHookFunc)
//   - Nil callback functions (RegisterCallback, RegisterHook)
//   - Unknown hook names (RegisterCallback)
//   - Missing hook chains (ExecuteChain)
//   - Hook execution failures (logged in error)
//   - Invalid configuration (empty names, nil chains)
//
// Example:
//
//	chain := hooks.NewHookChain()
//	err := chain.RegisterHook(hooks.PreValidation, nil)
//	if err != nil {
//	    log.Printf("Error: %v", err)  // "hook function cannot be nil"
//	}
//
// # Use Cases
//
// ## DICOM Parsing Customization
//
// Customize VR and value handling during DICOM parsing:
//
//	hooks.RegisterCallback("raw_element_vr", customVRLookup)
//	hooks.RegisterCallback("raw_element_value", customConversion)
//
// ## Multi-Stage Validation Pipeline
//
// Validate elements at multiple processing stages:
//
//	chain := hooks.NewHookChain()
//	chain.RegisterHook(hooks.PreValidation, structuralValidator)
//	chain.RegisterHook(hooks.PostValidation, semanticValidator)
//
// ## Data Transformation Pipeline
//
// Transform data during processing:
//
//	chain := hooks.NewHookChain()
//	chain.RegisterHook(hooks.PreCompression, anonymizer)
//	chain.RegisterHook(hooks.PostCompression, encryptor)
//
// ## Element Filtering
//
// Filter elements based on predicates:
//
//	filter := hooks.FilteringHookFunc(func(elem *dataelem.DataElement) bool {
//	    return !isPrivateTag(elem.Tag)
//	})
//	chain.RegisterHook(hooks.PreSerialization, filter)
//
// # Global Hook Management
//
// Application-wide hook management:
//
//	// Register global chain
//	hooks.RegisterGlobalChain("standard", chain)
//
//	// Execute global chain
//	result, _ := hooks.ExecuteGlobalChain("standard", elem, level)
//
//	// List registered chains
//	names := hooks.ListGlobalChains()
//
// # See Also
//
//   - dataelem package: Data element structure and handling
//   - dataset package: DICOM dataset structure
//   - tag package: DICOM tag definitions
//   - values package: Value encoding and conversion
package hooks
