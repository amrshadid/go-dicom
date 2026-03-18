package dataelem_test

import (
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

func TestValueCache_Set_Get(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	cache.Set("key1", "value1", 0)

	val, ok := cache.Get("key1")
	if !ok {
		t.Error("Expected cache hit, got miss")
	}

	if val != "value1" {
		t.Errorf("Retrieved value = %v, want 'value1'", val)
	}
}

func TestValueCache_Get_Miss(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	_, ok := cache.Get("nonexistent")
	if ok {
		t.Error("Expected cache miss, got hit")
	}
}

func TestValueCache_Contains(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	cache.Set("key1", "value1", 0)

	if !cache.Contains("key1") {
		t.Error("Expected key to be in cache")
	}

	if cache.Contains("nonexistent") {
		t.Error("Unexpected key in cache")
	}
}

func TestValueCache_Remove(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	cache.Set("key1", "value1", 0)
	cache.Remove("key1")

	if cache.Contains("key1") {
		t.Error("Key should have been removed")
	}
}

func TestValueCache_Clear(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	cache.Set("key1", "value1", 0)
	cache.Set("key2", "value2", 0)

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("After Clear, size = %d, want 0", cache.Size())
	}
}

func TestValueCache_TTL(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	// Set with very short TTL
	cache.Set("key1", "value1", 10*time.Millisecond)

	// Should be in cache immediately
	if !cache.Contains("key1") {
		t.Error("Key should be in cache immediately")
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Should be expired now
	val, ok := cache.Get("key1")
	if ok {
		t.Errorf("Expected expired entry to be gone, got value: %v", val)
	}
}

func TestValueCache_Enable_Disable(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	cache.Set("key1", "value1", 0)

	cache.Disable()
	if cache.IsEnabled() {
		t.Error("Cache should be disabled")
	}

	// Should not retrieve from disabled cache
	_, ok := cache.Get("key1")
	if ok {
		t.Error("Should not retrieve from disabled cache")
	}

	cache.Enable()
	if !cache.IsEnabled() {
		t.Error("Cache should be enabled")
	}
}

func TestValueCache_Size(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	if cache.Size() != 0 {
		t.Errorf("Initial size = %d, want 0", cache.Size())
	}

	cache.Set("key1", "value1", 0)
	cache.Set("key2", "value2", 0)

	if cache.Size() != 2 {
		t.Errorf("Size = %d, want 2", cache.Size())
	}
}

func TestValueCache_Stats(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	cache.Set("key1", "value1", 0)

	// Hit
	cache.Get("key1")

	// Miss
	cache.Get("nonexistent")

	stats := cache.Stats()

	if stats["size"] != 1 {
		t.Errorf("Stats size = %v, want 1", stats["size"])
	}

	if stats["hits"] != 1 {
		t.Errorf("Stats hits = %v, want 1", stats["hits"])
	}

	if stats["misses"] != 1 {
		t.Errorf("Stats misses = %v, want 1", stats["misses"])
	}
}

func TestValueCache_HitRate(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	cache.Set("key1", "value1", 0)

	// 2 hits, 1 miss
	cache.Get("key1")
	cache.Get("key1")
	cache.Get("nonexistent")

	stats := cache.Stats()
	hitRate := stats["hit_rate"].(float64)

	if hitRate != 66.66666666666666 {
		// We expect approximately 66.67%
		if hitRate < 65 || hitRate > 67 {
			t.Errorf("Hit rate = %f, want ~66.67", hitRate)
		}
	}
}

func TestValueCache_MaxSize(t *testing.T) {
	cache := dataelem.NewValueCache(3)

	cache.Set("key1", "value1", 0)
	time.Sleep(time.Millisecond) // ensure distinct timestamps for eviction ordering
	cache.Set("key2", "value2", 0)
	time.Sleep(time.Millisecond)
	cache.Set("key3", "value3", 0)
	time.Sleep(time.Millisecond)

	// This should evict the oldest entry
	cache.Set("key4", "value4", 0)

	if cache.Size() > 3 {
		t.Errorf("Cache exceeded max size: %d > 3", cache.Size())
	}
}

func TestCacheableDataElement_GetValueCached(t *testing.T) {
	cache := dataelem.NewValueCache(10)
	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Smith^John")

	cacheable := dataelem.NewCacheableDataElement(elem, cache)

	// First call should cache
	val1 := cacheable.GetValueCached()
	if val1 != "Smith^John" {
		t.Errorf("First call = %v, want 'Smith^John'", val1)
	}

	// Verify it was cached
	if cache.Size() != 1 {
		t.Errorf("Cache size = %d, want 1", cache.Size())
	}

	// Second call should retrieve from cache
	val2 := cacheable.GetValueCached()
	if val2 != "Smith^John" {
		t.Errorf("Second call = %v, want 'Smith^John'", val2)
	}

	stats := cache.Stats()
	if stats["hits"] != 1 {
		t.Errorf("Expected 1 hit, got %v", stats["hits"])
	}
}

func TestCacheableDataElement_GetValueWithTTL(t *testing.T) {
	cache := dataelem.NewValueCache(10)
	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Test")

	cacheable := dataelem.NewCacheableDataElement(elem, cache)

	val := cacheable.GetValueWithTTL(5 * time.Minute)
	if val != "Test" {
		t.Errorf("Value = %v, want 'Test'", val)
	}
}

func TestCacheableDataElement_SetValue_InvalidatesCache(t *testing.T) {
	cache := dataelem.NewValueCache(10)
	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Original")

	cacheable := dataelem.NewCacheableDataElement(elem, cache)

	// Cache the original value
	cacheable.GetValueCached()

	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.Size())
	}

	// Set new value
	cacheable.SetValue("Updated")

	// Cache should be invalidated (size might be 0 or just not contain old value)
	stats := cache.Stats()
	if stats["size"] == 1 {
		// The old value might still be there, but next Get should return the new value
		val := cacheable.GetValueCached()
		if val != "Updated" {
			t.Errorf("After SetValue, got %v, want 'Updated'", val)
		}
	}
}

func TestCacheableDataElement_GetElement(t *testing.T) {
	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "Test")
	cache := dataelem.NewValueCache(10)

	cacheable := dataelem.NewCacheableDataElement(elem, cache)

	retrieved := cacheable.GetElement()
	if retrieved != elem {
		t.Error("GetElement should return the wrapped element")
	}
}

func TestGlobalValueCache(t *testing.T) {
	globalCache := dataelem.GetGlobalCache()

	if globalCache == nil {
		t.Error("Global cache should not be nil")
	}

	// Test basic operations
	globalCache.Set("testKey", "testValue", 0)

	val, ok := globalCache.Get("testKey")
	if !ok || val != "testValue" {
		t.Error("Global cache operations failed")
	}

	// Clean up
	globalCache.Clear()
}

func TestValueCache_SetEmpty(t *testing.T) {
	cache := dataelem.NewValueCache(10)

	cache.Set("", "value", 0) // Empty key
	if cache.Contains("") {
		t.Error("Empty key should not be cached")
	}
}
