package cmd

import (
	"context"
	"testing"
)

// TestBuildBeadTree_EpicHierarchy tests handling of epic (parent-child) relationships
func TestBuildBeadTree_EpicHierarchy(t *testing.T) {
	items := []beadItem{
		{id: "epic-1", title: "Epic 1", status: "open", priority: 1, dependencyCount: 0, dependentCount: 2},
		{id: "task-1", title: "Task 1", status: "open", priority: 2, dependencyCount: 1, dependentCount: 0},
		{id: "task-2", title: "Task 2", status: "open", priority: 2, dependencyCount: 1, dependentCount: 0},
	}

	// Manually set dependencies since we don't have a real database
	// In real scenario, this would come from GetIssuesWithDeps
	items[1].dependencies = []string{"epic-1"}
	items[2].dependencies = []string{"epic-1"}

	result := buildBeadTree(context.Background(), items, nil)

	// Verify epic is root and tasks are children
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}

	if result[0].id != "epic-1" || result[0].treeDepth != 0 {
		t.Errorf("expected epic-1 at root level, got %s at depth %d", result[0].id, result[0].treeDepth)
	}

	// Both tasks should be at depth 1
	if result[1].treeDepth != 1 || result[2].treeDepth != 1 {
		t.Errorf("expected tasks at depth 1, got depths %d and %d", result[1].treeDepth, result[2].treeDepth)
	}
}

// TestBuildBeadTree_BlocksDependencies tests handling of "blocks" type dependencies
func TestBuildBeadTree_BlocksDependencies(t *testing.T) {
	items := []beadItem{
		{id: "blocker", title: "Blocker", status: "open", priority: 1},
		{id: "blocked", title: "Blocked", status: "open", priority: 2, dependencyCount: 1},
	}

	items[1].dependencies = []string{"blocker"}

	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}

	// Blocker should be root, blocked should be child
	if result[0].id != "blocker" {
		t.Errorf("expected blocker first, got %s", result[0].id)
	}
	if result[1].id != "blocked" || result[1].treeDepth != 1 {
		t.Errorf("expected blocked at depth 1, got %s at depth %d", result[1].id, result[1].treeDepth)
	}
}

// TestBuildBeadTree_ClosedParentVisibility tests filtering of closed parents
func TestBuildBeadTree_ClosedParentVisibility(t *testing.T) {
	items := []beadItem{
		{id: "parent", title: "Parent", status: "closed", priority: 1, isClosedParent: true},
		{id: "child", title: "Child", status: "open", priority: 2, dependencyCount: 1},
	}

	items[1].dependencies = []string{"parent"}

	result := buildBeadTree(context.Background(), items, nil)

	// Both parent and child should be visible since parent has visible child
	if len(result) != 2 {
		t.Errorf("expected both parent and child visible, got %d items", len(result))
	}
}

// TestBuildBeadTree_ClosedParentNoVisibleChildren tests filtering out closed parents without visible children
func TestBuildBeadTree_ClosedParentNoVisibleChildren(t *testing.T) {
	items := []beadItem{
		{id: "parent", title: "Parent", status: "closed", priority: 1, isClosedParent: true},
	}

	result := buildBeadTree(context.Background(), items, nil)

	// Parent should be filtered out since it has no visible children
	if len(result) != 0 {
		t.Errorf("expected closed parent without children to be filtered out, got %d items", len(result))
	}
}

// TestBuildBeadTree_MultiLevelNesting tests deep hierarchy
func TestBuildBeadTree_MultiLevelNesting(t *testing.T) {
	items := []beadItem{
		{id: "level-0", title: "Level 0", status: "open", priority: 1},
		{id: "level-1", title: "Level 1", status: "open", priority: 2, dependencyCount: 1},
		{id: "level-2", title: "Level 2", status: "open", priority: 3, dependencyCount: 1},
		{id: "level-3", title: "Level 3", status: "open", priority: 4, dependencyCount: 1},
	}

	items[1].dependencies = []string{"level-0"}
	items[2].dependencies = []string{"level-1"}
	items[3].dependencies = []string{"level-2"}

	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result))
	}

	// Verify each level has correct depth
	expectedDepths := []int{0, 1, 2, 3}
	for i, item := range result {
		if item.treeDepth != expectedDepths[i] {
			t.Errorf("item %s expected depth %d, got %d", item.id, expectedDepths[i], item.treeDepth)
		}
	}
}

