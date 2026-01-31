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

// TestBeadsWithDepsResult_GetBeadWithDependencies tests getting a bead with dependencies and dependents.
func TestBeadsWithDepsResult_GetBeadWithDependencies(t *testing.T) {
	result := &BeadsWithDepsResult{
		Beads: map[string]Bead{
			"parent-1": {ID: "parent-1", Title: "Parent Issue", Status: StatusOpen},
			"child-1":  {ID: "child-1", Title: "Child 1", Status: StatusClosed},
			"child-2":  {ID: "child-2", Title: "Child 2", Status: StatusClosed},
		},
		Dependencies: map[string][]Dependency{
			"child-1": {{IssueID: "child-1", DependsOnID: "parent-1", Type: "parent-child", Status: StatusOpen, Title: "Parent Issue"}},
			"child-2": {{IssueID: "child-2", DependsOnID: "parent-1", Type: "parent-child", Status: StatusOpen, Title: "Parent Issue"}},
		},
		Dependents: map[string][]Dependent{
			"parent-1": {
				{IssueID: "child-1", DependsOnID: "parent-1", Type: "parent-child", Status: StatusClosed, Title: "Child 1"},
				{IssueID: "child-2", DependsOnID: "parent-1", Type: "parent-child", Status: StatusClosed, Title: "Child 2"},
			},
		},
	}

	// Get parent bead
	parent := result.GetBead("parent-1")
	require.NotNil(t, parent)
	require.Equal(t, "parent-1", parent.ID)
	require.Equal(t, StatusOpen, parent.Status)
	require.Len(t, parent.Dependents, 2)

	// Get child bead
	child := result.GetBead("child-1")
	require.NotNil(t, child)
	require.Equal(t, StatusClosed, child.Status)
	require.Len(t, child.Dependencies, 1)
	require.Equal(t, "parent-child", child.Dependencies[0].Type)
}

// TestBeadsWithDepsResult_IsParentEligibleForClose tests the logic for determining if a parent is eligible for closing.
func TestBeadsWithDepsResult_IsParentEligibleForClose(t *testing.T) {
	t.Run("parent with all children closed", func(t *testing.T) {
		result := &BeadsWithDepsResult{
			Beads: map[string]Bead{
				"parent": {ID: "parent", Status: StatusOpen},
				"child1": {ID: "child1", Status: StatusClosed},
				"child2": {ID: "child2", Status: StatusClosed},
			},
			Dependents: map[string][]Dependent{
				"parent": {
					{IssueID: "child1", DependsOnID: "parent", Type: "parent-child", Status: StatusClosed},
					{IssueID: "child2", DependsOnID: "parent", Type: "parent-child", Status: StatusClosed},
				},
			},
		}

		// Simulate the CloseEligibleParents logic
		parent := result.Beads["parent"]
		dependents := result.Dependents["parent"]

		var children []string
		for _, dep := range dependents {
			if dep.Type == "parent-child" {
				children = append(children, dep.IssueID)
			}
		}

		allClosed := true
		for _, childID := range children {
			child := result.Beads[childID]
			if child.Status != StatusClosed {
				allClosed = false
				break
			}
		}

		require.True(t, allClosed, "parent should be eligible for closing")
		require.Equal(t, StatusOpen, parent.Status)
	})

	t.Run("parent with some children open", func(t *testing.T) {
		result := &BeadsWithDepsResult{
			Beads: map[string]Bead{
				"parent": {ID: "parent", Status: StatusOpen},
				"child1": {ID: "child1", Status: StatusClosed},
				"child2": {ID: "child2", Status: StatusOpen}, // Still open
			},
			Dependents: map[string][]Dependent{
				"parent": {
					{IssueID: "child1", DependsOnID: "parent", Type: "parent-child", Status: StatusClosed},
					{IssueID: "child2", DependsOnID: "parent", Type: "parent-child", Status: StatusOpen},
				},
			},
		}

		var children []string
		for _, dep := range result.Dependents["parent"] {
			if dep.Type == "parent-child" {
				children = append(children, dep.IssueID)
			}
		}

		allClosed := true
		for _, childID := range children {
			child := result.Beads[childID]
			if child.Status != StatusClosed {
				allClosed = false
				break
			}
		}

		require.False(t, allClosed, "parent should NOT be eligible for closing")
	})

	t.Run("bead with no children is not a parent", func(t *testing.T) {
		result := &BeadsWithDepsResult{
			Beads: map[string]Bead{
				"standalone": {ID: "standalone", Status: StatusOpen},
			},
			Dependents: map[string][]Dependent{},
		}

		dependents := result.Dependents["standalone"]
		var children []string
		for _, dep := range dependents {
			if dep.Type == "parent-child" {
				children = append(children, dep.IssueID)
			}
		}

		require.Len(t, children, 0, "standalone bead should have no children")
	})

	t.Run("nested hierarchy with grandparent", func(t *testing.T) {
		result := &BeadsWithDepsResult{
			Beads: map[string]Bead{
				"grandparent": {ID: "grandparent", Status: StatusOpen},
				"parent":      {ID: "parent", Status: StatusClosed},
				"child":       {ID: "child", Status: StatusClosed},
			},
			Dependents: map[string][]Dependent{
				"grandparent": {
					{IssueID: "parent", DependsOnID: "grandparent", Type: "parent-child", Status: StatusClosed},
				},
				"parent": {
					{IssueID: "child", DependsOnID: "parent", Type: "parent-child", Status: StatusClosed},
				},
			},
		}

		// Check grandparent - should be eligible since its direct child (parent) is closed
		dependents := result.Dependents["grandparent"]
		var children []string
		for _, dep := range dependents {
			if dep.Type == "parent-child" {
				children = append(children, dep.IssueID)
			}
		}

		allClosed := true
		for _, childID := range children {
			child := result.Beads[childID]
			if child.Status != StatusClosed {
				allClosed = false
				break
			}
		}

		require.True(t, allClosed, "grandparent should be eligible for closing")
	})
}

// TestDependencyTypes tests that dependency types are correctly identified.
func TestDependencyTypes(t *testing.T) {
	testCases := []struct {
		name     string
		depType  string
		expected string
	}{
		{"blocks relationship", "blocks", "blocks"},
		{"blocked_by relationship", "blocked_by", "blocked_by"},
		{"parent-child relationship", "parent-child", "parent-child"},
		{"relates-to relationship", "relates-to", "relates-to"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dep := Dependency{
				IssueID:     "issue-1",
				DependsOnID: "issue-2",
				Type:        tc.depType,
			}
			require.Equal(t, tc.expected, dep.Type)
		})
	}
}

// TestBeadWithDepsStruct tests the BeadWithDeps struct.
func TestBeadWithDepsStruct(t *testing.T) {
	bead := &Bead{
		ID:     "test-bead",
		Title:  "Test Bead",
		Status: StatusOpen,
	}

	beadWithDeps := &BeadWithDeps{
		Bead: bead,
		Dependencies: []Dependency{
			{IssueID: "test-bead", DependsOnID: "dep-1", Type: "blocks"},
		},
		Dependents: []Dependent{
			{IssueID: "child-1", DependsOnID: "test-bead", Type: "parent-child"},
		},
	}

	require.Equal(t, "test-bead", beadWithDeps.ID)
	require.Equal(t, "Test Bead", beadWithDeps.Title)
	require.Len(t, beadWithDeps.Dependencies, 1)
	require.Len(t, beadWithDeps.Dependents, 1)
}
