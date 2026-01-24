package tui

import (
	"context"
	"testing"
)

// TestBuildBeadTree_EpicHierarchy tests handling of epic (parent-child) relationships
func TestBuildBeadTree_EpicHierarchy(t *testing.T) {
	items := []beadItem{
		testBeadItem("epic-1", "Epic 1", "open", 1, "epic"),
		testBeadItem("task-1", "Task 1", "open", 2, "task", "epic-1"),
		testBeadItem("task-2", "Task 2", "open", 2, "task", "epic-1"),
	}

	result := buildBeadTree(context.Background(), items, nil)

	// Verify epic is root and tasks are children
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}

	if result[0].ID != "epic-1" || result[0].treeDepth != 0 {
		t.Errorf("expected epic-1 at root level, got %s at depth %d", result[0].ID, result[0].treeDepth)
	}

	// Both tasks should be at depth 1
	if result[1].treeDepth != 1 || result[2].treeDepth != 1 {
		t.Errorf("expected tasks at depth 1, got depths %d and %d", result[1].treeDepth, result[2].treeDepth)
	}
}

// TestBuildBeadTree_BlocksDependencies tests handling of "blocks" type dependencies
func TestBuildBeadTree_BlocksDependencies(t *testing.T) {
	items := []beadItem{
		testBeadItem("blocker", "Blocker", "open", 1, "task"),
		testBeadItem("blocked", "Blocked", "open", 2, "task", "blocker"),
	}

	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}

	// Blocker should be root, blocked should be child
	if result[0].ID != "blocker" {
		t.Errorf("expected blocker first, got %s", result[0].ID)
	}
	if result[1].ID != "blocked" || result[1].treeDepth != 1 {
		t.Errorf("expected blocked at depth 1, got %s at depth %d", result[1].ID, result[1].treeDepth)
	}
}

// TestBuildBeadTree_ClosedParentVisibility tests filtering of closed parents
func TestBuildBeadTree_ClosedParentVisibility(t *testing.T) {
	items := []beadItem{
		testBeadItemWithOptions("parent", "Parent", "closed", 1, "epic", true),
		testBeadItem("child", "Child", "open", 2, "task", "parent"),
	}

	result := buildBeadTree(context.Background(), items, nil)

	// Both parent and child should be visible since parent has visible child
	if len(result) != 2 {
		t.Errorf("expected both parent and child visible, got %d items", len(result))
	}
}

// TestBuildBeadTree_ClosedParentNoVisibleChildren tests filtering out closed parents without visible children
func TestBuildBeadTree_ClosedParentNoVisibleChildren(t *testing.T) {
	items := []beadItem{
		testBeadItemWithOptions("parent", "Parent", "closed", 1, "epic", true),
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
		testBeadItem("level-0", "Level 0", "open", 1, "task"),
		testBeadItem("level-1", "Level 1", "open", 2, "task", "level-0"),
		testBeadItem("level-2", "Level 2", "open", 3, "task", "level-1"),
		testBeadItem("level-3", "Level 3", "open", 4, "task", "level-2"),
	}

	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result))
	}

	// Verify each level has correct depth
	expectedDepths := []int{0, 1, 2, 3}
	for i, item := range result {
		if item.treeDepth != expectedDepths[i] {
			t.Errorf("item %s expected depth %d, got %d", item.ID, expectedDepths[i], item.treeDepth)
		}
	}
}

// TestBuildBeadTree_MultipleRoots tests handling of multiple independent trees
func TestBuildBeadTree_MultipleRoots(t *testing.T) {
	items := []beadItem{
		testBeadItem("root-1", "Root 1", "open", 1, "task"),
		testBeadItem("root-2", "Root 2", "open", 2, "task"),
		testBeadItem("child-1", "Child 1", "open", 3, "task", "root-1"),
		testBeadItem("child-2", "Child 2", "open", 4, "task", "root-2"),
	}

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
		testBeadItem("epic", "Epic", "open", 1, "epic"),
		testBeadItem("task", "Task", "open", 2, "task", "epic"),
		testBeadItem("bug", "Bug", "open", 3, "bug"),
		testBeadItem("feature", "Feature", "open", 4, "feature", "bug"),
	}

	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result))
	}

	// Verify mixed types are handled correctly
	rootTypes := make(map[string]bool)
	for _, item := range result {
		if item.treeDepth == 0 {
			rootTypes[item.Type] = true
		}
	}

	if len(rootTypes) < 2 {
		t.Errorf("expected multiple types at root level")
	}
}

// TestBuildBeadTree_CircularDependencies tests handling of circular dependency detection
func TestBuildBeadTree_CircularDependencies(t *testing.T) {
	items := []beadItem{
		testBeadItem("item-1", "Item 1", "open", 1, "task", "item-3"),
		testBeadItem("item-2", "Item 2", "open", 2, "task", "item-1"),
		testBeadItem("item-3", "Item 3", "open", 3, "task", "item-2"),
	}

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
		testBeadItem("item-1", "Item 1", "open", 1, "task"),
	}

	result := buildBeadTree(context.Background(), items, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
}

// TestBuildBeadTree_ParentChildRelationship tests that parent-child relationships are preserved
func TestBuildBeadTree_ParentChildRelationship(t *testing.T) {
	items := []beadItem{
		testBeadItemWithOptions("parent", "Parent", "closed", 1, "epic", true),
		testBeadItem("child", "Child", "open", 2, "task", "parent"),
	}

	// Parents should be fetched and visible when they have visible children
	result := buildBeadTree(context.Background(), items, nil)

	// Both parent and child should be visible
	if len(result) != 2 {
		t.Errorf("expected parent to be visible with open child, got %d items", len(result))
	}
}
