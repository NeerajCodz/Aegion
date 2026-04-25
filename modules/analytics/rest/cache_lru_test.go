package rest_test

import (
	"context"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/analytics/rest"
)

func TestLRUQueryCache_Set_Get(t *testing.T) {
	config := rest.QueryCacheConfig{
		Enabled:       true,
		MaxSize:       100,
		TTL:           1 * time.Minute,
		StatsInterval: 10 * time.Second,
	}
	cache := rest.NewLRUQueryCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Set a value
	err := cache.Set(ctx, "test_key", "test_value", 0)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get the value
	value, found, err := cache.Get(ctx, "test_key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if !found {
		t.Fatal("Value not found in cache")
	}

	if value != "test_value" {
		t.Fatalf("Expected 'test_value', got '%v'", value)
	}
}

func TestLRUQueryCache_Expiration(t *testing.T) {
	config := rest.QueryCacheConfig{
		Enabled:       true,
		MaxSize:       100,
		TTL:           100 * time.Millisecond,
		StatsInterval: 10 * time.Second,
	}
	cache := rest.NewLRUQueryCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Set a value with short TTL
	cache.Set(ctx, "expiring_key", "value", 50*time.Millisecond)

	// Should be found immediately
	_, found, _ := cache.Get(ctx, "expiring_key")
	if !found {
		t.Fatal("Value should be found immediately after Set")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	_, found, _ = cache.Get(ctx, "expiring_key")
	if found {
		t.Fatal("Value should be expired")
	}
}

func TestLRUQueryCache_LRU_Eviction(t *testing.T) {
	config := rest.QueryCacheConfig{
		Enabled:       true,
		MaxSize:       3,
		TTL:           1 * time.Minute,
		StatsInterval: 10 * time.Second,
	}
	cache := rest.NewLRUQueryCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Fill cache to capacity
	cache.Set(ctx, "key1", "value1", 0)
	cache.Set(ctx, "key2", "value2", 0)
	cache.Set(ctx, "key3", "value3", 0)

	if cache.Size() != 3 {
		t.Fatalf("Cache should have 3 items, got %d", cache.Size())
	}

	// Add another item, should evict LRU (key1)
	cache.Set(ctx, "key4", "value4", 0)

	if cache.Size() != 3 {
		t.Fatalf("Cache should still have 3 items after eviction, got %d", cache.Size())
	}

	// key1 should be evicted
	_, found, _ := cache.Get(ctx, "key1")
	if found {
		t.Fatal("key1 should have been evicted")
	}

	// key4 should be present
	_, found, _ = cache.Get(ctx, "key4")
	if !found {
		t.Fatal("key4 should be in cache")
	}
}

func TestLRUQueryCache_Delete(t *testing.T) {
	config := rest.QueryCacheConfig{
		Enabled:       true,
		MaxSize:       100,
		TTL:           1 * time.Minute,
		StatsInterval: 10 * time.Second,
	}
	cache := rest.NewLRUQueryCache(config)
	defer cache.Close()

	ctx := context.Background()

	cache.Set(ctx, "delete_key", "value", 0)

	// Verify it exists
	_, found, _ := cache.Get(ctx, "delete_key")
	if !found {
		t.Fatal("Value should exist before delete")
	}

	// Delete it
	cache.Delete(ctx, "delete_key")

	// Verify it's gone
	_, found, _ = cache.Get(ctx, "delete_key")
	if found {
		t.Fatal("Value should not exist after delete")
	}
}

func TestLRUQueryCache_Clear(t *testing.T) {
	config := rest.QueryCacheConfig{
		Enabled:       true,
		MaxSize:       100,
		TTL:           1 * time.Minute,
		StatsInterval: 10 * time.Second,
	}
	cache := rest.NewLRUQueryCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Add multiple items
	for i := 0; i < 5; i++ {
		cache.Set(ctx, "key"+string(rune(i)), "value", 0)
	}

	if cache.Size() != 5 {
		t.Fatalf("Cache should have 5 items, got %d", cache.Size())
	}

	// Clear cache
	cache.Clear(ctx)

	if cache.Size() != 0 {
		t.Fatalf("Cache should be empty after Clear, got %d items", cache.Size())
	}
}

func TestLRUQueryCache_Statistics(t *testing.T) {
	config := rest.QueryCacheConfig{
		Enabled:       true,
		MaxSize:       100,
		TTL:           1 * time.Minute,
		StatsInterval: 10 * time.Second,
	}
	cache := rest.NewLRUQueryCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Add value
	cache.Set(ctx, "stats_key", "value", 0)

	// Hit
	cache.Get(ctx, "stats_key")
	cache.Get(ctx, "stats_key")

	// Miss
	cache.Get(ctx, "missing_key")

	stats := cache.GetStats()

	if stats.Hits < 2 {
		t.Fatalf("Should have at least 2 hits, got %d", stats.Hits)
	}

	if stats.Misses < 1 {
		t.Fatalf("Should have at least 1 miss, got %d", stats.Misses)
	}

	if stats.HitRate <= 0 || stats.HitRate > 1 {
		t.Fatalf("Hit rate should be between 0 and 1, got %.2f", stats.HitRate)
	}
}

func TestLRUQueryCache_InvalidatePattern(t *testing.T) {
	config := rest.QueryCacheConfig{
		Enabled:       true,
		MaxSize:       100,
		TTL:           1 * time.Minute,
		StatsInterval: 10 * time.Second,
	}
	cache := rest.NewLRUQueryCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Add keys with pattern
	cache.Set(ctx, "dashboard:user:1", "value1", 0)
	cache.Set(ctx, "dashboard:user:2", "value2", 0)
	cache.Set(ctx, "query:user:1", "value3", 0)

	// Invalidate dashboard keys
	cache.InvalidatePattern(ctx, "dashboard:")

	// dashboard keys should be gone
	_, found, _ := cache.Get(ctx, "dashboard:user:1")
	if found {
		t.Fatal("dashboard:user:1 should be invalidated")
	}

	// query key should still exist
	_, found, _ = cache.Get(ctx, "query:user:1")
	if !found {
		t.Fatal("query:user:1 should still exist")
	}
}

func TestLRUQueryCache_DisabledCache(t *testing.T) {
	config := rest.QueryCacheConfig{
		Enabled:       false, // Disabled
		MaxSize:       100,
		TTL:           1 * time.Minute,
		StatsInterval: 10 * time.Second,
	}
	cache := rest.NewLRUQueryCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Set should still succeed but not cache
	err := cache.Set(ctx, "test", "value", 0)
	if err != nil {
		t.Fatalf("Set should succeed when disabled: %v", err)
	}

	// Get should not find anything
	_, found, _ := cache.Get(ctx, "test")
	if found {
		t.Fatal("Cache should not return anything when disabled")
	}
}

func TestLRUQueryCache_MRU_Movement(t *testing.T) {
	config := rest.QueryCacheConfig{
		Enabled:       true,
		MaxSize:       3,
		TTL:           1 * time.Minute,
		StatsInterval: 10 * time.Second,
	}
	cache := rest.NewLRUQueryCache(config)
	defer cache.Close()

	ctx := context.Background()

	// Fill cache
	cache.Set(ctx, "key1", "value1", 0)
	cache.Set(ctx, "key2", "value2", 0)
	cache.Set(ctx, "key3", "value3", 0)

	// Access key1 to move it to MRU
	cache.Get(ctx, "key1")

	// Add key4, should evict key2 (not key1 since we just accessed it)
	cache.Set(ctx, "key4", "value4", 0)

	// key1 should still exist (was recently accessed)
	_, found, _ := cache.Get(ctx, "key1")
	if !found {
		t.Fatal("key1 should still be in cache (recently accessed)")
	}

	// key2 should be evicted (least recently used)
	_, found, _ = cache.Get(ctx, "key2")
	if found {
		t.Fatal("key2 should have been evicted")
	}
}
