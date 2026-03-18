package charset

import (
	"hash/fnv"
	"sync"
)

// Cache provides caching for decoded values to improve performance.
type Cache struct {
	mu      sync.RWMutex
	entries map[uint64]string
	maxSize int
	hits    uint64
	misses  uint64
	enabled bool
}

// NewCache creates a new cache with the specified maximum size.
func NewCache(maxSize int) *Cache {
	if maxSize < 0 {
		maxSize = 0
	}

	return &Cache{
		entries: make(map[uint64]string),
		maxSize: maxSize,
		enabled: true,
	}
}

// hash computes a hash for the cache key.
func (c *Cache) hash(value []byte, encoding string) uint64 {
	h := fnv.New64a()
	h.Write(value)
	h.Write([]byte(encoding))
	return h.Sum64()
}

// Get retrieves a decoded value from the cache.
func (c *Cache) Get(value []byte, encoding string) (string, bool) {
	if !c.enabled {
		return "", false
	}

	key := c.hash(value, encoding)

	c.mu.RLock()
	result, found := c.entries[key]
	c.mu.RUnlock()

	if found {
		c.mu.Lock()
		c.hits++
		c.mu.Unlock()
	} else {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
	}

	return result, found
}

// Put stores a decoded value in the cache.
func (c *Cache) Put(value []byte, encoding string, decoded string) {
	if !c.enabled {
		return
	}

	key := c.hash(value, encoding)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict entries
	if c.maxSize > 0 && len(c.entries) >= c.maxSize {
		// Simple random eviction - remove first entry found
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}

	c.entries[key] = decoded
}

// Clear clears all cached entries.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[uint64]string)
	c.hits = 0
	c.misses = 0
}

// Stats returns cache statistics.
func (c *Cache) Stats() (hits, misses uint64, size int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.hits, c.misses, len(c.entries)
}

// HitRate returns the cache hit rate as a percentage.
func (c *Cache) HitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	if total == 0 {
		return 0.0
	}

	return float64(c.hits) / float64(total) * 100.0
}

// Enable enables or disables the cache.
func (c *Cache) Enable(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enabled = enabled
}

// IsEnabled returns whether the cache is enabled.
func (c *Cache) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.enabled
}

// defaultCache is a global cache instance that can be used across the application.
var defaultCache = NewCache(10000) // Cache up to 10,000 entries by default

// DecodeBytesWithCache decodes bytes using the default global cache.
func DecodeBytesWithCache(value []byte, encodings []string, delimiters DelimiterSet) (string, error) {
	if len(value) == 0 {
		return "", nil
	}

	if len(encodings) == 0 {
		encodings = []string{DefaultEncoding}
	}

	// Try to get from cache
	encodingKey := encodings[0] // Use first encoding as key
	if cached, found := defaultCache.Get(value, encodingKey); found {
		return cached, nil
	}

	// Not in cache, decode normally
	decoded, err := DecodeBytes(value, encodings, delimiters)
	if err != nil {
		return "", err
	}

	// Store in cache
	defaultCache.Put(value, encodingKey, decoded)

	return decoded, nil
}

// GetDefaultCache returns the default global cache instance.
func GetDefaultCache() *Cache {
	return defaultCache
}

// ClearDefaultCache clears the default global cache.
func ClearDefaultCache() {
	defaultCache.Clear()
}

// GetCacheStats returns statistics for the default global cache.
func GetCacheStats() (hits, misses uint64, size int, hitRate float64) {
	hits, misses, size = defaultCache.Stats()
	hitRate = defaultCache.HitRate()
	return
}

// EnableCache enables or disables the default global cache.
func EnableCache(enabled bool) {
	defaultCache.Enable(enabled)
}
