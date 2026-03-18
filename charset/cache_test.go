package charset_test

import (
	"fmt"
	"testing"

	"github.com/amrshadid/go-dicom/charset"
)

func TestCache_Basic(t *testing.T) {
	cache := charset.NewCache(100)

	value := []byte("Hello World")
	encoding := "UTF-8"
	decoded := "Hello World"

	// First get should miss
	_, found := cache.Get(value, encoding)
	if found {
		t.Error("Cache.Get() should miss on first access")
	}

	// Put value
	cache.Put(value, encoding, decoded)

	// Second get should hit
	result, found := cache.Get(value, encoding)
	if !found {
		t.Error("Cache.Get() should hit after Put()")
	}
	if result != decoded {
		t.Errorf("Cache.Get() = %q, want %q", result, decoded)
	}
}

func TestCache_Stats(t *testing.T) {
	cache := charset.NewCache(100)

	value := []byte("Test")
	encoding := "UTF-8"

	// Initial stats
	hits, misses, size := cache.Stats()
	if hits != 0 || misses != 0 || size != 0 {
		t.Errorf("Initial stats = (%d, %d, %d), want (0, 0, 0)", hits, misses, size)
	}

	// Miss
	_, _ = cache.Get(value, encoding)

	hits, misses, _ = cache.Stats()
	if hits != 0 || misses != 1 {
		t.Errorf("After miss stats = (%d, %d), want (0, 1)", hits, misses)
	}

	// Put and hit
	cache.Put(value, encoding, "Test")
	_, _ = cache.Get(value, encoding)

	hits, misses, size = cache.Stats()
	if hits != 1 || misses != 1 || size != 1 {
		t.Errorf("After hit stats = (%d, %d, %d), want (1, 1, 1)", hits, misses, size)
	}
}

func TestCache_HitRate(t *testing.T) {
	cache := charset.NewCache(100)

	// No accesses yet
	hitRate := cache.HitRate()
	if hitRate != 0.0 {
		t.Errorf("HitRate() with no accesses = %f, want 0.0", hitRate)
	}

	value := []byte("Test")
	encoding := "UTF-8"

	// 1 miss
	cache.Get(value, encoding)
	hitRate = cache.HitRate()
	if hitRate != 0.0 {
		t.Errorf("HitRate() with 1 miss = %f, want 0.0", hitRate)
	}

	// Put and 1 hit
	cache.Put(value, encoding, "Test")
	cache.Get(value, encoding)
	hitRate = cache.HitRate()
	if hitRate != 50.0 {
		t.Errorf("HitRate() with 1 hit, 1 miss = %f, want 50.0", hitRate)
	}

	// Another hit
	cache.Get(value, encoding)
	hitRate = cache.HitRate()
	expectedRate := (2.0 / 3.0) * 100.0
	if fmt.Sprintf("%.1f", hitRate) != fmt.Sprintf("%.1f", expectedRate) {
		t.Errorf("HitRate() with 2 hits, 1 miss = %f, want ~%f", hitRate, expectedRate)
	}
}

func TestCache_Clear(t *testing.T) {
	cache := charset.NewCache(100)

	// Add some entries
	for i := 0; i < 10; i++ {
		value := []byte(fmt.Sprintf("Value%d", i))
		cache.Put(value, "UTF-8", fmt.Sprintf("Decoded%d", i))
	}

	_, _, size := cache.Stats()
	if size != 10 {
		t.Errorf("Cache size before clear = %d, want 10", size)
	}

	// Clear cache
	cache.Clear()

	hits, misses, size := cache.Stats()
	if hits != 0 || misses != 0 || size != 0 {
		t.Errorf("Stats after clear = (%d, %d, %d), want (0, 0, 0)", hits, misses, size)
	}
}

func TestCache_MaxSize(t *testing.T) {
	maxSize := 10
	cache := charset.NewCache(maxSize)

	// Add more than maxSize entries
	for i := 0; i < maxSize+5; i++ {
		value := []byte(fmt.Sprintf("Value%d", i))
		cache.Put(value, "UTF-8", fmt.Sprintf("Decoded%d", i))
	}

	_, _, size := cache.Stats()
	if size > maxSize {
		t.Errorf("Cache size = %d, should not exceed maxSize %d", size, maxSize)
	}
}

func TestCache_Enable(t *testing.T) {
	cache := charset.NewCache(100)

	value := []byte("Test")
	encoding := "UTF-8"

	// Cache is enabled by default
	if !cache.IsEnabled() {
		t.Error("Cache should be enabled by default")
	}

	// Put and get with enabled cache
	cache.Put(value, encoding, "Test")
	result, found := cache.Get(value, encoding)
	if !found || result != "Test" {
		t.Error("Cache should work when enabled")
	}

	// Disable cache
	cache.Enable(false)
	if cache.IsEnabled() {
		t.Error("Cache should be disabled after Enable(false)")
	}

	// Get should miss when disabled
	_, found = cache.Get(value, encoding)
	if found {
		t.Error("Cache.Get() should always miss when disabled")
	}

	// Put should not store when disabled
	value2 := []byte("Test2")
	cache.Put(value2, encoding, "Test2")
	_, found = cache.Get(value2, encoding)
	if found {
		t.Error("Cache.Put() should not store when disabled")
	}
}

