package cmd

import (
	"context"
	"sort"

	"github.com/newhook/co/internal/beads"
)

// fetchDependencyIDs gets the list of issue IDs that block the given issue
func fetchDependencyIDs(dir, beadID string) ([]string, error) {
	deps, err := beads.GetDependencies(context.Background(), beadID, dir)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, d := range deps {
		ids = append(ids, d.ID)
	}
	return ids, nil
}

// fetchBeadByID fetches a single bead by ID and returns a beadItem
func fetchBeadByID(dir, id string) (*beadItem, error) {
	b, err := beads.GetBeadFull(context.Background(), id, dir)
	if err != nil {
		return nil, err
	}

	return &beadItem{
		id:              b.ID,
		title:           b.Title,
		status:          b.Status,
		priority:        b.Priority,
		beadType:        b.Type,
		description:     b.Description,
		dependencyCount: b.DependencyCount,
		dependentCount:  b.DependentCount,
	}, nil
}

// buildBeadTree takes a flat list of beads and organizes them into a tree
// based on dependency relationships. Returns the items in tree order with
// treeDepth set for each item.
// When searchText is non-empty, skip fetching parent beads to avoid adding
// unfiltered items that don't match the search.
func buildBeadTree(items []beadItem, client *beads.Client, dir string, searchText string) []beadItem {
	if len(items) == 0 {
		return items
	}

	// Build a map of ID -> beadItem for quick lookup
	itemMap := make(map[string]*beadItem)
	for i := range items {
		itemMap[items[i].id] = &items[i]
	}

	// Collect all issue IDs
	issueIDs := make([]string, 0, len(items))
	for i := range items {
		issueIDs = append(issueIDs, items[i].id)
	}

	// Use database client if available, otherwise fall back to CLI
	if client != nil {
		// Fetch all issues with their dependencies in a single query
		result, err := client.GetIssuesWithDeps(context.Background(), issueIDs)
		if err == nil {
			// Populate dependencies from result
			for i := range items {
				if deps, ok := result.Dependencies[items[i].id]; ok {
					depIDs := make([]string, 0, len(deps))
					for _, dep := range deps {
						// Only include "blocks" type dependencies
						if dep.Type == "blocks" {
							depIDs = append(depIDs, dep.DependsOnID)
						}
					}
					items[i].dependencies = depIDs
				}
			}

			// Identify and fetch missing parent beads (dependencies not in our item list)
			// to preserve tree structure. Loop until no more missing parents are found
			// to handle multiple levels of closed ancestors.
			// Always fetch parents to preserve hierarchy context, even during search.
			fetchedParents := make(map[string]bool)
			for {
				missingParentIDs := make([]string, 0)
				for i := range items {
					for _, depID := range items[i].dependencies {
						if _, exists := itemMap[depID]; !exists && !fetchedParents[depID] {
							missingParentIDs = append(missingParentIDs, depID)
							fetchedParents[depID] = true
						}
					}
				}

				if len(missingParentIDs) == 0 {
					break
				}

				// Fetch missing parents in a single query
				parentResult, err := client.GetIssuesWithDeps(context.Background(), missingParentIDs)
				if err == nil {
					// Add missing parents to items
					for _, parentID := range missingParentIDs {
						if issue, ok := parentResult.Issues[parentID]; ok {
							parentBead := &beadItem{
								id:              issue.ID,
								title:           issue.Title,
								status:          issue.Status,
								priority:        int(issue.Priority),
								beadType:        issue.IssueType,
								description:     issue.Description,
								isClosedParent:  true,
							}

							// Populate dependencies for this parent
							if deps, ok := parentResult.Dependencies[parentID]; ok {
								depIDs := make([]string, 0, len(deps))
								for _, dep := range deps {
									if dep.Type == "blocks" {
										depIDs = append(depIDs, dep.DependsOnID)
									}
								}
								parentBead.dependencies = depIDs
							}

							items = append(items, *parentBead)
							itemMap[parentBead.id] = &items[len(items)-1]
						}
					}
				} else {
					break
				}
			}

			// Rebuild itemMap to fix stale pointers.
			itemMap = make(map[string]*beadItem)
			for i := range items {
				itemMap[items[i].id] = &items[i]
			}
		} else {
			// Fall back to CLI-based approach on error
			for i := range items {
				if items[i].dependencyCount > 0 {
					deps, err := fetchDependencyIDs(dir, items[i].id)
					if err == nil {
						items[i].dependencies = deps
					}
				}
			}
		}
	} else {
		// Use CLI-based approach when client is not available
		for i := range items {
			if items[i].dependencyCount > 0 {
				deps, err := fetchDependencyIDs(dir, items[i].id)
				if err == nil {
					items[i].dependencies = deps
				}
			}
		}

		fetchedParents := make(map[string]bool)
		for {
			missingParentIDs := make(map[string]bool)
			for i := range items {
				for _, depID := range items[i].dependencies {
					if _, exists := itemMap[depID]; !exists && !fetchedParents[depID] {
						missingParentIDs[depID] = true
					}
				}
			}

			if len(missingParentIDs) == 0 {
				break
			}

			for parentID := range missingParentIDs {
				fetchedParents[parentID] = true
				parentBead, err := fetchBeadByID(dir, parentID)
				if err == nil {
					parentBead.isClosedParent = true
					items = append(items, *parentBead)
					itemMap[parentBead.id] = &items[len(items)-1]

					if parentBead.dependencyCount > 0 {
						deps, err := fetchDependencyIDs(dir, parentBead.id)
						if err == nil {
							items[len(items)-1].dependencies = deps
						}
					}
				}
			}
		}

		itemMap = make(map[string]*beadItem)
		for i := range items {
			itemMap[items[i].id] = &items[i]
		}
	}

	// Build parent -> children map (issues that block -> issues they block)
	// If A blocks B, then B depends on A, so A is parent, B is child
	childrenMap := make(map[string][]string)
	for i := range items {
		for _, depID := range items[i].dependencies {
			// This item depends on depID, so depID is the parent
			childrenMap[depID] = append(childrenMap[depID], items[i].id)
		}
	}

	// Store children in each item
	for i := range items {
		items[i].children = childrenMap[items[i].id]
	}

	// Find root nodes (items with no visible dependencies within our set)
	// A bead is a root if it has no dependencies, OR if none of its dependencies
	// are in our visible set (e.g., all dependencies were deleted or unavailable)
	roots := []string{}
	for i := range items {
		hasVisibleDep := false
		for _, depID := range items[i].dependencies {
			if _, exists := itemMap[depID]; exists {
				hasVisibleDep = true
				break
			}
		}
		if !hasVisibleDep {
			roots = append(roots, items[i].id)
		}
	}

	// Sort roots: closed parents first (so their open children appear under them),
	// then by priority, then by ID
	sort.Slice(roots, func(i, j int) bool {
		a, b := itemMap[roots[i]], itemMap[roots[j]]
		// Closed parents come first
		if a.isClosedParent != b.isClosedParent {
			return a.isClosedParent
		}
		if a.priority != b.priority {
			return a.priority < b.priority
		}
		return a.id < b.id
	})

	// DFS to build tree order
	var result []beadItem
	visited := make(map[string]bool)

	// ancestorPattern tracks the prefix pattern for ancestor continuation lines.
	// Each character represents one depth level:
	// - "│" means the ancestor at that level has more siblings (needs continuation line)
	// - " " means the ancestor at that level is the last child (no continuation needed)
	var visit func(id string, depth int, ancestorPattern string, isLast bool)
	visit = func(id string, depth int, ancestorPattern string, isLast bool) {
		if visited[id] {
			return
		}
		visited[id] = true

		item, ok := itemMap[id]
		if !ok {
			return
		}

		item.treeDepth = depth
		item.isLastChild = isLast

		// Build the tree prefix pattern for this item
		if depth > 0 {
			// Start with ancestor continuation pattern (each character becomes "│ " or "  ")
			var prefix string
			for _, c := range ancestorPattern {
				if c == '│' {
					prefix += "│ "
				} else {
					prefix += "  "
				}
			}
			// Add the connector for this item
			if isLast {
				prefix += "└─"
			} else {
				prefix += "├─"
			}
			item.treePrefixPattern = prefix
		}

		result = append(result, *item)

		// Sort children by priority
		childIDs := childrenMap[id]
		sort.Slice(childIDs, func(i, j int) bool {
			a, b := itemMap[childIDs[i]], itemMap[childIDs[j]]
			if a == nil || b == nil {
				return childIDs[i] < childIDs[j]
			}
			if a.priority != b.priority {
				return a.priority < b.priority
			}
			return a.id < b.id
		})

		// Compute the ancestor pattern for children
		// If this item is the last child, its continuation is " " (no vertical line)
		// Otherwise, it's "│" (vertical line for siblings below)
		var childAncestorPattern string
		if depth == 0 {
			// Root nodes don't add to ancestor pattern
			childAncestorPattern = ancestorPattern
		} else if isLast {
			childAncestorPattern = ancestorPattern + " "
		} else {
			childAncestorPattern = ancestorPattern + "│"
		}

		for idx, childID := range childIDs {
			isLastChild := idx == len(childIDs)-1
			visit(childID, depth+1, childAncestorPattern, isLastChild)
		}
	}

	// Visit all roots
	for idx, rootID := range roots {
		isLastRoot := idx == len(roots)-1
		visit(rootID, 0, "", isLastRoot)
	}

	// Add any orphaned items (not reachable from roots)
	for i := range items {
		if !visited[items[i].id] {
			items[i].treeDepth = 0
			result = append(result, items[i])
		}
	}

	// Filter out closed parents that have no visible children directly under them.
	// They were only fetched to show tree structure, but if their children
	// appear under other parents, these closed parents add no value.
	// We check by looking at the next items in the result - if a closed parent
	// at depth N has no items at depth N+1 immediately following, it has no visible children.
	var filtered []beadItem
	for i, item := range result {
		// Keep the item if it's not a closed parent
		if !item.isClosedParent {
			filtered = append(filtered, item)
			continue
		}
		// For closed parents, check if there are children directly following
		hasVisibleChild := false
		expectedChildDepth := item.treeDepth + 1
		for j := i + 1; j < len(result); j++ {
			nextItem := result[j]
			if nextItem.treeDepth <= item.treeDepth {
				// We've moved past this parent's subtree
				break
			}
			if nextItem.treeDepth == expectedChildDepth {
				// Found a direct child
				hasVisibleChild = true
				break
			}
		}
		if hasVisibleChild {
			filtered = append(filtered, item)
		}
	}

	return filtered
}
