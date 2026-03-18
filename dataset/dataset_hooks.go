package dataset

import (
	"fmt"
	"sync"

	"github.com/amrshadid/go-dicom/dataelem"
)

// DatasetEventType represents the type of dataset event
type DatasetEventType int

const (
	// EventElementAdded fires when an element is added to the dataset
	EventElementAdded DatasetEventType = iota
	// EventElementRemoved fires when an element is removed from the dataset
	EventElementRemoved
	// EventElementUpdated fires when an element is updated in the dataset
	EventElementUpdated
	// EventDatasetCleared fires when the dataset is cleared
	EventDatasetCleared
	// EventDatasetMerged fires when another dataset is merged
	EventDatasetMerged
)

// String returns the string representation of the event type
func (et DatasetEventType) String() string {
	switch et {
	case EventElementAdded:
		return "ElementAdded"
	case EventElementRemoved:
		return "ElementRemoved"
	case EventElementUpdated:
		return "ElementUpdated"
	case EventDatasetCleared:
		return "DatasetCleared"
	case EventDatasetMerged:
		return "DatasetMerged"
	default:
		return "UnknownEvent"
	}
}

// DatasetEvent contains information about a dataset event
type DatasetEvent struct {
	EventType    DatasetEventType
	Dataset      *Dataset
	Element      *dataelem.DataElement
	OldElement   *dataelem.DataElement // For updates
	ElementCount int
	Error        error
}

// DatasetHookFunc is a callback function for dataset events
// Return nil to allow the operation, or an error to block it (for pre-events)
type DatasetHookFunc func(event *DatasetEvent) error

// DatasetEventHooks manages dataset-level event hooks
// Different from parsing-level hooks in the hooks package
type DatasetEventHooks struct {
	beforeAdd    []DatasetHookFunc
	afterAdd     []DatasetHookFunc
	beforeRemove []DatasetHookFunc
	afterRemove  []DatasetHookFunc
	beforeUpdate []DatasetHookFunc
	afterUpdate  []DatasetHookFunc
	onClear      []DatasetHookFunc
	onMerge      []DatasetHookFunc
	mu           sync.RWMutex
}

// NewDatasetEventHooks creates a new dataset event hooks manager
func NewDatasetEventHooks() *DatasetEventHooks {
	return &DatasetEventHooks{
		beforeAdd:    make([]DatasetHookFunc, 0),
		afterAdd:     make([]DatasetHookFunc, 0),
		beforeRemove: make([]DatasetHookFunc, 0),
		afterRemove:  make([]DatasetHookFunc, 0),
		beforeUpdate: make([]DatasetHookFunc, 0),
		afterUpdate:  make([]DatasetHookFunc, 0),
		onClear:      make([]DatasetHookFunc, 0),
		onMerge:      make([]DatasetHookFunc, 0),
	}
}

// AddBeforeAddHook registers a hook to run before an element is added
func (dh *DatasetEventHooks) AddBeforeAddHook(fn DatasetHookFunc) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.beforeAdd = append(dh.beforeAdd, fn)
}

// AddAfterAddHook registers a hook to run after an element is added
func (dh *DatasetEventHooks) AddAfterAddHook(fn DatasetHookFunc) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.afterAdd = append(dh.afterAdd, fn)
}

// AddBeforeRemoveHook registers a hook to run before an element is removed
func (dh *DatasetEventHooks) AddBeforeRemoveHook(fn DatasetHookFunc) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.beforeRemove = append(dh.beforeRemove, fn)
}

// AddAfterRemoveHook registers a hook to run after an element is removed
func (dh *DatasetEventHooks) AddAfterRemoveHook(fn DatasetHookFunc) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.afterRemove = append(dh.afterRemove, fn)
}

// AddBeforeUpdateHook registers a hook to run before an element is updated
func (dh *DatasetEventHooks) AddBeforeUpdateHook(fn DatasetHookFunc) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.beforeUpdate = append(dh.beforeUpdate, fn)
}

// AddAfterUpdateHook registers a hook to run after an element is updated
func (dh *DatasetEventHooks) AddAfterUpdateHook(fn DatasetHookFunc) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.afterUpdate = append(dh.afterUpdate, fn)
}

// AddOnClearHook registers a hook to run when dataset is cleared
func (dh *DatasetEventHooks) AddOnClearHook(fn DatasetHookFunc) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onClear = append(dh.onClear, fn)
}

// AddOnMergeHook registers a hook to run when another dataset is merged
func (dh *DatasetEventHooks) AddOnMergeHook(fn DatasetHookFunc) {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.onMerge = append(dh.onMerge, fn)
}

// RemoveAllHooks removes all registered hooks
func (dh *DatasetEventHooks) RemoveAllHooks() {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	dh.beforeAdd = make([]DatasetHookFunc, 0)
	dh.afterAdd = make([]DatasetHookFunc, 0)
	dh.beforeRemove = make([]DatasetHookFunc, 0)
	dh.afterRemove = make([]DatasetHookFunc, 0)
	dh.beforeUpdate = make([]DatasetHookFunc, 0)
	dh.afterUpdate = make([]DatasetHookFunc, 0)
	dh.onClear = make([]DatasetHookFunc, 0)
	dh.onMerge = make([]DatasetHookFunc, 0)
}

// FireBeforeAdd fires all before-add hooks
func (dh *DatasetEventHooks) FireBeforeAdd(event *DatasetEvent) error {
	dh.mu.RLock()
	hooks := make([]DatasetHookFunc, len(dh.beforeAdd))
	copy(hooks, dh.beforeAdd)
	dh.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(event); err != nil {
			return fmt.Errorf("beforeAdd hook error: %w", err)
		}
	}
	return nil
}