func TestDecodeBytesWithCache(t *testing.T) {
	// Clear default cache
	charset.ClearDefaultCache()

	data := []byte("Hello World")
	encodings := []string{"UTF-8"}

	// First call - should decode and cache
	result1, err := charset.DecodeBytesWithCache(data, encodings, charset.DefaultTextDelimiters)
	if err != nil {
		t.Errorf("DecodeBytesWithCache() error = %v", err)
		return
	}
	if result1 != "Hello World" {
		t.Errorf("DecodeBytesWithCache() = %q, want %q", result1, "Hello World")
	}

	// Check cache stats
	hits, misses, _, _ := charset.GetCacheStats()
	if hits != 0 || misses != 1 {
		t.Errorf("After first decode: hits=%d misses=%d, want hits=0 misses=1", hits, misses)
	}

	// Second call - should hit cache
	result2, err := charset.DecodeBytesWithCache(data, encodings, charset.DefaultTextDelimiters)
	if err != nil {
		t.Errorf("DecodeBytesWithCache() second call error = %v", err)
		return
	}
	if result2 != result1 {
		t.Errorf("DecodeBytesWithCache() second call = %q, want %q", result2, result1)
	}

	// Check cache stats
	hits, misses, size, hitRate := charset.GetCacheStats()
	if hits != 1 || misses != 1 {
		t.Errorf("After second decode: hits=%d misses=%d, want hits=1 misses=1", hits, misses)
	}
	if size != 1 {
		t.Errorf("Cache size = %d, want 1", size)
	}
	if hitRate != 50.0 {
		t.Errorf("Hit rate = %f, want 50.0", hitRate)
	}
}

func TestEnableCache(t *testing.T) {
	cache := charset.GetDefaultCache()

	// Cache should be enabled by default
	if !cache.IsEnabled() {
		t.Error("Default cache should be enabled by default")
	}

	// Disable cache
	charset.EnableCache(false)
	if cache.IsEnabled() {
		t.Error("Cache should be disabled after EnableCache(false)")
	}

	// Re-enable cache
	charset.EnableCache(true)
	if !cache.IsEnabled() {
		t.Error("Cache should be enabled after EnableCache(true)")
	}
}

func TestCache_DifferentEncodings(t *testing.T) {
	cache := charset.NewCache(100)

	value := []byte{0xE9} // é in Latin-1

	// Same value, different encodings should be different cache entries
	cache.Put(value, "ISO-8859-1", "é")
	cache.Put(value, "UTF-8", "�") // Invalid UTF-8

	result1, found1 := cache.Get(value, "ISO-8859-1")
	result2, found2 := cache.Get(value, "UTF-8")

	if !found1 || !found2 {
		t.Error("Both encodings should be cached")
	}
	if result1 == result2 {
		t.Errorf("Different encodings should cache different results")
	}
}

func TestCache_Concurrent(t *testing.T) {
	cache := charset.NewCache(1000)

	// Test concurrent access
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func(id int) {
			value := []byte(fmt.Sprintf("Value%d", id%10))
			encoding := "UTF-8"
			decoded := fmt.Sprintf("Decoded%d", id%10)

			// Mix of puts and gets
			cache.Put(value, encoding, decoded)
			cache.Get(value, encoding)

			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	// Cache should still be functional
	_, _, size := cache.Stats()
	if size == 0 {
		t.Error("Cache should have entries after concurrent operations")
	}
}

// Benchmark cache operations
func BenchmarkCache_Get_Hit(b *testing.B) {
	cache := charset.NewCache(1000)
	value := []byte("Hello World")
	encoding := "UTF-8"
	cache.Put(value, encoding, "Hello World")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get(value, encoding)
	}
}

func BenchmarkCache_Get_Miss(b *testing.B) {
	cache := charset.NewCache(1000)
	encoding := "UTF-8"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value := []byte(fmt.Sprintf("Value%d", i))
		_, _ = cache.Get(value, encoding)
	}
}

func BenchmarkCache_Put(b *testing.B) {
	cache := charset.NewCache(10000)
	encoding := "UTF-8"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value := []byte(fmt.Sprintf("Value%d", i))
		cache.Put(value, encoding, fmt.Sprintf("Decoded%d", i))
	}
}

func BenchmarkDecodeBytesWithCache_Hit(b *testing.B) {
	charset.ClearDefaultCache()
	data := []byte("Hello World")
	encodings := []string{"UTF-8"}

	// Prime the cache
	charset.DecodeBytesWithCache(data, encodings, charset.DefaultTextDelimiters)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytesWithCache(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkDecodeBytesWithCache_Miss(b *testing.B) {
	charset.ClearDefaultCache()
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data := []byte(fmt.Sprintf("Value%d", i))
		_, _ = charset.DecodeBytesWithCache(data, encodings, charset.DefaultTextDelimiters)
	}
}

func BenchmarkDecodeBytes_NoCache(b *testing.B) {
	data := []byte("Hello World")
	encodings := []string{"UTF-8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
	}
}

// Comparison benchmark
func BenchmarkDecodeBytes_WithVsWithoutCache(b *testing.B) {
	data := []byte("Hello World")
	encodings := []string{"UTF-8"}

	b.Run("WithoutCache", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = charset.DecodeBytes(data, encodings, charset.DefaultTextDelimiters)
		}
	})

	b.Run("WithCache", func(b *testing.B) {
		charset.ClearDefaultCache()
		// Prime cache
		charset.DecodeBytesWithCache(data, encodings, charset.DefaultTextDelimiters)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = charset.DecodeBytesWithCache(data, encodings, charset.DefaultTextDelimiters)
		}
	})
}
