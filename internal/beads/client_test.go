package beads

import (
	"context"
	"testing"
	"time"

	"github.com/newhook/co/internal/beads/cachemanager"
	"github.com/stretchr/testify/require"
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
		require.True(t, found, "expected cache hit")
		require.NotNil(t, result)
		require.Equal(t, "Test Bead", result.Beads["bead-1"].Title)

		// Cache miss
		result, found = mock.Get(ctx, "bead-2")
		require.False(t, found, "expected cache miss")
		require.Nil(t, result, "expected nil result on miss")
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

		require.Equal(t, "test-key", storedKey)
		require.Equal(t, testResult, storedValue)
		require.Equal(t, 5*time.Minute, storedTTL)
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
		require.NoError(t, err)
		require.True(t, flushCalled, "expected Flush to be called")
	})

	t.Run("nil function returns zero value", func(t *testing.T) {
		mock := &cachemanager.CacheManagerMock[string, *BeadsWithDepsResult]{}

		result, found := mock.Get(ctx, "any")
		require.False(t, found, "expected false when GetFunc is nil")
		require.Nil(t, result, "expected nil result when GetFunc is nil")

		// Set with nil func should not panic
		mock.Set(ctx, "key", nil, time.Minute)

		// Flush with nil func should return nil
		err := mock.Flush(ctx)
		require.NoError(t, err)
	})
}

// TestFlushCacheWithNilCache tests that FlushCache handles a nil cache gracefully.
func TestFlushCacheWithNilCache(t *testing.T) {
	ctx := context.Background()
	client := &Client{
		cache:        nil,
		cacheEnabled: false,
	}

	// FlushCache with nil cache should not panic and return nil
	err := client.FlushCache(ctx)
	require.NoError(t, err)
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
		require.NotNil(t, beadWithDeps)
		require.Equal(t, "bead-1", beadWithDeps.ID)
		require.Equal(t, "Test Bead", beadWithDeps.Title)
		require.Len(t, beadWithDeps.Dependencies, 1)
		require.Len(t, beadWithDeps.Dependents, 1)
	})

	t.Run("GetBead returns nil for non-existing bead", func(t *testing.T) {
		result := &BeadsWithDepsResult{
			Beads:        map[string]Bead{},
			Dependencies: make(map[string][]Dependency),
			Dependents:   make(map[string][]Dependent),
		}

		beadWithDeps := result.GetBead("nonexistent")
		require.Nil(t, beadWithDeps, "expected nil for non-existing bead")
	})
}

// TestDefaultClientConfig tests the default configuration.
func TestDefaultClientConfig(t *testing.T) {
	cfg := DefaultClientConfig("/path/to/db")

	require.Equal(t, "/path/to/db", cfg.DBPath)
	require.True(t, cfg.CacheEnabled, "expected CacheEnabled to be true by default")
	require.Equal(t, 10*time.Minute, cfg.CacheExpiration)
	require.Equal(t, 30*time.Minute, cfg.CacheCleanupTime)
}
