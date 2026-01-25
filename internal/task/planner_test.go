package task

import (
	"context"
	"testing"

	"github.com/newhook/co/internal/beads"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEstimator returns fixed complexity scores for testing.
type mockEstimator struct {
	scores map[string]int
}

func (m *mockEstimator) Estimate(ctx context.Context, bead beads.Bead) (int, int, error) {
	score := m.scores[bead.ID]
	if score == 0 {
		score = 5 // default
	}
	return score, score * 1000, nil
}

func TestBuildDependencyGraph(t *testing.T) {
	beadList := []beads.Bead{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}

	dependencies := map[string][]beads.Dependency{
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
		"c": {{IssueID: "c", DependsOnID: "b", Type: "blocks"}},
	}

	graph := BuildDependencyGraph(beadList, dependencies)

	// b depends on a
	require.Len(t, graph.DependsOn["b"], 1, "expected b to have 1 dependency")
	assert.Equal(t, "a", graph.DependsOn["b"][0], "expected b to depend on a")

	// c depends on b
	require.Len(t, graph.DependsOn["c"], 1, "expected c to have 1 dependency")
	assert.Equal(t, "b", graph.DependsOn["c"][0], "expected c to depend on b")

	// a blocks b (now called Dependents)
	require.Len(t, graph.Dependents["a"], 1, "expected a to have 1 dependent")
	assert.Equal(t, "b", graph.Dependents["a"][0], "expected a to have dependent b")
}

func TestBuildDependencyGraphIgnoresExternalDeps(t *testing.T) {
	beadList := []beads.Bead{
		{ID: "a", Title: "A"},
	}

	// Dependencies on external beads (not in beads list) should be ignored
	dependencies := map[string][]beads.Dependency{
		"a": {{IssueID: "a", DependsOnID: "external", Type: "blocks"}},
	}

	graph := BuildDependencyGraph(beadList, dependencies)

	// External dependency should be filtered out since "external" is not in the beads list
	assert.Empty(t, graph.DependsOn["a"], "external dependency should be ignored")
	assert.Empty(t, graph.Dependents["external"], "external should not have dependents since it's not in beads")
}

func TestTopologicalSort(t *testing.T) {
	beadList := []beads.Bead{
		{ID: "c", Title: "C"},
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	}

	dependencies := map[string][]beads.Dependency{
		"c": {{IssueID: "c", DependsOnID: "b", Type: "blocks"}},
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
	}

	graph := BuildDependencyGraph(beadList, dependencies)
	sorted, err := TopologicalSort(graph, beadList)
	require.NoError(t, err, "TopologicalSort failed")

	// a should come before b, b should come before c
	positions := make(map[string]int)
	for i, bead := range sorted {
		positions[bead.ID] = i
	}

	assert.Less(t, positions["a"], positions["b"], "a should come before b")
	assert.Less(t, positions["b"], positions["c"], "b should come before c")
}

func TestTopologicalSortDetectsCycle(t *testing.T) {
	beadList := []beads.Bead{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	}

	dependencies := map[string][]beads.Dependency{
		"a": {{IssueID: "a", DependsOnID: "b", Type: "blocks"}},
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
	}

	graph := BuildDependencyGraph(beadList, dependencies)
	_, err := TopologicalSort(graph, beadList)
	assert.Error(t, err, "expected error for cycle detection")
}

func TestPlanSimple(t *testing.T) {
	ctx := context.Background()
	estimator := &mockEstimator{
		scores: map[string]int{"a": 3, "b": 3, "c": 3},
	}
	planner := NewDefaultPlanner(estimator)

	beadList := []beads.Bead{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}

	dependencies := map[string][]beads.Dependency{}

	// Token budget of 10000 should fit all beads (3000+3000+3000=9000 tokens)
	tasks, err := planner.Plan(ctx, beadList, dependencies, 10000)
	require.NoError(t, err, "Plan failed")

	assert.Len(t, tasks, 1, "expected 1 task")
	assert.Len(t, tasks[0].BeadIDs, 3, "expected 3 beads in task")
}

func TestPlanSplitByBudget(t *testing.T) {
	ctx := context.Background()
	estimator := &mockEstimator{
		scores: map[string]int{"a": 5, "b": 5, "c": 5},
	}
	planner := NewDefaultPlanner(estimator)

	beadList := []beads.Bead{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}

	dependencies := map[string][]beads.Dependency{}

	// Token budget of 7000 should split into multiple tasks (each bead is 5000 tokens)
	tasks, err := planner.Plan(ctx, beadList, dependencies, 7000)
	require.NoError(t, err, "Plan failed")

	assert.GreaterOrEqual(t, len(tasks), 2, "expected at least 2 tasks")

	// Verify all beads are assigned
	totalBeads := 0
	for _, task := range tasks {
		totalBeads += len(task.BeadIDs)
	}
	assert.Equal(t, 3, totalBeads, "expected 3 total beads")
}

func TestPlanRespectsDependencies(t *testing.T) {
	ctx := context.Background()
	estimator := &mockEstimator{
		scores: map[string]int{"a": 3, "b": 3},
	}
	planner := NewDefaultPlanner(estimator)

	beadList := []beads.Bead{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	}

	dependencies := map[string][]beads.Dependency{
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
	}

	// Small token budget to force multiple tasks (each bead is 3000 tokens)
	tasks, err := planner.Plan(ctx, beadList, dependencies, 4000)
	require.NoError(t, err, "Plan failed")

	// Find which tasks contain a and b
	taskForBead := make(map[string]int)
	for i, task := range tasks {
		for _, id := range task.BeadIDs {
			taskForBead[id] = i
		}
	}

	// b depends on a, so a's task index must be <= b's task index
	assert.LessOrEqual(t, taskForBead["a"], taskForBead["b"], "dependency violated: a should be in same or earlier task than b")
}

func TestPlanEmpty(t *testing.T) {
	ctx := context.Background()
	estimator := &mockEstimator{}
	planner := NewDefaultPlanner(estimator)

	dependencies := map[string][]beads.Dependency{}

	tasks, err := planner.Plan(ctx, nil, dependencies, 10000)
	require.NoError(t, err, "Plan failed")

	assert.Empty(t, tasks, "expected no tasks for empty input")
}

func TestPlanFirstFitDecreasing(t *testing.T) {
	ctx := context.Background()
	// Larger beads are assigned first (by token estimate)
	estimator := &mockEstimator{
		scores: map[string]int{"small": 2, "medium": 4, "large": 6},
	}
	planner := NewDefaultPlanner(estimator)

	beadList := []beads.Bead{
		{ID: "small", Title: "Small"},
		{ID: "medium", Title: "Medium"},
		{ID: "large", Title: "Large"},
	}

	dependencies := map[string][]beads.Dependency{}

	// Token budget of 10000: large (6000) goes first, then small (2000) fits, medium (4000) won't fit
	// So we expect: task1=[large, small], task2=[medium]
	tasks, err := planner.Plan(ctx, beadList, dependencies, 10000)
	require.NoError(t, err, "Plan failed")

	assert.Len(t, tasks, 2, "expected 2 tasks")
}

func TestCanAddToTask(t *testing.T) {
	graph := &DependencyGraph{
		DependsOn: map[string][]string{
			"b": {"a"},
		},
		Dependents: map[string][]string{
			"a": {"b"},
		},
	}

	assigned := map[string]int{
		"a": 0, // a is in task 0
	}

	// b depends on a which is in task 0, so b can be added to task 0 or later
	assert.True(t, canAddToTask("b", 0, assigned, graph), "b should be addable to task 0 (same as dependency)")
	assert.True(t, canAddToTask("b", 1, assigned, graph), "b should be addable to task 1 (after dependency)")

	// Can't add b to task before a
	assigned["a"] = 1
	assert.False(t, canAddToTask("b", 0, assigned, graph), "b should not be addable to task 0 (before dependency in task 1)")
}

// TestPlanChainDependencySplitAcrossTasks tests that beads with chain dependency
// (A→B→C) split across tasks create a task chain with correct ordering.
func TestPlanChainDependencySplitAcrossTasks(t *testing.T) {
	ctx := context.Background()
	// Small budget to force each bead into separate task
	estimator := &mockEstimator{
		scores: map[string]int{"a": 5, "b": 5, "c": 5},
	}
	planner := NewDefaultPlanner(estimator)

	beadList := []beads.Bead{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}

	// Chain: c depends on b, b depends on a
	dependencies := map[string][]beads.Dependency{
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
		"c": {{IssueID: "c", DependsOnID: "b", Type: "blocks"}},
	}

	// Budget of 6000 allows one bead per task (each is 5000 tokens)
	tasks, err := planner.Plan(ctx, beadList, dependencies, 6000)
	require.NoError(t, err, "Plan failed")
	require.Len(t, tasks, 3, "expected 3 tasks for 3 beads with chain dependency")

	// Find which task contains each bead
	taskForBead := make(map[string]int)
	for i, task := range tasks {
		for _, id := range task.BeadIDs {
			taskForBead[id] = i
		}
	}

	// Verify chain ordering: task(a) < task(b) < task(c)
	assert.Less(t, taskForBead["a"], taskForBead["b"], "a should be in earlier task than b")
	assert.Less(t, taskForBead["b"], taskForBead["c"], "b should be in earlier task than c")
}

// TestPlanDiamondDependencySplitAcrossTasks tests that beads with diamond dependency
// create the correct task graph. Diamond: A, B depend on nothing; C depends on both A and B.
func TestPlanDiamondDependencySplitAcrossTasks(t *testing.T) {
	ctx := context.Background()
	estimator := &mockEstimator{
		scores: map[string]int{"a": 5, "b": 5, "c": 5, "d": 5},
	}
	planner := NewDefaultPlanner(estimator)

	// Diamond: a and b are independent, c depends on both, d depends on c
	beadList := []beads.Bead{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
		{ID: "d", Title: "D"},
	}

	dependencies := map[string][]beads.Dependency{
		"c": {
			{IssueID: "c", DependsOnID: "a", Type: "blocks"},
			{IssueID: "c", DependsOnID: "b", Type: "blocks"},
		},
		"d": {{IssueID: "d", DependsOnID: "c", Type: "blocks"}},
	}

	// Budget of 6000 allows one bead per task
	tasks, err := planner.Plan(ctx, beadList, dependencies, 6000)
	require.NoError(t, err, "Plan failed")
	require.Len(t, tasks, 4, "expected 4 tasks for 4 beads with diamond dependency")

	// Find which task contains each bead
	taskForBead := make(map[string]int)
	for i, task := range tasks {
		for _, id := range task.BeadIDs {
			taskForBead[id] = i
		}
	}

	// c depends on both a and b, so task(c) > task(a) and task(c) > task(b)
	assert.Less(t, taskForBead["a"], taskForBead["c"], "a should be in earlier task than c")
	assert.Less(t, taskForBead["b"], taskForBead["c"], "b should be in earlier task than c")
	// d depends on c
	assert.Less(t, taskForBead["c"], taskForBead["d"], "c should be in earlier task than d")
}

// TestPlanSameTaskDependenciesNoSelfDep tests that beads in the same task
// with dependencies do not create self-dependency (task depending on itself).
func TestPlanSameTaskDependenciesNoSelfDep(t *testing.T) {
	ctx := context.Background()
	estimator := &mockEstimator{
		scores: map[string]int{"a": 2, "b": 2},
	}
	planner := NewDefaultPlanner(estimator)

	beadList := []beads.Bead{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	}

	// b depends on a
	dependencies := map[string][]beads.Dependency{
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
	}

	// Large budget to fit both beads in same task (each is 2000 tokens)
	tasks, err := planner.Plan(ctx, beadList, dependencies, 10000)
	require.NoError(t, err, "Plan failed")
	require.Len(t, tasks, 1, "expected 1 task with both beads")

	// Both beads should be in the same task
	taskForBead := make(map[string]int)
	for i, task := range tasks {
		for _, id := range task.BeadIDs {
			taskForBead[id] = i
		}
	}

	assert.Equal(t, taskForBead["a"], taskForBead["b"], "a and b should be in the same task")
}

// ComputeInterTaskDeps is a helper function that mimics the inter-task dependency
// computation logic from handlePostEstimation. Used for testing.
func ComputeInterTaskDeps(tasks []Task, dependencies map[string][]beads.Dependency) map[int][]int {
	// Build beadID → task index mapping
	beadToTask := make(map[string]int)
	for i, t := range tasks {
		for _, beadID := range t.BeadIDs {
			beadToTask[beadID] = i
		}
	}

	// Compute inter-task dependencies
	// Returns map of taskIdx → list of task indices it depends on
	interTaskDeps := make(map[int]map[int]bool)
	for beadID, deps := range dependencies {
		taskIdx, ok := beadToTask[beadID]
		if !ok {
			continue
		}
		for _, dep := range deps {
			depTaskIdx, ok := beadToTask[dep.DependsOnID]
			if !ok {
				continue
			}
			if taskIdx == depTaskIdx {
				continue // same task, no inter-task dependency
			}
			if interTaskDeps[taskIdx] == nil {
				interTaskDeps[taskIdx] = make(map[int]bool)
			}
			interTaskDeps[taskIdx][depTaskIdx] = true
		}
	}

	// Convert to slice representation
	result := make(map[int][]int)
	for taskIdx, depSet := range interTaskDeps {
		for depIdx := range depSet {
			result[taskIdx] = append(result[taskIdx], depIdx)
		}
	}
	return result
}

// TestComputeInterTaskDepsChain tests inter-task dependency computation for chain.
func TestComputeInterTaskDepsChain(t *testing.T) {
	// Chain: c depends on b, b depends on a - each in separate task
	tasks := []Task{
		{ID: "task-1", BeadIDs: []string{"a"}},
		{ID: "task-2", BeadIDs: []string{"b"}},
		{ID: "task-3", BeadIDs: []string{"c"}},
	}

	dependencies := map[string][]beads.Dependency{
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
		"c": {{IssueID: "c", DependsOnID: "b", Type: "blocks"}},
	}

	interDeps := ComputeInterTaskDeps(tasks, dependencies)

	// task-2 (index 1) should depend on task-1 (index 0)
	require.Contains(t, interDeps, 1, "task-2 should have dependencies")
	assert.Contains(t, interDeps[1], 0, "task-2 should depend on task-1")

	// task-3 (index 2) should depend on task-2 (index 1)
	require.Contains(t, interDeps, 2, "task-3 should have dependencies")
	assert.Contains(t, interDeps[2], 1, "task-3 should depend on task-2")

	// task-1 (index 0) should have no dependencies
	assert.NotContains(t, interDeps, 0, "task-1 should have no inter-task dependencies")
}

// TestComputeInterTaskDepsDiamond tests inter-task dependency computation for diamond.
func TestComputeInterTaskDepsDiamond(t *testing.T) {
	// Diamond: a and b independent, c depends on both, d depends on c
	tasks := []Task{
		{ID: "task-1", BeadIDs: []string{"a"}},
		{ID: "task-2", BeadIDs: []string{"b"}},
		{ID: "task-3", BeadIDs: []string{"c"}},
		{ID: "task-4", BeadIDs: []string{"d"}},
	}

	dependencies := map[string][]beads.Dependency{
		"c": {
			{IssueID: "c", DependsOnID: "a", Type: "blocks"},
			{IssueID: "c", DependsOnID: "b", Type: "blocks"},
		},
		"d": {{IssueID: "d", DependsOnID: "c", Type: "blocks"}},
	}

	interDeps := ComputeInterTaskDeps(tasks, dependencies)

	// task-3 (index 2) should depend on both task-1 (index 0) and task-2 (index 1)
	require.Contains(t, interDeps, 2, "task-3 should have dependencies")
	assert.Contains(t, interDeps[2], 0, "task-3 should depend on task-1")
	assert.Contains(t, interDeps[2], 1, "task-3 should depend on task-2")

	// task-4 (index 3) should depend on task-3 (index 2)
	require.Contains(t, interDeps, 3, "task-4 should have dependencies")
	assert.Contains(t, interDeps[3], 2, "task-4 should depend on task-3")

	// task-1 and task-2 should have no dependencies
	assert.NotContains(t, interDeps, 0, "task-1 should have no inter-task dependencies")
	assert.NotContains(t, interDeps, 1, "task-2 should have no inter-task dependencies")
}

// TestComputeInterTaskDepsSameTaskNoDeps tests that beads in the same task
// do not create self-dependencies.
func TestComputeInterTaskDepsSameTaskNoDeps(t *testing.T) {
	// Both beads in same task, b depends on a
	tasks := []Task{
		{ID: "task-1", BeadIDs: []string{"a", "b"}},
	}

	dependencies := map[string][]beads.Dependency{
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
	}

	interDeps := ComputeInterTaskDeps(tasks, dependencies)

	// No inter-task dependencies since both beads are in the same task
	assert.Empty(t, interDeps, "same-task dependencies should not create inter-task deps")
}
