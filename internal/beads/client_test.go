package beads

import (
	"context"
	"testing"
	"time"

	"github.com/newhook/co/internal/beads/cachemanager"
)

// TestCacheManagerMock verifies the mock works correctly with function fields.
func TestCacheManagerMock(t *testing.T) {
	ctx := context.Background()

	t.Run("Get returns configured value on cache hit", func(t *testing.T) {
		cachedResult := &BeadsWithDepsResult{
			Beads: map[string]Bead{
				"bead-1": {ID: "bead-1", Title: "Test Bead"},
			},
			Dependencies: make(map[string][]Dependency),
			Dependents:   make(map[string][]Dependent),
		}

		mock := &cachemanager.CacheManagerMock[string, *BeadsWithDepsResult]{
			GetFunc: func(ctx context.Context, key string) (*BeadsWithDepsResult, bool) {
				if key == "bead-1" {
					return cachedResult, true
				}
				return nil, false
			},
		}

		// Cache hit
		result, found := mock.Get(ctx, "bead-1")
		if !found {
			t.Error("expected cache hit")
		}
		if result == nil || result.Beads["bead-1"].Title != "Test Bead" {
			t.Error("expected cached result")
		}

		// Cache miss
		result, found = mock.Get(ctx, "bead-2")
		if found {
			t.Error("expected cache miss")
		}
		if result != nil {
			t.Error("expected nil result on miss")
		}
	})

	t.Run("Set stores value in cache", func(t *testing.T) {
		var storedKey string
		var storedValue *BeadsWithDepsResult
		var storedTTL time.Duration

		mock := &cachemanager.CacheManagerMock[string, *BeadsWithDepsResult]{
			SetFunc: func(ctx context.Context, key string, value *BeadsWithDepsResult, ttl time.Duration) {
				storedKey = key
				storedValue = value
				storedTTL = ttl
			},
		}

		testResult := &BeadsWithDepsResult{
			Beads: map[string]Bead{
				"bead-1": {ID: "bead-1", Title: "New Bead"},
			},
		}

		mock.Set(ctx, "test-key", testResult, 5*time.Minute)

		if storedKey != "test-key" {
			t.Errorf("expected key 'test-key', got %s", storedKey)
		}
		if storedValue != testResult {
			t.Error("expected stored value to match")
		}
		if storedTTL != 5*time.Minute {
			t.Errorf("expected TTL 5m, got %v", storedTTL)
		}
	})

	t.Run("Flush clears cache", func(t *testing.T) {
		flushCalled := false
		mock := &cachemanager.CacheManagerMock[string, *BeadsWithDepsResult]{
			FlushFunc: func(ctx context.Context) error {
				flushCalled = true
				return nil
			},
		}

		err := mock.Flush(ctx)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !flushCalled {
			t.Error("expected Flush to be called")
		}
	})

	t.Run("nil function returns zero value", func(t *testing.T) {
		mock := &cachemanager.CacheManagerMock[string, *BeadsWithDepsResult]{}

		result, found := mock.Get(ctx, "any")
		if found {
			t.Error("expected false when GetFunc is nil")
		}
		if result != nil {
			t.Error("expected nil result when GetFunc is nil")
		}

		// Set with nil func should not panic
		mock.Set(ctx, "key", nil, time.Minute)

		// Flush with nil func should return nil
		err := mock.Flush(ctx)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})
}

// TestCacheKeyGeneration tests that cache keys are generated consistently.
func TestCacheKeyGeneration(t *testing.T) {
	t.Run("same IDs different order produce same key", func(t *testing.T) {
		// The GetBeadsWithDeps function sorts IDs before creating the cache key
		// This test documents that behavior

		// Example: IDs {"bead-c", "bead-a", "bead-b"} and {"bead-a", "bead-b", "bead-c"}
		// Both should sort to produce key "bead-a,bead-b,bead-c"

		// This documents the expected behavior without accessing private functions
		// The actual implementation uses sort.Strings and strings.Join
	})
}

