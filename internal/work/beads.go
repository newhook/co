package work

import (
	"context"
	"fmt"

	"github.com/newhook/co/internal/beads"
)

// CollectIssueIDsForAutomatedWorkflow collects all issue IDs to include in the workflow.
// For an issue with parent-child dependents, it includes all children recursively.
// For other issues, it includes all transitive dependencies.
func CollectIssueIDsForAutomatedWorkflow(ctx context.Context, beadID string, beadsClient *beads.Client) ([]string, error) {
	if beadsClient == nil {
		return nil, fmt.Errorf("beads client is nil")
	}

	// First, get the main issue
	mainIssue, err := beadsClient.GetBead(ctx, beadID)
	if err != nil {
		return nil, fmt.Errorf("failed to get bead %s: %w", beadID, err)
	}
	if mainIssue == nil {
		return nil, fmt.Errorf("bead %s not found", beadID)
	}

	// Check if this issue has children (parent-child relationships)
	var hasChildren bool
	for _, dep := range mainIssue.Dependents {
		if dep.Type == "parent-child" {
			hasChildren = true
			break
		}
	}

	if hasChildren {
		// Collect all children recursively
		allIssues, err := beadsClient.GetBeadWithChildren(ctx, beadID)
		if err != nil {
			return nil, fmt.Errorf("failed to get children for %s: %w", beadID, err)
		}

		// Include all open issues for tracking
		var result []string
		for _, issue := range allIssues {
			// Skip closed issues
			if issue.Status == beads.StatusClosed {
				continue
			}
			result = append(result, issue.ID)
		}
		return result, nil
	}

	// For regular issues, collect transitive dependencies
	transitiveIssues, err := beadsClient.GetTransitiveDependencies(ctx, beadID)
	if err != nil {
		return nil, err
	}

	// Extract issue IDs, filtering out closed issues
	var issueIDs []string
	for _, issue := range transitiveIssues {
		if issue.Status != beads.StatusClosed {
			issueIDs = append(issueIDs, issue.ID)
		}
	}

	return issueIDs, nil
}

// CollectIssuesForMultipleIDs collects all issues to include for multiple bead IDs.
// It collects transitive dependencies for each bead and deduplicates the results.
func CollectIssuesForMultipleIDs(ctx context.Context, beadIDList []string, beadsClient *beads.Client) (*beads.BeadsWithDepsResult, error) {
	// Use a map to deduplicate issue IDs
	issueIDSet := make(map[string]bool)

	for _, beadID := range beadIDList {
		issueIDs, err := CollectIssueIDsForAutomatedWorkflow(ctx, beadID, beadsClient)
		if err != nil {
			return nil, err
		}
		for _, id := range issueIDs {
			issueIDSet[id] = true
		}
	}

	// Convert set to slice
	var issueIDs []string
	for id := range issueIDSet {
		issueIDs = append(issueIDs, id)
	}

	// Get all issues with dependencies in one call
	return beadsClient.GetBeadsWithDeps(ctx, issueIDs)
}
