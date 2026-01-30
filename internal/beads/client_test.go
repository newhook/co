package beads

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