// TestBuildBeadTree_MultipleRoots tests handling of multiple independent trees
func TestBuildBeadTree_MultipleRoots(t *testing.T) {
	items := []beadItem{
		{id: "root-1", title: "Root 1", status: "open", priority: 1},
		{id: "root-2", title: "Root 2", status: "open", priority: 2},
		{id: "child-1", title: "Child 1", status: "open", priority: 3, dependencyCount: 1},
		{id: "child-2", title: "Child 2", status: "open", priority: 4, dependencyCount: 1},
	}

	items[2].dependencies = []string{"root-1"}
	items[3].dependencies = []string{"root-2"}

	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result))
	}

	// Count roots (depth 0)
	rootCount := 0
	for _, item := range result {
		if item.treeDepth == 0 {
			rootCount++
		}
	}

	if rootCount != 2 {
		t.Errorf("expected 2 roots, got %d", rootCount)
	}
}

// TestBuildBeadTree_MixedTypes tests handling of different dependency types together
func TestBuildBeadTree_MixedTypes(t *testing.T) {
	items := []beadItem{
		{id: "epic", title: "Epic", status: "open", priority: 1, beadType: "epic"},
		{id: "task", title: "Task", status: "open", priority: 2, beadType: "task", dependencyCount: 1},
		{id: "bug", title: "Bug", status: "open", priority: 3, beadType: "bug"},
		{id: "feature", title: "Feature", status: "open", priority: 4, beadType: "feature", dependencyCount: 1},
	}

	items[1].dependencies = []string{"epic"}
	items[3].dependencies = []string{"bug"}

	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result))
	}

	// Verify mixed types are handled correctly
	rootTypes := make(map[string]bool)
	for _, item := range result {
		if item.treeDepth == 0 {
			rootTypes[item.beadType] = true
		}
	}

	if len(rootTypes) < 2 {
		t.Errorf("expected multiple types at root level")
	}
}

// TestBuildBeadTree_CircularDependencies tests handling of circular dependency detection
func TestBuildBeadTree_CircularDependencies(t *testing.T) {
	items := []beadItem{
		{id: "item-1", title: "Item 1", status: "open", priority: 1, dependencyCount: 1},
		{id: "item-2", title: "Item 2", status: "open", priority: 2, dependencyCount: 1},
		{id: "item-3", title: "Item 3", status: "open", priority: 3, dependencyCount: 1},
	}

	// Create circular dependency: 1 -> 2 -> 3 -> 1
	items[0].dependencies = []string{"item-3"}
	items[1].dependencies = []string{"item-1"}
	items[2].dependencies = []string{"item-2"}

	// The function should handle this gracefully without infinite loop
	result := buildBeadTree(context.Background(), items, nil)

	// Should still produce all 3 items
	if len(result) != 3 {
		t.Fatalf("expected 3 items despite circular dependency, got %d", len(result))
	}
}

// TestBuildBeadTree_EmptyInput tests handling of empty input
func TestBuildBeadTree_EmptyInput(t *testing.T) {
	items := []beadItem{}
	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d items", len(result))
	}
}

// TestBuildBeadTree_WithNilClient tests that the function works with nil client
func TestBuildBeadTree_WithNilClient(t *testing.T) {
	// With nil client, function uses dependencies already set on items
	items := []beadItem{
		{id: "item-1", title: "Item 1", status: "open", priority: 1},
	}

	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
}

// TestBuildBeadTree_ParentChildRelationship tests that parent-child relationships are preserved
func TestBuildBeadTree_ParentChildRelationship(t *testing.T) {
	items := []beadItem{
		{id: "parent", title: "Parent", status: "closed", priority: 1, isClosedParent: true},
		{id: "child", title: "Child", status: "open", priority: 2, dependencyCount: 1},
	}

	items[1].dependencies = []string{"parent"}

	// Parents should be fetched and visible when they have visible children
	result := buildBeadTree(context.Background(), items, nil)

	// Both parent and child should be visible
	if len(result) != 2 {
		t.Errorf("expected parent to be visible with open child, got %d items", len(result))
	}
}
