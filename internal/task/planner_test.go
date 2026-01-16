package task

import (
	"context"
	"testing"

	"github.com/newhook/co/internal/beads/queries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEstimator returns fixed complexity scores for testing.
type mockEstimator struct {
	scores map[string]int
}

func (m *mockEstimator) Estimate(ctx context.Context, issue queries.Issue) (int, int, error) {
	score := m.scores[issue.ID]
	if score == 0 {
		score = 5 // default
	}
	return score, score * 1000, nil
}

func TestBuildDependencyGraph(t *testing.T) {
	issues := []queries.Issue{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}

	dependencies := map[string][]queries.GetDependenciesForIssuesRow{
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
		"c": {{IssueID: "c", DependsOnID: "b", Type: "blocks"}},
	}

	graph := BuildDependencyGraph(issues, dependencies)

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
	issues := []queries.Issue{
		{ID: "a", Title: "A"},
	}

	// Dependencies on external issues (not in issues list) should be ignored
	dependencies := map[string][]queries.GetDependenciesForIssuesRow{
		"a": {{IssueID: "a", DependsOnID: "external", Type: "blocks"}},
	}

	graph := BuildDependencyGraph(issues, dependencies)

	// External dependency should be filtered out since "external" is not in the issues list
	assert.Empty(t, graph.DependsOn["a"], "external dependency should be ignored")
	assert.Empty(t, graph.Dependents["external"], "external should not have dependents since it's not in issues")
}

func TestTopologicalSort(t *testing.T) {
	issues := []queries.Issue{
		{ID: "c", Title: "C"},
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	}

	dependencies := map[string][]queries.GetDependenciesForIssuesRow{
		"c": {{IssueID: "c", DependsOnID: "b", Type: "blocks"}},
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
	}

	graph := BuildDependencyGraph(issues, dependencies)
	sorted, err := TopologicalSort(graph, issues)
	require.NoError(t, err, "TopologicalSort failed")

	// a should come before b, b should come before c
	positions := make(map[string]int)
	for i, issue := range sorted {
		positions[issue.ID] = i
	}

	assert.Less(t, positions["a"], positions["b"], "a should come before b")
	assert.Less(t, positions["b"], positions["c"], "b should come before c")
}

func TestTopologicalSortDetectsCycle(t *testing.T) {
	issues := []queries.Issue{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	}

	dependencies := map[string][]queries.GetDependenciesForIssuesRow{
		"a": {{IssueID: "a", DependsOnID: "b", Type: "blocks"}},
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
	}

	graph := BuildDependencyGraph(issues, dependencies)
	_, err := TopologicalSort(graph, issues)
	assert.Error(t, err, "expected error for cycle detection")
}

func TestPlanSimple(t *testing.T) {
	ctx := context.Background()
	estimator := &mockEstimator{
		scores: map[string]int{"a": 3, "b": 3, "c": 3},
	}
	planner := NewDefaultPlanner(estimator)

	issues := []queries.Issue{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}

	dependencies := map[string][]queries.GetDependenciesForIssuesRow{}

	// Budget of 10 should fit all beads in one task (3+3+3=9)
	tasks, err := planner.Plan(ctx, issues, dependencies, 10)
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

	issues := []queries.Issue{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
		{ID: "c", Title: "C"},
	}

	dependencies := map[string][]queries.GetDependenciesForIssuesRow{}

	// Budget of 7 should split into multiple tasks
	tasks, err := planner.Plan(ctx, issues, dependencies, 7)
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

	issues := []queries.Issue{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B"},
	}

	dependencies := map[string][]queries.GetDependenciesForIssuesRow{
		"b": {{IssueID: "b", DependsOnID: "a", Type: "blocks"}},
	}

	// Small budget to force multiple tasks
	tasks, err := planner.Plan(ctx, issues, dependencies, 4)
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

	dependencies := map[string][]queries.GetDependenciesForIssuesRow{}

	tasks, err := planner.Plan(ctx, nil, dependencies, 10)
	require.NoError(t, err, "Plan failed")

	assert.Empty(t, tasks, "expected no tasks for empty input")
}

func TestPlanFirstFitDecreasing(t *testing.T) {
	ctx := context.Background()
	// Larger beads are assigned first
	estimator := &mockEstimator{
		scores: map[string]int{"small": 2, "medium": 4, "large": 6},
	}
	planner := NewDefaultPlanner(estimator)

	issues := []queries.Issue{
		{ID: "small", Title: "Small"},
		{ID: "medium", Title: "Medium"},
		{ID: "large", Title: "Large"},
	}

	dependencies := map[string][]queries.GetDependenciesForIssuesRow{}

	tasks, err := planner.Plan(ctx, issues, dependencies, 10)
	require.NoError(t, err, "Plan failed")

	// With budget 10, large (6) goes first, then small (2) fits, medium (4) won't fit
	// So we expect: task1=[large, small], task2=[medium]
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