// FireAfterAdd fires all after-add hooks
func (dh *DatasetEventHooks) FireAfterAdd(event *DatasetEvent) error {
	dh.mu.RLock()
	hooks := make([]DatasetHookFunc, len(dh.afterAdd))
	copy(hooks, dh.afterAdd)
	dh.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(event); err != nil {
			return fmt.Errorf("afterAdd hook error: %w", err)
		}
	}
	return nil
}

// FireBeforeRemove fires all before-remove hooks
func (dh *DatasetEventHooks) FireBeforeRemove(event *DatasetEvent) error {
	dh.mu.RLock()
	hooks := make([]DatasetHookFunc, len(dh.beforeRemove))
	copy(hooks, dh.beforeRemove)
	dh.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(event); err != nil {
			return fmt.Errorf("beforeRemove hook error: %w", err)
		}
	}
	return nil
}

// FireAfterRemove fires all after-remove hooks
func (dh *DatasetEventHooks) FireAfterRemove(event *DatasetEvent) error {
	dh.mu.RLock()
	hooks := make([]DatasetHookFunc, len(dh.afterRemove))
	copy(hooks, dh.afterRemove)
	dh.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(event); err != nil {
			return fmt.Errorf("afterRemove hook error: %w", err)
		}
	}
	return nil
}

// FireBeforeUpdate fires all before-update hooks
func (dh *DatasetEventHooks) FireBeforeUpdate(event *DatasetEvent) error {
	dh.mu.RLock()
	hooks := make([]DatasetHookFunc, len(dh.beforeUpdate))
	copy(hooks, dh.beforeUpdate)
	dh.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(event); err != nil {
			return fmt.Errorf("beforeUpdate hook error: %w", err)
		}
	}
	return nil
}

// FireAfterUpdate fires all after-update hooks
func (dh *DatasetEventHooks) FireAfterUpdate(event *DatasetEvent) error {
	dh.mu.RLock()
	hooks := make([]DatasetHookFunc, len(dh.afterUpdate))
	copy(hooks, dh.afterUpdate)
	dh.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(event); err != nil {
			return fmt.Errorf("afterUpdate hook error: %w", err)
		}
	}
	return nil
}

// FireOnClear fires all on-clear hooks
func (dh *DatasetEventHooks) FireOnClear(event *DatasetEvent) error {
	dh.mu.RLock()
	hooks := make([]DatasetHookFunc, len(dh.onClear))
	copy(hooks, dh.onClear)
	dh.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(event); err != nil {
			return fmt.Errorf("onClear hook error: %w", err)
		}
	}
	return nil
}

// FireOnMerge fires all on-merge hooks
func (dh *DatasetEventHooks) FireOnMerge(event *DatasetEvent) error {
	dh.mu.RLock()
	hooks := make([]DatasetHookFunc, len(dh.onMerge))
	copy(hooks, dh.onMerge)
	dh.mu.RUnlock()

	for _, hook := range hooks {
		if err := hook(event); err != nil {
			return fmt.Errorf("onMerge hook error: %w", err)
		}
	}
	return nil
}

// HookCount returns the total number of registered hooks
func (dh *DatasetEventHooks) HookCount() int {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	return len(dh.beforeAdd) +
		len(dh.afterAdd) +
		len(dh.beforeRemove) +
		len(dh.afterRemove) +
		len(dh.beforeUpdate) +
		len(dh.afterUpdate) +
		len(dh.onClear) +
		len(dh.onMerge)
}

// HasHooks returns true if any hooks are registered
func (dh *DatasetEventHooks) HasHooks() bool {
	return dh.HookCount() > 0
}

// GlobalDatasetEventHooks is the global dataset event hooks instance
var GlobalDatasetEventHooks = NewDatasetEventHooks()

// RegisterDatasetHook registers a hook in the global dataset event hooks
func RegisterDatasetHook(eventType DatasetEventType, position string, fn DatasetHookFunc) error {
	if fn == nil {
		return fmt.Errorf("hook function cannot be nil")
	}

	switch eventType {
	case EventElementAdded:
		if position == "before" {
			GlobalDatasetEventHooks.AddBeforeAddHook(fn)
		} else if position == "after" {
			GlobalDatasetEventHooks.AddAfterAddHook(fn)
		} else {
			return fmt.Errorf("invalid position: %s (use 'before' or 'after')", position)
		}
	case EventElementRemoved:
		if position == "before" {
			GlobalDatasetEventHooks.AddBeforeRemoveHook(fn)
		} else if position == "after" {
			GlobalDatasetEventHooks.AddAfterRemoveHook(fn)
		} else {
			return fmt.Errorf("invalid position: %s", position)
		}
	case EventElementUpdated:
		if position == "before" {
			GlobalDatasetEventHooks.AddBeforeUpdateHook(fn)
		} else if position == "after" {
			GlobalDatasetEventHooks.AddAfterUpdateHook(fn)
		} else {
			return fmt.Errorf("invalid position: %s", position)
		}
	case EventDatasetCleared:
		GlobalDatasetEventHooks.AddOnClearHook(fn)
	case EventDatasetMerged:
		GlobalDatasetEventHooks.AddOnMergeHook(fn)
	default:
		return fmt.Errorf("unknown event type: %v", eventType)
	}

	return nil
}

// ClearDatasetHooks clears all global dataset event hooks
func ClearDatasetHooks() {
	GlobalDatasetEventHooks.RemoveAllHooks()
}

// GetDatasetHookCount returns the total number of global hooks
func GetDatasetHookCount() int {
	return GlobalDatasetEventHooks.HookCount()
}
