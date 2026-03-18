package dataelem

import (
	"fmt"
	"sync"
	"time"
)

// CacheEntry represents a cached value with metadata
type CacheEntry struct {
	Value     interface{}
	Timestamp time.Time
	HitCount  int
	TTL       time.Duration // Time To Live
}

// IsExpired checks if the cache entry has expired
func (ce *CacheEntry) IsExpired() bool {
	if ce.TTL == 0 {
		return false // No expiration
	}
	return time.Since(ce.Timestamp) > ce.TTL
}

// ValueCache provides thread-safe caching for converted values
type ValueCache struct {
	cache     map[string]*CacheEntry
	mu        sync.RWMutex
	maxSize   int
	hitCount  int
	missCount int
	mu2       sync.Mutex // Locks hit/miss counters separately to reduce contention on the main mu
	enabled   bool
}

// NewValueCache creates a new value cache with specified max size
func NewValueCache(maxSize int) *ValueCache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &ValueCache{
		cache:   make(map[string]*CacheEntry),
		maxSize: maxSize,
		enabled: true,
	}
}

// Set stores a value in the cache
func (vc *ValueCache) Set(key string, value interface{}, ttl time.Duration) {
	if !vc.enabled || key == "" {
		return
	}

	vc.mu.Lock()
	defer vc.mu.Unlock()

	// Evict oldest entry if cache is full
	if len(vc.cache) >= vc.maxSize && vc.cache[key] == nil {
		vc.evictOldest()
	}

	vc.cache[key] = &CacheEntry{
		Value:     value,
		Timestamp: time.Now(),
		HitCount:  0,
		TTL:       ttl,
	}
}

// Get retrieves a value from the cache
func (vc *ValueCache) Get(key string) (interface{}, bool) {
	if !vc.enabled || key == "" {
		return nil, false
	}

	vc.mu.RLock()
	entry, exists := vc.cache[key]
	vc.mu.RUnlock()

	if !exists {
		vc.recordMiss()
		return nil, false
	}

	// Check if expired
	if entry.IsExpired() {
		vc.mu.Lock()
		delete(vc.cache, key)
		vc.mu.Unlock()
		vc.recordMiss()
		return nil, false
	}

	// Update hit count
	vc.mu.Lock()
	entry.HitCount++
	vc.mu.Unlock()

	vc.recordHit()
	return entry.Value, true
}

// Contains checks if a key exists in the cache (without updating hit count)
func (vc *ValueCache) Contains(key string) bool {
	if !vc.enabled || key == "" {
		return false
	}

	vc.mu.RLock()
	entry, exists := vc.cache[key]
	vc.mu.RUnlock()

	if !exists {
		return false
	}

	return !entry.IsExpired()
}

// Remove deletes a specific entry from the cache
func (vc *ValueCache) Remove(key string) {
	if !vc.enabled {
		return
	}

	vc.mu.Lock()
	defer vc.mu.Unlock()

	delete(vc.cache, key)
}

// Clear removes all entries from the cache
func (vc *ValueCache) Clear() {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.cache = make(map[string]*CacheEntry)
	vc.resetStats()
}

// Enable enables the cache
func (vc *ValueCache) Enable() {
	vc.enabled = true
}

// Disable disables the cache
func (vc *ValueCache) Disable() {
	vc.enabled = false
}

// IsEnabled checks if the cache is enabled
func (vc *ValueCache) IsEnabled() bool {
	return vc.enabled
}

// Size returns the current number of entries in the cache
func (vc *ValueCache) Size() int {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	count := 0
	for _, entry := range vc.cache {
		if !entry.IsExpired() {
			count++
		}
	}
	return count
}

