package task

import (
	"fmt"

	"github.com/newhook/co/internal/beads"
)

// DependencyGraph represents bead dependencies.
type DependencyGraph struct {
	// DependsOn maps bead ID to IDs it depends on
	DependsOn map[string][]string
	// BlockedBy maps bead ID to IDs that depend on it
	BlockedBy map[string][]string
}

// BuildDependencyGraph creates a dependency graph from beads.
func BuildDependencyGraph(inputBeads []beads.BeadWithDeps) *DependencyGraph {
	graph := &DependencyGraph{
		DependsOn: make(map[string][]string),
		BlockedBy: make(map[string][]string),
	}

	// Create set of valid bead IDs
	validIDs := make(map[string]bool)
	for _, b := range inputBeads {
		validIDs[b.ID] = true
	}

	for _, b := range inputBeads {
		for _, dep := range b.Dependencies {
			if dep.DependencyType == "depends_on" && validIDs[dep.ID] {
				graph.DependsOn[b.ID] = append(graph.DependsOn[b.ID], dep.ID)
				graph.BlockedBy[dep.ID] = append(graph.BlockedBy[dep.ID], b.ID)
			}
		}
	}

	return graph
}

// TopologicalSort returns beads in dependency order (dependencies before dependents).
func TopologicalSort(graph *DependencyGraph, inputBeads []beads.BeadWithDeps) ([]beads.BeadWithDeps, error) {
	beadMap := make(map[string]beads.BeadWithDeps)
	for _, b := range inputBeads {
		beadMap[b.ID] = b
	}

	// Kahn's algorithm
	inDegree := make(map[string]int)
	for _, b := range inputBeads {
		inDegree[b.ID] = len(graph.DependsOn[b.ID])
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
		for _, dependent := range graph.BlockedBy[id] {
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
