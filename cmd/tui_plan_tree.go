package cmd

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/newhook/co/internal/beads"
)

// buildBeadTree takes a flat list of beads and organizes them into a tree
// based on dependency relationships. Returns the items in tree order with
// treeDepth set for each item.
// When searchText is non-empty, skip fetching parent beads to avoid adding
// unfiltered items that don't match the search.
func buildBeadTree(ctx context.Context, items []beadItem, client *beads.Client, dir string, searchText string) []beadItem {
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
		result, err := client.GetBeadsWithDeps(ctx, issueIDs)
		if err == nil {
			// Populate dependencies from result
			for i := range items {
				if deps, ok := result.Dependencies[items[i].id]; ok {
					depIDs := make([]string, 0, len(deps))
					for _, dep := range deps {
						// Include "blocks" and "parent-child" type dependencies
						if dep.Type == "blocks" || dep.Type == "parent-child" {
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
				parentResult, err := client.GetBeadsWithDeps(ctx, missingParentIDs)
				if err == nil {
					// Add missing parents to items
					for _, parentID := range missingParentIDs {
						if issue, ok := parentResult.Beads[parentID]; ok {
							parentBead := &beadItem{
								id:              issue.ID,
								title:           issue.Title,
								status:          issue.Status,
								priority:        issue.Priority,
								beadType:        issue.Type,
								description:     issue.Description,
								isClosedParent:  true,
							}

							// Populate dependencies for this parent
							if deps, ok := parentResult.Dependencies[parentID]; ok {
								depIDs := make([]string, 0, len(deps))
								for _, dep := range deps {
									if dep.Type == "blocks" || dep.Type == "parent-child" {
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
		}
	} else if dir != "" {
		// Client not available but dir provided - create temporary client
		beadsDBPath := filepath.Join(dir, ".beads", "beads.db")
		tempClient, err := beads.NewClient(ctx, beads.DefaultClientConfig(beadsDBPath))
		if err == nil {
			defer tempClient.Close()

			// Use the temp client to fetch dependencies
			result, err := tempClient.GetBeadsWithDeps(ctx, issueIDs)
			if err == nil {
				// Populate dependencies from result
				for i := range items {
					if deps, ok := result.Dependencies[items[i].id]; ok {
						depIDs := make([]string, 0, len(deps))
						for _, dep := range deps {
							if dep.Type == "blocks" || dep.Type == "parent-child" {
								depIDs = append(depIDs, dep.DependsOnID)
							}
						}
						items[i].dependencies = depIDs
					}
				}

				// Identify and fetch missing parent beads
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
					parentResult, err := tempClient.GetBeadsWithDeps(ctx, missingParentIDs)
					if err != nil {
						break
					}

					// Add missing parents to items
					for _, parentID := range missingParentIDs {
						if issue, ok := parentResult.Beads[parentID]; ok {
							parentBead := &beadItem{
								id:              issue.ID,
								title:           issue.Title,
								status:          issue.Status,
								priority:        issue.Priority,
								beadType:        issue.Type,
								description:     issue.Description,
								isClosedParent:  true,
							}

							// Populate dependencies for this parent
							if deps, ok := parentResult.Dependencies[parentID]; ok {
								depIDs := make([]string, 0, len(deps))
								for _, dep := range deps {
									if dep.Type == "blocks" || dep.Type == "parent-child" {
										depIDs = append(depIDs, dep.DependsOnID)
									}
								}
								parentBead.dependencies = depIDs
							}

							items = append(items, *parentBead)
							itemMap[parentBead.id] = &items[len(items)-1]
						}
					}
				}

				// Rebuild itemMap to fix stale pointers
				itemMap = make(map[string]*beadItem)
				for i := range items {
					itemMap[items[i].id] = &items[i]
				}
			}
		}
		// If client creation or queries fail, fall through to use existing dependencies
	}
	// else: No client and no dir - use dependencies already set on items (for tests)

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

	// Filter out closed parents that have no visible children.
	// Build set of visible IDs from the result
	visibleIDs := make(map[string]bool)
	for _, item := range result {
		visibleIDs[item.id] = true
	}

	// Filter closed parents: only keep them if they have at least one visible child
	var filtered []beadItem
	for _, item := range result {
		// Keep the item if it's not a closed parent
		if !item.isClosedParent {
			filtered = append(filtered, item)
			continue
		}

		// For closed parents, check if any of their children are visible
		hasVisibleChild := false
		if children, ok := childrenMap[item.id]; ok {
			for _, childID := range children {
				if visibleIDs[childID] {
					hasVisibleChild = true
					break
				}
			}
		}

		if hasVisibleChild {
			filtered = append(filtered, item)
		}
	}

	return filtered
}
