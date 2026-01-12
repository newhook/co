package cmd

import (
	"testing"

	"github.com/newhook/co/internal/beads"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSortGroupsByDependencies_ReordersGroups(t *testing.T) {
	// Given: child depends on parent, but child's group is listed first
	groups := []taskGroup{
		{index: 0, beadIDs: []string{"child"}, beads: []beads.Bead{{ID: "child", Title: "Child"}}},
		{index: 1, beadIDs: []string{"parent"}, beads: []beads.Bead{{ID: "parent", Title: "Parent"}}},
	}

	beadsWithDeps := []beads.BeadWithDeps{
		{ID: "child", Title: "Child", Dependencies: []beads.Dependency{{ID: "parent", DependencyType: "depends_on"}}},
		{ID: "parent", Title: "Parent"},
	}

	beadToGroup := map[string]int{
		"child":  0,
		"parent": 1,
	}

	// When: sorting by dependencies
	sorted, err := sortGroupsByDependencies(groups, beadsWithDeps, beadToGroup)

	// Then: parent's group should come first
	require.NoError(t, err)
	require.Len(t, sorted, 2)
	assert.Equal(t, []string{"parent"}, sorted[0].beadIDs, "parent group should be first")
	assert.Equal(t, []string{"child"}, sorted[1].beadIDs, "child group should be second")
}

func TestSortGroupsByDependencies_PreservesGroupings(t *testing.T) {
	// Given: two beads in one group, another bead in second group
	// The second group depends on one bead from first group
	groups := []taskGroup{
		{index: 0, beadIDs: []string{"b1", "b2"}, beads: []beads.Bead{{ID: "b1"}, {ID: "b2"}}},
		{index: 1, beadIDs: []string{"b3"}, beads: []beads.Bead{{ID: "b3"}}},
	}

	beadsWithDeps := []beads.BeadWithDeps{
		{ID: "b1"},
		{ID: "b2"},
		{ID: "b3", Dependencies: []beads.Dependency{{ID: "b1", DependencyType: "depends_on"}}},
	}

	beadToGroup := map[string]int{
		"b1": 0,
		"b2": 0,
		"b3": 1,
	}

	// When: sorting
	sorted, err := sortGroupsByDependencies(groups, beadsWithDeps, beadToGroup)

	// Then: groups stay intact, just reordered
	require.NoError(t, err)
	require.Len(t, sorted, 2)
	// First group should have both b1 and b2
	assert.Equal(t, []string{"b1", "b2"}, sorted[0].beadIDs, "first group should be preserved")
	assert.Equal(t, []string{"b3"}, sorted[1].beadIDs, "second group should be second")
}

func TestSortGroupsByDependencies_DetectsCycle(t *testing.T) {
	// Given: circular dependency between two groups
	groups := []taskGroup{
		{index: 0, beadIDs: []string{"a"}, beads: []beads.Bead{{ID: "a"}}},
		{index: 1, beadIDs: []string{"b"}, beads: []beads.Bead{{ID: "b"}}},
	}

	beadsWithDeps := []beads.BeadWithDeps{
		{ID: "a", Dependencies: []beads.Dependency{{ID: "b", DependencyType: "depends_on"}}},
		{ID: "b", Dependencies: []beads.Dependency{{ID: "a", DependencyType: "depends_on"}}},
	}

	beadToGroup := map[string]int{
		"a": 0,
		"b": 1,
	}

	// When: sorting
	_, err := sortGroupsByDependencies(groups, beadsWithDeps, beadToGroup)

	// Then: cycle should be detected
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestSortGroupsByDependencies_IndependentGroups(t *testing.T) {
	// Given: two groups with no dependencies between them
	groups := []taskGroup{
		{index: 0, beadIDs: []string{"a"}, beads: []beads.Bead{{ID: "a"}}},
		{index: 1, beadIDs: []string{"b"}, beads: []beads.Bead{{ID: "b"}}},
	}

	beadsWithDeps := []beads.BeadWithDeps{
		{ID: "a"},
		{ID: "b"},
	}

	beadToGroup := map[string]int{
		"a": 0,
		"b": 1,
	}

	// When: sorting
	sorted, err := sortGroupsByDependencies(groups, beadsWithDeps, beadToGroup)

	// Then: order is preserved (or at least both present)
	require.NoError(t, err)
	require.Len(t, sorted, 2)
	// Both groups should be present
	ids := []string{sorted[0].beadIDs[0], sorted[1].beadIDs[0]}
	assert.Contains(t, ids, "a")
	assert.Contains(t, ids, "b")
}

func TestSortGroupsByDependencies_DeepChain(t *testing.T) {
	// Given: a -> b -> c -> d (deep dependency chain)
	groups := []taskGroup{
		{index: 0, beadIDs: []string{"d"}, beads: []beads.Bead{{ID: "d"}}},
		{index: 1, beadIDs: []string{"b"}, beads: []beads.Bead{{ID: "b"}}},
		{index: 2, beadIDs: []string{"a"}, beads: []beads.Bead{{ID: "a"}}},
		{index: 3, beadIDs: []string{"c"}, beads: []beads.Bead{{ID: "c"}}},
	}

	beadsWithDeps := []beads.BeadWithDeps{
		{ID: "a"},
		{ID: "b", Dependencies: []beads.Dependency{{ID: "a", DependencyType: "depends_on"}}},
		{ID: "c", Dependencies: []beads.Dependency{{ID: "b", DependencyType: "depends_on"}}},
		{ID: "d", Dependencies: []beads.Dependency{{ID: "c", DependencyType: "depends_on"}}},
	}

	beadToGroup := map[string]int{
		"a": 2,
		"b": 1,
		"c": 3,
		"d": 0,
	}

	// When: sorting
	sorted, err := sortGroupsByDependencies(groups, beadsWithDeps, beadToGroup)

	// Then: order should be a, b, c, d
	require.NoError(t, err)
	require.Len(t, sorted, 4)

	positions := make(map[string]int)
	for i, g := range sorted {
		positions[g.beadIDs[0]] = i
	}

	assert.Less(t, positions["a"], positions["b"], "a should come before b")
	assert.Less(t, positions["b"], positions["c"], "b should come before c")
	assert.Less(t, positions["c"], positions["d"], "c should come before d")
}

func TestSortGroupsByDependencies_SingleGroup(t *testing.T) {
	// Given: single group
	groups := []taskGroup{
		{index: 0, beadIDs: []string{"a"}, beads: []beads.Bead{{ID: "a"}}},
	}

	beadsWithDeps := []beads.BeadWithDeps{
		{ID: "a"},
	}

	beadToGroup := map[string]int{
		"a": 0,
	}

	// When: sorting
	sorted, err := sortGroupsByDependencies(groups, beadsWithDeps, beadToGroup)

	// Then: single group returned unchanged
	require.NoError(t, err)
	require.Len(t, sorted, 1)
	assert.Equal(t, []string{"a"}, sorted[0].beadIDs)
}

func TestSortGroupsByDependencies_ExternalDependency(t *testing.T) {
	// Given: bead depends on something not in any group (external)
	groups := []taskGroup{
		{index: 0, beadIDs: []string{"a"}, beads: []beads.Bead{{ID: "a"}}},
		{index: 1, beadIDs: []string{"b"}, beads: []beads.Bead{{ID: "b"}}},
	}

	beadsWithDeps := []beads.BeadWithDeps{
		{ID: "a", Dependencies: []beads.Dependency{{ID: "external", DependencyType: "depends_on"}}},
		{ID: "b"},
	}

	beadToGroup := map[string]int{
		"a": 0,
		"b": 1,
	}

	// When: sorting
	sorted, err := sortGroupsByDependencies(groups, beadsWithDeps, beadToGroup)

	// Then: external dependency is ignored, no error
	require.NoError(t, err)
	require.Len(t, sorted, 2)
}

func TestSortGroupsByDependencies_MultipleBeadsInGroupWithMixedDeps(t *testing.T) {
	// Given: group with multiple beads, one depends on another group
	// group 0: [a1, a2] where a2 depends on b
	// group 1: [b]
	groups := []taskGroup{
		{index: 0, beadIDs: []string{"a1", "a2"}, beads: []beads.Bead{{ID: "a1"}, {ID: "a2"}}},
		{index: 1, beadIDs: []string{"b"}, beads: []beads.Bead{{ID: "b"}}},
	}

	beadsWithDeps := []beads.BeadWithDeps{
		{ID: "a1"},
		{ID: "a2", Dependencies: []beads.Dependency{{ID: "b", DependencyType: "depends_on"}}},
		{ID: "b"},
	}

	beadToGroup := map[string]int{
		"a1": 0,
		"a2": 0,
		"b":  1,
	}

	// When: sorting
	sorted, err := sortGroupsByDependencies(groups, beadsWithDeps, beadToGroup)

	// Then: group with b comes first
	require.NoError(t, err)
	require.Len(t, sorted, 2)
	assert.Equal(t, []string{"b"}, sorted[0].beadIDs, "b group should be first")
	assert.Equal(t, []string{"a1", "a2"}, sorted[1].beadIDs, "a group should be second, preserving both beads")
}