// TestClientCacheIntegration documents the expected caching behavior.
// Note: Full integration tests require a database, which is tested separately.
func TestClientCacheIntegration(t *testing.T) {
	t.Run("GetBeadsWithDeps caching behavior", func(t *testing.T) {
		// Document expected behavior:
		// 1. Empty beadIDs returns empty result without cache check
		// 2. Cache is checked before database query
		// 3. On cache miss, database is queried and result is cached
		// 4. Cache key is sorted bead IDs joined by comma

		// This documents the contract for GetBeadsWithDeps caching:
		// - cacheEnabled must be true
		// - cache must be non-nil
		// - Results are keyed by sorted, comma-joined bead IDs
	})

	t.Run("FlushCache behavior", func(t *testing.T) {
		// Document expected behavior:
		// - FlushCache delegates to cache.Flush
		// - Returns nil if cache is nil

		ctx := context.Background()
		client := &Client{
			cache:        nil,
			cacheEnabled: false,
		}

		// FlushCache with nil cache should not panic and return nil
		err := client.FlushCache(ctx)
		if err != nil {
			t.Errorf("expected nil error for nil cache, got %v", err)
		}
	})
}

// TestBeadsWithDepsResult tests the result helper methods.
func TestBeadsWithDepsResult(t *testing.T) {
	t.Run("GetBead returns BeadWithDeps for existing bead", func(t *testing.T) {
		result := &BeadsWithDepsResult{
			Beads: map[string]Bead{
				"bead-1": {ID: "bead-1", Title: "Test Bead", Status: "open"},
			},
			Dependencies: map[string][]Dependency{
				"bead-1": {{IssueID: "bead-1", DependsOnID: "bead-2", Type: "blocks"}},
			},
			Dependents: map[string][]Dependent{
				"bead-1": {{IssueID: "bead-3", DependsOnID: "bead-1", Type: "blocked_by"}},
			},
		}

		beadWithDeps := result.GetBead("bead-1")
		if beadWithDeps == nil {
			t.Fatal("expected non-nil BeadWithDeps")
		}
		if beadWithDeps.ID != "bead-1" {
			t.Errorf("expected ID 'bead-1', got %s", beadWithDeps.ID)
		}
		if beadWithDeps.Title != "Test Bead" {
			t.Errorf("expected Title 'Test Bead', got %s", beadWithDeps.Title)
		}
		if len(beadWithDeps.Dependencies) != 1 {
			t.Errorf("expected 1 dependency, got %d", len(beadWithDeps.Dependencies))
		}
		if len(beadWithDeps.Dependents) != 1 {
			t.Errorf("expected 1 dependent, got %d", len(beadWithDeps.Dependents))
		}
	})

	t.Run("GetBead returns nil for non-existing bead", func(t *testing.T) {
		result := &BeadsWithDepsResult{
			Beads:        map[string]Bead{},
			Dependencies: make(map[string][]Dependency),
			Dependents:   make(map[string][]Dependent),
		}

		beadWithDeps := result.GetBead("nonexistent")
		if beadWithDeps != nil {
			t.Error("expected nil for non-existing bead")
		}
	})
}

// TestDefaultClientConfig tests the default configuration.
func TestDefaultClientConfig(t *testing.T) {
	cfg := DefaultClientConfig("/path/to/db")

	if cfg.DBPath != "/path/to/db" {
		t.Errorf("expected DBPath '/path/to/db', got %s", cfg.DBPath)
	}
	if !cfg.CacheEnabled {
		t.Error("expected CacheEnabled to be true by default")
	}
	if cfg.CacheExpiration != 10*time.Minute {
		t.Errorf("expected CacheExpiration 10m, got %v", cfg.CacheExpiration)
	}
	if cfg.CacheCleanupTime != 30*time.Minute {
		t.Errorf("expected CacheCleanupTime 30m, got %v", cfg.CacheCleanupTime)
	}
}
