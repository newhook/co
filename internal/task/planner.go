package task

import (
	"context"
	"fmt"
	"sort"

	"github.com/newhook/co/internal/beads/queries"
)

// DefaultPlanner implements the Planner interface using bin-packing.
type DefaultPlanner struct {
	estimator ComplexityEstimator
}

// NewDefaultPlanner creates a new planner with the given complexity estimator.
func NewDefaultPlanner(estimator ComplexityEstimator) *DefaultPlanner {
	return &DefaultPlanner{estimator: estimator}
}

// Plan creates task assignments from beads using bin-packing algorithm.
// The budget represents the target complexity per task.
func (p *DefaultPlanner) Plan(
	ctx context.Context,
	issues []queries.Issue,
	dependencies map[string][]queries.GetDependenciesForIssuesRow,
	budget int,
) ([]Task, error) {
	if len(issues) == 0 {
		return nil, nil
	}

	// Build dependency graph
	graph := BuildDependencyGraph(issues, dependencies)

	// Get topologically sorted issues (respecting dependencies)
	sorted, err := TopologicalSort(graph, issues)
	if err != nil {
		return nil, fmt.Errorf("failed to sort issues: %w", err)
	}

	// Estimate complexity for each issue - no conversion needed!
	complexities := make(map[string]int)
	for _, issue := range sorted {
		score, _, err := p.estimator.Estimate(ctx, issue)
		if err != nil {
			return nil, fmt.Errorf("failed to estimate complexity for %s: %w", issue.ID, err)
		}
		complexities[issue.ID] = score
	}

	// Sort issues by complexity (descending) for first-fit decreasing
	sortedByComplexity := make([]queries.Issue, len(sorted))
	copy(sortedByComplexity, sorted)
	sort.Slice(sortedByComplexity, func(i, j int) bool {
		return complexities[sortedByComplexity[i].ID] > complexities[sortedByComplexity[j].ID]
	})

	// First-fit decreasing bin-packing
	tasks := binPackBeads(sortedByComplexity, complexities, graph, budget)

	return tasks, nil
}

// binPackBeads assigns beads to tasks using first-fit decreasing algorithm.
func binPackBeads(issuesByComplexity []queries.Issue, complexities map[string]int, graph *DependencyGraph, budget int) []Task {
	var tasks []Task
	assigned := make(map[string]int) // bead ID -> task index

	for _, issue := range issuesByComplexity {
		complexity := complexities[issue.ID]
		taskIdx := findBestTask(issue.ID, complexity, tasks, assigned, graph, budget)

		if taskIdx == -1 {
			// Create new task
			taskIdx = len(tasks)
			tasks = append(tasks, Task{
				ID:         fmt.Sprintf("task-%d", taskIdx+1),
				BeadIDs:    []string{},
				Beads:      []queries.Issue{},
				Complexity: 0,
				Status:     StatusPending,
			})
		}

		// Add issue to task - no conversion needed!
		tasks[taskIdx].BeadIDs = append(tasks[taskIdx].BeadIDs, issue.ID)
		tasks[taskIdx].Beads = append(tasks[taskIdx].Beads, issue)
		tasks[taskIdx].Complexity += complexity
		assigned[issue.ID] = taskIdx
	}

	return tasks
}

// findBestTask finds the best task for a bead, or -1 if a new task is needed.
func findBestTask(beadID string, complexity int, tasks []Task, assigned map[string]int, graph *DependencyGraph, budget int) int {
	bestIdx := -1
	bestFit := budget + 1 // Initialize to impossible value

	for i := range tasks {
		// Check if adding this bead would exceed budget
		newComplexity := tasks[i].Complexity + complexity
		if newComplexity > budget {
			continue
		}

		// Check dependency constraints:
		// All dependencies must either be:
		// 1. In a previous task (already completed)
		// 2. In the same task
		if !canAddToTask(beadID, i, assigned, graph) {
			continue
		}

		// First-fit: take the first valid task
		// Could be changed to best-fit by tracking remaining space
		remaining := budget - newComplexity
		if remaining < bestFit {
			bestIdx = i
			bestFit = remaining
		}
	}

	return bestIdx
}

// canAddToTask checks if a bead can be added to a task based on dependency constraints.
func canAddToTask(beadID string, taskIdx int, assigned map[string]int, graph *DependencyGraph) bool {
	// Check that all dependencies are either in an earlier task or the same task
	for _, depID := range graph.DependsOn[beadID] {
		depTaskIdx, ok := assigned[depID]
		if !ok {
			// Dependency not yet assigned - can't add this bead yet
			return false
		}
		if depTaskIdx > taskIdx {
			// Dependency is in a later task - would create cycle
			return false
		}
	}
	return true
}