// Stats returns cache statistics
func (vc *ValueCache) Stats() map[string]interface{} {
	vc.mu.RLock()
	size := len(vc.cache)
	vc.mu.RUnlock()

	vc.mu2.Lock()
	hitRate := float64(0)
	if vc.hitCount+vc.missCount > 0 {
		hitRate = float64(vc.hitCount) / float64(vc.hitCount+vc.missCount) * 100
	}
	vc.mu2.Unlock()

	return map[string]interface{}{
		"size":     size,
		"max_size": vc.maxSize,
		"hits":     vc.hitCount,
		"misses":   vc.missCount,
		"hit_rate": hitRate,
		"enabled":  vc.enabled,
	}
}

// evictOldest removes the oldest (least recently used) entry
func (vc *ValueCache) evictOldest() {
	oldestKey := ""
	oldestTime := time.Now()

	for key, entry := range vc.cache {
		if entry.Timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Timestamp
		}
	}

	if oldestKey != "" {
		delete(vc.cache, oldestKey)
	}
}

// recordHit increments the hit counter
func (vc *ValueCache) recordHit() {
	vc.mu2.Lock()
	vc.hitCount++
	vc.mu2.Unlock()
}

// recordMiss increments the miss counter
func (vc *ValueCache) recordMiss() {
	vc.mu2.Lock()
	vc.missCount++
	vc.mu2.Unlock()
}

// resetStats resets hit/miss counters
func (vc *ValueCache) resetStats() {
	vc.mu2.Lock()
	defer vc.mu2.Unlock()
	vc.hitCount = 0
	vc.missCount = 0
}

// GlobalValueCache is the module-level cache for data elements
var globalValueCache = NewValueCache(1000)

// SetGlobalCache replaces the global cache instance
func SetGlobalCache(cache *ValueCache) {
	if cache != nil {
		globalValueCache = cache
	}
}

// GetGlobalCache returns the global cache instance
func GetGlobalCache() *ValueCache {
	return globalValueCache
}

// CacheKey generates a cache key for a data element value
func CacheKey(tag interface{}, vr VR, value interface{}) string {
	return fmt.Sprintf("%v:%s:%v", tag, vr, value)
}

// CacheableDataElement wraps a DataElement with caching support
type CacheableDataElement struct {
	elem  *DataElement
	cache *ValueCache
}

// NewCacheableDataElement creates a cacheable wrapper around a DataElement
func NewCacheableDataElement(elem *DataElement, cache *ValueCache) *CacheableDataElement {
	if cache == nil {
		cache = globalValueCache
	}
	return &CacheableDataElement{
		elem:  elem,
		cache: cache,
	}
}

// GetValueCached retrieves the value with caching
func (cde *CacheableDataElement) GetValueCached() interface{} {
	if !cde.cache.IsEnabled() {
		return cde.elem.GetValue()
	}

	key := fmt.Sprintf("%s:%s", cde.elem.GetTag(), cde.elem.GetVR())

	// Try cache
	if val, ok := cde.cache.Get(key); ok {
		return val
	}

	// Get value and cache it
	val := cde.elem.GetValue()
	cde.cache.Set(key, val, time.Hour) // 1 hour TTL

	return val
}

// GetValueWithTTL retrieves the value with custom TTL
func (cde *CacheableDataElement) GetValueWithTTL(ttl time.Duration) interface{} {
	if !cde.cache.IsEnabled() {
		return cde.elem.GetValue()
	}

	key := fmt.Sprintf("%s:%s", cde.elem.GetTag(), cde.elem.GetVR())

	// Try cache
	if val, ok := cde.cache.Get(key); ok {
		return val
	}

	// Get value and cache it
	val := cde.elem.GetValue()
	cde.cache.Set(key, val, ttl)

	return val
}

// SetValue updates the value and invalidates cache
func (cde *CacheableDataElement) SetValue(value interface{}) {
	cde.elem.SetValue(value)

	// Invalidate cache
	key := fmt.Sprintf("%s:%s", cde.elem.GetTag(), cde.elem.GetVR())
	cde.cache.Remove(key)
}

// GetElement returns the underlying DataElement
func (cde *CacheableDataElement) GetElement() *DataElement {
	return cde.elem
}
