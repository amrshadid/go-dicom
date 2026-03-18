# Hooks

Plugin/callback system for DICOM parsing and processing. Provides raw data hooks for VR lookup and value conversion, plus advanced hook chains for multi-stage element processing pipelines.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/hooks"

// Register a raw data VR hook
hooks.RegisterCallback("raw_element_vr", func(raw *hooks.RawDataElement, data, kwargs map[string]interface{}) error {
    if raw.VR != nil {
        data["VR"] = *raw.VR
    } else {
        data["VR"] = "UN"
    }
    return nil
})

// Advanced hook chains
chain := hooks.NewHookChain()
chain.RegisterHook(hooks.PreValidation, hooks.ValidatingHookFunc(func(elem *dataelem.DataElement) error {
    if elem == nil { return errors.New("nil element") }
    return nil
}))
result, err := chain.ExecuteHooks(elem, hooks.PreValidation)

// Built-in hooks
fixHook := hooks.FixSeparatorHook(',', '\\')
retryHook := hooks.RetryAlternateVRHook([]string{"DS"}, []string{"US", "SS"})
```

## API Reference

```go
// Raw data hooks
type RawDataElement struct { Tag string; VR *string; Value []byte }
type RawDataHook func(raw *RawDataElement, data, kwargs map[string]interface{}) error
func RegisterCallback(name string, hook RawDataHook)
func ExecuteRawElementVR(raw *RawDataElement) (map[string]interface{}, error)

// Hook chains
type HookLevel int // PreValidation, PostValidation, PreCompression, PostCompression, PreSerialization, PostSerialization
type AdvancedHookFunc func(elem *dataelem.DataElement) (*dataelem.DataElement, error)
func NewHookChain() *HookChain
func (hc *HookChain) RegisterHook(level HookLevel, hook AdvancedHookFunc)
func (hc *HookChain) ExecuteHooks(elem *dataelem.DataElement, level HookLevel) (*dataelem.DataElement, error)

// Hook types
func ValidatingHookFunc(fn func(*dataelem.DataElement) error) AdvancedHookFunc
func TransformingHookFunc(fn func(*dataelem.DataElement) (*dataelem.DataElement, error)) AdvancedHookFunc
func FilteringHookFunc(fn func(*dataelem.DataElement) bool) AdvancedHookFunc

// Built-in hooks
func FixSeparatorHook(oldSep, newSep byte) RawDataHook
func RetryAlternateVRHook(primaryVRs, alternateVRs []string) RawDataHook

// Registry and sessions
func NewHookRegistry() *HookRegistry
func NewHookSession(registry *HookRegistry, chainName string) *HookSession
```

## References

- [DICOM PS3.5 Section 7.1](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - VR determination for implicit transfer syntaxes
