package task

import (
	"fmt"
	"sort"

	"github.com/newhook/co/internal/beads"
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
func (p *DefaultPlanner) Plan(inputBeads []beads.BeadWithDeps, budget int) ([]Task, error) {
	if len(inputBeads) == 0 {
		return nil, nil
	}

	// Build dependency graph
	graph := buildDependencyGraph(inputBeads)

	// Get topologically sorted beads (respecting dependencies)
	sorted, err := topologicalSort(graph, inputBeads)
	if err != nil {
		return nil, fmt.Errorf("failed to sort beads: %w", err)
	}

	// Estimate complexity for each bead
	complexities := make(map[string]int)
	for _, b := range sorted {
		score, _, err := p.estimator.Estimate(beads.Bead{
			ID:          b.ID,
			Title:       b.Title,
			Description: b.Description,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to estimate complexity for %s: %w", b.ID, err)
		}
		complexities[b.ID] = score
	}

	// Sort beads by complexity (descending) for first-fit decreasing
	sortedByComplexity := make([]beads.BeadWithDeps, len(sorted))
	copy(sortedByComplexity, sorted)
	sort.Slice(sortedByComplexity, func(i, j int) bool {
		return complexities[sortedByComplexity[i].ID] > complexities[sortedByComplexity[j].ID]
	})

	// First-fit decreasing bin-packing
	tasks := binPackBeads(sortedByComplexity, complexities, graph, budget)

	return tasks, nil
}

// dependencyGraph represents bead dependencies.
type dependencyGraph struct {
	// dependsOn maps bead ID to IDs it depends on
	dependsOn map[string][]string
	// blockedBy maps bead ID to IDs that depend on it
	blockedBy map[string][]string
}

// buildDependencyGraph creates a dependency graph from beads.
func buildDependencyGraph(inputBeads []beads.BeadWithDeps) *dependencyGraph {
	graph := &dependencyGraph{
		dependsOn: make(map[string][]string),
		blockedBy: make(map[string][]string),
	}

	// Create set of valid bead IDs
	validIDs := make(map[string]bool)
	for _, b := range inputBeads {
		validIDs[b.ID] = true
	}

	for _, b := range inputBeads {
		for _, dep := range b.Dependencies {
			if dep.DependencyType == "depends_on" && validIDs[dep.ID] {
				graph.dependsOn[b.ID] = append(graph.dependsOn[b.ID], dep.ID)
				graph.blockedBy[dep.ID] = append(graph.blockedBy[dep.ID], b.ID)
			}
		}
	}

	return graph
}

// topologicalSort returns beads in dependency order (dependencies before dependents).
func topologicalSort(graph *dependencyGraph, inputBeads []beads.BeadWithDeps) ([]beads.BeadWithDeps, error) {
	beadMap := make(map[string]beads.BeadWithDeps)
	for _, b := range inputBeads {
		beadMap[b.ID] = b
	}

	// Kahn's algorithm
	inDegree := make(map[string]int)
	for _, b := range inputBeads {
		inDegree[b.ID] = len(graph.dependsOn[b.ID])
	}

	// Start with beads that have no dependencies
	var queue []string
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	var result []beads.BeadWithDeps
	for len(queue) > 0 {
		// Pop from queue
		id := queue[0]
		queue = queue[1:]

		result = append(result, beadMap[id])

		// Reduce in-degree of dependents
		for _, dependent := range graph.blockedBy[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Check for cycles
	if len(result) != len(inputBeads) {
		return nil, fmt.Errorf("dependency cycle detected")
	}

	return result, nil
}

// binPackBeads assigns beads to tasks using first-fit decreasing algorithm.
func binPackBeads(beadsByComplexity []beads.BeadWithDeps, complexities map[string]int, graph *dependencyGraph, budget int) []Task {
	var tasks []Task
	assigned := make(map[string]int) // bead ID -> task index

	for _, b := range beadsByComplexity {
		complexity := complexities[b.ID]
		taskIdx := findBestTask(b, complexity, tasks, assigned, graph, budget)

		if taskIdx == -1 {
			// Create new task
			taskIdx = len(tasks)
			tasks = append(tasks, Task{
				ID:         fmt.Sprintf("task-%d", taskIdx+1),
				BeadIDs:    []string{},
				Beads:      []beads.Bead{},
				Complexity: 0,
				Status:     StatusPending,
			})
		}

		// Add bead to task
		tasks[taskIdx].BeadIDs = append(tasks[taskIdx].BeadIDs, b.ID)
		tasks[taskIdx].Beads = append(tasks[taskIdx].Beads, beads.Bead{
			ID:          b.ID,
			Title:       b.Title,
			Description: b.Description,
		})
		tasks[taskIdx].Complexity += complexity
		assigned[b.ID] = taskIdx
	}

	return tasks
}

// findBestTask finds the best task for a bead, or -1 if a new task is needed.
func findBestTask(b beads.BeadWithDeps, complexity int, tasks []Task, assigned map[string]int, graph *dependencyGraph, budget int) int {
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
		if !canAddToTask(b.ID, i, assigned, graph) {
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
func canAddToTask(beadID string, taskIdx int, assigned map[string]int, graph *dependencyGraph) bool {
	// Check that all dependencies are either in an earlier task or the same task
	for _, depID := range graph.dependsOn[beadID] {
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
