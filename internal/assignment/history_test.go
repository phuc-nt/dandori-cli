//go:build server

package assignment

import (
	"testing"
	"time"
)

func TestDBHistoryProviderCache(t *testing.T) {
	// Test with nil pool (no DB)
	provider := NewDBHistoryProvider(nil, time.Minute)

	stats := provider.GetStats("alpha", "Bug")

	// Should return empty stats when no DB
	if stats.TotalRuns != 0 {
		t.Errorf("TotalRuns = %d, want 0", stats.TotalRuns)
	}
}

func TestDBHistoryProviderCacheTTL(t *testing.T) {
	provider := NewDBHistoryProvider(nil, 100*time.Millisecond)

	// First call
	_ = provider.GetStats("alpha", "Bug")

	// Should be cached
	provider.mu.RLock()
	_, cached := provider.cache["alpha:Bug"]
	provider.mu.RUnlock()

	if !cached {
		t.Error("stats should be cached after first call")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Cache should be stale (but entry still exists)
	provider.mu.RLock()
	entry, exists := provider.cache["alpha:Bug"]
	provider.mu.RUnlock()

	if !exists {
		t.Error("cache entry should still exist")
	}

	// Check that it's considered stale
	if time.Since(entry.fetchedAt) < provider.ttl {
		t.Error("entry should be stale after TTL")
	}
}

func TestDBHistoryProviderInvalidateCache(t *testing.T) {
	provider := NewDBHistoryProvider(nil, time.Hour)

	// Populate cache
	_ = provider.GetStats("alpha", "Bug")

	// Invalidate
	provider.InvalidateCache("alpha", "Bug")

	// Should be removed
	provider.mu.RLock()
	_, exists := provider.cache["alpha:Bug"]
	provider.mu.RUnlock()

	if exists {
		t.Error("cache entry should be invalidated")
	}
}

func TestDBHistoryProviderClearCache(t *testing.T) {
	provider := NewDBHistoryProvider(nil, time.Hour)

	// Populate multiple entries
	_ = provider.GetStats("alpha", "Bug")
	_ = provider.GetStats("beta", "Story")

	// Clear all
	provider.ClearCache()

	// Should be empty
	provider.mu.RLock()
	count := len(provider.cache)
	provider.mu.RUnlock()

	if count != 0 {
		t.Errorf("cache should be empty after clear, got %d entries", count)
	}
}
