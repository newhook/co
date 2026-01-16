package task

import (
	"fmt"

	"github.com/newhook/co/internal/beads/queries"
)

// DependencyGraph represents bead dependencies.
type DependencyGraph struct {
	// DependsOn maps bead ID to IDs it depends on
	DependsOn map[string][]string
	// Dependents maps bead ID to IDs that depend on it (renamed from BlockedBy)
	Dependents map[string][]string
}

// BuildDependencyGraph creates a dependency graph from issues and their dependencies.
func BuildDependencyGraph(
	issues []queries.Issue,
	dependencies map[string][]queries.GetDependenciesForIssuesRow,
) *DependencyGraph {
	graph := &DependencyGraph{
		DependsOn:  make(map[string][]string),
		Dependents: make(map[string][]string),
	}

	// Create set of valid issue IDs
	validIDs := make(map[string]bool)
	for _, issue := range issues {
		validIDs[issue.ID] = true
		// Initialize empty slices for this issue
		graph.DependsOn[issue.ID] = []string{}
		graph.Dependents[issue.ID] = []string{}
	}

	// Build dependency relationships from the dependencies map
	for issueID, deps := range dependencies {
		for _, dep := range deps {
			// Only add dependencies that are in our issue set
			if validIDs[dep.DependsOnID] {
				if dep.Type == "blocks" || dep.Type == "blocked_by" {
					graph.DependsOn[issueID] = append(graph.DependsOn[issueID], dep.DependsOnID)
					graph.Dependents[dep.DependsOnID] = append(graph.Dependents[dep.DependsOnID], issueID)
				}
			}
		}
	}

	return graph
}

// TopologicalSort returns issues in dependency order (dependencies before dependents).
func TopologicalSort(graph *DependencyGraph, issues []queries.Issue) ([]queries.Issue, error) {
	issueMap := make(map[string]queries.Issue)
	for _, issue := range issues {
		issueMap[issue.ID] = issue
	}

	// Kahn's algorithm
	inDegree := make(map[string]int)
	for _, issue := range issues {
		inDegree[issue.ID] = len(graph.DependsOn[issue.ID])
	}

	// Start with issues that have no dependencies
	var queue []string
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	var result []queries.Issue
	for len(queue) > 0 {
		// Pop from queue
		id := queue[0]
		queue = queue[1:]

		result = append(result, issueMap[id])

		// Reduce in-degree of dependents
		for _, dependent := range graph.Dependents[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Check for cycles
	if len(result) != len(issues) {
		return nil, fmt.Errorf("dependency cycle detected")
	}

	return result, nil
}
