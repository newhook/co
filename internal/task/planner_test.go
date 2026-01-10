package task

import (
	"testing"

	"github.com/newhook/co/internal/beads"
)

// mockEstimator returns fixed complexity scores for testing.
type mockEstimator struct {
	scores map[string]int
}

func (m *mockEstimator) Estimate(bead beads.Bead) (int, int, error) {
	score := m.scores[bead.ID]
	if score == 0 {
		score = 5 // default
	}
	return score, score * 1000, nil
}

func TestBuildDependencyGraph(t *testing.T) {
	inputBeads := []beads.BeadWithDeps{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B", Dependencies: []beads.Dependency{{ID: "a", DependencyType: "depends_on"}}},
		{ID: "c", Title: "C", Dependencies: []beads.Dependency{{ID: "b", DependencyType: "depends_on"}}},
	}

	graph := buildDependencyGraph(inputBeads)

	// b depends on a
	if len(graph.dependsOn["b"]) != 1 || graph.dependsOn["b"][0] != "a" {
		t.Errorf("expected b to depend on a, got %v", graph.dependsOn["b"])
	}

	// c depends on b
	if len(graph.dependsOn["c"]) != 1 || graph.dependsOn["c"][0] != "b" {
		t.Errorf("expected c to depend on b, got %v", graph.dependsOn["c"])
	}

	// a blocks b
	if len(graph.blockedBy["a"]) != 1 || graph.blockedBy["a"][0] != "b" {
		t.Errorf("expected a to block b, got %v", graph.blockedBy["a"])
	}
}

func TestBuildDependencyGraphIgnoresExternalDeps(t *testing.T) {
	inputBeads := []beads.BeadWithDeps{
		{ID: "a", Title: "A", Dependencies: []beads.Dependency{{ID: "external", DependencyType: "depends_on"}}},
	}

	graph := buildDependencyGraph(inputBeads)

	// External dependency should be ignored
	if len(graph.dependsOn["a"]) != 0 {
		t.Errorf("expected no dependencies for a, got %v", graph.dependsOn["a"])
	}
}

func TestTopologicalSort(t *testing.T) {
	inputBeads := []beads.BeadWithDeps{
		{ID: "c", Title: "C", Dependencies: []beads.Dependency{{ID: "b", DependencyType: "depends_on"}}},
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B", Dependencies: []beads.Dependency{{ID: "a", DependencyType: "depends_on"}}},
	}

	graph := buildDependencyGraph(inputBeads)
	sorted, err := topologicalSort(graph, inputBeads)
	if err != nil {
		t.Fatalf("topologicalSort failed: %v", err)
	}

	// a should come before b, b should come before c
	positions := make(map[string]int)
	for i, b := range sorted {
		positions[b.ID] = i
	}

	if positions["a"] > positions["b"] {
		t.Error("a should come before b")
	}
	if positions["b"] > positions["c"] {
		t.Error("b should come before c")
	}
}

func TestTopologicalSortDetectsCycle(t *testing.T) {
	inputBeads := []beads.BeadWithDeps{
		{ID: "a", Title: "A", Dependencies: []beads.Dependency{{ID: "b", DependencyType: "depends_on"}}},
		{ID: "b", Title: "B", Dependencies: []beads.Dependency{{ID: "a", DependencyType: "depends_on"}}},
	}

	graph := buildDependencyGraph(inputBeads)
	_, err := topologicalSort(graph, inputBeads)
	if err == nil {
		t.Error("expected error for cycle detection")
	}
}

func TestPlanSimple(t *testing.T) {
	estimator := &mockEstimator{
		scores: map[string]int{"a": 3, "b": 3, "c": 3},
	}
	planner := NewDefaultPlanner(estimator)

	inputBeads := []beads.BeadWithDeps{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}

	// Budget of 10 should fit all beads in one task (3+3+3=9)
	tasks, err := planner.Plan(inputBeads, 10)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}

	if len(tasks[0].BeadIDs) != 3 {
		t.Errorf("expected 3 beads in task, got %d", len(tasks[0].BeadIDs))
	}
}

func TestPlanSplitByBudget(t *testing.T) {
	estimator := &mockEstimator{
		scores: map[string]int{"a": 5, "b": 5, "c": 5},
	}
	planner := NewDefaultPlanner(estimator)

	inputBeads := []beads.BeadWithDeps{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}

	// Budget of 7 should split into multiple tasks
	tasks, err := planner.Plan(inputBeads, 7)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if len(tasks) < 2 {
		t.Errorf("expected at least 2 tasks, got %d", len(tasks))
	}

	// Verify all beads are assigned
	totalBeads := 0
	for _, task := range tasks {
		totalBeads += len(task.BeadIDs)
	}
	if totalBeads != 3 {
		t.Errorf("expected 3 total beads, got %d", totalBeads)
	}
}

func TestPlanRespectsDependencies(t *testing.T) {
	estimator := &mockEstimator{
		scores: map[string]int{"a": 3, "b": 3},
	}
	planner := NewDefaultPlanner(estimator)

	inputBeads := []beads.BeadWithDeps{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B", Dependencies: []beads.Dependency{{ID: "a", DependencyType: "depends_on"}}},
	}

	// Small budget to force multiple tasks
	tasks, err := planner.Plan(inputBeads, 4)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Find which tasks contain a and b
	taskForBead := make(map[string]int)
	for i, task := range tasks {
		for _, id := range task.BeadIDs {
			taskForBead[id] = i
		}
	}

	// b depends on a, so a's task index must be <= b's task index
	if taskForBead["a"] > taskForBead["b"] {
		t.Error("dependency violated: a should be in same or earlier task than b")
	}
}

func TestPlanEmpty(t *testing.T) {
	estimator := &mockEstimator{}
	planner := NewDefaultPlanner(estimator)

	tasks, err := planner.Plan(nil, 10)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if tasks != nil && len(tasks) != 0 {
		t.Errorf("expected no tasks for empty input, got %d", len(tasks))
	}
}

func TestPlanFirstFitDecreasing(t *testing.T) {
	// Larger beads are assigned first
	estimator := &mockEstimator{
		scores: map[string]int{"small": 2, "medium": 4, "large": 6},
	}
	planner := NewDefaultPlanner(estimator)

	inputBeads := []beads.BeadWithDeps{
		{ID: "small", Title: "Small"},
		{ID: "medium", Title: "Medium"},
		{ID: "large", Title: "Large"},
	}

	tasks, err := planner.Plan(inputBeads, 10)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// With budget 10, large (6) goes first, then small (2) fits, medium (4) won't fit
	// So we expect: task1=[large, small], task2=[medium]
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestCanAddToTask(t *testing.T) {
	graph := &dependencyGraph{
		dependsOn: map[string][]string{
			"b": {"a"},
		},
		blockedBy: map[string][]string{
			"a": {"b"},
		},
	}

	assigned := map[string]int{
		"a": 0, // a is in task 0
	}

	// b depends on a which is in task 0, so b can be added to task 0 or later
	if !canAddToTask("b", 0, assigned, graph) {
		t.Error("b should be addable to task 0 (same as dependency)")
	}
	if !canAddToTask("b", 1, assigned, graph) {
		t.Error("b should be addable to task 1 (after dependency)")
	}

	// Can't add b to task before a
	assigned["a"] = 1
	if canAddToTask("b", 0, assigned, graph) {
		t.Error("b should not be addable to task 0 (before dependency in task 1)")
	}
}
