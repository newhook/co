package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/newhook/co/internal/beads"
	"github.com/newhook/co/internal/db"
)

// ResolveFeedbackForBeads posts resolution comments on GitHub for closed beads.
// It checks the provided beads and posts resolution comments for any associated
// unresolved feedback items.
func ResolveFeedbackForBeads(ctx context.Context, database *db.DB, beadClient *beads.Client, workID string, closedBeadIDs []string) error {
	if len(closedBeadIDs) == 0 {
		return nil
	}

	// Get unresolved feedback for the closed beads
	feedbacks, err := database.GetUnresolvedFeedbackForBeads(ctx, closedBeadIDs)
	if err != nil {
		return fmt.Errorf("failed to get unresolved feedback: %w", err)
	}

	if len(feedbacks) == 0 {
		return nil
	}

	fmt.Printf("\nResolving %d GitHub feedback items for closed beads...\n", len(feedbacks))

	for _, feedback := range feedbacks {
		if feedback.BeadID == nil || feedback.SourceID == nil {
			continue
		}

		// Get bead details for close reason
		bead, err := beadClient.GetBead(ctx, *feedback.BeadID)
		if err != nil {
			fmt.Printf("Warning: failed to get bead %s: %v\n", *feedback.BeadID, err)
			continue
		}

		if bead == nil {
			continue
		}

		// Construct resolution message
		resolutionMessage := fmt.Sprintf("✅ Resolved in work %s", workID)
		if bead.CloseReason != "" {
			resolutionMessage = fmt.Sprintf("✅ Resolved in work %s: %s", workID, bead.CloseReason)
		}

		// Parse PR URL to get owner/repo/pr_number
		// Expected format: https://github.com/owner/repo/pull/123
		parts := strings.Split(feedback.PRURL, "/")
		if len(parts) < 7 || parts[5] != "pull" {
			fmt.Printf("Warning: invalid PR URL format: %s\n", feedback.PRURL)
			continue
		}

		owner := parts[3]
		repo := parts[4]
		prNumber := parts[6]

		// Post comment using gh CLI
		cmd := exec.CommandContext(ctx, "gh", "api", "-X", "POST",
			fmt.Sprintf("/repos/%s/%s/issues/%s/comments", owner, repo, prNumber),
			"--field", fmt.Sprintf("body=%s", resolutionMessage),
			"--header", "Accept: application/vnd.github.v3+json")

		// If we have a source_id that looks like a review comment ID, reply to that specific thread
		if strings.Contains(feedback.Source, "Review:") && *feedback.SourceID != "" {
			// For review comments, reply to the specific comment thread
			cmd = exec.CommandContext(ctx, "gh", "api", "-X", "POST",
				fmt.Sprintf("/repos/%s/%s/pulls/%s/comments/%s/replies", owner, repo, prNumber, *feedback.SourceID),
				"--field", fmt.Sprintf("body=%s", resolutionMessage),
				"--header", "Accept: application/vnd.github.v3+json")
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Warning: failed to post resolution comment: %v\nOutput: %s\n", err, string(output))
			continue
		}

		// Mark feedback as resolved in database
		if err := database.MarkFeedbackResolved(ctx, feedback.ID); err != nil {
			fmt.Printf("Warning: failed to mark feedback %s as resolved: %v\n", feedback.ID, err)
		} else {
			fmt.Printf("Successfully resolved feedback for bead %s\n", *feedback.BeadID)
		}
	}

	return nil
}